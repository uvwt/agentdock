package skill

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	skillstate "github.com/uvwt/agentdock/internal/skill/state"
)

type Manager struct {
	State       *skillstate.Store
	HTTPClient  *http.Client
	MaxDownload int64
	MaxFiles    int
}

func New(state *skillstate.Store) (*Manager, error) {
	if state == nil {
		return nil, errors.New("managed Skill store is required")
	}
	manager := &Manager{
		State:       state,
		HTTPClient:  &http.Client{Timeout: 2 * time.Minute},
		MaxDownload: 128 << 20,
		MaxFiles:    10000,
	}
	if err := manager.recoverInterruptedSwaps(); err != nil {
		return nil, err
	}
	return manager, nil
}

func (m *Manager) Install(ctx context.Context, req InstallRequest) (InstallResult, error) {
	if strings.TrimSpace(req.Source) == "" {
		return InstallResult{}, packageError(ErrInvalidPackage, "source", errors.New("source is required"))
	}
	maxBytes := req.MaxBytes
	if maxBytes <= 0 {
		maxBytes = m.MaxDownload
	}
	maxFiles := req.MaxFiles
	if maxFiles <= 0 {
		maxFiles = m.MaxFiles
	}

	work, err := m.State.TempPath("install")
	if err != nil {
		return InstallResult{}, packageError(ErrInstallFailed, "temp", err)
	}
	defer os.RemoveAll(work)

	packageDir, sourceDigest, err := m.prepareSource(ctx, req.Source, work, maxBytes, maxFiles)
	if err != nil {
		return InstallResult{}, err
	}
	if expected := normalizeDigest(req.DigestSHA256); expected != "" && expected != sourceDigest {
		return InstallResult{}, packageError(ErrDigestMismatch, "digest", fmt.Errorf("expected %s, got %s", expected, sourceDigest))
	}
	if err := ValidatePackage(packageDir); err != nil {
		return InstallResult{}, err
	}
	doc, err := LoadSkillDocument(packageDir)
	if err != nil {
		return InstallResult{}, err
	}
	contentDigest, err := digestPackageContent(packageDir)
	if err != nil {
		return InstallResult{}, packageError(ErrInstallFailed, "content_digest", err)
	}

	staged := packageDir

	release, err := m.State.AcquireWrite(ctx, doc.Name)
	if err != nil {
		return InstallResult{}, packageError(ErrInstallFailed, "lock", err)
	}
	defer release()
	if err := m.recoverSwapLocked(doc.Name); err != nil {
		return InstallResult{}, packageError(ErrInstallFailed, "recover", err)
	}

	destination, err := m.State.SkillPath(doc.Name)
	if err != nil {
		return InstallResult{}, packageError(ErrInstallFailed, "destination", err)
	}
	currentDigest, exists, err := installedContentDigest(destination)
	if err != nil {
		return InstallResult{}, packageError(ErrInstallFailed, "current_digest", err)
	}
	if exists && currentDigest == contentDigest {
		return InstallResult{Skill: doc.Name, ContentDigest: contentDigest, Path: destination, Changed: false}, nil
	}

	backup := ""
	var transaction skillstate.SwapTransaction
	if exists {
		backup, err = m.State.SwapBackupPath(doc.Name)
		if err != nil {
			return InstallResult{}, packageError(ErrInstallFailed, "backup", err)
		}
		if _, err := os.Lstat(backup); err == nil {
			return InstallResult{}, packageError(ErrInstallFailed, "backup", errors.New("stale Skill swap backup remains after recovery"))
		} else if !errors.Is(err, os.ErrNotExist) {
			return InstallResult{}, packageError(ErrInstallFailed, "backup", err)
		}
		transaction = newSkillSwapTransaction(doc.Name, currentDigest, contentDigest)
		if err := m.State.SaveSwapTransaction(transaction); err != nil {
			return InstallResult{}, packageError(ErrInstallFailed, "journal", err)
		}
		if err := os.Rename(destination, backup); err != nil {
			_ = m.State.DeleteSwapTransaction(doc.Name)
			return InstallResult{}, packageError(ErrInstallFailed, "backup", err)
		}
	}

	if err := os.Rename(staged, destination); err != nil {
		restoreErr := restoreReplacedSkill(backup, destination)
		if backup != "" && restoreErr == nil {
			_ = m.State.DeleteSwapTransaction(doc.Name)
		}
		return InstallResult{}, packageError(ErrInstallFailed, "commit", errors.Join(err, restoreErr))
	}
	if backup != "" {
		transaction.Phase = "candidate_published"
		if err := m.State.SaveSwapTransaction(transaction); err != nil {
			restoreErr := restoreReplacedSkill(backup, destination)
			if restoreErr == nil {
				_ = m.State.DeleteSwapTransaction(doc.Name)
			}
			return InstallResult{}, packageError(ErrInstallFailed, "journal_commit", errors.Join(err, restoreErr))
		}
		if err := os.RemoveAll(backup); err != nil {
			slog.Warn("cleanup committed Skill swap backup failed", "skill", doc.Name, "path", backup, "error", err)
		} else if err := m.State.DeleteSwapTransaction(doc.Name); err != nil {
			slog.Warn("cleanup committed Skill swap journal failed", "skill", doc.Name, "error", err)
		}
	}
	return InstallResult{Skill: doc.Name, ContentDigest: contentDigest, Path: destination, Changed: true}, nil
}

func installedContentDigest(destination string) (string, bool, error) {
	info, err := os.Lstat(destination)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", true, errors.New("managed Skill destination is not a regular directory")
	}
	digest, err := digestPackageContent(destination)
	return digest, true, err
}

func restoreReplacedSkill(backup, destination string) error {
	if backup == "" {
		return nil
	}
	if _, err := os.Lstat(destination); err == nil {
		if removeErr := os.RemoveAll(destination); removeErr != nil {
			return fmt.Errorf("remove incomplete replacement: %w", removeErr)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect incomplete replacement: %w", err)
	}
	if err := os.Rename(backup, destination); err != nil {
		return fmt.Errorf("restore previous Skill content: %w", err)
	}
	return nil
}

func (m *Manager) prepareSource(ctx context.Context, source, work string, maxBytes int64, maxFiles int) (string, string, error) {
	parsed, parseErr := url.Parse(source)
	if parseErr == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		archive := filepath.Join(work, "package.zip")
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			return "", "", packageError(ErrInvalidPackage, "download", err)
		}
		response, err := m.HTTPClient.Do(req)
		if err != nil {
			return "", "", packageError(ErrInvalidPackage, "download", err)
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return "", "", packageError(ErrInvalidPackage, "download", fmt.Errorf("HTTP %s", response.Status))
		}
		out, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return "", "", packageError(ErrInvalidPackage, "download", err)
		}
		written, copyErr := io.Copy(out, io.LimitReader(response.Body, maxBytes+1))
		closeErr := out.Close()
		if copyErr != nil {
			return "", "", packageError(ErrInvalidPackage, "download", copyErr)
		}
		if closeErr != nil {
			return "", "", packageError(ErrInvalidPackage, "download", closeErr)
		}
		if written > maxBytes {
			return "", "", packageError(ErrInvalidPackage, "download", fmt.Errorf("package exceeds %d bytes", maxBytes))
		}
		return m.prepareArchive(archive, work, maxBytes, maxFiles)
	}

	info, err := os.Lstat(source)
	if err != nil {
		return "", "", packageError(ErrInvalidPackage, "source", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", "", packageError(ErrInvalidPackage, "source", errors.New("local Skill source cannot be a symlink"))
	}
	if info.IsDir() {
		candidate := filepath.Join(work, "snapshot")
		if err := snapshotLocalDirectory(source, candidate, maxBytes, maxFiles); err != nil {
			return "", "", packageError(ErrInvalidPackage, "snapshot", err)
		}
		digest, err := DigestDirectory(candidate)
		if err != nil {
			return "", "", packageError(ErrInvalidPackage, "digest", err)
		}
		return candidate, digest, nil
	}
	if !info.Mode().IsRegular() {
		return "", "", packageError(ErrInvalidPackage, "source", errors.New("local Skill source must be a regular directory or ZIP file"))
	}
	archive := filepath.Join(work, "local-package.zip")
	if err := snapshotLocalFile(source, archive, maxBytes); err != nil {
		return "", "", packageError(ErrInvalidPackage, "snapshot", err)
	}
	return m.prepareArchive(archive, work, maxBytes, maxFiles)
}

func (m *Manager) prepareArchive(archive, work string, maxBytes int64, maxFiles int) (string, string, error) {
	digest, err := DigestFile(archive)
	if err != nil {
		return "", "", packageError(ErrInvalidPackage, "digest", err)
	}
	packageDir := filepath.Join(work, "extracted")
	if err := os.MkdirAll(packageDir, 0o700); err != nil {
		return "", "", packageError(ErrInvalidPackage, "extract", err)
	}
	if err := extractZip(archive, packageDir, maxBytes, maxFiles); err != nil {
		return "", "", packageError(ErrInvalidPackage, "extract", err)
	}
	entries, err := os.ReadDir(packageDir)
	if err == nil && len(entries) == 1 && entries[0].IsDir() {
		candidate := filepath.Join(packageDir, entries[0].Name())
		if info, statErr := os.Stat(filepath.Join(candidate, "SKILL.md")); statErr == nil && !info.IsDir() {
			packageDir = candidate
		}
	}
	return packageDir, digest, nil
}

func snapshotLocalDirectory(source, destination string, maxBytes int64, maxFiles int) error {
	source = filepath.Clean(source)
	root, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}

	var total int64
	files := 0
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil || !filepath.IsLocal(rel) || rel == "." {
			return fmt.Errorf("invalid local Skill source path %q", path)
		}
		info, err := validateSnapshotSourcePath(root, rel, entry.IsDir())
		if err != nil {
			return fmt.Errorf("unsafe local Skill source path %q: %w", path, err)
		}
		target := filepath.Join(destination, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o700)
		}

		files++
		if files > maxFiles {
			return fmt.Errorf("package exceeds %d files", maxFiles)
		}
		remaining := maxBytes - total
		if remaining < 0 {
			return fmt.Errorf("package exceeds %d bytes", maxBytes)
		}
		mode := info.Mode().Perm() & 0o755
		if mode == 0 {
			mode = 0o600
		}
		copied, err := snapshotRegularFile(root, rel, target, mode, remaining)
		if err != nil {
			return err
		}
		total += copied
		if total > maxBytes {
			return fmt.Errorf("package exceeds %d bytes", maxBytes)
		}
		return nil
	})
}

func snapshotLocalFile(source, destination string, maxBytes int64) error {
	absolute, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	before, err := os.Lstat(absolute)
	if err != nil {
		return err
	}
	if !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 {
		return errors.New("local Skill archive must be a regular file")
	}
	in, err := os.Open(absolute)
	if err != nil {
		return err
	}
	defer in.Close()
	opened, err := in.Stat()
	if err != nil {
		return err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return errors.New("local Skill archive changed while opening snapshot")
	}
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	copied, copyErr := io.Copy(out, io.LimitReader(in, maxBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if copied > maxBytes {
		return fmt.Errorf("package exceeds %d bytes", maxBytes)
	}
	afterOpen, err := in.Stat()
	if err != nil {
		return err
	}
	afterPath, err := os.Lstat(absolute)
	if err != nil {
		return err
	}
	if !afterPath.Mode().IsRegular() || !os.SameFile(opened, afterOpen) || !os.SameFile(opened, afterPath) ||
		opened.Size() != afterOpen.Size() || opened.ModTime() != afterOpen.ModTime() || opened.Mode() != afterOpen.Mode() {
		return errors.New("local Skill archive changed while creating snapshot")
	}
	return nil
}

// validateSnapshotSourcePath rejects symlink/reparse-style path substitution
// one segment at a time. os.Root supplies the containment boundary; this
// stricter check additionally requires every observed component to remain an
// ordinary directory or regular file throughout the snapshot.
func validateSnapshotSourcePath(root *os.Root, relative string, wantDirectory bool) (os.FileInfo, error) {
	clean := filepath.Clean(relative)
	if !filepath.IsLocal(clean) || clean == "." {
		return nil, errors.New("path is not local to the Skill source")
	}
	parts := strings.Split(clean, string(filepath.Separator))
	current := ""
	var info os.FileInfo
	for index, part := range parts {
		if current == "" {
			current = part
		} else {
			current = filepath.Join(current, part)
		}
		var err error
		info, err = root.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("path contains symlink component %q", filepath.ToSlash(current))
		}
		if index < len(parts)-1 && !info.IsDir() {
			return nil, fmt.Errorf("path component %q is not a directory", filepath.ToSlash(current))
		}
	}
	if wantDirectory {
		if info == nil || !info.IsDir() {
			return nil, errors.New("path is not a directory")
		}
	} else if info == nil || !info.Mode().IsRegular() {
		return nil, errors.New("path is not a regular file")
	}
	return info, nil
}

func snapshotRegularFile(root *os.Root, relative, destination string, mode os.FileMode, remaining int64) (int64, error) {
	before, err := validateSnapshotSourcePath(root, relative, false)
	if err != nil {
		return 0, err
	}
	in, err := root.Open(relative)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	opened, err := in.Stat()
	if err != nil {
		return 0, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return 0, errors.New("source file changed while opening snapshot")
	}

	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return 0, err
	}
	copied, copyErr := io.Copy(out, io.LimitReader(in, remaining+1))
	closeErr := out.Close()
	if copyErr != nil {
		return copied, copyErr
	}
	if closeErr != nil {
		return copied, closeErr
	}
	if copied > remaining {
		return copied, fmt.Errorf("package exceeds byte limit")
	}

	afterOpen, err := in.Stat()
	if err != nil {
		return copied, err
	}
	afterPath, err := validateSnapshotSourcePath(root, relative, false)
	if err != nil {
		return copied, err
	}
	if !os.SameFile(opened, afterOpen) || !os.SameFile(opened, afterPath) ||
		opened.Size() != afterOpen.Size() || opened.ModTime() != afterOpen.ModTime() || opened.Mode() != afterOpen.Mode() {
		return copied, errors.New("source file changed while creating snapshot")
	}
	return copied, nil
}

func copyRegularFile(source, destination string, mode os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		_ = in.Close()
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeOutErr := out.Close()
	closeInErr := in.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeOutErr != nil {
		return closeOutErr
	}
	return closeInErr
}

func validateRelativePackagePath(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("path is empty")
	}
	if strings.HasPrefix(value, "/") || filepath.IsAbs(value) || strings.Contains(value, `\`) {
		return errors.New("path must be slash-separated and relative")
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("path contains empty, dot, or parent segment")
		}
	}
	return nil
}

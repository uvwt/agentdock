package skill

import (
	"context"
	"errors"
	"fmt"
	"io"
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
}

func New(state *skillstate.Store) (*Manager, error) {
	if state == nil {
		return nil, errors.New("managed Skill store is required")
	}
	return &Manager{
		State:       state,
		HTTPClient:  &http.Client{Timeout: 2 * time.Minute},
		MaxDownload: 128 << 20,
	}, nil
}

func (m *Manager) Install(ctx context.Context, req InstallRequest) (InstallResult, error) {
	if strings.TrimSpace(req.Source) == "" {
		return InstallResult{}, packageError(ErrInvalidPackage, "source", errors.New("source is required"))
	}
	maxBytes := req.MaxBytes
	if maxBytes <= 0 {
		maxBytes = m.MaxDownload
	}

	work, err := m.State.TempPath("install")
	if err != nil {
		return InstallResult{}, packageError(ErrInstallFailed, "temp", err)
	}
	defer os.RemoveAll(work)

	packageDir, sourceDigest, err := m.prepareSource(ctx, req.Source, work, maxBytes)
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

	staged, err := m.State.TempPath("candidate-" + doc.Name)
	if err != nil {
		return InstallResult{}, packageError(ErrInstallFailed, "stage", err)
	}
	defer os.RemoveAll(staged)
	if err := copyPackage(packageDir, staged); err != nil {
		return InstallResult{}, packageError(ErrInstallFailed, "stage", err)
	}

	release, err := m.State.AcquireWrite(ctx, doc.Name)
	if err != nil {
		return InstallResult{}, packageError(ErrInstallFailed, "lock", err)
	}
	defer release()

	destination, err := m.State.SkillPath(doc.Name)
	if err != nil {
		return InstallResult{}, packageError(ErrInstallFailed, "destination", err)
	}
	if currentDigest, exists, err := installedContentDigest(destination); err != nil {
		return InstallResult{}, packageError(ErrInstallFailed, "current_digest", err)
	} else if exists && currentDigest == contentDigest {
		return InstallResult{Skill: doc.Name, ContentDigest: contentDigest, Path: destination, Changed: false}, nil
	}

	backup := ""
	if _, err := os.Lstat(destination); err == nil {
		backup, err = m.State.TempPath("replace-" + doc.Name)
		if err != nil {
			return InstallResult{}, packageError(ErrInstallFailed, "backup", err)
		}
		if err := os.Remove(backup); err != nil {
			return InstallResult{}, packageError(ErrInstallFailed, "backup", err)
		}
		if err := os.Rename(destination, backup); err != nil {
			return InstallResult{}, packageError(ErrInstallFailed, "backup", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return InstallResult{}, packageError(ErrInstallFailed, "destination", err)
	}

	if err := os.Rename(staged, destination); err != nil {
		restoreErr := restoreReplacedSkill(backup, destination)
		return InstallResult{}, packageError(ErrInstallFailed, "commit", errors.Join(err, restoreErr))
	}
	if backup != "" {
		_ = os.RemoveAll(backup)
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

func (m *Manager) prepareSource(ctx context.Context, source, work string, maxBytes int64) (string, string, error) {
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
		return m.prepareArchive(archive, work, maxBytes)
	}

	info, err := os.Stat(source)
	if err != nil {
		return "", "", packageError(ErrInvalidPackage, "source", err)
	}
	if info.IsDir() {
		// Reject symlinks and special files before hashing a local directory.
		// In particular, opening a FIFO while computing a digest could block the
		// installer before package validation gets a chance to reject it.
		if err := ValidatePackage(source); err != nil {
			return "", "", err
		}
		digest, err := DigestDirectory(source)
		if err != nil {
			return "", "", packageError(ErrInvalidPackage, "digest", err)
		}
		return source, digest, nil
	}
	return m.prepareArchive(source, work, maxBytes)
}

func (m *Manager) prepareArchive(archive, work string, maxBytes int64) (string, string, error) {
	digest, err := DigestFile(archive)
	if err != nil {
		return "", "", packageError(ErrInvalidPackage, "digest", err)
	}
	packageDir := filepath.Join(work, "extracted")
	if err := os.MkdirAll(packageDir, 0o700); err != nil {
		return "", "", packageError(ErrInvalidPackage, "extract", err)
	}
	if err := extractZip(archive, packageDir, maxBytes); err != nil {
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

func copyPackage(source, destination string) error {
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not allowed: %s", path)
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		mode := info.Mode().Perm() & 0o755
		if mode == 0 {
			mode = 0o600
		}
		return copyRegularFile(path, target, mode)
	})
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

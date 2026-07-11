package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/skillstate"
)

type Manager struct {
	State       *skillstate.Store
	HTTPClient  *http.Client
	MaxDownload int64
}

func New(state *skillstate.Store) (*Manager, error) {
	if state == nil {
		return nil, errors.New("skill state store is required")
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

	packageDir, digest, err := m.prepareSource(ctx, req.Source, work, maxBytes)
	if err != nil {
		return InstallResult{}, err
	}
	if expected := normalizeDigest(req.DigestSHA256); expected != "" && expected != digest {
		return InstallResult{}, packageError(ErrDigestMismatch, "digest", fmt.Errorf("expected %s, got %s", expected, digest))
	}
	if err := ValidatePackage(packageDir); err != nil {
		return InstallResult{}, err
	}
	doc, err := LoadSkillDocument(packageDir)
	if err != nil {
		return InstallResult{}, err
	}
	destination, err := m.State.InstalledPath(doc.Name, doc.Version)
	if err != nil {
		return InstallResult{}, packageError(ErrInstallFailed, "destination", err)
	}
	if _, err := os.Stat(destination); err == nil {
		existingDigest, digestErr := installedDigest(destination)
		if digestErr == nil && existingDigest == digest {
			return m.finishInstall(ctx, req, doc, destination, digest)
		}
		return InstallResult{}, packageError(ErrInstallFailed, "install", errors.New("version already exists with different content"))
	} else if !os.IsNotExist(err) {
		return InstallResult{}, packageError(ErrInstallFailed, "install", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return InstallResult{}, packageError(ErrInstallFailed, "install", err)
	}
	staged := destination + fmt.Sprintf(".tmp-%d", time.Now().UnixNano())
	if err := copyPackage(packageDir, staged); err != nil {
		_ = os.RemoveAll(staged)
		return InstallResult{}, packageError(ErrInstallFailed, "copy", err)
	}
	metadata := struct {
		Digest      string    `json:"digest"`
		Source      string    `json:"source"`
		InstalledAt time.Time `json:"installed_at"`
	}{Digest: digest, Source: req.Source, InstalledAt: time.Now().UTC()}
	metaData, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		_ = os.RemoveAll(staged)
		return InstallResult{}, packageError(ErrInstallFailed, "metadata", err)
	}
	if err := os.WriteFile(filepath.Join(staged, ".agentdock-install.json"), metaData, 0o600); err != nil {
		_ = os.RemoveAll(staged)
		return InstallResult{}, packageError(ErrInstallFailed, "metadata", err)
	}
	if err := os.Rename(staged, destination); err != nil {
		_ = os.RemoveAll(staged)
		return InstallResult{}, packageError(ErrInstallFailed, "install", err)
	}
	return m.finishInstall(ctx, req, doc, destination, digest)
}

func (m *Manager) finishInstall(ctx context.Context, req InstallRequest, doc SkillDocument, destination, digest string) (InstallResult, error) {
	result := InstallResult{
		Skill:       doc.Name,
		Version:     doc.Version,
		Digest:      digest,
		InstalledAt: time.Now().UTC(),
		Path:        destination,
		Channel:     req.Channel,
	}
	if req.Activate {
		channel := req.Channel
		if channel == "" {
			channel = skillstate.ChannelStable
		}
		if err := m.State.Activate(ctx, doc.Name, doc.Version, channel); err != nil {
			return InstallResult{}, packageError(ErrInstallFailed, "activate", err)
		}
		result.Activated = true
		result.Channel = channel
	}
	return result, nil
}

func installedDigest(destination string) (string, error) {
	data, err := os.ReadFile(filepath.Join(destination, ".agentdock-install.json"))
	if err != nil {
		return "", err
	}
	var metadata struct {
		Digest string `json:"digest"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return "", err
	}
	if metadata.Digest == "" {
		return "", errors.New("installed package metadata has no digest")
	}
	return metadata.Digest, nil
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
		if filepath.ToSlash(rel) == ".agentdock-install.json" {
			return nil
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

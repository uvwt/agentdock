package skillruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/skillstate"
)

type Reporter interface {
	InstallationChanged(context.Context, InstallResult) error
	RunCompleted(context.Context, RunResult) error
}

type Runtime struct {
	State       *skillstate.Store
	Bindings    *BindingStore
	EnvProvider EnvProvider
	HTTPClient  *http.Client
	Reporter    Reporter
	Events      EventSink
	Authorizer  PermissionAuthorizer
	BaseEnv     []string
	MaxDownload int64
}

type EnvProvider interface {
	EnvForSkill(skill string, definitions []EnvDefinition) (map[string]string, []string, error)
}

type EnvDefinition struct {
	Skill  string `json:"skill"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Source string `json:"source"`
}

func New(state *skillstate.Store, bindings *BindingStore) (*Runtime, error) {
	if state == nil {
		return nil, errors.New("skill state store is required")
	}
	return &Runtime{State: state, Bindings: bindings, HTTPClient: &http.Client{Timeout: 2 * time.Minute}, BaseEnv: os.Environ(), MaxDownload: 128 << 20}, nil
}

func (r *Runtime) Install(ctx context.Context, req InstallRequest) (InstallResult, error) {
	if strings.TrimSpace(req.Source) == "" {
		return InstallResult{}, runtimeError(ErrInvalidPackage, "download", errors.New("source is required"))
	}
	maxBytes := req.MaxBytes
	if maxBytes <= 0 {
		maxBytes = r.MaxDownload
	}
	if maxBytes <= 0 {
		maxBytes = 128 << 20
	}
	work, err := r.State.TempPath("install")
	if err != nil {
		return InstallResult{}, runtimeError(ErrInstallFailed, "temp", err)
	}
	defer os.RemoveAll(work)
	packageDir, digest, err := r.prepareSource(ctx, req.Source, work, maxBytes)
	if err != nil {
		return InstallResult{}, err
	}
	if expected := normalizeDigest(req.DigestSHA256); expected != "" && expected != digest {
		return InstallResult{}, runtimeError(ErrDigestMismatch, "digest", fmt.Errorf("expected %s, got %s", expected, digest))
	}
	manifest, err := LoadManifest(packageDir)
	if err != nil {
		return InstallResult{}, err
	}
	if err := validateInstallEnvDeclarations(manifest, req.ConfirmedNoEnv); err != nil {
		return InstallResult{}, err
	}
	for _, command := range manifest.Spec.Permissions.Commands {
		if _, err := exec.LookPath(command); err != nil {
			return InstallResult{}, runtimeError(ErrDependencyMissing, "dependency", fmt.Errorf("command %s: %w", command, err))
		}
	}
	destination, err := r.State.InstalledPath(manifest.Metadata.Name, manifest.Metadata.Version)
	if err != nil {
		return InstallResult{}, runtimeError(ErrInstallFailed, "destination", err)
	}
	if _, err := os.Stat(destination); err == nil {
		existingDigest, digestErr := installedDigest(destination)
		if digestErr == nil && existingDigest == digest {
			return r.finishInstall(ctx, req, manifest, destination, digest)
		}
		return InstallResult{}, runtimeError(ErrInstallFailed, "install", fmt.Errorf("version already exists with different content"))
	} else if !os.IsNotExist(err) {
		return InstallResult{}, runtimeError(ErrInstallFailed, "install", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return InstallResult{}, runtimeError(ErrInstallFailed, "install", err)
	}
	staged := destination + fmt.Sprintf(".tmp-%d", time.Now().UnixNano())
	if err := copyPackage(packageDir, staged); err != nil {
		return InstallResult{}, runtimeError(ErrInstallFailed, "copy", err)
	}
	metadata := map[string]any{"digest": digest, "source": req.Source, "installed_at": time.Now().UTC()}
	metaData, _ := json.MarshalIndent(metadata, "", "  ")
	if err := os.WriteFile(filepath.Join(staged, ".agentdock-install.json"), metaData, 0o600); err != nil {
		os.RemoveAll(staged)
		return InstallResult{}, runtimeError(ErrInstallFailed, "metadata", err)
	}
	if err := os.Rename(staged, destination); err != nil {
		os.RemoveAll(staged)
		return InstallResult{}, runtimeError(ErrInstallFailed, "activate.install", err)
	}
	return r.finishInstall(ctx, req, manifest, destination, digest)
}

func (r *Runtime) finishInstall(ctx context.Context, req InstallRequest, manifest Manifest, destination, digest string) (InstallResult, error) {
	result := InstallResult{Skill: manifest.Metadata.Name, Version: manifest.Metadata.Version, Digest: digest, InstalledAt: time.Now().UTC(), Path: destination, Channel: req.Channel}
	if req.Activate {
		channel := req.Channel
		if channel == "" {
			channel = skillstate.ChannelStable
		}
		if err := r.State.Activate(ctx, manifest.Metadata.Name, manifest.Metadata.Version, channel); err != nil {
			return InstallResult{}, runtimeError(ErrInstallFailed, "activate", err)
		}
		result.Activated = true
		result.Channel = channel
	}
	if r.Reporter != nil {
		_ = r.Reporter.InstallationChanged(ctx, result)
	}
	r.emit(ctx, Event{
		Type:      "skill.installation.changed",
		Skill:     result.Skill,
		Version:   result.Version,
		Timestamp: time.Now().UTC(),
		Payload: map[string]any{
			"digest":    result.Digest,
			"activated": result.Activated,
			"channel":   result.Channel,
		},
	})
	return result, nil
}

func validateInstallEnvDeclarations(manifest Manifest, confirmedNoEnv bool) error {
	if len(EnvDefinitionsForManifest(manifest)) > 0 || confirmedNoEnv {
		return nil
	}
	return runtimeError(ErrManifestInvalid, "manifest.env", fmt.Errorf("skill %s declares no env requirements; pass confirmed_no_env=true to confirm it does not need Env Manager configuration", manifest.Metadata.Name))
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

func (r *Runtime) prepareSource(ctx context.Context, source, work string, maxBytes int64) (string, string, error) {
	parsed, parseErr := url.Parse(source)
	if parseErr == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		archive := filepath.Join(work, "package.zip")
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			return "", "", runtimeError(ErrInvalidPackage, "download", err)
		}
		response, err := r.HTTPClient.Do(req)
		if err != nil {
			return "", "", runtimeError(ErrInvalidPackage, "download", err)
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return "", "", runtimeError(ErrInvalidPackage, "download", fmt.Errorf("HTTP %s", response.Status))
		}
		out, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return "", "", runtimeError(ErrInvalidPackage, "download", err)
		}
		written, copyErr := io.Copy(out, io.LimitReader(response.Body, maxBytes+1))
		closeErr := out.Close()
		if copyErr != nil {
			return "", "", runtimeError(ErrInvalidPackage, "download", copyErr)
		}
		if closeErr != nil {
			return "", "", runtimeError(ErrInvalidPackage, "download", closeErr)
		}
		if written > maxBytes {
			return "", "", runtimeError(ErrInvalidPackage, "download", fmt.Errorf("package exceeds %d bytes", maxBytes))
		}
		return r.prepareArchive(archive, work, maxBytes)
	}
	info, err := os.Stat(source)
	if err != nil {
		return "", "", runtimeError(ErrInvalidPackage, "source", err)
	}
	if info.IsDir() {
		digest, err := DigestDirectory(source)
		if err != nil {
			return "", "", runtimeError(ErrInvalidPackage, "digest", err)
		}
		return source, digest, nil
	}
	return r.prepareArchive(source, work, maxBytes)
}

func (r *Runtime) prepareArchive(archive, work string, maxBytes int64) (string, string, error) {
	digest, err := DigestFile(archive)
	if err != nil {
		return "", "", runtimeError(ErrInvalidPackage, "digest", err)
	}
	packageDir := filepath.Join(work, "extracted")
	if err := os.MkdirAll(packageDir, 0o700); err != nil {
		return "", "", runtimeError(ErrInvalidPackage, "extract", err)
	}
	if err := extractZip(archive, packageDir, maxBytes); err != nil {
		return "", "", runtimeError(ErrInvalidPackage, "extract", err)
	}
	entries, err := os.ReadDir(packageDir)
	if err == nil && len(entries) == 1 && entries[0].IsDir() {
		candidate := filepath.Join(packageDir, entries[0].Name())
		if _, statErr := os.Stat(filepath.Join(candidate, "agentdock.yaml")); statErr == nil {
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
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm()&0o755)
		if err != nil {
			in.Close()
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
	})
}

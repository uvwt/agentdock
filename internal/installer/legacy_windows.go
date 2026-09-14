package installer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uvwt/agentdock/internal/fs/processlock"
	"github.com/uvwt/agentdock/internal/updateengine"
)

// WindowsLegacyBootstrapRequest describes the one-time transition from the
// pre-generation Windows layout to stable shims + side-by-side generations.
// Core/Tray come from the installed known-good version; Arbiter/WSL helper come
// from the new payload and are migration infrastructure, not the active product version.
type WindowsLegacyBootstrapRequest struct {
	InstallRoot string
	Version     string
	CorePath    string
	TrayPath    string
	PayloadDir  string
}

type WindowsLegacyBootstrapResult struct {
	Version       string `json:"version"`
	GenerationDir string `json:"generation_dir"`
	AlreadyReady  bool   `json:"already_ready"`
}

// PrepareWindowsLegacyGeneration establishes a committed source generation before
// Setup replaces the old stable binaries with shims. The ordering is deliberate:
// until WriteActive succeeds, the legacy stable binaries remain authoritative; after
// it succeeds, a newly installed shim can always route back to this source generation.
func PrepareWindowsLegacyGeneration(ctx context.Context, request WindowsLegacyBootstrapRequest) (WindowsLegacyBootstrapResult, error) {
	root := strings.TrimSpace(request.InstallRoot)
	if root == "" {
		return WindowsLegacyBootstrapResult{}, fmt.Errorf("install-root 不能为空")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return WindowsLegacyBootstrapResult{}, fmt.Errorf("install-root: %w", err)
	}
	if err := updateengine.ValidateVersion(request.Version); err != nil {
		return WindowsLegacyBootstrapResult{}, fmt.Errorf("legacy version 无效：%w", err)
	}
	version := updateengine.NormalizeVersion(request.Version)
	if version == "vunknown" || version == "vdev" {
		return WindowsLegacyBootstrapResult{}, fmt.Errorf("legacy version 无效：%q", request.Version)
	}
	installStore, err := NewStore(absoluteRoot)
	if err != nil {
		return WindowsLegacyBootstrapResult{}, err
	}
	lock, err := processlock.Acquire(ctx, installStore.LockPath())
	if err != nil {
		return WindowsLegacyBootstrapResult{}, fmt.Errorf("lock legacy migration transaction: %w", err)
	}
	defer lock.Release()
	for name, path := range map[string]string{
		"legacy core": request.CorePath,
		"legacy tray": request.TrayPath,
	} {
		if err := requireRegularFile(path); err != nil {
			return WindowsLegacyBootstrapResult{}, fmt.Errorf("%s: %w", name, err)
		}
	}
	payload := strings.TrimSpace(request.PayloadDir)
	if payload == "" {
		return WindowsLegacyBootstrapResult{}, fmt.Errorf("payload-dir 不能为空")
	}
	for _, relative := range []string{
		"agentdock-arbiter.exe",
		filepath.Join("wsl-helper", "manifest.json"),
		filepath.Join("wsl-helper", "agentdock-wsl-helper-linux-amd64"),
		filepath.Join("wsl-helper", "agentdock-wsl-helper-linux-arm64"),
	} {
		if err := requireRegularFile(filepath.Join(payload, relative)); err != nil {
			return WindowsLegacyBootstrapResult{}, fmt.Errorf("迁移 payload 缺少 %s: %w", relative, err)
		}
	}

	layout, err := updateengine.NewWindowsLayout(absoluteRoot)
	if err != nil {
		return WindowsLegacyBootstrapResult{}, err
	}
	if err := layout.EnsureBase(); err != nil {
		return WindowsLegacyBootstrapResult{}, err
	}
	store, err := updateengine.NewStore(absoluteRoot)
	if err != nil {
		return WindowsLegacyBootstrapResult{}, err
	}
	generation := layout.GenerationDir(version)
	if active, readErr := store.ReadActive(); readErr == nil {
		if active.State != updateengine.StateCommitted || updateengine.NormalizeVersion(active.ActiveVersion) != version {
			return WindowsLegacyBootstrapResult{}, fmt.Errorf("已有 Windows generation 状态不能作为 legacy source：active=%s state=%s", active.ActiveVersion, active.State)
		}
		if err := requireLegacySourceGeneration(layout, version); err != nil {
			return WindowsLegacyBootstrapResult{}, err
		}
		return WindowsLegacyBootstrapResult{Version: version, GenerationDir: generation, AlreadyReady: true}, nil
	} else if !os.IsNotExist(readErr) {
		return WindowsLegacyBootstrapResult{}, fmt.Errorf("读取 active-version.json: %w", readErr)
	}

	transactionID, err := newTransactionID()
	if err != nil {
		return WindowsLegacyBootstrapResult{}, err
	}
	staging := filepath.Join(layout.VersionsDir(), ".legacy-bootstrap-"+transactionID)
	if err := os.RemoveAll(staging); err != nil {
		return WindowsLegacyBootstrapResult{}, err
	}
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return WindowsLegacyBootstrapResult{}, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(staging)
		}
	}()

	copies := []struct {
		source string
		target string
		mode   os.FileMode
	}{
		{request.CorePath, filepath.Join(staging, updateengine.GenerationCoreName), 0o755},
		{request.TrayPath, filepath.Join(staging, updateengine.GenerationTrayName), 0o755},
		{filepath.Join(payload, "agentdock-arbiter.exe"), filepath.Join(staging, updateengine.GenerationArbiterName), 0o755},
	}
	for _, item := range copies {
		if err := copyTree(item.source, item.target, item.mode); err != nil {
			return WindowsLegacyBootstrapResult{}, err
		}
	}
	if err := copyTree(filepath.Join(payload, "wsl-helper"), filepath.Join(staging, "wsl-helper"), 0o755); err != nil {
		return WindowsLegacyBootstrapResult{}, err
	}

	// With no active pointer the final generation cannot be live. Replacing an orphan
	// left by a crash is therefore safe and makes retry idempotent.
	if err := os.RemoveAll(generation); err != nil {
		return WindowsLegacyBootstrapResult{}, fmt.Errorf("清理未激活的 legacy generation: %w", err)
	}
	if err := os.Rename(staging, generation); err != nil {
		return WindowsLegacyBootstrapResult{}, fmt.Errorf("发布 legacy generation: %w", err)
	}
	cleanup = false
	if err := requireLegacySourceGeneration(layout, version); err != nil {
		return WindowsLegacyBootstrapResult{}, err
	}
	if err := store.WriteActive(updateengine.ActiveVersion{
		SchemaVersion: updateengine.SchemaVersion,
		ActiveVersion: version,
		State:         updateengine.StateCommitted,
	}); err != nil {
		// Leave the complete but inactive generation in place. The old stable binaries
		// still run normally, and the next attempt can safely replace this orphan.
		return WindowsLegacyBootstrapResult{}, fmt.Errorf("提交 legacy source pointer: %w", err)
	}
	return WindowsLegacyBootstrapResult{Version: version, GenerationDir: generation}, nil
}

func requireLegacySourceGeneration(layout updateengine.WindowsLayout, version string) error {
	for _, path := range []string{
		layout.GenerationCore(version),
		layout.GenerationTray(version),
		layout.GenerationArbiter(version),
		filepath.Join(layout.GenerationDir(version), "wsl-helper", "manifest.json"),
		filepath.Join(layout.GenerationDir(version), "wsl-helper", "agentdock-wsl-helper-linux-amd64"),
		filepath.Join(layout.GenerationDir(version), "wsl-helper", "agentdock-wsl-helper-linux-arm64"),
	} {
		if err := requireRegularFile(path); err != nil {
			return fmt.Errorf("legacy source generation 不完整：%s: %w", path, err)
		}
	}
	return nil
}

func requireRegularFile(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("路径为空")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("不是普通文件")
	}
	return nil
}

//go:build windows

package desktopruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/uvwt/agentdock/internal/fs/atomicfile"
	toolbrowser "github.com/uvwt/agentdock/internal/tool/browser"
)

type fileSnapshot struct {
	path   string
	data   []byte
	mode   os.FileMode
	exists bool
}

func snapshotFile(path string) (fileSnapshot, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileSnapshot{path: path}, nil
	}
	if err != nil {
		return fileSnapshot{}, err
	}
	if !info.Mode().IsRegular() {
		return fileSnapshot{}, fmt.Errorf("配置文件不是普通文件: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fileSnapshot{}, err
	}
	return fileSnapshot{path: path, data: data, mode: info.Mode().Perm(), exists: true}, nil
}

func restoreSnapshots(snapshots []fileSnapshot) error {
	var restoreErr error
	for _, snapshot := range snapshots {
		if snapshot.exists {
			mode := snapshot.mode
			if mode == 0 {
				mode = 0o600
			}
			if err := atomicfile.Write(snapshot.path, snapshot.data, mode); err != nil {
				restoreErr = errors.Join(restoreErr, err)
			}
		} else if err := os.Remove(snapshot.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			restoreErr = errors.Join(restoreErr, err)
		}
	}
	return restoreErr
}

func platformUpdateConfig(ctx context.Context, request ConfigUpdateRequest) error {
	if request.BrowserEnabled && request.BrowserCDPURL == "" && !request.BrowserReuseExistingCDP {
		if _, err := toolbrowser.FindExecutable("", toolbrowser.BrowserAuto); err != nil {
			return fmt.Errorf("未检测到受支持的 Chrome、Chromium 或 Microsoft Edge，且未配置外部 CDP: %w", err)
		}
	}
	runtime, err := loadTunnelRuntime(request.RuntimeRoot)
	if err != nil {
		return err
	}
	var acpAdapter desktopACPAdapter
	if request.ACPEnabled {
		configuredCommand := request.ACPCommand
		configuredArgs := request.ACPArgs
		if request.ACPAgent != "custom" {
			configuredCommand = ""
			configuredArgs = nil
			if runtime.settings.ACPAgent == request.ACPAgent {
				configuredCommand = runtime.settings.ACPCommand
				configuredArgs = runtime.settings.ACPArgs
			}
		}
		acpAdapter, err = resolveDesktopACPAdapter(request.ACPAgent, runtime.root, configuredCommand, configuredArgs)
		if err != nil {
			return err
		}
	}
	settingsPath := filepath.Join(runtime.root, "control-panel-settings.json")
	snapshotPaths := []string{
		settingsPath,
		runtime.files.manifest,
		runtime.files.serverURL,
		runtime.files.quickURL,
	}
	snapshots := make([]fileSnapshot, 0, len(snapshotPaths))
	for _, path := range snapshotPaths {
		snapshot, snapshotErr := snapshotFile(path)
		if snapshotErr != nil {
			return fmt.Errorf("备份配置失败: %w", snapshotErr)
		}
		snapshots = append(snapshots, snapshot)
	}

	if err := stopTunnel(ctx, runtime); err != nil {
		return err
	}
	rollback := func(cause error) error {
		restoreErr := restoreSnapshots(snapshots)
		oldRuntime, loadErr := loadTunnelRuntime(request.RuntimeRoot)
		if loadErr == nil {
			_ = platformServiceAction(ctx, oldRuntime.root, "restart")
			if oldRuntime.mode != "none" {
				_ = startTunnel(ctx, oldRuntime)
			}
		}
		if restoreErr != nil {
			return fmt.Errorf("%w；同时恢复配置失败: %v", cause, restoreErr)
		}
		return cause
	}

	acpCommand := acpAdapter.Command
	acpArgs := append([]string(nil), acpAdapter.Args...)
	if !request.ACPEnabled && request.ACPAgent == "custom" {
		acpCommand = request.ACPCommand
		acpArgs = append([]string(nil), request.ACPArgs...)
	}
	settings := controlPanelSettings{
		Port:                    request.Port,
		LogLevel:                request.LogLevel,
		OAuthAccessTokenTTL:     request.OAuthAccessTokenTTL,
		BrowserEnabled:          request.BrowserEnabled,
		BrowserCDPURL:           request.BrowserCDPURL,
		BrowserReuseExistingCDP: request.BrowserReuseExistingCDP,
		ACPEnabled:              request.ACPEnabled,
		ACPAgent:                request.ACPAgent,
		ACPCommand:              acpCommand,
		ACPArgs:                 acpArgs,
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return rollback(err)
	}
	data = append(data, '\n')
	if err := atomicfile.Write(settingsPath, data, 0o600); err != nil {
		return rollback(fmt.Errorf("保存控制面板设置失败: %w", err))
	}

	runtime.settings = settings
	publicURL := ""
	manifestMode := runtime.mode
	switch runtime.mode {
	case "quick":
		if err := clearActivePublicURL(runtime.files); err != nil {
			return rollback(err)
		}
		manifestMode = "none"
	case "named":
		publicURL, err = readTrimmedText(runtime.files.namedServerURL)
		if err != nil {
			return rollback(err)
		}
	}
	if err := runtime.updateManifest(manifestMode, publicURL); err != nil {
		return rollback(err)
	}
	if err := platformServiceAction(ctx, runtime.root, "restart"); err != nil {
		return rollback(err)
	}
	if runtime.mode != "none" {
		if err := startTunnel(ctx, runtime); err != nil {
			return rollback(err)
		}
	}
	return nil
}

//go:build windows

package desktopruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

func platformConfigureTunnel(ctx context.Context, request TunnelConfigureRequest) error {
	runtime, err := loadTunnelRuntime(request.RuntimeRoot)
	if err != nil {
		return err
	}
	if err := ensureDesktopCredentials(runtime.root); err != nil {
		return err
	}

	// A configure request updates several files plus the startup entry before the
	// replacement tunnel is proven ready. Keep the previously committed state so
	// any later failure can restore it as one transaction.
	snapshotPaths := []string{
		runtime.files.manifest,
		runtime.files.mode,
		runtime.files.serverURL,
		runtime.files.namedServerURL,
		runtime.files.quickURL,
		runtime.files.token,
	}
	snapshots := make([]fileSnapshot, 0, len(snapshotPaths))
	for _, path := range snapshotPaths {
		snapshot, snapshotErr := snapshotFile(path)
		if snapshotErr != nil {
			return fmt.Errorf("备份 Tunnel 配置失败: %w", snapshotErr)
		}
		snapshots = append(snapshots, snapshot)
	}
	oldAutostart, err := tunnelAutostartEnabled(runtime.manifest)
	if err != nil {
		return fmt.Errorf("读取 Tunnel 开机启动状态失败: %w", err)
	}

	tunnelStopped := false
	rollback := func(cause error) error {
		var restoreErr error
		if tunnelStopped {
			if err := stopTunnel(ctx, runtime); err != nil {
				restoreErr = errors.Join(restoreErr, err)
			}
		}
		restoreErr = errors.Join(restoreErr, restoreSnapshots(snapshots))
		if tunnelStopped {
			if err := platformSetTunnelAutostart(ctx, runtime.root, oldAutostart); err != nil {
				restoreErr = errors.Join(restoreErr, err)
			}
			oldRuntime, loadErr := loadTunnelRuntime(request.RuntimeRoot)
			if loadErr != nil {
				restoreErr = errors.Join(restoreErr, loadErr)
			} else {
				if err := platformServiceAction(ctx, oldRuntime.root, "restart"); err != nil {
					restoreErr = errors.Join(restoreErr, err)
				}
				if oldRuntime.mode != "none" {
					if err := startTunnel(ctx, oldRuntime); err != nil {
						restoreErr = errors.Join(restoreErr, err)
					}
				}
			}
		}
		if restoreErr != nil {
			return fmt.Errorf("%w；同时恢复 Tunnel 配置失败: %v", cause, restoreErr)
		}
		return cause
	}

	if err := preserveNamedServerURL(runtime); err != nil {
		return rollback(err)
	}

	namedServerURL := ""
	if request.Mode == "named" {
		candidate := strings.TrimSpace(request.ServerURL)
		if candidate == "" {
			candidate, err = readTrimmedText(runtime.files.namedServerURL)
			if err != nil {
				return rollback(err)
			}
		}
		namedServerURL, err = normalizeHTTPSOrigin(candidate)
		if err != nil {
			return rollback(err)
		}

		providedToken, err := readSecretFile(request.TokenFile)
		if err != nil {
			return rollback(err)
		}
		if providedToken != "" {
			if err := writeProtectedText(runtime.files.token, providedToken, tunnelTokenEntropy); err != nil {
				return rollback(fmt.Errorf("保存 Cloudflare Tunnel Token 失败: %w", err))
			}
		}
		storedToken, err := readProtectedText(runtime.files.token, tunnelTokenEntropy)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return rollback(errors.New("固定域名模式需要 Cloudflare Tunnel Token"))
			}
			return rollback(fmt.Errorf("读取 Cloudflare Tunnel Token 失败: %w", err))
		}
		if strings.TrimSpace(storedToken) == "" {
			return rollback(errors.New("固定域名模式需要 Cloudflare Tunnel Token"))
		}
	}

	if err := stopTunnel(ctx, runtime); err != nil {
		return rollback(err)
	}
	tunnelStopped = true
	switch request.Mode {
	case "none":
		if err := writeRuntimeText(runtime.files.mode, "none"); err != nil {
			return rollback(err)
		}
		if err := clearActivePublicURL(runtime.files); err != nil {
			return rollback(err)
		}
		if err := runtime.updateManifest("none", ""); err != nil {
			return rollback(err)
		}
		if err := platformSetTunnelAutostart(ctx, runtime.root, false); err != nil {
			return rollback(err)
		}
		if err := platformServiceAction(ctx, runtime.root, "restart"); err != nil {
			return rollback(err)
		}
		return nil
	case "quick":
		if err := writeRuntimeText(runtime.files.mode, "quick"); err != nil {
			return rollback(err)
		}
		if err := clearActivePublicURL(runtime.files); err != nil {
			return rollback(err)
		}
		if err := runtime.updateManifest("none", ""); err != nil {
			return rollback(err)
		}
		if err := platformSetTunnelAutostart(ctx, runtime.root, true); err != nil {
			return rollback(err)
		}
		if err := platformServiceAction(ctx, runtime.root, "restart"); err != nil {
			return rollback(err)
		}
		runtime.mode = "quick"
		if err := startTunnel(ctx, runtime); err != nil {
			return rollback(err)
		}
		return nil
	case "named":
		if err := writeRuntimeText(runtime.files.namedServerURL, namedServerURL); err != nil {
			return rollback(err)
		}
		if err := writeRuntimeText(runtime.files.serverURL, namedServerURL); err != nil {
			return rollback(err)
		}
		if err := writeRuntimeText(runtime.files.mode, "named"); err != nil {
			return rollback(err)
		}
		if err := os.Remove(runtime.files.quickURL); err != nil && !errors.Is(err, os.ErrNotExist) {
			return rollback(fmt.Errorf("删除 Quick Tunnel ready 文件失败: %w", err))
		}
		if err := runtime.updateManifest("named", namedServerURL); err != nil {
			return rollback(err)
		}
		if err := platformSetTunnelAutostart(ctx, runtime.root, true); err != nil {
			return rollback(err)
		}
		if err := platformServiceAction(ctx, runtime.root, "restart"); err != nil {
			return rollback(err)
		}
		runtime.mode = "named"
		if err := startTunnel(ctx, runtime); err != nil {
			return rollback(err)
		}
		return nil
	default:
		return rollback(fmt.Errorf("不支持的公网模式：%s", request.Mode))
	}
}

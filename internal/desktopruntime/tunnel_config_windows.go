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
	runtime, err := loadTunnelRuntime(ctx, request.RuntimeRoot)
	if err != nil {
		return err
	}
	if request.Mode != "none" {
		// 先证明 component 可运行再触碰 Token、mode 或 Core 配置，避免依赖缺失时
		// 留下“配置已切换但 Tunnel 永远起不来”的半状态。none 是恢复路径，不需要依赖。
		probe := runtime
		probe.mode = request.Mode
		if err := prepareCloudflaredRuntime(ctx, &probe); err != nil {
			return err
		}
		runtime.manifest.CloudflaredBinary = probe.manifest.CloudflaredBinary
	}
	if err := ensureDesktopCredentials(runtime.root); err != nil {
		return err
	}
	if err := preserveNamedServerURL(runtime); err != nil {
		return err
	}

	namedServerURL := ""
	if request.Mode == "named" {
		candidate := strings.TrimSpace(request.ServerURL)
		if candidate == "" {
			candidate, err = readTrimmedText(runtime.files.namedServerURL)
			if err != nil {
				return err
			}
		}
		namedServerURL, err = normalizeHTTPSOrigin(candidate)
		if err != nil {
			return err
		}

		providedToken, err := readSecretFile(request.TokenFile)
		if err != nil {
			return err
		}
		if providedToken != "" {
			if err := writeProtectedText(runtime.files.token, providedToken, tunnelTokenEntropy); err != nil {
				return fmt.Errorf("保存 Cloudflare Tunnel Token 失败: %w", err)
			}
		}
		storedToken, err := readProtectedText(runtime.files.token, tunnelTokenEntropy)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return errors.New("固定域名模式需要 Cloudflare Tunnel Token")
			}
			return fmt.Errorf("读取 Cloudflare Tunnel Token 失败: %w", err)
		}
		if strings.TrimSpace(storedToken) == "" {
			return errors.New("固定域名模式需要 Cloudflare Tunnel Token")
		}
	}

	if err := stopTunnel(ctx, runtime); err != nil {
		return err
	}
	switch request.Mode {
	case "none":
		if err := writeRuntimeText(runtime.files.mode, "none"); err != nil {
			return err
		}
		if err := clearActivePublicURL(runtime.files); err != nil {
			return err
		}
		if err := runtime.updateManifest("none", ""); err != nil {
			return err
		}
		if err := platformSetTunnelAutostart(ctx, runtime.root, false); err != nil {
			return err
		}
		return platformServiceAction(ctx, runtime.root, "restart")
	case "quick":
		if err := writeRuntimeText(runtime.files.mode, "quick"); err != nil {
			return err
		}
		if err := clearActivePublicURL(runtime.files); err != nil {
			return err
		}
		if err := runtime.updateManifest("none", ""); err != nil {
			return err
		}
		if err := platformSetTunnelAutostart(ctx, runtime.root, true); err != nil {
			return err
		}
		if err := platformServiceAction(ctx, runtime.root, "restart"); err != nil {
			return err
		}
		runtime.mode = "quick"
		return startTunnel(ctx, runtime)
	case "named":
		if err := writeRuntimeText(runtime.files.namedServerURL, namedServerURL); err != nil {
			return err
		}
		if err := writeRuntimeText(runtime.files.serverURL, namedServerURL); err != nil {
			return err
		}
		if err := writeRuntimeText(runtime.files.mode, "named"); err != nil {
			return err
		}
		if err := os.Remove(runtime.files.quickURL); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("删除 Quick Tunnel ready 文件失败: %w", err)
		}
		if err := runtime.updateManifest("named", namedServerURL); err != nil {
			return err
		}
		if err := platformSetTunnelAutostart(ctx, runtime.root, true); err != nil {
			return err
		}
		if err := platformServiceAction(ctx, runtime.root, "restart"); err != nil {
			return err
		}
		runtime.mode = "named"
		return startTunnel(ctx, runtime)
	default:
		return fmt.Errorf("不支持的公网模式：%s", request.Mode)
	}
}

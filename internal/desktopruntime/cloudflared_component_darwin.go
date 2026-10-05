//go:build darwin

package desktopruntime

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/uvwt/agentdock/internal/component"
)

func unixCloudflaredComponentStatus(runtimeRoot string, _ unixRuntimeManifest) (path, state, version string) {
	store, err := component.NewStore(runtimeRoot)
	if err != nil {
		return "", "broken", ""
	}
	status := store.Status()
	return status.Path, status.State, status.Version
}

func prepareUnixCloudflared(ctx context.Context, runtimeRoot string, manifest *unixRuntimeManifest) error {
	if manifest == nil {
		return errors.New("macOS Tunnel runtime is missing")
	}
	store, err := component.NewStore(runtimeRoot)
	if err != nil {
		return err
	}
	if binary, err := store.Resolve(); err == nil {
		manifest.CloudflaredBinary = binary
		return nil
	} else if !errors.Is(err, component.ErrNotInstalled) {
		return fmt.Errorf("Cloudflare Tunnel component 不可用: %w", err)
	}

	// 一次性兼容仍从旧 Bundle/CLI 启动的安装。只接受 AgentDock 自己的固定 legacy
	// 路径，不读取 PATH。App 原子升级的旧 Bundle 位于 rollback slot，另由更新 handoff
	// 在恢复 Tunnel 前显式导入；这里的 fallback 在 legacy 安装基数消失后自然退出。
	for _, legacy := range component.LegacyPaths(runtimeRoot) {
		if _, statErr := os.Lstat(legacy); statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			return statErr
		}
		if _, importErr := store.ImportLegacy(ctx, legacy); importErr != nil {
			return fmt.Errorf("导入旧 cloudflared component 失败: %w", importErr)
		}
		binary, resolveErr := store.Resolve()
		if resolveErr != nil {
			return resolveErr
		}
		manifest.CloudflaredBinary = binary
		return nil
	}
	return fmt.Errorf("Cloudflare Tunnel dependency-not-installed: %w；请先运行 agentdock component install cloudflared", component.ErrNotInstalled)
}

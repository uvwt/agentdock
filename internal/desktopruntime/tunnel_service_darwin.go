//go:build darwin

package desktopruntime

import (
	"context"
	"errors"
)

func platformTunnelServiceActive(ctx context.Context, manifest unixRuntimeManifest) bool {
	return launchdLoaded(ctx, manifest.TunnelServiceName)
}

func platformTunnelServiceEnabled(ctx context.Context, manifest unixRuntimeManifest) bool {
	// SMAppService 的注册状态由 AppKit 控制面板读取；Go 运行层只知道当前 job 是否已加载。
	return launchdLoaded(ctx, manifest.TunnelServiceName)
}

func platformTunnelServiceAction(ctx context.Context, manifest unixRuntimeManifest, action string) error {
	switch action {
	case "start", "restart":
		return kickstartRegisteredLaunchAgent(ctx, manifest.TunnelServiceName, manifest.ServiceManager)
	case "stop":
		if manifest.ServiceManager == "launchd" {
			_, err := runCommand(ctx, launchctlBinary(), "bootout", launchdTarget(manifest.TunnelServiceName))
			return err
		}
		return errors.New("macOS Tunnel 停用由 AgentDock.app 的 SMAppService 管理")
	default:
		return errors.New("不支持的 Tunnel 服务操作")
	}
}

func platformTunnelServiceSetEnabled(context.Context, unixRuntimeManifest, bool) error {
	return errors.New("macOS Tunnel 启停由 AgentDock.app 的 SMAppService 管理")
}

//go:build windows

package desktopruntime

import (
	"context"
	"errors"
	"strings"
)

func platformSetTunnelAutostart(ctx context.Context, runtimeRoot string, enabled bool) error {
	runtime, err := loadTunnelRuntime(ctx, runtimeRoot)
	if err != nil {
		return err
	}
	name := defaultString(runtime.manifest.CloudflaredStartupValueName, "AgentDockCloudflared")
	if !enabled {
		return removeRunValue(name)
	}
	command, err := tunnelStartupCommand(runtime.manifest, runtime.root)
	if err != nil {
		return err
	}
	return setRunValue(name, command)
}

func tunnelAutostartEnabled(manifest Manifest) (bool, error) {
	return runValuePresent(defaultString(manifest.CloudflaredStartupValueName, "AgentDockCloudflared"))
}

func tunnelStartupCommand(manifest Manifest, runtimeRoot string) (string, error) {
	if strings.TrimSpace(manifest.TrayBinary) != "" {
		return quoteWindowsArgument(manifest.TrayBinary) + " --start-tunnel --runtime-root " + quoteWindowsArgument(runtimeRoot), nil
	}
	if strings.TrimSpace(manifest.AgentDockBinary) == "" {
		return "", errors.New("Windows runtime manifest 缺少 agentdock_binary")
	}
	return quoteWindowsArgument(manifest.AgentDockBinary) + " tunnel start --runtime-root " + quoteWindowsArgument(runtimeRoot), nil
}

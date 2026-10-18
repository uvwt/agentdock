//go:build darwin

package desktopruntime

import (
	"os"
	"path/filepath"
)

// DefaultRuntimeRoot 返回 macOS 桌面版的标准运行目录。
// 显式 AGENTDOCK_RUNTIME_ROOT 用于自定义实例；否则由当前用户主目录解析。
func DefaultRuntimeRoot() string {
	if root := configuredRuntimeRoot(); root != "" {
		return root
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, "Library", "Application Support", "AgentDock")
}

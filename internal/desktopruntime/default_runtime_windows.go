//go:build windows

package desktopruntime

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultRuntimeRoot 返回 Windows 桌面版的标准运行目录。
func DefaultRuntimeRoot() string {
	if root := configuredRuntimeRoot(); root != "" {
		return root
	}
	localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if localAppData == "" {
		return ""
	}
	return filepath.Join(localAppData, "AgentDock")
}

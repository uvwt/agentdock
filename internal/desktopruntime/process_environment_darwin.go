//go:build darwin

package desktopruntime

import (
	"fmt"
	"os"
	"path/filepath"
)

func platformPrepareLaunchEnvironment(_ string) error {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return fmt.Errorf("解析 macOS 用户目录失败: %w", err)
	}
	workDir := filepath.Join(home, "AgentDock")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return fmt.Errorf("创建 AgentDock 工作目录失败: %w", err)
	}
	if err := os.Chdir(workDir); err != nil {
		return fmt.Errorf("进入 AgentDock 工作目录失败: %w", err)
	}

	logDir := filepath.Join(home, "Library", "Logs", "AgentDock")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return fmt.Errorf("创建 AgentDock 日志目录失败: %w", err)
	}
	// Core 与 Tunnel 都由进程内轮转 writer 接管日志，避免 launchd 固定文件无限增长。
	return nil
}

//go:build windows

package desktopruntime

import (
	"context"
	"fmt"
	"strings"
)

// StartInteractiveScheduledTask 用原生 Task Scheduler COM 启动 InteractiveToken 任务。
// 会话选择和 RunEx(TASK_RUN_USE_SESSION_ID) 走 Go 实现，不再调用 PowerShell 兼容垫片。
func StartInteractiveScheduledTask(ctx context.Context, runtimeRoot, taskName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_ = runtimeRoot
	taskName = strings.TrimLeft(strings.TrimSpace(taskName), `\`)
	if taskName == "" {
		taskName = "AgentDock"
	}
	if err := startInteractiveScheduledTaskNative(taskName, ""); err != nil {
		return fmt.Errorf("start Windows scheduled task %s: %w", taskName, err)
	}
	return nil
}

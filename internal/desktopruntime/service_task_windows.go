//go:build windows

package desktopruntime

import (
	"context"
	"fmt"
	"strings"
)

// platformStartScheduledTask 是 Setup/Installer 的 Windows 内部桥接入口。
// 会话选择和 RunEx(TASK_RUN_USE_SESSION_ID) 由 Go 原生实现统一拥有，PowerShell
// 只负责临时 Task 的注册和结果收集，不再维护另一份 WTS/COM 状态机。
func platformStartScheduledTask(ctx context.Context, taskName, expectedUserSID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	taskName = strings.TrimLeft(strings.TrimSpace(taskName), `\`)
	if taskName == "" {
		return fmt.Errorf("Windows Scheduled Task 名称不能为空")
	}
	if err := startInteractiveScheduledTaskNative(taskName, expectedUserSID); err != nil {
		return fmt.Errorf("start Windows scheduled task %s: %w", taskName, err)
	}
	return nil
}

//go:build windows

package desktopruntime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// StartInteractiveScheduledTask starts an InteractiveToken task through the
// shared Windows Task Scheduler helper so every caller uses the same session
// selection, user-SID validation, and RunEx(TASK_RUN_USE_SESSION_ID) behavior.
func StartInteractiveScheduledTask(ctx context.Context, runtimeRoot, taskName string) error {
	runtimeRoot = strings.TrimSpace(runtimeRoot)
	if runtimeRoot == "" {
		return fmt.Errorf("Windows runtime root is empty while starting scheduled task")
	}

	scriptPath := filepath.Join(runtimeRoot, "installer", "manage-windows.ps1")
	info, err := os.Stat(scriptPath)
	if err != nil || info.IsDir() {
		if err == nil {
			err = fmt.Errorf("path is a directory")
		}
		return fmt.Errorf("Windows task-session helper is unavailable at %s: %w", scriptPath, err)
	}

	taskName = strings.TrimLeft(strings.TrimSpace(taskName), `\`)
	if taskName == "" {
		taskName = "AgentDock"
	}
	command := exec.CommandContext(
		ctx,
		"powershell.exe",
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-WindowStyle", "Hidden",
		"-ExecutionPolicy", "Bypass",
		"-File", scriptPath,
		"-Action", "task-run-session",
		"-ScheduledTaskName", taskName,
		"-ScheduledTaskPath", `\`,
	)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return fmt.Errorf("start Windows scheduled task %s through session helper: %w", taskName, err)
		}
		return fmt.Errorf("start Windows scheduled task %s through session helper: %w: %s", taskName, err, message)
	}
	return nil
}

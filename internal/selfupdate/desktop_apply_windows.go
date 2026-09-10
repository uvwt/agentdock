//go:build windows

package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"github.com/uvwt/agentdock/internal/desktopruntime"
)

var windowsDesktopStagedFiles = []string{
	"agentdock-tray.exe",
	"agentdock.ico",
	"manage-windows.ps1",
	windowsDesktopVersionFile,
}

type windowsDesktopFileState struct {
	stagedPath string
	targetPath string
	backupPath string
	existed    bool
	installed  bool
}

type windowsDesktopUpdate struct {
	trayPath       string
	trayWasRunning bool
	backupRoot     string
	files          []windowsDesktopFileState
}

func applyWindowsDesktopOnlyUpdate(ctx context.Context, request applyRequest) (applyResult, error) {
	manifest, err := desktopruntime.LoadForBinary(request.CurrentPath)
	if err != nil {
		return applyResult{}, fmt.Errorf("读取 Windows runtime manifest 失败: %w", err)
	}
	coreWasRunning, err := desktopruntime.BinaryProcessRunning(request.CurrentPath)
	if err != nil {
		return applyResult{}, fmt.Errorf("检查 Windows 核心进程失败: %w", err)
	}
	taskWasRunning := manifest.UsesScheduledTask() && coreWasRunning
	taskName := windowsScheduledTaskPath(manifest.AgentDockTaskName)
	runtimeRoot := filepath.Dir(filepath.Dir(request.CurrentPath))

	update, err := prepareWindowsDesktopUpdate(request.CurrentPath, request.DesktopTargetPath, request.DesktopStagedPath)
	if err != nil {
		return applyResult{}, fmt.Errorf("准备 Windows 控制面板修复失败: %w", err)
	}
	if update == nil {
		return applyResult{}, errors.New("Windows 控制面板修复缺少更新内容")
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = update.Commit()
		}
	}()

	rollback := func(cause error) error {
		if taskWasRunning {
			_ = runWindowsCommand(ctx, "schtasks.exe", "/End", "/TN", taskName)
			_, _ = desktopruntime.WaitBinaryStopped(ctx, request.CurrentPath, 10*time.Second)
		}
		_ = desktopruntime.StopBinaryProcesses(ctx, update.trayPath, 15*time.Second)
		var failures []string
		if restoreErr := update.Restore(); restoreErr != nil {
			failures = append(failures, "恢复旧控制面板失败: "+restoreErr.Error())
		}
		if taskWasRunning {
			if restartErr := desktopruntime.StartInteractiveScheduledTask(ctx, runtimeRoot, taskName); restartErr != nil {
				failures = append(failures, "重新启动旧核心计划任务失败: "+restartErr.Error())
			} else if waitErr := waitForVersion(ctx, []string{manifest.HealthURL()}, request.CurrentVersion, 30*time.Second); waitErr != nil {
				failures = append(failures, "旧核心健康检查失败: "+waitErr.Error())
			}
		}
		if restartErr := update.RestartTray(ctx); restartErr != nil {
			failures = append(failures, "重新启动旧控制面板失败: "+restartErr.Error())
		}
		if len(failures) > 0 {
			return fmt.Errorf("%v；Windows 控制面板自动恢复不完整: %s", cause, strings.Join(failures, "；"))
		}
		return fmt.Errorf("%v；已自动恢复旧控制面板", cause)
	}

	if taskWasRunning {
		if err := runWindowsCommand(ctx, "schtasks.exe", "/End", "/TN", taskName); err != nil {
			return applyResult{}, fmt.Errorf("停止 Windows 管理员核心计划任务失败: %w", err)
		}
		if stopped, waitErr := desktopruntime.WaitBinaryStopped(ctx, request.CurrentPath, 15*time.Second); waitErr != nil {
			_ = desktopruntime.StartInteractiveScheduledTask(context.Background(), runtimeRoot, taskName)
			return applyResult{}, fmt.Errorf("等待 Windows 管理员核心退出失败: %w", waitErr)
		} else if !stopped {
			_ = desktopruntime.StartInteractiveScheduledTask(context.Background(), runtimeRoot, taskName)
			return applyResult{}, errors.New("Windows 管理员核心未在 15s 内退出")
		}
	}
	if err := update.StopTray(ctx); err != nil {
		if taskWasRunning {
			_ = desktopruntime.StartInteractiveScheduledTask(context.Background(), runtimeRoot, taskName)
		}
		return applyResult{}, err
	}
	fmt.Fprintln(request.Output, "正在修复 Windows 控制面板组件...")
	if err := update.Install(); err != nil {
		return applyResult{}, rollback(err)
	}
	if taskWasRunning {
		if err := desktopruntime.StartInteractiveScheduledTask(ctx, runtimeRoot, taskName); err != nil {
			return applyResult{}, rollback(fmt.Errorf("重新启动 Windows 管理员核心计划任务失败: %w", err))
		}
		if err := waitForVersion(ctx, []string{manifest.HealthURL()}, request.TargetVersion, 30*time.Second); err != nil {
			return applyResult{}, rollback(fmt.Errorf("Windows 管理员核心健康检查失败: %w", err))
		}
	}
	if err := update.RestartTray(ctx); err != nil {
		return applyResult{}, rollback(err)
	}
	if err := update.Commit(); err != nil {
		fmt.Fprintf(request.Output, "警告：清理 Windows 控制面板更新备份失败: %v\n", err)
	}
	cleanup = false
	fmt.Fprintf(request.Output, "Windows 控制面板已同步到 %s\n", normalizeVersion(request.TargetVersion))
	return applyResult{}, nil
}

func windowsScheduledTaskPath(name string) string {
	name = strings.TrimLeft(strings.TrimSpace(name), `\`)
	if name == "" {
		name = "AgentDock"
	}
	return `\` + name
}

func prepareWindowsDesktopUpdate(currentCorePath, runtimeRoot, stagedRoot string) (*windowsDesktopUpdate, error) {
	if strings.TrimSpace(runtimeRoot) == "" && strings.TrimSpace(stagedRoot) == "" {
		return nil, nil
	}
	if strings.TrimSpace(currentCorePath) == "" || strings.TrimSpace(runtimeRoot) == "" || strings.TrimSpace(stagedRoot) == "" {
		return nil, errors.New("Windows 桌面更新路径不完整")
	}

	currentCorePath = filepath.Clean(currentCorePath)
	runtimeRoot = filepath.Clean(runtimeRoot)
	stagedRoot = filepath.Clean(stagedRoot)
	expectedRuntimeRoot := filepath.Dir(filepath.Dir(currentCorePath))
	if !sameWindowsPath(runtimeRoot, expectedRuntimeRoot) {
		return nil, fmt.Errorf("Windows 桌面更新目录与核心不匹配: %s", runtimeRoot)
	}
	manifest, err := desktopruntime.LoadForBinary(currentCorePath)
	if err != nil {
		return nil, fmt.Errorf("读取 Windows runtime manifest 失败: %w", err)
	}
	expectedTrayPath := filepath.Join(filepath.Dir(currentCorePath), "agentdock-tray.exe")
	if !sameWindowsPath(manifest.TrayBinary, expectedTrayPath) {
		return nil, fmt.Errorf("Windows runtime manifest 的 tray_binary 不在核心目录: %s", manifest.TrayBinary)
	}

	backupRoot, err := os.MkdirTemp(filepath.Dir(stagedRoot), "windows-desktop-backup-*")
	if err != nil {
		return nil, fmt.Errorf("创建 Windows 桌面更新备份目录失败: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(backupRoot)
		}
	}()

	targets := map[string]string{
		"agentdock-tray.exe":      expectedTrayPath,
		"agentdock.ico":           filepath.Join(filepath.Dir(currentCorePath), "agentdock.ico"),
		"manage-windows.ps1":      filepath.Join(runtimeRoot, "installer", "manage-windows.ps1"),
		windowsDesktopVersionFile: filepath.Join(runtimeRoot, windowsDesktopVersionFile),
	}
	files := make([]windowsDesktopFileState, 0, len(windowsDesktopStagedFiles))
	for _, name := range windowsDesktopStagedFiles {
		stagedPath := filepath.Join(stagedRoot, name)
		info, statErr := os.Lstat(stagedPath)
		if statErr != nil {
			return nil, fmt.Errorf("Windows 桌面更新暂存文件缺失 %s: %w", name, statErr)
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("Windows 桌面更新暂存文件不是普通文件: %s", stagedPath)
		}

		targetPath := targets[name]
		state := windowsDesktopFileState{
			stagedPath: stagedPath,
			targetPath: targetPath,
			backupPath: filepath.Join(backupRoot, name),
		}
		if targetInfo, targetErr := os.Lstat(targetPath); targetErr == nil {
			if !targetInfo.Mode().IsRegular() || targetInfo.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("Windows 桌面更新目标不是普通文件: %s", targetPath)
			}
			state.existed = true
			if err := copyFileWindows(targetPath, state.backupPath); err != nil {
				return nil, fmt.Errorf("备份 Windows 桌面文件 %s 失败: %w", name, err)
			}
		} else if !os.IsNotExist(targetErr) {
			return nil, fmt.Errorf("检查 Windows 桌面文件 %s 失败: %w", name, targetErr)
		}
		files = append(files, state)
	}

	cleanup = false
	return &windowsDesktopUpdate{
		trayPath:   expectedTrayPath,
		backupRoot: backupRoot,
		files:      files,
	}, nil
}

func (update *windowsDesktopUpdate) StopTray(ctx context.Context) error {
	if update == nil {
		return nil
	}
	running, err := desktopruntime.BinaryProcessRunning(update.trayPath)
	if err != nil {
		return fmt.Errorf("检查 Windows 控制面板进程失败: %w", err)
	}
	update.trayWasRunning = running
	if !running {
		return nil
	}
	if err := desktopruntime.StopBinaryProcesses(ctx, update.trayPath, 15*time.Second); err != nil {
		return fmt.Errorf("停止 Windows 控制面板失败: %w", err)
	}
	return nil
}

func (update *windowsDesktopUpdate) Install() error {
	if update == nil {
		return nil
	}
	for index := range update.files {
		file := &update.files[index]
		if err := os.MkdirAll(filepath.Dir(file.targetPath), 0o755); err != nil {
			return fmt.Errorf("创建 Windows 桌面文件目录失败: %w", err)
		}
		newPath := file.targetPath + ".new"
		if err := os.Remove(newPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("清理 Windows 桌面临时文件失败 %s: %w", newPath, err)
		}
		if err := copyFileWindows(file.stagedPath, newPath); err != nil {
			return fmt.Errorf("暂存 Windows 桌面文件失败 %s: %w", filepath.Base(file.targetPath), err)
		}
		if err := moveFileReplace(newPath, file.targetPath); err != nil {
			_ = os.Remove(newPath)
			return fmt.Errorf("替换 Windows 桌面文件失败 %s: %w", filepath.Base(file.targetPath), err)
		}
		file.installed = true
	}
	return nil
}

func (update *windowsDesktopUpdate) Restore() error {
	if update == nil {
		return nil
	}
	var failures []string
	for index := len(update.files) - 1; index >= 0; index-- {
		file := &update.files[index]
		if !file.installed {
			continue
		}
		if !file.existed {
			if err := os.Remove(file.targetPath); err != nil && !os.IsNotExist(err) {
				failures = append(failures, fmt.Sprintf("删除新增文件 %s 失败: %v", filepath.Base(file.targetPath), err))
			}
			continue
		}
		rollbackPath := file.targetPath + ".rollback"
		_ = os.Remove(rollbackPath)
		if err := copyFileWindows(file.backupPath, rollbackPath); err != nil {
			failures = append(failures, fmt.Sprintf("准备回滚文件 %s 失败: %v", filepath.Base(file.targetPath), err))
			continue
		}
		if err := moveFileReplace(rollbackPath, file.targetPath); err != nil {
			_ = os.Remove(rollbackPath)
			failures = append(failures, fmt.Sprintf("恢复旧文件 %s 失败: %v", filepath.Base(file.targetPath), err))
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "；"))
	}
	return nil
}

func (update *windowsDesktopUpdate) RestartTray(ctx context.Context) error {
	if update == nil || !update.trayWasRunning {
		return nil
	}
	command := exec.Command(update.trayPath, "--background")
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS}
	if err := command.Start(); err != nil {
		return fmt.Errorf("重新启动 Windows 控制面板失败: %w", err)
	}
	_ = command.Process.Release()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		running, err := desktopruntime.BinaryProcessRunning(update.trayPath)
		if err == nil && running {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return errors.New("新版 Windows 控制面板启动超时")
}

func (update *windowsDesktopUpdate) Commit() error {
	if update == nil {
		return nil
	}
	var failures []string
	if err := os.RemoveAll(update.backupRoot); err != nil {
		failures = append(failures, err.Error())
	}
	for _, file := range update.files {
		for _, suffix := range []string{".new", ".rollback"} {
			if err := os.Remove(file.targetPath + suffix); err != nil && !os.IsNotExist(err) {
				failures = append(failures, err.Error())
			}
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "；"))
	}
	return nil
}

func copyWindowsDesktopStaging(sourceRoot, targetRoot string) error {
	if err := os.Mkdir(targetRoot, 0o700); err != nil {
		return err
	}
	for _, name := range windowsDesktopStagedFiles {
		source := filepath.Join(sourceRoot, name)
		info, err := os.Lstat(source)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("Windows 桌面暂存文件不是普通文件: %s", source)
		}
		if err := copyFileWindows(source, filepath.Join(targetRoot, name)); err != nil {
			return err
		}
	}
	return nil
}

func sameWindowsPath(left, right string) bool {
	left = strings.TrimRight(filepath.Clean(left), `\/`)
	right = strings.TrimRight(filepath.Clean(right), `\/`)
	return strings.EqualFold(left, right)
}

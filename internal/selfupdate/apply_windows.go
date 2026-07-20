//go:build windows

package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const windowsServiceName = "agentdock"

type windowsUpdatePlan struct {
	ParentPID      int      `json:"parent_pid"`
	TargetPath     string   `json:"target_path"`
	StagedPath     string   `json:"staged_path"`
	BundlePath     string   `json:"bundle_path"`
	CurrentVersion string   `json:"current_version"`
	TargetVersion  string   `json:"target_version"`
	RestartMode    string   `json:"restart_mode"`
	LauncherPath   string   `json:"launcher_path,omitempty"`
	HealthURLs     []string `json:"health_urls,omitempty"`
}

func applyPlatformUpdate(ctx context.Context, request applyRequest) (applyResult, error) {
	helperDir, err := os.MkdirTemp("", "agentdock-update-helper-*")
	if err != nil {
		return applyResult{}, fmt.Errorf("创建 Windows 更新辅助目录失败: %w", err)
	}
	cleanupOnError := true
	defer func() {
		if cleanupOnError {
			_ = os.RemoveAll(helperDir)
		}
	}()

	helperPath := filepath.Join(helperDir, "agentdock-update-helper.exe")
	stagedPath := filepath.Join(helperDir, "agentdock-new.exe")
	bundlePath := filepath.Join(helperDir, "core-skills")
	planPath := filepath.Join(helperDir, "update-plan.json")
	if err := copyFileWindows(request.CurrentPath, helperPath); err != nil {
		return applyResult{}, fmt.Errorf("准备 Windows 更新辅助程序失败: %w", err)
	}
	if err := copyFileWindows(request.StagedPath, stagedPath); err != nil {
		return applyResult{}, fmt.Errorf("准备 Windows 新版本文件失败: %w", err)
	}
	if err := copyDirectoryWindows(request.BundlePath, bundlePath); err != nil {
		return applyResult{}, fmt.Errorf("准备 Windows 核心 Skill Bundle 失败: %w", err)
	}

	restartMode := "none"
	launcherPath := filepath.Join(filepath.Dir(filepath.Dir(request.CurrentPath)), "start-agentdock.ps1")
	if serviceManagesTarget(ctx, request.CurrentPath) {
		restartMode = "service"
	} else if launcherManagesTarget(ctx, launcherPath, request.CurrentPath) {
		restartMode = "launcher"
	}
	healthURLs := uniqueStrings(append(platformHealthCandidates(ctx, request.CurrentPath), healthCandidates(request.CurrentPath)...))
	if healthyURL := findHealthyURL(ctx, healthURLs); healthyURL != "" {
		healthURLs = []string{healthyURL}
	}
	plan := windowsUpdatePlan{
		ParentPID:      os.Getpid(),
		TargetPath:     request.CurrentPath,
		StagedPath:     stagedPath,
		BundlePath:     bundlePath,
		CurrentVersion: request.CurrentVersion,
		TargetVersion:  request.TargetVersion,
		RestartMode:    restartMode,
		LauncherPath:   launcherPath,
		HealthURLs:     healthURLs,
	}
	planData, err := json.Marshal(plan)
	if err != nil {
		return applyResult{}, fmt.Errorf("编码 Windows 更新计划失败: %w", err)
	}
	if err := os.WriteFile(planPath, planData, 0o600); err != nil {
		return applyResult{}, fmt.Errorf("写入 Windows 更新计划失败: %w", err)
	}

	command := exec.Command(helperPath, "__update-finalize", planPath)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	if outputFile, ok := request.Output.(*os.File); ok {
		command.Stdout = outputFile
		command.Stderr = outputFile
	}
	if err := command.Start(); err != nil {
		return applyResult{}, fmt.Errorf("启动 Windows 更新辅助程序失败: %w", err)
	}
	cleanupOnError = false
	return applyResult{HandedOff: true}, nil
}

func HandleInternalCommand(ctx context.Context, args []string) (bool, error) {
	if len(args) == 0 || args[0] != "__update-finalize" {
		return false, nil
	}
	if len(args) != 2 {
		return true, errors.New("Windows 更新辅助命令参数无效")
	}
	data, err := os.ReadFile(args[1])
	if err != nil {
		return true, fmt.Errorf("读取 Windows 更新计划失败: %w", err)
	}
	var plan windowsUpdatePlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return true, fmt.Errorf("解析 Windows 更新计划失败: %w", err)
	}
	if plan.ParentPID <= 0 || strings.TrimSpace(plan.TargetPath) == "" || strings.TrimSpace(plan.StagedPath) == "" || strings.TrimSpace(plan.BundlePath) == "" || normalizeVersion(plan.CurrentVersion) == "" || normalizeVersion(plan.TargetVersion) == "" {
		return true, errors.New("Windows 更新计划缺少必要字段")
	}
	defer scheduleWindowsCleanup(filepath.Dir(args[1]))
	return true, finalizeWindowsUpdate(ctx, plan)
}

func finalizeWindowsUpdate(ctx context.Context, plan windowsUpdatePlan) error {
	if err := waitForWindowsProcessExit(plan.ParentPID, 30*time.Second); err != nil {
		return err
	}
	restartOldOnFailure := plan.RestartMode != "none"
	defer func() {
		if restartOldOnFailure {
			_ = restartWindowsMode(context.Background(), plan)
		}
	}()
	if plan.RestartMode == "service" {
		if err := runWindowsCommand(ctx, "sc.exe", "stop", windowsServiceName); err != nil {
			if stateErr := waitWindowsServiceState(ctx, windowsServiceName, "STOPPED", time.Second); stateErr != nil {
				return fmt.Errorf("停止 Windows Service 失败: %w", err)
			}
		}
		if err := waitWindowsServiceState(ctx, windowsServiceName, "STOPPED", 20*time.Second); err != nil {
			return err
		}
	}
	if err := stopWindowsProcessesAtPath(ctx, plan.TargetPath); err != nil {
		return fmt.Errorf("停止旧 AgentDock 进程失败: %w", err)
	}

	backupPath := plan.TargetPath + ".backup"
	newPath := plan.TargetPath + ".new"
	if err := os.Remove(newPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("清理 Windows 临时更新文件失败: %w", err)
	}
	if err := copyFileWindows(plan.StagedPath, newPath); err != nil {
		return fmt.Errorf("复制 Windows 新版本失败: %w", err)
	}
	if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(newPath)
		return fmt.Errorf("清理 Windows 旧备份失败: %w", err)
	}
	if err := copyFileWindows(plan.TargetPath, backupPath); err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("备份 Windows 当前版本失败: %w", err)
	}
	// Windows 不能覆盖运行中的 exe。辅助进程等旧进程退出后，用 MoveFileEx 原子替换并要求写穿磁盘。
	if err := moveFileReplace(newPath, plan.TargetPath); err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("原子替换 Windows 新版本失败: %w", err)
	}

	rollback := func(cause error) error {
		_ = stopWindowsProcessesAtPath(ctx, plan.TargetPath)
		if err := moveFileReplace(backupPath, plan.TargetPath); err != nil {
			return fmt.Errorf("%v；自动恢复 Windows 旧版本失败: %w", cause, err)
		}
		if err := restartWindowsMode(ctx, plan); err != nil {
			return fmt.Errorf("%v；旧版本已恢复，但重新启动失败: %w", cause, err)
		}
		if plan.RestartMode != "none" {
			if err := waitForVersion(ctx, plan.HealthURLs, plan.CurrentVersion, 30*time.Second); err != nil {
				return fmt.Errorf("%v；旧版本已恢复并重启，但健康检查失败: %w", cause, err)
			}
		}
		restartOldOnFailure = false
		return fmt.Errorf("%v；已自动恢复 Windows 旧版本", cause)
	}

	if err := verifyBinaryVersion(ctx, plan.TargetPath, plan.TargetVersion); err != nil {
		return rollback(fmt.Errorf("替换后的 Windows 二进制验证失败: %w", err))
	}
	if plan.RestartMode != "none" {
		if err := restartWindowsMode(ctx, plan); err != nil {
			return rollback(fmt.Errorf("重新启动 Windows AgentDock 失败: %w", err))
		}
		if err := waitForVersion(ctx, plan.HealthURLs, plan.TargetVersion, 30*time.Second); err != nil {
			return rollback(fmt.Errorf("Windows 新版本健康检查失败: %w", err))
		}
		fmt.Println("健康检查通过")
	}

	fmt.Println("正在更新官方核心 Skill...")
	if err := bootstrapBundledSkills(ctx, plan.TargetPath, plan.BundlePath, os.Stdout); err != nil {
		return rollback(err)
	}
	restartOldOnFailure = false
	if plan.RestartMode == "none" {
		fmt.Printf("Windows 更新完成到 %s；当前未检测到托管服务，请重新启动 AgentDock。\n", plan.TargetVersion)
	} else {
		fmt.Printf("Windows 更新完成并已重启到 %s\n", plan.TargetVersion)
	}
	return nil
}

func platformHealthCandidates(_ context.Context, targetPath string) []string {
	launcherPath := filepath.Join(filepath.Dir(filepath.Dir(targetPath)), "start-agentdock.ps1")
	data, err := os.ReadFile(launcherPath)
	if err != nil {
		return nil
	}
	match := regexp.MustCompile(`AGENTDOCK_PORT\s*=\s*'([0-9]+)'`).FindStringSubmatch(string(data))
	if len(match) != 2 {
		return nil
	}
	return []string{"http://127.0.0.1:" + match[1] + "/healthz"}
}

func launcherManagesTarget(ctx context.Context, launcherPath, targetPath string) bool {
	data, err := os.ReadFile(launcherPath)
	if err != nil || !strings.Contains(strings.ToLower(string(data)), strings.ToLower(filepath.Clean(targetPath))) {
		return false
	}
	output, err := exec.CommandContext(
		ctx,
		"reg.exe",
		"query",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
		"/v",
		"AgentDock",
	).CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(output)), strings.ToLower(filepath.Clean(launcherPath)))
}

func serviceManagesTarget(ctx context.Context, targetPath string) bool {
	output, err := exec.CommandContext(ctx, "sc.exe", "qc", windowsServiceName).CombinedOutput()
	if err != nil {
		return false
	}
	normalizedOutput := strings.ToLower(strings.ReplaceAll(string(output), "/", `\`))
	normalizedTarget := strings.ToLower(strings.ReplaceAll(filepath.Clean(targetPath), "/", `\`))
	return strings.Contains(normalizedOutput, normalizedTarget)
}

func restartWindowsMode(ctx context.Context, plan windowsUpdatePlan) error {
	switch plan.RestartMode {
	case "service":
		if err := runWindowsCommand(ctx, "sc.exe", "start", windowsServiceName); err != nil {
			return err
		}
		return waitWindowsServiceState(ctx, windowsServiceName, "RUNNING", 20*time.Second)
	case "launcher":
		if _, err := os.Stat(plan.LauncherPath); err != nil {
			return fmt.Errorf("Windows 启动脚本不可用: %w", err)
		}
		command := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", plan.LauncherPath)
		command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS}
		if err := command.Start(); err != nil {
			return fmt.Errorf("启动 Windows AgentDock launcher 失败: %w", err)
		}
		return nil
	case "none":
		return nil
	default:
		return fmt.Errorf("未知 Windows 重启模式: %s", plan.RestartMode)
	}
}

func stopWindowsProcessesAtPath(ctx context.Context, targetPath string) error {
	escapedPath := strings.ReplaceAll(filepath.Clean(targetPath), "'", "''")
	script := fmt.Sprintf(`$target=[IO.Path]::GetFullPath('%s'); Get-Process -Name agentdock -ErrorAction SilentlyContinue | ForEach-Object { try { if ([IO.Path]::GetFullPath($_.Path) -eq $target) { Stop-Process -Id $_.Id -Force } } catch {} }`, escapedPath)
	command := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		check := fmt.Sprintf(`$target=[IO.Path]::GetFullPath('%s'); $found=$false; Get-Process -Name agentdock -ErrorAction SilentlyContinue | ForEach-Object { try { if ([IO.Path]::GetFullPath($_.Path) -eq $target) { $found=$true } } catch {} }; if ($found) { exit 1 }`, escapedPath)
		if exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", check).Run() == nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errors.New("旧 AgentDock 进程在 15 秒内未退出")
}

func waitForWindowsProcessExit(pid int, timeout time.Duration) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(handle)
	status, err := windows.WaitForSingleObject(handle, uint32(timeout/time.Millisecond))
	if err != nil {
		return fmt.Errorf("等待更新命令退出失败: %w", err)
	}
	if status == uint32(windows.WAIT_TIMEOUT) {
		return errors.New("等待更新命令退出超时")
	}
	return nil
}

func waitWindowsServiceState(ctx context.Context, serviceName, wanted string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		output, err := exec.CommandContext(ctx, "sc.exe", "query", serviceName).CombinedOutput()
		if err == nil && strings.Contains(strings.ToUpper(string(output)), "STATE") && strings.Contains(strings.ToUpper(string(output)), wanted) {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("Windows Service %s 在 %s 内未进入 %s 状态", serviceName, timeout, wanted)
}

func runWindowsCommand(ctx context.Context, name string, args ...string) error {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s 失败: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func copyDirectoryWindows(sourceRoot, targetRoot string) error {
	info, err := os.Lstat(sourceRoot)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("核心 Skill Bundle 必须是普通目录")
	}
	if err := os.Mkdir(targetRoot, 0o700); err != nil {
		return err
	}
	return filepath.WalkDir(sourceRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == sourceRoot {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("核心 Skill Bundle 不允许符号链接: %s", path)
		}
		relative, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		target := filepath.Join(targetRoot, relative)
		if entry.IsDir() {
			return os.Mkdir(target, 0o700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("核心 Skill Bundle 只允许普通文件: %s", path)
		}
		return copyFileWindows(path, target)
	})
}

func copyFileWindows(sourcePath, targetPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(target, source); err != nil {
		_ = target.Close()
		return err
	}
	if err := target.Sync(); err != nil {
		_ = target.Close()
		return err
	}
	return target.Close()
}

func moveFileReplace(sourcePath, targetPath string) error {
	source, err := windows.UTF16PtrFromString(sourcePath)
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(targetPath)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(
		source,
		target,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
	)
}

func scheduleWindowsCleanup(directory string) {
	command := exec.Command(
		"powershell.exe",
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-WindowStyle", "Hidden",
		"-Command", "Start-Sleep -Seconds 2; Remove-Item -LiteralPath $env:AGENTDOCK_UPDATE_CLEANUP_DIR -Recurse -Force -ErrorAction SilentlyContinue",
	)
	command.Env = append(os.Environ(), "AGENTDOCK_UPDATE_CLEANUP_DIR="+directory)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS}
	_ = command.Start()
}

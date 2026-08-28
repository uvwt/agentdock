//go:build darwin

package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type macOSDesktopUpdate struct {
	targetPath    string
	stagedPath    string
	targetVersion string
	newPath       string
	backupPath    string
	appWasRunning bool
	installed     bool
}

type macOSDesktopUpdateHandoff struct {
	SchemaVersion int    `json:"schema_version"`
	TargetVersion string `json:"target_version"`
}

func prepareDesktopUpdate(
	ctx context.Context,
	targetPath string,
	stagedPath string,
	targetVersion string,
) (desktopUpdateTransaction, error) {
	targetPath = strings.TrimSpace(targetPath)
	stagedPath = strings.TrimSpace(stagedPath)
	if targetPath == "" && stagedPath == "" {
		return nil, nil
	}
	if targetPath == "" || stagedPath == "" {
		return nil, errors.New("macOS 控制面板目标路径和暂存路径必须同时存在")
	}
	targetPath = filepath.Clean(targetPath)
	stagedPath = filepath.Clean(stagedPath)
	if err := validateMacOSDesktopTarget(targetPath); err != nil {
		return nil, fmt.Errorf("当前 macOS App 无效: %w", err)
	}
	if err := validateMacOSDesktopRuntime(ctx, stagedPath, targetVersion); err != nil {
		return nil, fmt.Errorf("新版 macOS App 无效: %w", err)
	}
	parent := filepath.Dir(targetPath)
	parentInfo, err := os.Stat(parent)
	if err != nil || !parentInfo.IsDir() {
		return nil, fmt.Errorf("macOS App 父目录不可用: %s", parent)
	}
	probe, err := os.CreateTemp(parent, ".agentdock-update-permission-*")
	if err != nil {
		return nil, fmt.Errorf("macOS App 父目录不可写: %w", err)
	}
	probePath := probe.Name()
	closeErr := probe.Close()
	removeErr := os.Remove(probePath)
	if closeErr != nil {
		return nil, fmt.Errorf("关闭 macOS App 权限探针失败: %w", closeErr)
	}
	if removeErr != nil {
		return nil, fmt.Errorf("清理 macOS App 权限探针失败: %w", removeErr)
	}
	suffix := strconv.Itoa(os.Getpid()) + "." + time.Now().UTC().Format("20060102150405.000000000")
	return &macOSDesktopUpdate{
		targetPath:    targetPath,
		stagedPath:    stagedPath,
		targetVersion: targetVersion,
		newPath:       filepath.Join(parent, ".AgentDock.app.new."+suffix),
		backupPath:    filepath.Join(parent, ".AgentDock.app.backup."+suffix),
	}, nil
}

func (update *macOSDesktopUpdate) Install(ctx context.Context) error {
	if err := os.RemoveAll(update.newPath); err != nil {
		return fmt.Errorf("清理 App 暂存副本失败: %w", err)
	}
	if err := os.RemoveAll(update.backupPath); err != nil {
		return fmt.Errorf("清理 App 备份路径失败: %w", err)
	}
	output, err := exec.CommandContext(ctx, "/usr/bin/ditto", update.stagedPath, update.newPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("复制新版 App 失败: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if err := validateMacOSDesktopRuntime(ctx, update.newPath, update.targetVersion); err != nil {
		return fmt.Errorf("复制后的新版 App 验证失败: %w", err)
	}

	pids, err := runningMacOSAppPIDs(ctx, update.targetPath)
	if err != nil {
		return err
	}
	update.appWasRunning = len(pids) > 0
	var outputRedirect *processOutputRedirect
	if update.appWasRunning {
		outputRedirect, err = redirectProcessOutputForDesktopUpdate()
		if err != nil {
			return fmt.Errorf("准备独立更新日志失败: %w", err)
		}
	}
	if err := terminatePIDs(ctx, pids); err != nil {
		if outputRedirect != nil {
			_ = outputRedirect.Restore()
		}
		return fmt.Errorf("关闭旧控制面板失败: %w", err)
	}
	if outputRedirect != nil {
		outputRedirect.Commit()
	}

	if err := os.Rename(update.targetPath, update.backupPath); err != nil {
		return fmt.Errorf("备份旧 App 失败: %w", err)
	}
	if err := os.Rename(update.newPath, update.targetPath); err != nil {
		restoreErr := os.Rename(update.backupPath, update.targetPath)
		if restoreErr != nil {
			return fmt.Errorf("安装新版 App 失败: %w；恢复旧 App 也失败: %v", err, restoreErr)
		}
		return fmt.Errorf("安装新版 App 失败，旧 App 已恢复: %w", err)
	}
	update.installed = true
	if err := validateMacOSDesktopRuntime(ctx, update.targetPath, update.targetVersion); err != nil {
		return fmt.Errorf("安装后的新版 App 验证失败: %w", err)
	}
	return nil
}

func (update *macOSDesktopUpdate) Restore(ctx context.Context) error {
	var restoreErrors []string
	if update.installed {
		pids, err := runningMacOSAppPIDs(ctx, update.targetPath)
		if err == nil {
			if terminateErr := terminatePIDs(ctx, pids); terminateErr != nil {
				restoreErrors = append(restoreErrors, terminateErr.Error())
			}
		}
		if err := os.RemoveAll(update.targetPath); err != nil {
			restoreErrors = append(restoreErrors, "删除新版 App 失败: "+err.Error())
		} else if err := os.Rename(update.backupPath, update.targetPath); err != nil {
			restoreErrors = append(restoreErrors, "恢复旧 App 失败: "+err.Error())
		} else {
			update.installed = false
		}
	} else if _, err := os.Stat(update.targetPath); os.IsNotExist(err) {
		if _, backupErr := os.Stat(update.backupPath); backupErr == nil {
			if renameErr := os.Rename(update.backupPath, update.targetPath); renameErr != nil {
				restoreErrors = append(restoreErrors, "恢复旧 App 失败: "+renameErr.Error())
			}
		}
	}
	if err := os.RemoveAll(update.newPath); err != nil {
		restoreErrors = append(restoreErrors, "清理 App 暂存副本失败: "+err.Error())
	}
	if err := removeMacOSDesktopUpdateHandoff(); err != nil {
		restoreErrors = append(restoreErrors, "清理更新接管确认失败: "+err.Error())
	}
	if len(restoreErrors) > 0 {
		return errors.New(strings.Join(restoreErrors, "；"))
	}
	return nil
}

func (update *macOSDesktopUpdate) Finish(ctx context.Context, outcome desktopUpdateOutcome) error {
	if !update.appWasRunning {
		return nil
	}
	// 每次 handoff 前先清掉旧确认文件。新版 App 会在成功解析 update-result 和
	// update-services 后原子写入新的确认，避免上一次异常退出留下的文件误提交事务。
	if err := removeMacOSDesktopUpdateHandoff(); err != nil {
		return fmt.Errorf("清理旧的 macOS 更新接管确认失败: %w", err)
	}
	if err := writeMacOSDesktopUpdateResult(outcome); err != nil {
		return err
	}
	// App Bundle 刚被原子替换后必须经 LaunchServices 启动。直接执行 Contents/MacOS/AgentDock
	// 虽然能拉起 GUI，但系统可能还没有登记新版 Bundle；此时 GUI 内重新注册的
	// SMAppService 会保留无法解析 BundleProgram 的 BTM 记录，Core/Tunnel 随后以 EX_CONFIG 退出。
	command := exec.CommandContext(ctx, "/usr/bin/open", "-g", update.targetPath, "--args", "--background")
	command.Env = os.Environ()
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("通过 LaunchServices 启动 macOS 控制面板失败: %w: %s", err, strings.TrimSpace(string(output)))
	}

	return waitForMacOSDesktopHandoff(ctx, update.targetPath, outcome.TargetVersion, outcome.OK, 60*time.Second)
}

func (update *macOSDesktopUpdate) Commit() error {
	var cleanupErrors []string
	for _, path := range []string{update.backupPath, update.newPath} {
		if err := os.RemoveAll(path); err != nil {
			cleanupErrors = append(cleanupErrors, err.Error())
		}
	}
	if err := removeMacOSDesktopUpdateHandoff(); err != nil {
		cleanupErrors = append(cleanupErrors, err.Error())
	}
	if len(cleanupErrors) > 0 {
		return errors.New(strings.Join(cleanupErrors, "；"))
	}
	return nil
}

func waitForMacOSDesktopHandoff(
	ctx context.Context,
	targetPath string,
	targetVersion string,
	requireHandoff bool,
	timeout time.Duration,
) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pids, findErr := runningMacOSAppPIDs(ctx, targetPath)
		if findErr == nil && len(pids) > 0 {
			if !requireHandoff {
				return nil
			}
			handoff, handoffErr := readMacOSDesktopUpdateHandoff()
			if handoffErr != nil {
				return fmt.Errorf("读取新版 macOS 控制面板接管确认失败: %w", handoffErr)
			}
			if handoff != nil {
				if normalizeVersion(handoff.TargetVersion) != normalizeVersion(targetVersion) {
					return fmt.Errorf(
						"新版 macOS 控制面板接管确认版本为 %s，目标版本为 %s",
						normalizeVersion(handoff.TargetVersion),
						normalizeVersion(targetVersion),
					)
				}
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	if requireHandoff {
		return errors.New("新版 macOS 控制面板未在超时前确认更新接管")
	}
	return errors.New("macOS 控制面板未在超时前重新启动")
}

func readMacOSDesktopUpdateHandoff() (*macOSDesktopUpdateHandoff, error) {
	path, err := macOSDesktopUpdateHandoffPath()
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("更新接管确认文件不安全: %s", path)
	}
	if info.Size() > 4096 {
		return nil, fmt.Errorf("更新接管确认文件过大: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var handoff macOSDesktopUpdateHandoff
	if err := json.Unmarshal(data, &handoff); err != nil {
		return nil, fmt.Errorf("解析更新接管确认失败: %w", err)
	}
	if handoff.SchemaVersion != 1 || strings.TrimSpace(handoff.TargetVersion) == "" {
		return nil, errors.New("更新接管确认内容无效")
	}
	return &handoff, nil
}

func removeMacOSDesktopUpdateHandoff() error {
	path, err := macOSDesktopUpdateHandoffPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func macOSDesktopUpdateHandoffPath() (string, error) {
	directory, err := macOSDesktopUpdateDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "update-handoff.json"), nil
}

type processOutputRedirect struct {
	stdout int
	stderr int
}

func redirectProcessOutputForDesktopUpdate() (*processOutputRedirect, error) {
	directory, err := macOSDesktopUpdateDirectory()
	if err != nil {
		return nil, err
	}
	logPath := filepath.Join(directory, "update.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("打开更新日志失败: %w", err)
	}
	defer logFile.Close()
	if err := os.Chmod(logPath, 0o600); err != nil {
		return nil, fmt.Errorf("设置更新日志权限失败: %w", err)
	}

	savedStdout, err := syscall.Dup(syscall.Stdout)
	if err != nil {
		return nil, fmt.Errorf("保存标准输出失败: %w", err)
	}
	savedStderr, err := syscall.Dup(syscall.Stderr)
	if err != nil {
		_ = syscall.Close(savedStdout)
		return nil, fmt.Errorf("保存标准错误失败: %w", err)
	}
	redirect := &processOutputRedirect{stdout: savedStdout, stderr: savedStderr}
	if err := syscall.Dup2(int(logFile.Fd()), syscall.Stdout); err != nil {
		redirect.Commit()
		return nil, fmt.Errorf("重定向标准输出失败: %w", err)
	}
	if err := syscall.Dup2(int(logFile.Fd()), syscall.Stderr); err != nil {
		_ = syscall.Dup2(savedStdout, syscall.Stdout)
		redirect.Commit()
		return nil, fmt.Errorf("重定向标准错误失败: %w", err)
	}
	return redirect, nil
}

func (redirect *processOutputRedirect) Restore() error {
	var restoreErrors []string
	if err := syscall.Dup2(redirect.stdout, syscall.Stdout); err != nil {
		restoreErrors = append(restoreErrors, "恢复标准输出失败: "+err.Error())
	}
	if err := syscall.Dup2(redirect.stderr, syscall.Stderr); err != nil {
		restoreErrors = append(restoreErrors, "恢复标准错误失败: "+err.Error())
	}
	redirect.Commit()
	if len(restoreErrors) > 0 {
		return errors.New(strings.Join(restoreErrors, "；"))
	}
	return nil
}

func (redirect *processOutputRedirect) Commit() {
	if redirect.stdout >= 0 {
		_ = syscall.Close(redirect.stdout)
		redirect.stdout = -1
	}
	if redirect.stderr >= 0 {
		_ = syscall.Close(redirect.stderr)
		redirect.stderr = -1
	}
}

func runningMacOSAppPIDs(ctx context.Context, appPath string) ([]int, error) {
	executable := filepath.Join(filepath.Clean(appPath), "Contents", "MacOS", "AgentDock")
	output, err := exec.CommandContext(ctx, "/bin/ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return nil, fmt.Errorf("读取 macOS 进程列表失败: %w", err)
	}
	var result []int
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		separator := strings.IndexByte(line, ' ')
		if separator <= 0 {
			continue
		}
		pid, parseErr := strconv.Atoi(strings.TrimSpace(line[:separator]))
		if parseErr != nil || pid <= 0 {
			continue
		}
		command := strings.TrimSpace(line[separator+1:])
		if command == executable || strings.HasPrefix(command, executable+" ") {
			result = append(result, pid)
		}
	}
	return result, nil
}

func terminatePIDs(ctx context.Context, pids []int) error {
	for _, pid := range pids {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		allExited := true
		for _, pid := range pids {
			if processExists(pid) {
				allExited = false
				break
			}
		}
		if allExited {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	for _, pid := range pids {
		if processExists(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	killDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(killDeadline) {
		allExited := true
		for _, pid := range pids {
			if processExists(pid) {
				allExited = false
				break
			}
		}
		if allExited {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, pid := range pids {
		if processExists(pid) {
			return fmt.Errorf("进程 %d 未退出", pid)
		}
	}
	return nil
}

func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func macOSDesktopUpdateDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("解析用户目录失败: %w", err)
	}
	directory := filepath.Join(home, "Library", "Application Support", "AgentDock")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("创建桌面更新状态目录失败: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return "", fmt.Errorf("设置桌面更新状态目录权限失败: %w", err)
	}
	return directory, nil
}

func macOSDesktopUpdateResultPath() (string, error) {
	directory, err := macOSDesktopUpdateDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "update-result.json"), nil
}

func writeMacOSDesktopUpdateResult(outcome desktopUpdateOutcome) error {
	directory, err := macOSDesktopUpdateDirectory()
	if err != nil {
		return err
	}
	payload := struct {
		SchemaVersion  int    `json:"schema_version"`
		OK             bool   `json:"ok"`
		CurrentVersion string `json:"current_version"`
		TargetVersion  string `json:"target_version"`
		Message        string `json:"message"`
	}{
		SchemaVersion:  1,
		OK:             outcome.OK,
		CurrentVersion: outcome.CurrentVersion,
		TargetVersion:  outcome.TargetVersion,
		Message:        outcome.Message,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("编码更新结果失败: %w", err)
	}
	target := filepath.Join(directory, "update-result.json")
	temporary := target + ".tmp." + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("写入更新结果失败: %w", err)
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("设置更新结果权限失败: %w", err)
	}
	if err := os.Rename(temporary, target); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("提交更新结果失败: %w", err)
	}
	return nil
}

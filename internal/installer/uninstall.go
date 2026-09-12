package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

func uninstallPlatform(ctx context.Context, request Request) error {
	switch runtime.GOOS {
	case "darwin":
		if err := uninstallDarwin(ctx, request); err != nil {
			return err
		}
	case "windows":
		if err := uninstallWindows(ctx, request); err != nil {
			return err
		}
	}
	manager := request.ServiceManager
	if manager == "auto" {
		manager = detectLinuxServiceManager(request)
	}
	if err := stopManagedServices(ctx, request, manager); err != nil {
		return err
	}
	return removeManagedUnits(request, manager)
}

func uninstallDarwin(ctx context.Context, request Request) error {
	agentsDir := request.LaunchAgentsDir
	if agentsDir == "" {
		// Linux 布局测试会在 Darwin 上跑 uninstall，不能去 bootout 当前用户真实 LaunchAgent。
		if request.SystemdDir != "" || request.OpenRCDir != "" {
			return nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		agentsDir = filepath.Join(home, "Library", "LaunchAgents")
	}
	domain := "gui/" + strconv.Itoa(currentUnixUID())
	var failures []error
	for _, label := range []string{darwinCLITunnelLabel, darwinCLICoreLabel} {
		spec := domain + "/" + label
		loaded, err := launchctlJobLoaded(ctx, spec)
		if err != nil {
			failures = append(failures, fmt.Errorf("无法查询 LaunchAgent %s: %w", label, err))
			continue
		}
		if loaded {
			if err := runCmd(ctx, "launchctl", "bootout", spec); err != nil {
				failures = append(failures, fmt.Errorf("无法停止 LaunchAgent %s: %w", label, err))
				continue
			}
			stillLoaded, err := launchctlJobLoaded(ctx, spec)
			if err != nil {
				failures = append(failures, fmt.Errorf("无法确认 LaunchAgent %s 已停止: %w", label, err))
				continue
			}
			if stillLoaded {
				failures = append(failures, fmt.Errorf("LaunchAgent 仍在运行，未删除服务文件：%s", label))
				continue
			}
		}
		if err := removeExisting(filepath.Join(agentsDir, label+".plist")); err != nil {
			failures = append(failures, fmt.Errorf("删除 LaunchAgent %s: %w", label, err))
		}
	}
	return errors.Join(failures...)
}

// launchctlJobLoaded 把“服务不存在”和权限/launchctl 故障分开。
// 任意非零都当不存在会在 print 失败后删掉 plist，把还在跑的 LaunchAgent 变成假成功。
func launchctlJobLoaded(ctx context.Context, spec string) (bool, error) {
	cmd := exec.CommandContext(ctx, "launchctl", "print", spec)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true, nil
	}
	if isIdempotentAbsence("launchctl", []string{"print", spec}, out, err) {
		return false, nil
	}
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		return false, fmt.Errorf("launchctl print %s: %w", spec, err)
	}
	return false, fmt.Errorf("launchctl print %s: %w: %s", spec, err, msg)
}

func uninstallWindows(ctx context.Context, request Request) error {
	taskName := strings.TrimSpace(request.TaskName)
	if taskName == "" {
		taskName = "AgentDock"
	}
	var failures []error
	if err := runOptionalCmd(ctx, "schtasks", "/End", "/TN", taskName); err != nil {
		failures = append(failures, fmt.Errorf("停止计划任务 %s: %w", taskName, err))
	}
	if err := runOptionalCmd(ctx, "schtasks", "/Change", "/TN", taskName, "/DISABLE"); err != nil {
		failures = append(failures, fmt.Errorf("禁用计划任务 %s: %w", taskName, err))
	}
	if binary := windowsServiceBinary(request); binary != "" {
		if err := runOptionalCmd(ctx, binary, "service", "stop", "--runtime-root", request.RuntimeRoot); err != nil {
			failures = append(failures, fmt.Errorf("停止 Windows Core: %w", err))
		}
		if err := runOptionalCmd(ctx, binary, "tunnel", "stop", "--runtime-root", request.RuntimeRoot); err != nil {
			failures = append(failures, fmt.Errorf("停止 Windows Tunnel: %w", err))
		}
	}
	return errors.Join(failures...)
}

func stopManagedServices(ctx context.Context, request Request, manager string) error {
	names := []string{request.ServiceName, request.ServiceName + "-cloudflared"}
	var failures []error
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			continue
		}
		switch manager {
		case "systemd":
			if err := runOptionalCmd(ctx, "systemctl", "disable", "--now", name); err != nil {
				failures = append(failures, fmt.Errorf("systemctl disable --now %s: %w", name, err))
			}
		case "openrc":
			if err := runOptionalCmd(ctx, "rc-service", name, "stop"); err != nil {
				failures = append(failures, fmt.Errorf("rc-service %s stop: %w", name, err))
			}
			if err := runOptionalCmd(ctx, "rc-update", "del", name, "default"); err != nil {
				failures = append(failures, fmt.Errorf("rc-update del %s: %w", name, err))
			}
		}
	}
	return errors.Join(failures...)
}

func removeManagedUnits(request Request, manager string) error {
	var paths []string
	switch manager {
	case "systemd":
		systemdDir := request.SystemdDir
		if systemdDir == "" {
			systemdDir = "/etc/systemd/system"
		}
		paths = []string{
			filepath.Join(systemdDir, request.ServiceName+".service"),
			filepath.Join(systemdDir, request.ServiceName+"-cloudflared.service"),
		}
	case "openrc":
		openRCDir := request.OpenRCDir
		if openRCDir == "" {
			openRCDir = "/etc/init.d"
		}
		paths = []string{
			filepath.Join(openRCDir, request.ServiceName),
			filepath.Join(openRCDir, request.ServiceName+"-cloudflared"),
		}
	default:
		return nil
	}
	var failures []error
	for _, path := range paths {
		if err := removeExisting(path); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func purgeInstallConfig(request Request) error {
	names := []string{"agentdock.env", "cloudflared.env", "desktop-runtime.json", "runtime.json", "active-version.json"}
	var failures []error
	for _, name := range names {
		if err := removeExisting(filepath.Join(request.RuntimeRoot, name)); err != nil {
			failures = append(failures, fmt.Errorf("purge %s: %w", name, err))
		}
	}
	return errors.Join(failures...)
}

func purgeInstallData(request Request) error {
	var failures []error
	if request.InstallRoot != request.RuntimeRoot {
		if err := removeAllExisting(request.InstallRoot); err != nil {
			failures = append(failures, fmt.Errorf("purge install-root: %w", err))
		}
	}
	if err := removeAllExisting(request.RuntimeRoot); err != nil {
		failures = append(failures, fmt.Errorf("purge runtime-root: %w", err))
	}
	return errors.Join(failures...)
}

func removeExisting(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func removeAllExisting(path string) error {
	if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func runOptionalCmd(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	if isIdempotentAbsence(name, args, out, err) {
		return nil
	}
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, msg)
}

// isIdempotentAbsence 只承认“目标本来就不存在 / 本来就没在跑”这类可分类状态。
// 权限失败、exit 23、命令不存在都不能当成成功。
func isIdempotentAbsence(name string, args []string, output []byte, err error) bool {
	if err == nil {
		return true
	}
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return false
	}
	text := strings.ToLower(string(output))
	base := strings.ToLower(filepath.Base(name))
	switch {
	case strings.Contains(base, "systemctl"):
		return systemdUnitAbsent(text)
	case strings.Contains(base, "rc-service"), strings.Contains(base, "rc-update"):
		return openRCServiceAbsent(text)
	case strings.Contains(base, "launchctl"):
		return launchctlServiceAbsent(text)
	case strings.Contains(base, "schtasks"):
		return schtasksAbsent(args, text)
	default:
		return windowsProcessAlreadyStopped(text)
	}
}

func systemdUnitAbsent(text string) bool {
	if strings.Contains(text, "dependency") {
		return false
	}
	return strings.Contains(text, "does not exist") ||
		strings.Contains(text, "not loaded") ||
		strings.Contains(text, "could not be found") ||
		strings.Contains(text, "not found")
}

func openRCServiceAbsent(text string) bool {
	return strings.Contains(text, "does not exist") ||
		strings.Contains(text, "no such service") ||
		strings.Contains(text, "service not found") ||
		strings.Contains(text, "not found")
}

func launchctlServiceAbsent(text string) bool {
	return strings.Contains(text, "could not find specified service") ||
		strings.Contains(text, "no such process") ||
		strings.Contains(text, "service not found") ||
		strings.Contains(text, "could not find service")
}

func schtasksAbsent(args []string, text string) bool {
	if strings.Contains(text, "the specified task name was not found") ||
		strings.Contains(text, "the system cannot find the file specified") ||
		strings.Contains(text, "cannot find the file specified") {
		return true
	}
	endTask := false
	for _, arg := range args {
		if strings.EqualFold(arg, "/End") {
			endTask = true
			break
		}
	}
	if !endTask {
		return false
	}
	return strings.Contains(text, "not running") ||
		strings.Contains(text, "is not currently running") ||
		strings.Contains(text, "the task has not yet been run")
}

func windowsProcessAlreadyStopped(text string) bool {
	return strings.Contains(text, "not running") ||
		strings.Contains(text, "no matching processes") ||
		strings.Contains(text, "is not started")
}

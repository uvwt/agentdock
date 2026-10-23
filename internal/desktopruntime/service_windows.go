//go:build windows

package desktopruntime

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	processcontrol "github.com/uvwt/agentdock/internal/process"
	"github.com/uvwt/agentdock/internal/updateengine"
)

func platformServiceStatus(ctx context.Context, runtimeRoot string) (ServiceStatus, error) {
	manifest, _, err := loadDesktopManifest(runtimeRoot)
	if err != nil {
		return ServiceStatus{}, err
	}
	running, err := coreProcessRunning(runtimeRoot, manifest)
	if err != nil {
		return ServiceStatus{}, err
	}
	// Core 已明确不存在时不要再发起健康检查。桌面端会频繁读取该状态，
	// 停止态如果继续等待 HTTP 超时，会让主页看起来一直在加载。
	healthy := running && testHealth(ctx, manifest.HealthURL())
	startupEnabled, err := coreAutostartEnabled(ctx, manifest)
	if err != nil {
		return ServiceStatus{}, fmt.Errorf("读取 AgentDock 开机启动状态失败: %w", err)
	}
	return ServiceStatus{Running: running, Healthy: healthy, StartupEnabled: startupEnabled}, nil
}

func coreProcessRunning(runtimeRoot string, manifest Manifest) (bool, error) {
	coreBinaries, err := coreBinaryPaths(runtimeRoot, manifest)
	if err != nil {
		return false, err
	}
	supervisorPID, err := activeTunnelSupervisorPIDForRuntime(runtimeRoot, manifest)
	if err != nil {
		return false, fmt.Errorf("识别 Tunnel supervisor 失败: %w", err)
	}
	excluded := map[uint32]struct{}{}
	if supervisorPID != 0 {
		// Windows Core 与 Tunnel supervisor 共用同一个 generation 二进制。
		// Core 状态必须排除已确认的 supervisor，否则停止 Core 后仍会被误判为运行中。
		excluded[supervisorPID] = struct{}{}
	}
	processIDs, err := processIDsAtPathsExcept(coreBinaries, excluded)
	if err != nil {
		return false, err
	}
	return len(processIDs) > 0, nil
}

// coreBinaryPaths 限定同一安装根内的 Core 路径，包含未随 active pointer 退出的旧 generation。
// 只枚举固定层级与文件名，不按进程名扫描其他安装目录。
func coreBinaryPaths(runtimeRoot string, manifest Manifest) ([]string, error) {
	paths := []string{ActiveCoreBinary(runtimeRoot, manifest)}
	add := func(path string) {
		for _, existing := range paths {
			if samePath(existing, path) {
				return
			}
		}
		paths = append(paths, path)
	}
	// ActiveCoreBinary 已在旧布局中回退清单入口；generation 布局不能把稳定 shim
	// 也当作 Core，否则一次普通 CLI 调用就会被误报为服务运行，或被停止操作误杀。
	layout, err := updateengine.NewWindowsLayout(runtimeRoot)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(layout.VersionsDir())
	if os.IsNotExist(err) {
		return paths, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取 Core generation 目录失败: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(layout.VersionsDir(), entry.Name(), updateengine.GenerationCoreName)
		if regularFileExists(path) {
			add(path)
		}
	}
	return paths, nil
}

func platformServiceAction(ctx context.Context, runtimeRoot, action string) error {
	manifest, root, err := loadDesktopManifest(runtimeRoot)
	if err != nil {
		return err
	}

	switch action {
	case "start":
		return startCore(ctx, manifest, root)
	case "stop":
		return stopCore(ctx, manifest, root)
	case "restart":
		if err := stopCore(ctx, manifest, root); err != nil {
			return err
		}
		return startCore(ctx, manifest, root)
	default:
		return fmt.Errorf("不支持的 Windows 服务操作：%s", action)
	}
}

func loadDesktopManifest(runtimeRoot string) (Manifest, string, error) {
	root, err := filepath.Abs(strings.TrimSpace(runtimeRoot))
	if err != nil {
		return Manifest{}, "", fmt.Errorf("解析 Windows 运行目录失败: %w", err)
	}
	manifest, err := Load(filepath.Join(root, "runtime.json"))
	if err != nil {
		return Manifest{}, "", fmt.Errorf("读取 Windows 运行清单失败: %w", err)
	}
	return manifest, root, nil
}

func startCore(ctx context.Context, manifest Manifest, runtimeRoot string) error {
	if testHealth(ctx, manifest.HealthURL()) {
		return nil
	}
	// 不健康进程仍可能持有端口；先完成同一安装的旧 Core 清理，再创建新进程。
	if err := stopCore(ctx, manifest, runtimeRoot); err != nil {
		return err
	}
	if manifest.UsesScheduledTask() {
		if err := StartInteractiveScheduledTask(ctx, runtimeRoot, manifest.AgentDockTaskName); err != nil {
			return err
		}
	} else if err := startDetachedCore(manifest, runtimeRoot); err != nil {
		return err
	}
	return waitForHealth(ctx, manifest.HealthURL(), WindowsCoreStartTimeout)
}

func stopCore(ctx context.Context, manifest Manifest, runtimeRoot string) error {
	coreBinaries, err := coreBinaryPaths(runtimeRoot, manifest)
	if err != nil {
		return err
	}
	coreBinary := coreBinaries[0]
	excluded := map[uint32]struct{}{}
	ancestorPIDs, err := ancestorProcessIDsAtPath(coreBinary, coreBinaries[1:]...)
	if err != nil {
		return fmt.Errorf("识别 AgentDock Core 调用链失败: %w", err)
	}
	for processID := range ancestorPIDs {
		excluded[processID] = struct{}{}
	}
	supervisorPID, err := activeTunnelSupervisorPIDForRuntime(runtimeRoot, manifest)
	if err != nil {
		return fmt.Errorf("识别 Tunnel supervisor 失败: %w", err)
	}
	if supervisorPID != 0 {
		// Core 与 Tunnel supervisor 可共用同一 generation Core 二进制。停止 Core 时必须保留 supervisor，
		// 否则一次普通 Core 重启就会悄悄丢失 Tunnel 的后续自恢复能力。
		excluded[supervisorPID] = struct{}{}
	}

	if manifest.UsesScheduledTask() {
		// 先让任务计划程序正常结束最高权限进程，避免普通托盘立即申请 PROCESS_TERMINATE。
		_ = runScheduledTaskCommand(ctx, "/End", "/TN", scheduledTaskPath(manifest.AgentDockTaskName))
		stopped, waitErr := waitBinaryStoppedExcept(ctx, coreBinary, excluded, 5*time.Second, coreBinaries[1:]...)
		if waitErr != nil {
			return waitErr
		}
		if stopped {
			return nil
		}
	}
	if err := stopBinaryProcessesExcept(ctx, coreBinary, excluded, 15*time.Second, coreBinaries[1:]...); err != nil {
		return fmt.Errorf("停止 AgentDock 核心失败: %w", err)
	}
	return nil
}

func startDetachedCore(manifest Manifest, runtimeRoot string) error {
	coreBinary := ActiveCoreBinary(runtimeRoot, manifest)
	if info, err := os.Stat(coreBinary); err != nil || info.IsDir() {
		return fmt.Errorf("找不到 AgentDock 核心程序: %s", coreBinary)
	}
	command := exec.Command(coreBinary, "service", "launch-core", "--runtime-root", runtimeRoot)
	// launch-core 会自行把运行日志写入受限轮转文件；父进程不再持有同一路径的追加句柄。
	command.Dir = defaultWindowsWorkDir()
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
	processcontrol.Configure(command)
	if err := command.Start(); err != nil {
		return fmt.Errorf("启动 AgentDock 核心失败: %w", err)
	}
	if err := command.Process.Release(); err != nil {
		return fmt.Errorf("释放 AgentDock 后台进程句柄失败: %w", err)
	}
	return nil
}

func defaultWindowsWorkDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	workDir := filepath.Join(home, "AgentDock")
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return home
	}
	return workDir
}

func waitForHealth(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if testHealth(ctx, url) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("AgentDock 健康检查失败: %s", url)
}

func testHealth(ctx context.Context, url string) bool {
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode >= 200 && response.StatusCode < 300
}

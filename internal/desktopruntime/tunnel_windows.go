//go:build windows

package desktopruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

func platformLaunchTunnel(ctx context.Context, runtimeRoot string) error {
	// Win32 mutex 的 owner 是线程而不是进程。supervisor 持有 mutex 的整个生命周期固定在
	// 同一个 OS thread，确保最终 ReleaseMutex 一定由 owner thread 执行。
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()

	runtime, err := loadTunnelRuntime(runtimeRoot)
	if err != nil {
		return err
	}
	if runtime.mode == "none" {
		return errors.New("Tunnel 模式为 none")
	}

	guard, err := acquireTunnelSupervisor(runtime.root)
	if err != nil {
		return err
	}
	if guard == nil {
		// 已有同一 runtime root 的 supervisor；重复 launch 静默退出，由现有实例继续持有 Tunnel。
		return nil
	}
	defer guard.Close()

	logs, err := openProcessLogs(runtime.files.stdoutLog, runtime.files.stderrLog)
	if err != nil {
		return err
	}
	defer logs.Close()

	var retryDelay time.Duration
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		stopped, err := guard.stopRequested()
		if err != nil {
			return err
		}
		if stopped {
			return nil
		}

		// 每轮重读 mode/token 等运行状态，避免 supervisor 长驻后继续使用过期配置。
		runtime, err = loadTunnelRuntime(runtime.root)
		if err != nil {
			return err
		}
		if runtime.mode == "none" {
			return nil
		}

		startedAt := time.Now()
		runErr := runCloudflaredOnce(ctx, runtime, logs)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		stopped, err = guard.stopRequested()
		if err != nil {
			return err
		}
		if stopped {
			return nil
		}
		if runErr != nil {
			fmt.Fprintf(logs.stderr, "cloudflared 异常退出: %v\n", runErr)
		} else {
			fmt.Fprintln(logs.stderr, "cloudflared 意外退出，准备自动恢复")
		}

		if runtime.mode == "quick" {
			if err := invalidateQuickTunnelAfterExit(ctx, runtime); err != nil {
				fmt.Fprintf(logs.stderr, "清理失效 Quick Tunnel 状态失败: %v\n", err)
			}
		}

		retryDelay = nextTunnelRetryDelay(retryDelay, time.Since(startedAt))
		fmt.Fprintf(logs.stderr, "将在 %s 后重启 cloudflared\n", retryDelay)
		stopped, err = guard.waitRetry(ctx, retryDelay)
		if err != nil {
			return err
		}
		if stopped {
			return nil
		}
	}
}

func runCloudflaredOnce(ctx context.Context, runtime tunnelRuntime, logs *processLogs) error {
	logCursors := quickTunnelLogCursors{}
	var err error
	if runtime.mode == "quick" {
		logCursors, err = captureQuickTunnelLogCursors(runtime.files)
		if err != nil {
			return err
		}
	}

	command, err := cloudflaredCommand(ctx, runtime)
	if err != nil {
		return err
	}
	command.Stdout = logs.stdout
	command.Stderr = logs.stderr
	if err := command.Start(); err != nil {
		return err
	}

	if runtime.mode == "quick" {
		publicURL, readyErr := waitQuickTunnelURL(ctx, runtime, logCursors, 35*time.Second)
		if readyErr != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
			return readyErr
		}
		if err := applyQuickTunnelURL(ctx, runtime, publicURL); err != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
			return err
		}
	}
	return command.Wait()
}

func platformTunnelStatus(ctx context.Context, runtimeRoot string) (TunnelStatus, error) {
	runtime, err := loadTunnelRuntime(runtimeRoot)
	if err != nil {
		return TunnelStatus{}, err
	}
	running, err := processRunningAtPath(runtime.manifest.CloudflaredBinary)
	if err != nil {
		return TunnelStatus{}, err
	}
	startupEnabled, err := tunnelAutostartEnabled(runtime.manifest)
	if err != nil {
		return TunnelStatus{}, fmt.Errorf("读取 Tunnel 开机启动状态失败: %w", err)
	}
	publicURL, err := readTunnelPublicURL(runtime)
	if err != nil {
		return TunnelStatus{}, err
	}
	ready := runtime.mode == "none"
	if runtime.mode == "quick" {
		ready = running && publicURL != ""
	}
	if runtime.mode == "named" {
		ready = running && publicURL != ""
	}
	return TunnelStatus{
		Mode:           runtime.mode,
		Running:        running,
		Ready:          ready,
		StartupEnabled: startupEnabled,
		PublicURL:      publicURL,
	}, nil
}

func platformTunnelAction(ctx context.Context, runtimeRoot, action string) error {
	runtime, err := loadTunnelRuntime(runtimeRoot)
	if err != nil {
		return err
	}
	switch action {
	case "start":
		return startTunnel(ctx, runtime)
	case "stop":
		return stopTunnel(ctx, runtime)
	case "restart":
		if runtime.mode == "quick" {
			return regenerateQuickTunnel(ctx, runtime)
		}
		if err := stopTunnel(ctx, runtime); err != nil {
			return err
		}
		return startTunnel(ctx, runtime)
	case "regenerate":
		if runtime.mode != "quick" {
			return errors.New("只有临时地址模式可以重新生成 Quick Tunnel")
		}
		return regenerateQuickTunnel(ctx, runtime)
	default:
		return fmt.Errorf("不支持的 Tunnel 操作：%s", action)
	}
}

type quickTunnelLogCursors struct {
	stdout quickTunnelLogCursor
	stderr quickTunnelLogCursor
}

func captureQuickTunnelLogCursors(files tunnelFiles) (quickTunnelLogCursors, error) {
	stdout, err := captureQuickTunnelLogCursor(files.stdoutLog)
	if err != nil {
		return quickTunnelLogCursors{}, fmt.Errorf("记录 cloudflared stdout 日志位置失败: %w", err)
	}
	stderr, err := captureQuickTunnelLogCursor(files.stderrLog)
	if err != nil {
		return quickTunnelLogCursors{}, fmt.Errorf("记录 cloudflared stderr 日志位置失败: %w", err)
	}
	return quickTunnelLogCursors{stdout: stdout, stderr: stderr}, nil
}

func startTunnel(ctx context.Context, runtime tunnelRuntime) error {
	if runtime.mode == "none" {
		return nil
	}
	if info, err := os.Stat(runtime.manifest.CloudflaredBinary); err != nil || info.IsDir() {
		return fmt.Errorf("找不到 cloudflared.exe，请运行 Setup.exe 修复安装: %s", runtime.manifest.CloudflaredBinary)
	}

	running, err := processRunningAtPath(runtime.manifest.CloudflaredBinary)
	if err != nil {
		return err
	}
	supervisorPID, err := activeTunnelSupervisorPID(runtime.root, runtime.manifest.AgentDockBinary)
	if err != nil {
		return err
	}
	if running && supervisorPID != 0 {
		if runtime.mode == "quick" {
			return waitQuickTunnelReady(ctx, runtime, 45*time.Second)
		}
		return nil
	}

	if running {
		// 升级或旧版本可能留下没有 supervisor 的孤立 cloudflared；重新纳入统一生命周期。
		if err := StopBinaryProcesses(ctx, runtime.manifest.CloudflaredBinary, 15*time.Second); err != nil {
			return fmt.Errorf("停止未受管 cloudflared 失败: %w", err)
		}
	}
	if supervisorPID != 0 {
		// supervisor 可能正处于退避期。显式 start 应立即重试，而不是继续等待旧退避计时。
		if err := signalTunnelSupervisorStop(runtime.root); err != nil {
			return err
		}
		if err := waitTunnelSupervisorStopped(ctx, runtime.root, 10*time.Second); err != nil {
			return err
		}
	}

	if runtime.mode == "quick" {
		// 旧临时地址在新进程真正拿到 URL 前不能继续暴露为 ready。
		if err := clearActivePublicURL(runtime.files); err != nil {
			return err
		}
		if err := runtime.updateManifest("none", ""); err != nil {
			return err
		}
	}
	if err := launchCloudflared(runtime); err != nil {
		return err
	}
	if err := waitCloudflaredRunning(ctx, runtime.manifest.CloudflaredBinary, 20*time.Second); err != nil {
		return err
	}
	if runtime.mode == "quick" {
		return waitQuickTunnelReady(ctx, runtime, 45*time.Second)
	}
	return nil
}

func stopTunnel(ctx context.Context, runtime tunnelRuntime) error {
	if err := signalTunnelSupervisorStop(runtime.root); err != nil {
		return err
	}
	if err := StopBinaryProcesses(ctx, runtime.manifest.CloudflaredBinary, 15*time.Second); err != nil {
		return fmt.Errorf("停止 cloudflared 失败: %w", err)
	}
	if err := waitTunnelSupervisorStopped(ctx, runtime.root, 15*time.Second); err != nil {
		return err
	}
	return nil
}

func regenerateQuickTunnel(ctx context.Context, runtime tunnelRuntime) error {
	if err := stopTunnel(ctx, runtime); err != nil {
		return err
	}
	if err := clearActivePublicURL(runtime.files); err != nil {
		return err
	}
	if err := runtime.updateManifest("none", ""); err != nil {
		return err
	}
	// 清掉旧公网地址后先重启核心，避免新地址准备期间继续使用失效的 OAuth Origin。
	if err := platformServiceAction(ctx, runtime.root, "restart"); err != nil {
		return err
	}
	return startTunnel(ctx, runtime)
}

func launchCloudflared(runtime tunnelRuntime) error {
	// Windows 不能把轮转 writer 直接交给脱离父进程的 cloudflared；因此先启动一个
	// 长驻的 AgentDock tunnel launch 监督进程，由它持有 cloudflared 并实时轮转日志。
	command := exec.Command(runtime.manifest.AgentDockBinary, "tunnel", "launch", "--runtime-root", runtime.root)
	command.Dir = runtime.root
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("启动 cloudflared 监督进程失败: %w", err)
	}
	if err := command.Process.Release(); err != nil {
		return fmt.Errorf("释放 cloudflared 监督进程句柄失败: %w", err)
	}
	return nil
}

func cloudflaredCommand(ctx context.Context, runtime tunnelRuntime) (*exec.Cmd, error) {
	arguments := []string{"tunnel", "--no-autoupdate"}
	environment := environmentWithout(os.Environ(), "TUNNEL_TOKEN")
	if runtime.mode == "quick" {
		arguments = append(arguments, "--url", fmt.Sprintf("http://127.0.0.1:%d", runtime.settings.Port))
	} else {
		token, err := readProtectedText(runtime.files.token, tunnelTokenEntropy)
		if err != nil {
			return nil, fmt.Errorf("读取 Cloudflare Tunnel Token 失败: %w", err)
		}
		if strings.TrimSpace(token) == "" {
			return nil, errors.New("固定域名模式没有保存 Cloudflare Tunnel Token")
		}
		environment = append(environment, "TUNNEL_TOKEN="+token)
		arguments = append(arguments, "run")
	}
	command := exec.CommandContext(ctx, runtime.manifest.CloudflaredBinary, arguments...)
	command.Env = environment
	command.Dir = runtime.root
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return command, nil
}

func applyQuickTunnelURL(ctx context.Context, runtime tunnelRuntime, publicURL string) error {
	if err := writeRuntimeText(runtime.files.serverURL, publicURL); err != nil {
		return err
	}
	if err := platformServiceAction(ctx, runtime.root, "restart"); err != nil {
		return err
	}
	if err := runtime.updateManifest("quick", publicURL); err != nil {
		return err
	}
	// ready 文件最后写入，保证桌面端读到地址时核心已经采用新 OAuth Origin。
	return writeRuntimeText(runtime.files.quickURL, publicURL)
}

func invalidateQuickTunnelAfterExit(ctx context.Context, runtime tunnelRuntime) error {
	readyURL, err := readTrimmedText(runtime.files.quickURL)
	if err != nil {
		return err
	}
	if readyURL == "" {
		return nil
	}
	if err := clearActivePublicURL(runtime.files); err != nil {
		return err
	}
	if err := runtime.updateManifest("none", ""); err != nil {
		return err
	}
	// 已对外发布过的 Quick URL 一旦失效，先让 Core 丢弃旧 OAuth Origin，再等待新 URL。
	return platformServiceAction(ctx, runtime.root, "restart")
}

func waitQuickTunnelReady(ctx context.Context, runtime tunnelRuntime, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		readyURL, err := readTrimmedText(runtime.files.quickURL)
		if err != nil {
			return err
		}
		if readyURL != "" {
			running, err := processRunningAtPath(runtime.manifest.CloudflaredBinary)
			if err != nil {
				return err
			}
			if running {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("Quick Tunnel 未在 %s 内进入 ready: %s", timeout, tunnelLogSummary(runtime.files))
}

func waitQuickTunnelURL(ctx context.Context, runtime tunnelRuntime, cursors quickTunnelLogCursors, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	logs := []struct {
		path   string
		cursor quickTunnelLogCursor
	}{
		{path: runtime.files.stdoutLog, cursor: cursors.stdout},
		{path: runtime.files.stderrLog, cursor: cursors.stderr},
	}
	for time.Now().Before(deadline) {
		for _, log := range logs {
			data, err := readQuickTunnelLogSince(log.path, log.cursor)
			if err == nil {
				if publicURL := findQuickTunnelURL(data); publicURL != "" {
					return publicURL, nil
				}
			}
		}
		running, err := processRunningAtPath(runtime.manifest.CloudflaredBinary)
		if err != nil {
			return "", err
		}
		if !running {
			return "", fmt.Errorf("cloudflared 在生成临时地址前退出: %s", tunnelLogSummary(runtime.files))
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return "", fmt.Errorf("cloudflared 未在 %s 内生成 trycloudflare.com 临时地址: %s", timeout, tunnelLogSummary(runtime.files))
}

func waitCloudflaredRunning(ctx context.Context, binaryPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		running, err := processRunningAtPath(binaryPath)
		if err != nil {
			return err
		}
		if running {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("cloudflared 未保持运行: %s", binaryPath)
}

func readTunnelPublicURL(runtime tunnelRuntime) (string, error) {
	if runtime.mode == "none" {
		return "", nil
	}
	if runtime.mode == "quick" {
		return readTrimmedText(runtime.files.quickURL)
	}
	return readTrimmedText(runtime.files.serverURL)
}

func environmentWithout(environment []string, name string) []string {
	prefix := strings.ToUpper(name) + "="
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		if strings.HasPrefix(strings.ToUpper(entry), prefix) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func tunnelLogSummary(files tunnelFiles) string {
	for _, path := range []string{files.stderrLog, files.stdoutLog} {
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			continue
		}
		if len(data) > 2048 {
			data = data[len(data)-2048:]
		}
		return strings.TrimSpace(string(data))
	}
	return "日志为空"
}

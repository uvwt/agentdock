//go:build windows

package desktopruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

func platformLaunchTunnel(ctx context.Context, runtimeRoot string) error {
	runtime, err := loadTunnelRuntime(runtimeRoot)
	if err != nil {
		return err
	}
	if runtime.mode == "none" {
		return errors.New("Tunnel 模式为 none")
	}
	logs, err := openProcessLogs(runtime.files.stdoutLog, runtime.files.stderrLog)
	if err != nil {
		return err
	}
	defer logs.Close()

	command, err := cloudflaredCommand(ctx, runtime)
	if err != nil {
		return err
	}
	command.Stdout = logs.stdout
	command.Stderr = logs.stderr
	if err := command.Run(); err != nil {
		fmt.Fprintf(logs.stderr, "cloudflared 退出: %v\n", err)
		return err
	}
	return nil
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
	if running {
		if runtime.mode == "quick" {
			readyURL, readyErr := readTrimmedText(runtime.files.quickURL)
			if readyErr != nil {
				return readyErr
			}
			if readyURL == "" {
				// 已有进程可能早于当前控制命令启动，此时需要从完整日志恢复 ready URL。
				return finalizeQuickTunnel(ctx, runtime, quickTunnelLogCursors{})
			}
		}
		return nil
	}

	logCursors := quickTunnelLogCursors{}
	if runtime.mode == "quick" {
		// 旧临时地址在新进程真正拿到 URL 前不能继续暴露为 ready。
		if err := clearActivePublicURL(runtime.files); err != nil {
			return err
		}
		if err := runtime.updateManifest("none", ""); err != nil {
			return err
		}
		// cloudflared 日志按设计持续追加；记录本轮启动前的位置，避免 regenerate 把历史 URL 当成新地址。
		var err error
		logCursors, err = captureQuickTunnelLogCursors(runtime.files)
		if err != nil {
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
		return finalizeQuickTunnel(ctx, runtime, logCursors)
	}
	return nil
}

func stopTunnel(ctx context.Context, runtime tunnelRuntime) error {
	if err := StopBinaryProcesses(ctx, runtime.manifest.CloudflaredBinary, 15*time.Second); err != nil {
		return fmt.Errorf("停止 cloudflared 失败: %w", err)
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

func finalizeQuickTunnel(ctx context.Context, runtime tunnelRuntime, cursors quickTunnelLogCursors) error {
	publicURL, err := waitQuickTunnelURL(ctx, runtime, cursors, 35*time.Second)
	if err != nil {
		_ = StopBinaryProcesses(context.Background(), runtime.manifest.CloudflaredBinary, 5*time.Second)
		return err
	}
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

package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/desktopruntime"
	"github.com/uvwt/agentdock/internal/envstore"
)

func startPlatformServices(ctx context.Context, request Request, journal *rollbackJournal) error {
	switch runtimeGOOS() {
	case "linux":
		return startLinuxServices(ctx, request, journal)
	case "darwin":
		return startDarwinServices(ctx, request, journal)
	case "windows":
		return startWindowsServices(ctx, request, journal)
	default:
		return nil
	}
}

func startTunnelServices(ctx context.Context, request Request, journal *rollbackJournal) error {
	if request.TunnelMode != "quick" && request.TunnelMode != "named" {
		return nil
	}
	switch runtimeGOOS() {
	case "linux":
		return startLinuxTunnel(ctx, request, journal)
	case "darwin":
		return startDarwinTunnel(ctx, request, journal)
	case "windows":
		return startWindowsTunnel(ctx, request, journal)
	default:
		return nil
	}
}

func shouldWaitForHealth(request Request) bool {
	if !request.StartService || request.SkipHealth {
		return false
	}
	switch runtimeGOOS() {
	case "linux":
		manager := request.ServiceManager
		if manager == "auto" {
			manager = detectLinuxServiceManager(request)
		}
		return manager == "systemd" || manager == "openrc"
	case "darwin":
		return request.RegisterService
	case "windows":
		return true
	default:
		return false
	}
}

func startLinuxServices(ctx context.Context, request Request, journal *rollbackJournal) error {
	manager := request.ServiceManager
	if manager == "auto" {
		manager = detectLinuxServiceManager(request)
	}
	if manager != "systemd" && manager != "openrc" {
		return nil
	}
	name := request.ServiceName
	active, enabled := probeLinuxService(ctx, manager, name)
	if err := journal.NoteService(journalService{
		Manager:    manager,
		Name:       name,
		WasActive:  active,
		WasEnabled: enabled,
	}); err != nil {
		return err
	}
	if manager == "systemd" {
		if err := runCmd(ctx, "systemctl", "daemon-reload"); err != nil {
			return err
		}
		if active {
			if err := runCmd(ctx, "systemctl", "restart", name); err != nil {
				return err
			}
			if err := runCmd(ctx, "systemctl", "enable", name); err != nil {
				return err
			}
		} else {
			if err := runCmd(ctx, "systemctl", "enable", "--now", name); err != nil {
				return err
			}
		}
	} else {
		if err := runCmd(ctx, "rc-update", "add", name, "default"); err != nil {
			return err
		}
		if active {
			if err := runCmd(ctx, "rc-service", name, "restart"); err != nil {
				return err
			}
		} else if err := runCmd(ctx, "rc-service", name, "start"); err != nil {
			return err
		}
	}
	return journal.updateService(name, func(service *journalService) {
		service.StartedByUs = true
		if active {
			service.StoppedByUs = true
		}
	})
}

func startLinuxTunnel(ctx context.Context, request Request, journal *rollbackJournal) error {
	manager := request.ServiceManager
	if manager == "auto" {
		manager = detectLinuxServiceManager(request)
	}
	if manager != "systemd" && manager != "openrc" {
		return nil
	}
	name := request.ServiceName + "-cloudflared"
	active, enabled := probeLinuxService(ctx, manager, name)
	if err := journal.NoteService(journalService{
		Manager:    manager,
		Name:       name,
		WasActive:  active,
		WasEnabled: enabled,
	}); err != nil {
		return err
	}
	if manager == "systemd" {
		if err := runCmd(ctx, "systemctl", "daemon-reload"); err != nil {
			return err
		}
		if active {
			if err := runCmd(ctx, "systemctl", "restart", name); err != nil {
				return fmt.Errorf("systemctl restart %s: %w", name, err)
			}
			if err := runCmd(ctx, "systemctl", "enable", name); err != nil {
				return fmt.Errorf("systemctl enable %s: %w", name, err)
			}
		} else if err := runCmd(ctx, "systemctl", "enable", "--now", name); err != nil {
			return fmt.Errorf("systemctl enable --now %s: %w", name, err)
		}
	} else {
		if err := runCmd(ctx, "rc-update", "add", name, "default"); err != nil {
			return fmt.Errorf("rc-update add %s: %w", name, err)
		}
		if active {
			if err := runCmd(ctx, "rc-service", name, "restart"); err != nil {
				return fmt.Errorf("rc-service %s restart: %w", name, err)
			}
		} else if err := runCmd(ctx, "rc-service", name, "start"); err != nil {
			return fmt.Errorf("rc-service %s start: %w", name, err)
		}
	}
	return journal.updateService(name, func(service *journalService) {
		service.StartedByUs = true
		if active {
			service.StoppedByUs = true
		}
	})
}

func probeLinuxService(ctx context.Context, manager, name string) (active, enabled bool) {
	switch manager {
	case "systemd":
		active = cmdOK(ctx, "systemctl", "is-active", "--quiet", name)
		enabled = cmdOK(ctx, "systemctl", "is-enabled", "--quiet", name)
	case "openrc":
		active = cmdOK(ctx, "rc-service", name, "status")
		enabled = cmdOK(ctx, "rc-update", "show", "default") &&
			bytes.Contains(cmdOutput(ctx, "rc-update", "show", "default"), []byte(name))
	}
	return active, enabled
}

func startDarwinServices(ctx context.Context, request Request, journal *rollbackJournal) error {
	if !request.RegisterService {
		return nil
	}
	domain := "gui/" + strconv.Itoa(currentUnixUID())
	label := darwinCLICoreLabel
	plist := filepath.Join(launchAgentsDir(request), label+".plist")
	loaded := cmdOK(ctx, "launchctl", "print", domain+"/"+label)
	if err := journal.NoteService(journalService{
		Manager:   "launchd",
		Name:      label,
		Domain:    domain,
		Plist:     plist,
		WasActive: loaded,
	}); err != nil {
		return err
	}
	if loaded {
		if err := runCmd(ctx, "launchctl", "bootout", domain+"/"+label); err != nil {
			return err
		}
		if err := journal.updateService(label, func(service *journalService) {
			service.StoppedByUs = true
		}); err != nil {
			return err
		}
	}
	if err := runCmd(ctx, "launchctl", "bootstrap", domain, plist); err != nil {
		return err
	}
	if err := journal.updateService(label, func(service *journalService) {
		service.LoadedByUs = true
	}); err != nil {
		return err
	}
	if err := runCmd(ctx, "launchctl", "kickstart", "-k", domain+"/"+label); err != nil {
		return err
	}
	return journal.updateService(label, func(service *journalService) {
		service.StartedByUs = true
	})
}

func startDarwinTunnel(ctx context.Context, request Request, journal *rollbackJournal) error {
	if !request.RegisterService {
		return nil
	}
	domain := "gui/" + strconv.Itoa(currentUnixUID())
	label := darwinCLITunnelLabel
	plist := filepath.Join(launchAgentsDir(request), label+".plist")
	loaded := cmdOK(ctx, "launchctl", "print", domain+"/"+label)
	if err := journal.NoteService(journalService{
		Manager:   "launchd",
		Name:      label,
		Domain:    domain,
		Plist:     plist,
		WasActive: loaded,
	}); err != nil {
		return err
	}
	if loaded {
		if err := runCmd(ctx, "launchctl", "bootout", domain+"/"+label); err != nil {
			return err
		}
		if err := journal.updateService(label, func(service *journalService) {
			service.StoppedByUs = true
		}); err != nil {
			return err
		}
	}
	if err := runCmd(ctx, "launchctl", "bootstrap", domain, plist); err != nil {
		return err
	}
	if err := journal.updateService(label, func(service *journalService) {
		service.LoadedByUs = true
	}); err != nil {
		return err
	}
	if err := runCmd(ctx, "launchctl", "kickstart", "-k", domain+"/"+label); err != nil {
		return err
	}
	return journal.updateService(label, func(service *journalService) {
		service.StartedByUs = true
	})
}

func snapshotWindowsRuntimeState(request Request, journal *rollbackJournal) error {
	if !request.StartService || !fileExists(filepath.Join(request.RuntimeRoot, "runtime.json")) {
		return nil
	}
	binary := windowsServiceBinary(request)
	if binary == "" {
		return fmt.Errorf("Windows 状态快照找不到 agentdock 二进制")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, component := range []struct {
		command string
		name    string
	}{
		{command: "service", name: "agentdock"},
		{command: "tunnel", name: "agentdock-tunnel"},
	} {
		running, err := probeWindowsComponentRunning(ctx, binary, component.command, request.RuntimeRoot)
		if err != nil {
			return err
		}
		if err := journal.NoteService(journalService{Manager: "windows", Name: component.name, WasActive: running}); err != nil {
			return err
		}
	}
	return nil
}

func probeWindowsComponentRunning(ctx context.Context, binary, component, runtimeRoot string) (bool, error) {
	cmd := exec.CommandContext(ctx, binary, component, "status", "--runtime-root", runtimeRoot)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("读取 Windows %s 安装前状态失败: %w: %s", component, err, strings.TrimSpace(string(out)))
	}
	switch component {
	case "service":
		var status desktopruntime.ServiceStatus
		if err := json.Unmarshal(out, &status); err != nil {
			return false, fmt.Errorf("解析 Windows Core 状态失败: %w", err)
		}
		return status.Running, nil
	case "tunnel":
		var status desktopruntime.TunnelStatus
		if err := json.Unmarshal(out, &status); err != nil {
			return false, fmt.Errorf("解析 Windows Tunnel 状态失败: %w", err)
		}
		return status.Running, nil
	default:
		return false, fmt.Errorf("未知 Windows 运行组件: %s", component)
	}
}

func startWindowsServices(ctx context.Context, request Request, journal *rollbackJournal) error {
	binary := windowsServiceBinary(request)
	if binary == "" {
		return fmt.Errorf("Windows 启动找不到 agentdock 二进制")
	}
	if !journal.hasService("agentdock") {
		if err := journal.NoteService(journalService{Manager: "windows", Name: "agentdock"}); err != nil {
			return err
		}
	}
	if err := runCmd(ctx, binary, "service", "start", "--runtime-root", request.RuntimeRoot); err != nil {
		return err
	}
	return journal.updateService("agentdock", func(service *journalService) {
		service.StartedByUs = true
	})
}

func startWindowsTunnel(ctx context.Context, request Request, journal *rollbackJournal) error {
	binary := windowsServiceBinary(request)
	if binary == "" {
		return fmt.Errorf("Windows Tunnel 启动找不到 agentdock 二进制")
	}
	if !journal.hasService("agentdock-tunnel") {
		if err := journal.NoteService(journalService{Manager: "windows", Name: "agentdock-tunnel"}); err != nil {
			return err
		}
	}
	if err := runCmd(ctx, binary, "tunnel", "start", "--runtime-root", request.RuntimeRoot); err != nil {
		return err
	}
	return journal.updateService("agentdock-tunnel", func(service *journalService) {
		service.StartedByUs = true
	})
}

func windowsServiceBinary(request Request) string {
	candidates := []string{
		filepath.Join(request.InstallRoot, "agentdock.exe"),
		filepath.Join(request.InstallRoot, "bin", "agentdock.exe"),
	}
	if request.PayloadDir != "" {
		candidates = append([]string{filepath.Join(request.PayloadDir, "agentdock.exe")}, candidates...)
	}
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate
		}
	}
	return ""
}

func launchAgentsDir(request Request) string {
	if request.LaunchAgentsDir != "" {
		return request.LaunchAgentsDir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents")
}

func stopJournalService(ctx context.Context, request Request, service journalService) error {
	switch service.Manager {
	case "systemd":
		return runCmd(ctx, "systemctl", "stop", service.Name)
	case "openrc":
		return runCmd(ctx, "rc-service", service.Name, "stop")
	case "launchd":
		if service.Domain != "" {
			return runCmd(ctx, "launchctl", "bootout", service.Domain+"/"+service.Name)
		}
	case "windows":
		binary := windowsServiceBinary(request)
		if binary == "" {
			return fmt.Errorf("Windows 停止找不到 agentdock 二进制")
		}
		if service.Name == "agentdock-tunnel" {
			return runCmd(ctx, binary, "tunnel", "stop", "--runtime-root", request.RuntimeRoot)
		}
		return runCmd(ctx, binary, "service", "stop", "--runtime-root", request.RuntimeRoot)
	}
	return nil
}

func reloadJournalServices(ctx context.Context, request Request, services []journalService) error {
	_ = request
	for _, service := range services {
		if service.Manager == "systemd" {
			return runCmd(ctx, "systemctl", "daemon-reload")
		}
	}
	return nil
}

func restoreJournalService(ctx context.Context, request Request, service journalService) error {
	_ = request
	switch service.Manager {
	case "systemd":
		_, action := linuxAutostartRestore("systemd", service.WasEnabled)
		if err := runCmd(ctx, "systemctl", action, service.Name); err != nil {
			return err
		}
		if service.WasActive {
			return runCmd(ctx, "systemctl", "start", service.Name)
		}
	case "openrc":
		_, action := linuxAutostartRestore("openrc", service.WasEnabled)
		if err := runCmd(ctx, "rc-update", action, service.Name, "default"); err != nil {
			return err
		}
		if service.WasActive {
			return runCmd(ctx, "rc-service", service.Name, "start")
		}
	case "launchd":
		if service.WasActive && service.Plist != "" && service.Domain != "" {
			if err := runCmd(ctx, "launchctl", "bootstrap", service.Domain, service.Plist); err != nil {
				return err
			}
			return runCmd(ctx, "launchctl", "kickstart", "-k", service.Domain+"/"+service.Name)
		}
	case "windows":
		if service.WasActive {
			if binary := windowsServiceBinary(request); binary != "" {
				if service.Name == "agentdock-tunnel" {
					return runCmd(ctx, binary, "tunnel", "start", "--runtime-root", request.RuntimeRoot)
				}
				return runCmd(ctx, binary, "service", "start", "--runtime-root", request.RuntimeRoot)
			}
		}
	}
	return nil
}

func runCmd(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
		}
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, msg)
	}
	return nil
}

func cmdOK(ctx context.Context, name string, args ...string) bool {
	return exec.CommandContext(ctx, name, args...).Run() == nil
}

func cmdOutput(ctx context.Context, name string, args ...string) []byte {
	out, _ := exec.CommandContext(ctx, name, args...).Output()
	return out
}

func linuxAutostartRestore(manager string, wasEnabled bool) (bin, action string) {
	switch manager {
	case "systemd":
		if wasEnabled {
			return "systemctl", "enable"
		}
		return "systemctl", "disable"
	case "openrc":
		if wasEnabled {
			return "rc-update", "add"
		}
		return "rc-update", "del"
	}
	return "", ""
}

func resolveListenAddress(request Request) (host string, port int) {
	host, port = request.Host, request.Port
	if host != "" && port != 0 {
		return host, port
	}
	if runtimeGOOS() == "windows" {
		manifest, err := desktopruntime.Load(filepath.Join(request.RuntimeRoot, "runtime.json"))
		if err == nil {
			if host == "" {
				host = manifest.Host
			}
			if port == 0 {
				port = manifest.Port
			}
		}
		return host, port
	}
	values, err := envstore.ParseFile(filepath.Join(request.RuntimeRoot, "agentdock.env"))
	if err != nil {
		return host, port
	}
	if host == "" {
		host = values["AGENTDOCK_HOST"]
	}
	if port == 0 {
		port, _ = strconv.Atoi(values["AGENTDOCK_PORT"])
	}
	return host, port
}

func waitTunnelReady(ctx context.Context, request Request, timeout time.Duration) error {
	switch request.TunnelMode {
	case "named":
		return waitNamedTunnelReady(ctx, request, timeout)
	case "quick":
		if runtimeGOOS() == "windows" {
			return waitWindowsQuickTunnelReady(ctx, request, timeout)
		}
		return waitUnixQuickTunnelReady(ctx, request, timeout)
	default:
		return nil
	}
}

func waitNamedTunnelReady(ctx context.Context, request Request, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		if err := namedTunnelRunning(ctx, request); err != nil {
			last = err
		} else {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	if last == nil {
		last = fmt.Errorf("Named Tunnel 未在超时前 ready")
	}
	return last
}

func namedTunnelRunning(ctx context.Context, request Request) error {
	switch runtimeGOOS() {
	case "linux":
		manager := request.ServiceManager
		if manager == "auto" {
			manager = detectLinuxServiceManager(request)
		}
		name := request.ServiceName + "-cloudflared"
		switch manager {
		case "systemd":
			if !cmdOK(ctx, "systemctl", "is-active", "--quiet", name) {
				return fmt.Errorf("Named Tunnel 服务 %s 未在运行", name)
			}
			pid := strings.TrimSpace(string(cmdOutput(ctx, "systemctl", "show", "-p", "MainPID", "--value", name)))
			if pid == "" || pid == "0" {
				return fmt.Errorf("Named Tunnel 服务 %s 没有活动进程", name)
			}
		case "openrc":
			if !cmdOK(ctx, "rc-service", name, "status") {
				return fmt.Errorf("Named Tunnel 服务 %s 未在运行", name)
			}
		default:
			return fmt.Errorf("Named Tunnel 没有可用的 Linux 服务管理器")
		}
	case "darwin":
		domain := "gui/" + strconv.Itoa(currentUnixUID())
		label := darwinCLITunnelLabel
		if !cmdOK(ctx, "launchctl", "print", domain+"/"+label) {
			return fmt.Errorf("Named Tunnel LaunchAgent %s 未加载", label)
		}
	case "windows":
		return windowsNamedTunnelRunning(ctx, request)
	default:
		return fmt.Errorf("Named Tunnel 不支持当前平台")
	}
	if !cloudflaredProcessRunning(ctx, request) {
		return fmt.Errorf("Named Tunnel cloudflared 进程未在运行")
	}
	return nil
}

func windowsNamedTunnelRunning(ctx context.Context, request Request) error {
	binary := windowsServiceBinary(request)
	if binary == "" {
		return fmt.Errorf("Windows Named Tunnel 找不到 agentdock 二进制")
	}
	cmd := exec.CommandContext(ctx, binary, "tunnel", "status", "--runtime-root", request.RuntimeRoot)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("读取 Windows Named Tunnel 状态失败: %w: %s", err, strings.TrimSpace(string(out)))
	}
	var status desktopruntime.TunnelStatus
	if err := json.Unmarshal(out, &status); err != nil {
		return fmt.Errorf("解析 Windows Named Tunnel 状态失败: %w", err)
	}
	if !strings.EqualFold(status.Mode, "named") || !status.Running || !status.Ready {
		return fmt.Errorf("Windows Named Tunnel 尚未 ready: mode=%s running=%t ready=%t", status.Mode, status.Running, status.Ready)
	}
	if want := strings.TrimSpace(request.ServerURL); want != "" && !strings.EqualFold(strings.TrimSpace(status.PublicURL), want) {
		return fmt.Errorf("Windows Named Tunnel 公网地址不一致: got=%s want=%s", status.PublicURL, want)
	}
	return nil
}

func cloudflaredProcessRunning(ctx context.Context, request Request) bool {
	path := strings.TrimSpace(request.CloudflaredPath)
	if path == "" {
		if runtimeGOOS() == "windows" {
			path = filepath.Join(request.InstallRoot, "bin", "cloudflared.exe")
		} else {
			path = filepath.Join(request.InstallRoot, "bin", "cloudflared")
			if !fileExists(path) {
				path = "cloudflared"
			}
		}
	}
	base := filepath.Base(path)
	if path != base {
		// 有绝对路径时不要退回 pgrep -x 进程名，避免命中别人的 cloudflared。
		return cmdOK(ctx, "pgrep", "-f", path)
	}
	return cmdOK(ctx, "pgrep", "-x", base)
}

func waitUnixQuickTunnelReady(ctx context.Context, request Request, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		url := readQuickTunnelURL(request.RuntimeRoot)
		values, err := envstore.ParseFile(filepath.Join(request.RuntimeRoot, "agentdock.env"))
		if err != nil {
			last = err
		} else if url == "" {
			last = fmt.Errorf("Quick Tunnel 尚未写入公网地址")
		} else if strings.TrimSpace(values["AGENTDOCK_SERVER_URL"]) != url {
			last = fmt.Errorf("Quick Tunnel 公网地址尚未回写 Core env")
		} else if !strings.EqualFold(strings.TrimSpace(values["AGENTDOCK_OAUTH_ENABLED"]), "true") {
			last = fmt.Errorf("Quick Tunnel 尚未启用 OAuth")
		} else {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	if last == nil {
		last = fmt.Errorf("Quick Tunnel 未在超时前 ready")
	}
	return last
}

func waitWindowsQuickTunnelReady(ctx context.Context, request Request, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	manifestPath := filepath.Join(request.RuntimeRoot, "runtime.json")
	serverURLPath := filepath.Join(request.RuntimeRoot, "server-url.txt")
	for time.Now().Before(deadline) {
		url := readQuickTunnelURL(request.RuntimeRoot)
		serverURL := strings.TrimSpace(readTextFile(serverURLPath))
		manifest, err := desktopruntime.Load(manifestPath)
		switch {
		case url == "":
			last = fmt.Errorf("Quick Tunnel 尚未写入公网地址")
		case serverURL != url:
			last = fmt.Errorf("Quick Tunnel 公网地址尚未回写 server-url.txt")
		case err != nil:
			last = err
		case !strings.EqualFold(manifest.TunnelMode, "quick"):
			last = fmt.Errorf("runtime.json tunnel_mode=%s, want quick", manifest.TunnelMode)
		case strings.TrimSpace(manifest.PublicURL) != url:
			last = fmt.Errorf("runtime.json public_url 尚未回写 Quick Tunnel 地址")
		default:
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	if last == nil {
		last = fmt.Errorf("Quick Tunnel 未在超时前 ready")
	}
	return last
}

func readTextFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func readQuickTunnelURL(runtimeRoot string) string {
	data, err := os.ReadFile(filepath.Join(runtimeRoot, "quick-tunnel-url.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func waitHealthyWithProbe(ctx context.Context, request Request, endpoint string, timeout time.Duration) error {
	if runtimeGOOS() == "darwin" {
		return waitHealthyCurl(ctx, endpoint, request.Version, timeout)
	}
	return waitHealthy(ctx, endpoint, timeout)
}

func waitHealthyCurl(ctx context.Context, endpoint, version string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	want := strings.TrimPrefix(version, "v")
	for time.Now().Before(deadline) {
		cmd := exec.CommandContext(ctx, "curl", "-fsS", "--max-time", "2", endpoint)
		out, err := cmd.Output()
		if err != nil {
			last = err
		} else {
			var body struct {
				OK      bool   `json:"ok"`
				Version string `json:"version"`
			}
			if json.Unmarshal(out, &body) == nil && body.OK {
				if want == "" || want == "unknown" || strings.TrimPrefix(body.Version, "v") == want {
					return nil
				}
				last = fmt.Errorf("health version %s, want %s", body.Version, version)
			} else {
				last = fmt.Errorf("%s 返回非健康响应", endpoint)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	if last == nil {
		last = fmt.Errorf("health check timed out: %s", endpoint)
	}
	return last
}

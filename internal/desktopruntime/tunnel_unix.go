//go:build darwin || linux

package desktopruntime

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/uvwt/agentdock/internal/envstore"
	"github.com/uvwt/agentdock/internal/fs/atomicfile"
)

func loadTunnelEnvironment(runtimeRoot string) (unixRuntimeManifest, string, map[string]string, error) {
	manifest, root, err := loadUnixRuntime(runtimeRoot)
	if err != nil {
		return unixRuntimeManifest{}, "", nil, err
	}
	values, err := envstore.ParseFile(manifest.TunnelEnvironment)
	if err != nil {
		return unixRuntimeManifest{}, "", nil, err
	}
	return manifest, root, values, nil
}

func tunnelMode(values map[string]string) string {
	mode := strings.ToLower(strings.TrimSpace(values["AGENTDOCK_TUNNEL_MODE"]))
	if mode != "quick" && mode != "named" {
		return "none"
	}
	return mode
}

func platformTunnelStatus(ctx context.Context, runtimeRoot string) (TunnelStatus, error) {
	manifest, root, values, err := loadTunnelEnvironment(runtimeRoot)
	if errors.Is(err, os.ErrNotExist) {
		_, dependencyState, componentVersion := unixCloudflaredComponentStatus(runtimeRoot, manifest)
		return TunnelStatus{
			Mode:             "none",
			Ready:            true,
			DependencyState:  dependencyState,
			ComponentVersion: componentVersion,
		}, nil
	}
	if err != nil {
		return TunnelStatus{}, err
	}
	mode := tunnelMode(values)
	componentPath, dependencyState, componentVersion := unixCloudflaredComponentStatus(runtimeRoot, manifest)
	if componentPath != "" {
		manifest.CloudflaredBinary = componentPath
	}
	running := tunnelServiceActive(ctx, manifest)
	publicURL := ""
	quickCoreReady := false
	if mode == "quick" {
		data, _ := os.ReadFile(filepath.Join(root, "quick-tunnel-url.txt"))
		publicURL = strings.TrimSpace(string(data))
		_, _, core, coreErr := loadCoreEnvironment(runtimeRoot)
		quickCoreReady = coreErr == nil && quickTunnelCoreReady(core, publicURL)
	} else if mode == "named" {
		_, _, core, coreErr := loadCoreEnvironment(runtimeRoot)
		if coreErr == nil {
			publicURL = strings.TrimSpace(core["AGENTDOCK_SERVER_URL"])
		}
	}
	ready := mode == "none"
	if mode == "quick" {
		ready = dependencyState != "not_installed" && dependencyState != "broken" && running && quickCoreReady
	} else if mode == "named" {
		ready = dependencyState != "not_installed" && dependencyState != "broken" && running
	}
	return TunnelStatus{
		Mode:             mode,
		Running:          running,
		Ready:            ready,
		StartupEnabled:   tunnelServiceEnabled(ctx, manifest),
		PublicURL:        publicURL,
		DependencyState:  dependencyState,
		ComponentVersion: componentVersion,
	}, nil
}

func platformTunnelAction(ctx context.Context, runtimeRoot, action string) error {
	manifest, root, values, err := loadTunnelEnvironment(runtimeRoot)
	if err != nil {
		return err
	}
	mode := tunnelMode(values)
	if mode == "none" && action != "stop" {
		return errors.New("Tunnel 模式为 none")
	}
	if action == "regenerate" {
		if mode != "quick" {
			return errors.New("只有 Quick Tunnel 可以重新生成地址")
		}
		if err := invalidateQuickTunnelPublicState(ctx, manifest, root, runtimeRoot); err != nil {
			return err
		}
		action = "restart"
	}
	switch action {
	case "start", "restart", "stop":
		return tunnelServiceAction(ctx, manifest, action)
	default:
		return errors.New("不支持的 Tunnel 操作")
	}
}

func platformConfigureTunnel(ctx context.Context, request TunnelConfigureRequest) error {
	manifest, root, core, err := loadCoreEnvironment(request.RuntimeRoot)
	if err != nil {
		return err
	}
	if request.Mode != "none" {
		// 先验证 optional component，再修改 Tunnel/Core 配置。none 始终是无需依赖的恢复路径。
		if err := prepareUnixCloudflared(ctx, request.RuntimeRoot, &manifest); err != nil {
			return err
		}
	}
	if manifest.ServiceManager != "smappservice" {
		if err := tunnelServiceAction(ctx, manifest, "stop"); err != nil && !strings.Contains(err.Error(), "not loaded") {
			return err
		}
	}

	mode := request.Mode
	tunnelValues := map[string]string{"AGENTDOCK_TUNNEL_MODE": mode}
	quickURL := filepath.Join(root, "quick-tunnel-url.txt")
	_ = os.Remove(quickURL)
	switch mode {
	case "none":
		delete(core, "AGENTDOCK_SERVER_URL")
		core["AGENTDOCK_OAUTH_ENABLED"] = "false"
	case "quick":
		tunnelValues["AGENTDOCK_TUNNEL_TARGET"] = strings.TrimSuffix(healthURL(core), "/healthz")
		prepareQuickTunnelCoreEnvironment(core)
	case "named":
		candidate := strings.TrimSpace(request.ServerURL)
		if candidate == "" {
			data, readErr := os.ReadFile(filepath.Join(root, "named-server-url.txt"))
			if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
				return readErr
			}
			candidate = strings.TrimSpace(string(data))
		}
		origin, err := normalizeHTTPSOrigin(candidate)
		if err != nil {
			return err
		}
		if err := atomicfile.Write(filepath.Join(root, "named-server-url.txt"), []byte(origin+"\n"), 0o600); err != nil {
			return err
		}
		token, err := configuredTunnelToken(root, request.TokenFile)
		if err != nil {
			return err
		}
		if err := atomicfile.Write(filepath.Join(root, "cloudflare-tunnel-token"), []byte(token+"\n"), 0o600); err != nil {
			return err
		}
		core["AGENTDOCK_SERVER_URL"] = origin
		core["AGENTDOCK_OAUTH_ENABLED"] = "true"
	default:
		return errors.New("Tunnel 模式必须是 none、quick 或 named")
	}
	if err := writeEnvironment(manifest.EnvironmentFile, core); err != nil {
		return err
	}
	if err := writeEnvironment(manifest.TunnelEnvironment, tunnelValues); err != nil {
		return err
	}
	if err := platformServiceAction(ctx, request.RuntimeRoot, "restart"); err != nil {
		return err
	}
	if manifest.ServiceManager == "smappservice" {
		// macOS App owns SMAppService registration. The Go runtime only writes
		// configuration and restarts Core; the App registers/unregisters Tunnel.
		return nil
	}
	if mode != "none" {
		return tunnelServiceAction(ctx, manifest, "start")
	}
	return nil
}

func invalidateQuickTunnelPublicState(
	ctx context.Context,
	manifest unixRuntimeManifest,
	root string,
	runtimeRoot string,
) error {
	_, _, core, err := loadCoreEnvironment(runtimeRoot)
	if err != nil {
		return err
	}
	prepareQuickTunnelCoreEnvironment(core)
	if err := writeEnvironment(manifest.EnvironmentFile, core); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(root, "quick-tunnel-url.txt")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("删除 Quick Tunnel ready 文件失败: %w", err)
	}
	// 旧公网 Origin 先失效，Core 回到本地健康态，再等待下一代 Quick Tunnel 地址。
	return platformServiceAction(ctx, runtimeRoot, "restart")
}

func prepareQuickTunnelCoreEnvironment(core map[string]string) {
	// Quick Tunnel 的 Origin 由 cloudflared 动态产生。在新地址真正 ready 之前，
	// Core 必须保持本地可用，不能留下 OAuth=true 但没有 SERVER_URL 的非法中间态。
	delete(core, "AGENTDOCK_SERVER_URL")
	core["AGENTDOCK_OAUTH_ENABLED"] = "false"
}

func quickTunnelCoreReady(core map[string]string, publicURL string) bool {
	publicURL = strings.TrimSpace(publicURL)
	return publicURL != "" &&
		strings.TrimSpace(core["AGENTDOCK_SERVER_URL"]) == publicURL &&
		strings.EqualFold(strings.TrimSpace(core["AGENTDOCK_OAUTH_ENABLED"]), "true")
}

func normalizeHTTPSOrigin(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("Named Tunnel 公网地址必须是有效的 HTTPS Origin")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

func configuredTunnelToken(root, tokenFile string) (string, error) {
	paths := []string{strings.TrimSpace(tokenFile), filepath.Join(root, "cloudflare-tunnel-token")}
	for _, path := range paths {
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		token := strings.TrimSpace(string(data))
		if token != "" && !strings.ContainsAny(token, "\r\n") && len(token) <= 16*1024 {
			return token, nil
		}
		return "", errors.New("Cloudflare Tunnel Token 格式无效")
	}
	return "", errors.New("Named Tunnel 缺少 Token")
}

func platformSetTunnelAutostart(ctx context.Context, runtimeRoot string, enabled bool) error {
	manifest, _, err := loadUnixRuntime(runtimeRoot)
	if err != nil {
		return err
	}
	return tunnelServiceSetEnabled(ctx, manifest, enabled)
}

func platformLaunchTunnel(ctx context.Context, runtimeRoot string) error {
	if err := platformPrepareLaunchEnvironment("tunnel"); err != nil {
		return err
	}
	manifest, root, values, err := loadTunnelEnvironment(runtimeRoot)
	if err != nil {
		return err
	}
	stdout, stderr := io.Writer(os.Stdout), io.Writer(os.Stderr)
	logs, err := platformOpenTunnelLogs(manifest)
	if err != nil {
		return err
	}
	if logs != nil {
		defer logs.Close()
		stdout, stderr = logs.stdout, logs.stderr
	}
	mode := tunnelMode(values)
	if mode == "none" {
		return nil
	}
	if err := prepareUnixCloudflared(ctx, runtimeRoot, &manifest); err != nil {
		return err
	}
	switch mode {
	case "quick":
		target := strings.TrimSpace(values["AGENTDOCK_TUNNEL_TARGET"])
		if target == "" {
			return errors.New("Quick Tunnel 缺少目标地址")
		}
		return runQuickTunnel(ctx, manifest, root, runtimeRoot, target, stdout)
	case "named":
		token := strings.TrimSpace(values["TUNNEL_TOKEN"])
		if token == "" {
			token, err = configuredTunnelToken(root, "")
			if err != nil {
				return err
			}
		}
		arguments, err := prepareCloudflaredTunnelArgs(root, "run")
		if err != nil {
			return err
		}
		command := exec.CommandContext(ctx, manifest.CloudflaredBinary, arguments...)
		command.Env = append(os.Environ(), "TUNNEL_TOKEN="+token)
		command.Stdout = stdout
		command.Stderr = stderr
		return command.Run()
	default:
		return nil
	}
}

func runQuickTunnel(ctx context.Context, manifest unixRuntimeManifest, root, runtimeRoot, target string, logOutput io.Writer) error {
	arguments, err := prepareCloudflaredTunnelArgs(root, "--url", target)
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, manifest.CloudflaredBinary, arguments...)
	reader, writer := io.Pipe()
	command.Stdout = writer
	command.Stderr = writer
	if err := command.Start(); err != nil {
		return err
	}
	wait := make(chan error, 1)
	go func() {
		wait <- command.Wait()
		_ = writer.Close()
	}()

	addressApplied := false
	quickURLParser := quickTunnelLogParser{}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintln(logOutput, line)
		if addressApplied {
			continue
		}
		publicURL := quickURLParser.URL(line)
		if publicURL == "" {
			continue
		}
		_, _, core, err := loadCoreEnvironment(runtimeRoot)
		if err != nil {
			_ = command.Process.Kill()
			return err
		}
		core["AGENTDOCK_SERVER_URL"] = publicURL
		core["AGENTDOCK_OAUTH_ENABLED"] = "true"
		if err := writeEnvironment(manifest.EnvironmentFile, core); err != nil {
			_ = command.Process.Kill()
			return err
		}
		if err := platformServiceAction(ctx, runtimeRoot, "restart"); err != nil {
			_ = command.Process.Kill()
			return err
		}
		if err := atomicfile.Write(filepath.Join(root, "quick-tunnel-url.txt"), []byte(publicURL+"\n"), 0o600); err != nil {
			_ = command.Process.Kill()
			return err
		}
		addressApplied = true
	}
	if err := scanner.Err(); err != nil {
		_ = command.Process.Kill()
		return err
	}
	runErr := <-wait
	if addressApplied && ctx.Err() == nil {
		// Windows supervisor 在 cloudflared 意外退出时会立即撤销旧地址；Unix 也保持同一语义，
		// 避免 launchd/systemd 重试窗口里继续把已经失效的地址暴露为 ready。
		cleanupErr := invalidateQuickTunnelPublicState(ctx, manifest, root, runtimeRoot)
		return errors.Join(runErr, cleanupErr)
	}
	return runErr
}

func tunnelServiceActive(ctx context.Context, manifest unixRuntimeManifest) bool {
	return platformTunnelServiceActive(ctx, manifest)
}

func tunnelServiceEnabled(ctx context.Context, manifest unixRuntimeManifest) bool {
	return platformTunnelServiceEnabled(ctx, manifest)
}

func tunnelServiceAction(ctx context.Context, manifest unixRuntimeManifest, action string) error {
	return platformTunnelServiceAction(ctx, manifest, action)
}

func tunnelServiceSetEnabled(ctx context.Context, manifest unixRuntimeManifest, enabled bool) error {
	return platformTunnelServiceSetEnabled(ctx, manifest, enabled)
}

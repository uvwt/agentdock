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
		return TunnelStatus{Mode: "none"}, nil
	}
	if err != nil {
		return TunnelStatus{}, err
	}
	mode := tunnelMode(values)
	running := tunnelServiceActive(ctx, manifest)
	publicURL := ""
	if mode == "quick" {
		data, _ := os.ReadFile(filepath.Join(root, "quick-tunnel-url.txt"))
		publicURL = strings.TrimSpace(string(data))
	} else if mode == "named" {
		_, _, core, coreErr := loadCoreEnvironment(runtimeRoot)
		if coreErr == nil {
			publicURL = strings.TrimSpace(core["AGENTDOCK_SERVER_URL"])
		}
	}
	return TunnelStatus{
		Mode:           mode,
		Running:        running,
		Ready:          running && (mode == "named" || publicURL != ""),
		StartupEnabled: tunnelServiceEnabled(ctx, manifest),
		PublicURL:      publicURL,
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
		_ = os.Remove(filepath.Join(root, "quick-tunnel-url.txt"))
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
	if err := tunnelServiceAction(ctx, manifest, "stop"); err != nil && !strings.Contains(err.Error(), "not loaded") {
		return err
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
		delete(core, "AGENTDOCK_SERVER_URL")
		core["AGENTDOCK_OAUTH_ENABLED"] = "true"
	case "named":
		origin, err := normalizeHTTPSOrigin(request.ServerURL)
		if err != nil {
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
	if mode != "none" {
		return tunnelServiceAction(ctx, manifest, "start")
	}
	return nil
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
		command := exec.CommandContext(ctx, manifest.CloudflaredBinary, "tunnel", "--no-autoupdate", "run")
		command.Env = append(os.Environ(), "TUNNEL_TOKEN="+token)
		command.Stdout = stdout
		command.Stderr = stderr
		return command.Run()
	default:
		return errors.New("Tunnel 模式为 none")
	}
}

func runQuickTunnel(ctx context.Context, manifest unixRuntimeManifest, root, runtimeRoot, target string, logOutput io.Writer) error {
	command := exec.CommandContext(ctx, manifest.CloudflaredBinary, "tunnel", "--no-autoupdate", "--url", target)
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
	return <-wait
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

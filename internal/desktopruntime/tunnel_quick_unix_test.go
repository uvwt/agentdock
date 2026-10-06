//go:build darwin || linux

package desktopruntime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// runQuickTunnel 是 Quick Tunnel 公网地址回写的唯一权威（平台脚本的 legacy
// 刷新逻辑已删除）。回写必须：把新地址写进 AGENTDOCK_SERVER_URL、开启 OAuth、
// 重启 Core 并落 quick-tunnel-url.txt，同时不能轮换已有的稳定凭据。
func TestPrepareQuickTunnelCoreEnvironmentClearsStaleOrigin(t *testing.T) {
	core := map[string]string{
		"AGENTDOCK_AUTH_TOKEN":         "stable-bearer-token",
		"AGENTDOCK_SERVER_URL":         "https://stale.trycloudflare.com",
		"AGENTDOCK_OAUTH_ENABLED":      "true",
		"AGENTDOCK_OAUTH_PASSWORD":     "stable-oauth-password",
		"AGENTDOCK_OAUTH_TOKEN_SECRET": "stable-oauth-secret",
	}

	prepareQuickTunnelCoreEnvironment(core)

	if _, ok := core["AGENTDOCK_SERVER_URL"]; ok {
		t.Fatal("Quick Tunnel staging must clear the stale public Origin")
	}
	if got := core["AGENTDOCK_OAUTH_ENABLED"]; got != "false" {
		t.Fatalf("AGENTDOCK_OAUTH_ENABLED = %q, want false", got)
	}
	for key, want := range map[string]string{
		"AGENTDOCK_AUTH_TOKEN":         "stable-bearer-token",
		"AGENTDOCK_OAUTH_PASSWORD":     "stable-oauth-password",
		"AGENTDOCK_OAUTH_TOKEN_SECRET": "stable-oauth-secret",
	} {
		if got := core[key]; got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestQuickTunnelCoreReadyRequiresMatchingOriginAndOAuth(t *testing.T) {
	core := map[string]string{
		"AGENTDOCK_SERVER_URL":    "https://fresh.trycloudflare.com",
		"AGENTDOCK_OAUTH_ENABLED": "true",
	}
	if !quickTunnelCoreReady(core, "https://fresh.trycloudflare.com") {
		t.Fatal("matching public Origin with OAuth enabled should be ready")
	}

	core["AGENTDOCK_OAUTH_ENABLED"] = "false"
	if quickTunnelCoreReady(core, "https://fresh.trycloudflare.com") {
		t.Fatal("OAuth-disabled Quick Tunnel must not be ready")
	}

	core["AGENTDOCK_OAUTH_ENABLED"] = "true"
	if quickTunnelCoreReady(core, "https://other.trycloudflare.com") {
		t.Fatal("mismatched public Origin must not be ready")
	}
}

func TestRunQuickTunnelWritesBackPublicURLAndRestartsCore(t *testing.T) {
	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"version":"test"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer healthServer.Close()
	healthPort := healthServer.URL[strings.LastIndex(healthServer.URL, ":")+1:]

	root := t.TempDir()
	runtimeRoot := filepath.Join(root, "runtime")
	envFile := filepath.Join(runtimeRoot, "agentdock.env")
	cloudflared := filepath.Join(root, "fake-cloudflared")
	serviceActionLog := filepath.Join(root, "service-actions.log")

	// 假 cloudflared：先保持运行，允许测试观察 ready 状态；后面再改成主动退出，
	// 验证 Unix 与 Windows 一样会撤销失效地址。
	fakeCloudflared := "#!/bin/sh\n" +
		`echo "2026-09-13T00:00:00Z INF +--------------------------------------------------------------------------------------------+"` + "\n" +
		`echo "2026-09-13T00:00:00Z INF |  Your quick Tunnel has been created! Visit it at https://fresh.trycloudflare.com  |"` + "\n" +
		"exec sleep 30\n"
	if err := os.WriteFile(cloudflared, []byte(fakeCloudflared), 0o755); err != nil {
		t.Fatal(err)
	}
	serviceManager := "launchd"
	if runtime.GOOS == "darwin" {
		// 假 launchctl：print 视为已加载，kickstart 计数并成功，模拟 Core 重启。
		launchctl := filepath.Join(root, "fake-launchctl")
		fakeLaunchctl := "#!/bin/sh\n" +
			`case "$1" in` + "\n" +
			`  print) exit 0 ;;` + "\n" +
			`  kickstart) echo restart >> "$SERVICE_ACTION_LOG"; exit 0 ;;` + "\n" +
			`esac` + "\n" +
			"exit 1\n"
		if err := os.WriteFile(launchctl, []byte(fakeLaunchctl), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("AGENTDOCK_LAUNCHCTL_BIN", launchctl)
	} else {
		// Linux CI 不一定运行 systemd/OpenRC。manifest 明确声明 systemd，PATH 中
		// 放一个只记录 restart 的假 systemctl，测试真实的平台分发而不依赖宿主 init。
		serviceManager = "systemd"
		systemctl := filepath.Join(root, "systemctl")
		fakeSystemctl := "#!/bin/sh\n" +
			`echo restart >> "$SERVICE_ACTION_LOG"` + "\n"
		if err := os.WriteFile(systemctl, []byte(fakeSystemctl), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	t.Setenv("SERVICE_ACTION_LOG", serviceActionLog)

	env := "AGENTDOCK_HOST=127.0.0.1\n" +
		"AGENTDOCK_PORT=" + healthPort + "\n" +
		"AGENTDOCK_AUTH_TOKEN=stable-bearer-token\n" +
		"AGENTDOCK_OAUTH_ENABLED=false\n" +
		"AGENTDOCK_OAUTH_PASSWORD=stable-oauth-password\n" +
		"AGENTDOCK_OAUTH_TOKEN_SECRET=stable-oauth-secret\n"
	if err := os.MkdirAll(runtimeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envFile, []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := unixRuntimeManifest{
		SchemaVersion:     1,
		ServiceManager:    serviceManager,
		ServiceName:       "com.uvwt.agentdock",
		TunnelServiceName: "com.uvwt.agentdock.cloudflared",
		AgentDockBinary:   filepath.Join(root, "agentdock"),
		CloudflaredBinary: cloudflared,
		EnvironmentFile:   envFile,
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeRoot, "desktop-runtime.json"), manifestBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() {
		runErr <- runQuickTunnel(ctx, manifest, root, runtimeRoot, "http://127.0.0.1:18766", io.Discard)
	}()

	quickURLPath := filepath.Join(root, "quick-tunnel-url.txt")
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, readErr := os.ReadFile(quickURLPath)
		if readErr == nil && strings.TrimSpace(string(data)) == "https://fresh.trycloudflare.com" {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-runErr
			t.Fatal("Quick Tunnel did not become ready before test timeout")
		}
		time.Sleep(25 * time.Millisecond)
	}

	updated, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	text := string(updated)
	for _, want := range []string{
		"AGENTDOCK_SERVER_URL='https://fresh.trycloudflare.com'",
		"AGENTDOCK_OAUTH_ENABLED='true'",
		"AGENTDOCK_AUTH_TOKEN='stable-bearer-token'",
		"AGENTDOCK_OAUTH_PASSWORD='stable-oauth-password'",
		"AGENTDOCK_OAUTH_TOKEN_SECRET='stable-oauth-secret'",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("quick tunnel write-back missing %q in env:\n%s", want, text)
		}
	}
	urlBytes, err := os.ReadFile(quickURLPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(urlBytes)) != "https://fresh.trycloudflare.com" {
		t.Fatalf("quick-tunnel-url.txt=%q", string(urlBytes))
	}
	serviceActions, err := os.ReadFile(serviceActionLog)
	if err != nil {
		t.Fatal(err)
	}
	if restarts := strings.Count(string(serviceActions), "restart"); restarts != 1 {
		t.Fatalf("core restart actions=%d, want 1", restarts)
	}

	cancel()
	<-runErr

	// 意外退出不是显式 stop：已经发布过的 URL 必须立刻失效，Core 回到 OAuth=false。
	fakeCloudflared = "#!/bin/sh\n" +
		`echo "INF Your quick Tunnel has been created! Visit it at https://fresh.trycloudflare.com"` + "\n"
	if err := os.WriteFile(cloudflared, []byte(fakeCloudflared), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = runQuickTunnel(context.Background(), manifest, root, runtimeRoot, "http://127.0.0.1:18766", io.Discard)

	if _, err := os.Stat(quickURLPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("quick URL ready file must be removed after unexpected exit, stat error=%v", err)
	}
	updated, err = os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	text = string(updated)
	if strings.Contains(text, "AGENTDOCK_SERVER_URL=") {
		t.Fatalf("stale server URL remained after Quick Tunnel exit:\n%s", text)
	}
	if !strings.Contains(text, "AGENTDOCK_OAUTH_ENABLED='false'") {
		t.Fatalf("OAuth was not disabled after Quick Tunnel exit:\n%s", text)
	}
	serviceActions, err = os.ReadFile(serviceActionLog)
	if err != nil {
		t.Fatal(err)
	}
	if restarts := strings.Count(string(serviceActions), "restart"); restarts != 3 {
		t.Fatalf("core restart actions=%d, want 3 (ready + ready + invalidation)", restarts)
	}
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/buildinfo"
	"github.com/uvwt/agentdock/internal/desktopruntime"
)

func TestRunPrintsVersionWithoutLoadingServerConfiguration(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(context.Background(), []string{"--version"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "AgentDock v"+strings.TrimPrefix(buildinfo.Version, "v")) || !strings.Contains(stdout.String(), "platform:") {
		t.Fatalf("unexpected version output: %s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestRunPrintsMachineReadableBuildInfo(t *testing.T) {
	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"version", "--json"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var info buildinfo.Info
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		t.Fatalf("version --json returned invalid JSON: %v", err)
	}
	if info.Version != buildinfo.Version || info.Platform == "" || info.GoVersion == "" {
		t.Fatalf("unexpected build info: %#v", info)
	}
}

func TestRunRejectsUnexpectedUpdateArguments(t *testing.T) {
	err := run(context.Background(), []string{"update", "--check", "extra"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "agentdock update [--check|--progress-json|--local-archive") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstallEngineReadyDoesNotStartServer(t *testing.T) {
	stdout := &bytes.Buffer{}
	if err := run(context.Background(), []string{"install", "--engine-ready"}, stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "agentdock-installer-engine") {
		t.Fatalf("engine-ready handshake missing: %q", stdout.String())
	}
}

func TestInstallInspectRequiresStateRoot(t *testing.T) {
	err := run(context.Background(), []string{"install", "inspect"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--state-root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstallCommitRequiresInstallRoot(t *testing.T) {
	err := run(context.Background(), []string{"install", "commit"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--install-root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstallAbandonRequiresInstallRoot(t *testing.T) {
	err := run(context.Background(), []string{"install", "abandon"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--install-root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrepareWindowsLegacyIsRejectedOffWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows implementation has its own native tests")
	}
	err := run(context.Background(), []string{"install", "prepare-windows-legacy"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "仅支持 Windows") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDetachEngineIsRejectedOffWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows implementation has its own native tests")
	}
	err := run(context.Background(), []string{"install", "detach-engine"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "仅支持 Windows") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	err := run(context.Background(), []string{"unknown"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "未知命令或参数") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNexusStatusReportsUnpairedWithoutExposingToken(t *testing.T) {
	t.Setenv("AGENTDOCK_HOME", t.TempDir())
	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"nexus", "status", "--json"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var status struct {
		Paired            bool `json:"paired"`
		DeviceTokenStored bool `json:"device_token_stored"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &status); err != nil {
		t.Fatalf("nexus status --json returned invalid JSON: %v", err)
	}
	if status.Paired || status.DeviceTokenStored || strings.Contains(stdout.String(), "device_token\"") {
		t.Fatalf("unexpected unpaired status: %s", stdout.String())
	}
}

func TestDefaultRuntimeRootUsesEnvironmentOverride(t *testing.T) {
	runtimeRoot := t.TempDir()
	t.Setenv("AGENTDOCK_RUNTIME_ROOT", runtimeRoot)
	if got := desktopruntime.DefaultRuntimeRoot(); got != runtimeRoot {
		t.Fatalf("default runtime root = %q, want %q", got, runtimeRoot)
	}
}

func TestRunTunnelHelp(t *testing.T) {
	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"tunnel", "-h"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if output := stdout.String(); !strings.Contains(output, "agentdock tunnel status") || !strings.Contains(output, "[--runtime-root <目录>]") {
		t.Fatalf("unexpected tunnel help: %s", output)
	}
}

func TestRunNexusHelpIncludesRuntimeRootAndNoRestart(t *testing.T) {
	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"nexus", "--help"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	output := stdout.String()
	for _, want := range []string{"--runtime-root", "--no-restart", "--json"} {
		if !strings.Contains(output, want) {
			t.Fatalf("nexus help missing %q: %s", want, output)
		}
	}
}

func TestMaybeRestartLinuxServiceAfterPairRespectsNoRestartAndHomeMatch(t *testing.T) {
	serviceHome := t.TempDir()
	binding := desktopruntime.LinuxServiceBinding{
		Configured:     true,
		RuntimeRoot:    t.TempDir(),
		ServiceManager: "systemd",
		ServiceName:    "agentdock",
		AgentDockHome:  serviceHome,
	}

	restarted, msg, hint := maybeRestartLinuxServiceAfterPair(context.Background(), serviceHome, binding, true)
	if restarted || msg != "" || hint != "" {
		t.Fatalf("--no-restart should skip restart cleanly, got restarted=%v msg=%q hint=%q", restarted, msg, hint)
	}

	otherHome := t.TempDir()
	restarted, msg, hint = maybeRestartLinuxServiceAfterPair(context.Background(), otherHome, binding, false)
	if restarted || msg != "" || hint != "" {
		t.Fatalf("mismatched agentdock-home should not restart unrelated service, got restarted=%v msg=%q hint=%q", restarted, msg, hint)
	}

	// 当没有合法的重启目标（如非 Linux 宿主机或测试临时目录缺少 agentdock.env）时，
	// 重启失败必须安全降级并给出手动命令，而不是阻断已成功落盘的配对结果。
	restarted, msg, hint = maybeRestartLinuxServiceAfterPair(context.Background(), serviceHome, binding, false)
	if restarted || hint != "sudo systemctl restart agentdock" || !strings.Contains(msg, "sudo systemctl restart agentdock") {
		t.Fatalf("expected graceful downgrade with manual hint, got restarted=%v msg=%q hint=%q", restarted, msg, hint)
	}
}

func TestNexusPairWritesJSONWithManualRestartHintOnRestartFailure(t *testing.T) {
	serviceHome := t.TempDir()
	runtimeRoot := t.TempDir()
	if runtime.GOOS == "linux" {
		if err := os.WriteFile(filepath.Join(runtimeRoot, "desktop-runtime.json"), []byte(`{
  "schema_version": 1,
  "service_manager": "systemd",
  "service_name": "agentdock-e2e-missing",
  "agentdock_binary": "/usr/local/bin/agentdock",
  "environment_file": "`+filepath.Join(runtimeRoot, "agentdock.env")+`"
}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(runtimeRoot, "agentdock.env"), []byte("AGENTDOCK_HOME='"+serviceHome+"'\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/nodes/pair" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"node":{"id":"node_cli_test"},"device_token":"secret-token"}`))
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	args := []string{
		"nexus", "pair",
		"--endpoint", server.URL,
		"--code", "pair_1234",
		"--runtime-root", runtimeRoot,
		"--json",
	}
	if runtime.GOOS != "linux" {
		args = append(args, "--agentdock-home", serviceHome)
	}
	if err := run(context.Background(), args, &stdout, &stderr); err != nil {
		t.Fatalf("nexus pair should succeed even when restart cannot execute: %v (stderr=%s)", err, stderr.String())
	}

	var parsed nexusPairJSONOutput
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid json output: %v (%s)", err, stdout.String())
	}
	if !parsed.Paired || parsed.NodeID != "node_cli_test" || parsed.AgentDockHome != serviceHome {
		t.Fatalf("unexpected pair output: %#v", parsed)
	}
	if runtime.GOOS == "linux" {
		if parsed.ServiceRestarted || parsed.ManualRestartHint != "sudo systemctl restart agentdock-e2e-missing" {
			t.Fatalf("expected manual restart hint on linux failure, got %#v", parsed)
		}
	}
}

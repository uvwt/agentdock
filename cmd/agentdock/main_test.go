package main

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestRunServiceLaunchCoreRequiresRuntimeRoot(t *testing.T) {
	if runtime.GOOS == "darwin" {
		if desktopruntime.DefaultRuntimeRoot() == "" {
			t.Fatal("macOS App 内部 launch-core 必须能解析默认 runtime root")
		}
		return
	}
	err := run(context.Background(), []string{"service", "launch-core"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--runtime-root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunTunnelRequiresRuntimeRoot(t *testing.T) {
	err := run(context.Background(), []string{"tunnel", "start"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--runtime-root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

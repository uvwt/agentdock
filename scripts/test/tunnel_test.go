package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuickTunnelParsingStaysInRuntime(t *testing.T) {
	const marker = "Your quick Tunnel has been created! Visit it at"
	tests := []struct {
		path      string
		wantCount int
	}{
		// Installer no longer waits for or parses public readiness; it starts Tunnel asynchronously.
		{path: "../install/install.ps1", wantCount: 0},
		// Runtime remains the single authority for Quick URL parsing and requires the success marker.
		{path: "../../internal/desktopruntime/quick_tunnel_log.go", wantCount: 1},
	}

	for _, tt := range tests {
		t.Run(filepath.Base(tt.path), func(t *testing.T) {
			data, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("read %s: %v", tt.path, err)
			}
			if got := strings.Count(string(data), marker); got != tt.wantCount {
				t.Fatalf("%s Quick Tunnel parser marker count = %d, want %d", tt.path, got, tt.wantCount)
			}
		})
	}
}

func TestWindowsTunnelLifecycleTestsIsolateAgentDockHome(t *testing.T) {
	for _, name := range []string{
		"test-windows-quick-tunnel-lifecycle.ps1",
		"test-windows-named-tunnel-lifecycle.ps1",
	} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			content := string(data)
			for _, want := range []string{
				"$oldHome = $env:AGENTDOCK_HOME",
				"$oldDefaultDir = $env:AGENTDOCK_DEFAULT_DIR",
				"$env:AGENTDOCK_HOME = Join-Path $root '.agentdock'",
				"$env:AGENTDOCK_DEFAULT_DIR = Join-Path $root 'workspace'",
				"$env:AGENTDOCK_HOME = $oldHome",
				"$env:AGENTDOCK_DEFAULT_DIR = $oldDefaultDir",
			} {
				if !strings.Contains(content, want) {
					t.Fatalf("%s must isolate the lifecycle fixture from the developer's AgentDock state; missing %q", name, want)
				}
			}
		})
	}
}

func TestDesktopControlSurfacesCanRefreshQuickTunnel(t *testing.T) {
	checks := map[string][]string{
		filepath.Join("..", "..", "desktop", "windows", "control-panel", "MainWindow.xaml.cs"): {
			"RegenerateQuickButton_Click",
			"RegenerateQuickTunnelAsync",
			`UiText.Get("OldAddressHidden")`,
			"PublicMcpTextBox.Text = \"\"",
		},
		filepath.Join("..", "..", "desktop", "macos", "AgentDockApp", "Sources", "SetupWindowController.swift"): {
			"refreshingQuickTunnel",
			`L10n.text("Regenerate temporary address")`,
			`L10n.text("Generating a new temporary public address…")`,
		},
	}
	for path, required := range checks {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		content := string(data)
		for _, want := range required {
			if !strings.Contains(content, want) {
				t.Fatalf("%s missing Quick Tunnel refresh behavior %q", path, want)
			}
		}
	}
}

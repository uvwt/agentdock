package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuickTunnelParsersRequireCloudflaredSuccessMarker(t *testing.T) {
	const marker = "Your quick Tunnel has been created! Visit it at"
	tests := []struct {
		path      string
		wantCount int
	}{
		// Windows 兼容 launcher 已委托原生 desktopruntime，不再维护第二份 Quick URL parser。
		{path: "../install/install.ps1", wantCount: 1},
		{path: "../install/install-macos-platform.sh", wantCount: 1},
		{path: "../install/install-linux-platform.sh", wantCount: 1},
		// Windows 当前运行链路由原生 desktopruntime 解析，manage-windows 只保留委托入口。
		{path: "../../internal/desktopruntime/quick_tunnel_log.go", wantCount: 1},
	}

	for _, tt := range tests {
		t.Run(filepath.Base(tt.path), func(t *testing.T) {
			data, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("read %s: %v", tt.path, err)
			}
			if got := strings.Count(string(data), marker); got != tt.wantCount {
				t.Fatalf("%s must gate Quick Tunnel URL parsing on cloudflared success marker; marker count = %d, want %d", tt.path, got, tt.wantCount)
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

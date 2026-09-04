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
		{path: "../install/install.ps1", wantCount: 2},
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
			"旧地址已隐藏，正在启动新的 Quick Tunnel",
			"PublicMcpTextBox.Text = \"\"",
		},
		filepath.Join("..", "..", "desktop", "macos", "AgentDockApp", "Sources", "SetupWindowController.swift"): {
			"refreshingQuickTunnel",
			"重新生成临时地址",
			"正在生成新的临时公网地址",
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

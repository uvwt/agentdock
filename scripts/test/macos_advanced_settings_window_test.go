package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacOSNativeControlPanelCarriesAdvancedSettingsParity(t *testing.T) {
	path := filepath.Join("..", "..", "desktop", "macos", "AgentDockApp", "Sources", "NativeControlPanelWindowController.swift")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read NativeControlPanelWindowController.swift: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		"Public MCP mode",
		"applyTunnel(mode:",
		"pairNexus(endpoint:",
		"Browser connection",
		"Add custom Coding Agent",
		"Default Coding Agent",
		"Save and restart Runtime",
		"Interface language",
		"setCoreAutostart",
		"setMenuAutostart",
		"Authentication token",
		"OAuth password",
		"requestUpdate()",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("native macOS control panel missing parity contract %q", want)
		}
	}
	legacy := filepath.Join("..", "..", "desktop", "macos", "AgentDockApp", "Sources", "AdvancedSettingsWindowController.swift")
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("legacy macOS advanced settings page must be removed after SwiftUI parity")
	}
}

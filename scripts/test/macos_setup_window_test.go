package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacOSAdvancedConnectionGatesCloudflareBehindOptionalComponent(t *testing.T) {
	root := filepath.Join("..", "..", "desktop", "macos", "AgentDockApp")
	data, err := os.ReadFile(filepath.Join(root, "Sources", "NativeControlPanelWindowController.swift"))
	if err != nil {
		t.Fatalf("read NativeControlPanelWindowController.swift: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		`model.settingsPage == .advancedConnection`,
		`await model.refreshCloudflaredComponent()`,
		`SettingsSection(L10n.text("Cloudflare Tunnel"))`,
		`model.cloudflaredComponent.state == "broken"`,
		`L10n.text("Repair")`,
		`L10n.text("Install")`,
		`L10n.text("Uninstall")`,
		`if model.cloudflaredComponent.ready {`,
		`L10n.text("Temporary domain")`,
		`L10n.text("Fixed domain")`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("macOS advanced connection missing optional component contract %q", want)
		}
	}

	forbidden := []string{
		`SetupWindowController`,
	}
	for _, value := range forbidden {
		if strings.Contains(content, value) {
			t.Fatalf("active macOS control panel must not reference legacy setup controller %q", value)
		}
	}
}

func TestMacOSLegacySetupControllerRemoved(t *testing.T) {
	path := filepath.Join("..", "..", "desktop", "macos", "AgentDockApp", "Sources", "SetupWindowController.swift")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("legacy SetupWindowController must be removed; stat err=%v", err)
	}
}

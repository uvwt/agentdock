package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacOSAppearancePreferenceSupportsSystemLightDark(t *testing.T) {
	path := filepath.Join("..", "..", "desktop", "macos", "AgentDockApp", "Sources", "AppearancePreference.swift")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read AppearancePreference.swift: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		"case system",
		"case light",
		"case dark",
		"AgentDockUITheme",
		"NSApp.appearance = nil",
		"NSAppearance(named: .aqua)",
		"NSAppearance(named: .darkAqua)",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("native macOS appearance preference missing %q", want)
		}
	}
}

func TestMacOSNativeControlPanelCarriesAdvancedSettingsParity(t *testing.T) {
	path := filepath.Join("..", "..", "desktop", "macos", "AgentDockApp", "Sources", "NativeControlPanelWindowController.swift")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read NativeControlPanelWindowController.swift: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		`SettingsSection("AgentDock")`,
		"private var serviceLoaded:",
		"private var serviceHealthy:",
		"agentDockStatusText",
		"AgentDock is running, but the connection service is not ready.",
		"Advanced connection settings",
		"Local MCP, access credentials, and direct public access.",
		"applyTunnel(mode:",
		"model.applyTunnel(mode: .quick",
		"model.applyTunnel(mode: .named",
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
		`case .runtime: return L10n.text("Runtime")`,
		`case .permissions: return L10n.text("Permissions")`,
		"setLanguagePreference(_ preference:",
		"case runtime, permissions, startup, appearance, about",
		`case .appearance: return L10n.text("Appearance")`,
		"case .about: return L10n.text(\"About\")",
		"case .appearance:",
		"case .about:",
		"Text(AppVersion.current)",
		"UIThemePreference.allCases",
		"model.setThemePreference(preference)",
		"https://uvwt.github.io/agentdock-docs/",
		"https://github.com/uvwt/agentdock",
		".toggleStyle(.switch)",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("native macOS control panel missing parity contract %q", want)
		}
	}
	for _, forbidden := range []string{
		`SettingsSection(L10n.text("Runtime status"))`,
		"case .credentials",
		"L10n.text(\"Public MCP mode\")",
		"L10n.text(\"Local only\")",
		"SettingsRow(L10n.text(\"Local address\"))",
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("native macOS control panel still exposes retired connection UI %q", forbidden)
		}
	}

	legacy := filepath.Join("..", "..", "desktop", "macos", "AgentDockApp", "Sources", "AdvancedSettingsWindowController.swift")
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("legacy macOS advanced settings page must be removed after SwiftUI parity")
	}
}

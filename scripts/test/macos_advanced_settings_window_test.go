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
		`SettingsSection(L10n.text("Connection"))`,
		`SettingsSection(L10n.text("Capabilities"))`,
		"private enum HomeCapabilityState",
		"private struct HomeCapabilityIndicator",
		"private var serviceLoaded:",
		"private var serviceHealthy:",
		"agentDockStatusText",
		"capabilityItems",
		"capabilitySummary",
		`L10n.format("%d / 3 available", enabled)`,
		`L10n.format("%d need attention", attention)`,
		`L10n.text("All available")`,
		`L10n.text("Not enabled yet")`,
		"This device is ready for AI.",
		"AgentDock is running, but the connection service is not ready.",
		"Advanced connection settings",
		`case .advancedConnection: return L10n.text("Advanced connection")`,
		"model.settingsPage = .advancedConnection",
		`SettingsSection(L10n.text("Remote connection"))`,
		`Self.officialEndpoint`,
		`L10n.text("Self-hosted service")`,
		"Local MCP, access credentials, and direct public access.",
		"applyTunnel(mode:",
		"model.applyTunnel(mode: .quick",
		"model.applyTunnel(mode: .named",
		"pairNexus(endpoint:",
		"Browser connection",
		"Add custom Coding Agent",
		"Default Coding Agent",
		"Adjust local AgentDock runtime settings.",
		"Changing runtime settings will automatically restart AgentDock.",
		"Apply changes",
		"Interface language",
		"setCoreAutostart",
		"setMenuAutostart",
		"Authentication token",
		"OAuth password",
		"requestUpdate()",
		`case .runtime: return L10n.text("Runtime")`,
		`case .permissions: return L10n.text("Permissions")`,
		"setLanguagePreference(_ preference:",
		"case runtime, permissions, startup, advancedConnection, appearance, about",
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
		`SettingsSection("NexusDock")`,
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("native macOS control panel still exposes retired connection UI %q", forbidden)
		}
	}

	runtimeStart := strings.Index(content, "case .runtime:")
	if runtimeStart < 0 {
		t.Fatal("native macOS Runtime settings block not found")
	}
	permissionsStart := strings.Index(content[runtimeStart:], "case .permissions:")
	if permissionsStart < 0 {
		t.Fatal("native macOS Permissions settings block not found")
	}
	runtimeBlock := content[runtimeStart : runtimeStart+permissionsStart]
	for _, forbidden := range []string{
		`SettingsRow(L10n.text("Status"))`,
		`SettingsRow(L10n.text("Service"))`,
		`Button(model.status.loaded ? L10n.text("Stop") : L10n.text("Start"))`,
		"Save and restart Runtime",
	} {
		if strings.Contains(runtimeBlock, forbidden) {
			t.Fatalf("native macOS Runtime settings still exposes duplicate service control %q", forbidden)
		}
	}

	legacy := filepath.Join("..", "..", "desktop", "macos", "AgentDockApp", "Sources", "AdvancedSettingsWindowController.swift")
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("legacy macOS advanced settings page must be removed after SwiftUI parity")
	}
}

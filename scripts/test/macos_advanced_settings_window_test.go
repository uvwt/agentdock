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
		"AgentDockLogoView(size: 20)",
		"AgentDockLogoView(size: 48)",
		"AgentDockLogoView(size: 40)",
		"private enum HomeCapabilityState",
		"private struct HomeMetric",
		"private struct HomeCapabilityRow",
		"private struct HomeRecentActivityRow",
		"private var serviceLoaded:",
		"private var serviceHealthy:",
		"agentDockStatusText",
		"capabilityItems",
		`SettingsSection(L10n.text("Core capabilities"))`,
		`SettingsSection(L10n.text("Recent activity"))`,
		"model.dashboard.skillCount",
		"model.dashboard.mcpCount",
		"model.dashboard.pluginCount",
		"model.dashboard.recentCalls.prefix(3)",
		"model.page = .activity",
		"await model.refreshDashboard()",
		"This device is ready for AI.",
		"AgentDock is running, but the connection service is not ready.",
		"Advanced connection settings",
		`case .advancedConnection: return L10n.text("Advanced connection")`,
		"model.settingsPage = .advancedConnection",
		`SettingsSection(L10n.text("Remote connection"))`,
		`Self.officialEndpoint`,
		`L10n.text("Self-hosted service")`,
		"https://mcp.nexusdock.co/workspace/devices",
		`L10n.text("No pairing code? Get one from NexusDock ↗")`,
		`L10n.text("Manage connected devices ↗")`,
		"nexusDevice.paired && !rePairing",
		"L10n.text(\"Re-pair\")",
		"Local MCP, access credentials, and direct public access.",
		"applyTunnel(mode:",
		"model.applyTunnel(mode: .quick",
		"model.applyTunnel(mode: .named",
		"pairNexus(endpoint:",
		"Browser connection",
		"Add custom Coding Agent",
		"Default Coding Agent",
		"Apply changes",
		"L10n.text(\"Custom port\")",
		"L10n.text(\"Service port\")",
		"Changing the service port will restart AgentDock.",
		"DisclosureGroup(isExpanded: $showCustomPort)",
		"Interface language",
		"setCoreAutostart",
		"setMenuAutostart",
		"Authentication token",
		"OAuth password",
		"requestUpdate()",
		`case .permissions: return L10n.text("Permissions")`,
		"setLanguagePreference(_ preference:",
		"case appearance, permissions, startup, logs, advancedConnection, about",
		`case .logs: return L10n.text("Logs")`,
		`case .appearance: return L10n.text("Appearance")`,
		"case .about: return L10n.text(\"About\")",
		"case .appearance:",
		"case .about:",
		"Text(AppVersion.current)",
		"UIThemePreference.allCases",
		"model.setThemePreference(preference)",
		"https://nexusdock.co/",
		"https://docs.nexusdock.co/agentdock/",
		"https://github.com/uvwt/agentdock",
		".toggleStyle(.switch)",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("native macOS control panel missing parity contract %q", want)
		}
	}
	for _, forbidden := range []string{
		"NSApp.applicationIconImage",
		"case .runtime:",
		"settingsPage: SettingsPage = .runtime",
		"Adjust local AgentDock runtime settings.",
		`L10n.text("Runtime settings")`,
		`L10n.format("%d / 3 available", enabled)`,
		"capabilitySummary",
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

	legacy := filepath.Join("..", "..", "desktop", "macos", "AgentDockApp", "Sources", "AdvancedSettingsWindowController.swift")
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("legacy macOS advanced settings page must be removed after SwiftUI parity")
	}
}

func TestMacOSDashboardUsesRuntimeOverviewAsSingleCountSource(t *testing.T) {
	root := filepath.Join("..", "..", "desktop", "macos", "AgentDockApp", "Sources")
	serviceData, err := os.ReadFile(filepath.Join(root, "ServiceController.swift"))
	if err != nil {
		t.Fatalf("read ServiceController.swift: %v", err)
	}
	service := string(serviceData)
	for _, want := range []string{
		`path: "/internal/runtime/overview"`,
		`path: "/internal/runtime/diagnostics"`,
		"countsAvailable: overviewPayload != nil",
		"skillCount: overviewPayload?.skills.count ?? 0",
		"mcpCount: overviewPayload?.mcp.count ?? 0",
		"pluginCount: overviewPayload?.plugins.count ?? 0",
	} {
		if !strings.Contains(service, want) {
			t.Fatalf("native macOS dashboard missing Runtime Overview contract %q", want)
		}
	}
	for _, forbidden := range []string{
		"RuntimeListCountPayload",
		`path: "/internal/runtime/skills"`,
		`path: "/internal/runtime/mcp"`,
		`path: "/internal/runtime/plugins"`,
	} {
		if strings.Contains(service, forbidden) {
			t.Fatalf("native macOS dashboard still contains legacy count fallback %q", forbidden)
		}
	}

	windowData, err := os.ReadFile(filepath.Join(root, "NativeControlPanelWindowController.swift"))
	if err != nil {
		t.Fatalf("read NativeControlPanelWindowController.swift: %v", err)
	}
	if !strings.Contains(string(windowData), "model.dashboard.countsAvailable") {
		t.Fatal("native macOS dashboard must render counts only when Runtime Overview is available")
	}
}

func TestMacOSActivityOwnsRuntimeAnalytics(t *testing.T) {
	base := filepath.Join("..", "..", "desktop", "macos", "AgentDockApp", "Sources")
	controlPanelData, err := os.ReadFile(filepath.Join(base, "NativeControlPanelWindowController.swift"))
	if err != nil {
		t.Fatalf("read NativeControlPanelWindowController.swift: %v", err)
	}
	serviceData, err := os.ReadFile(filepath.Join(base, "ServiceController.swift"))
	if err != nil {
		t.Fatalf("read ServiceController.swift: %v", err)
	}
	appDelegateData, err := os.ReadFile(filepath.Join(base, "AppDelegate.swift"))
	if err != nil {
		t.Fatalf("read AppDelegate.swift: %v", err)
	}
	content := string(controlPanelData) + string(serviceData) + string(appDelegateData)

	for _, want := range []string{
		"presentActivity(status:",
		"runtimeAnalytics(configuration:",
		`path: "/internal/runtime/analytics"`,
		`decodeIfPresent([RuntimeAnalyticsStage].self, forKey: .stages) ?? []`,
		"RuntimeActivityCallRow",
		"private var hasDetails: Bool",
		".frame(maxWidth: .infinity, alignment: .leading)",
		`SettingsSection(L10n.text("Recent calls"))`,
		`case .logs:`,
		`SettingsSection(L10n.text("Logs"))`,
		`L10n.text("Log level")`,
		`.onChange(of: logLevel)`,
		`applyLogLevel(value)`,
		`L10n.text("Advanced diagnostics")`,
		`L10n.text("Copy diagnostics")`,
		"recentP95(analytics)",
		"private static let pageSize = 20",
		"5_000_000_000",
		`L10n.text("Show more")`,
		"nextLatestID != currentLatestID",
		`case .activity: ActivityView(model: model)`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("macOS Activity/logs integration missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"openRuntimeAnalytics",
		`components.path = "/analytics"`,
		`SettingsSection(L10n.text("Call overview"))`,
		`SettingsSection(L10n.text("Call statistics"))`,
		`SettingsSection(L10n.text("Runtime resources"))`,
		`Button(L10n.text("Apply changes")) { applyLogLevel`,
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("macOS retained retired analytics UI %q", forbidden)
		}
	}
}

func TestMacOSUsesVectorBrandLogoAndTemplateMenuBarIcon(t *testing.T) {
	base := filepath.Join("..", "..", "desktop", "macos", "AgentDockApp", "Sources")
	logoData, err := os.ReadFile(filepath.Join(base, "AgentDockLogoArtwork.swift"))
	if err != nil {
		t.Fatalf("read AgentDockLogoArtwork.swift: %v", err)
	}
	logo := string(logoData)
	for _, want := range []string{
		"enum AgentDockLogoArtwork",
		"struct AgentDockLogoView: View",
		"Canvas { context, canvasSize in",
		"CGMutablePath()",
		"static func menuBarImage() -> NSImage",
		"image.isTemplate = true",
		"green: 226.0 / 255.0",
		"green: 142.0 / 255.0",
	} {
		if !strings.Contains(logo, want) {
			t.Fatalf("macOS vector brand logo missing %q", want)
		}
	}

	appDelegateData, err := os.ReadFile(filepath.Join(base, "AppDelegate.swift"))
	if err != nil {
		t.Fatalf("read AppDelegate.swift: %v", err)
	}
	appDelegate := string(appDelegateData)
	if !strings.Contains(appDelegate, "button.image = AgentDockLogoArtwork.menuBarImage()") {
		t.Fatal("macOS menu bar must use the AgentDock vector template image")
	}
	if strings.Contains(appDelegate, "shippingbox.fill") {
		t.Fatal("macOS menu bar must not fall back to the generic shippingbox symbol")
	}

	buildScript, err := os.ReadFile(filepath.Join("..", "..", "packaging", "macos", "build-app.sh"))
	if err != nil {
		t.Fatalf("read build-app.sh: %v", err)
	}
	if strings.Contains(string(buildScript), "AgentDockLogo.png") {
		t.Fatal("macOS control panel logo must not be packaged as a raster PNG")
	}
}

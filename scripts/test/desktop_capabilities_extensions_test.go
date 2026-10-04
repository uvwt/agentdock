package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readCapabilitySource(t *testing.T, parts ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{"..", ".."}, parts...)...)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestDesktopCapabilitiesDoNotDuplicateExtensionSummary(t *testing.T) {
	macView := readCapabilitySource(t, "desktop", "macos", "AgentDockApp", "Sources", "NativeControlPanelWindowController.swift")
	macService := readCapabilitySource(t, "desktop", "macos", "AgentDockApp", "Sources", "ServiceController.swift")
	winView := readCapabilitySource(t, "desktop", "windows", "winui", "CapabilitiesPage.xaml")
	winCode := readCapabilitySource(t, "desktop", "windows", "winui", "CapabilitiesPage.xaml.cs")
	winService := readCapabilitySource(t, "desktop", "windows", "shared", "Services", "RuntimeService.cs")

	for _, forbidden := range []string{
		"SettingsSection(L10n.text(\"Extensions\"))",
		"runtimeExtensionOverview(configuration: configuration)",
	} {
		if strings.Contains(macView, forbidden) {
			t.Fatalf("macOS capabilities must not duplicate extension summary %q", forbidden)
		}
	}
	for _, forbidden := range []string{
		"x:Name=\"ExtensionsSection\"",
		"x:Name=\"SkillsValue\"",
		"x:Name=\"PluginsValue\"",
		"x:Name=\"McpServersValue\"",
	} {
		if strings.Contains(winView, forbidden) {
			t.Fatalf("WinUI capabilities must not duplicate extension summary %q", forbidden)
		}
	}
	for _, forbidden := range []string{
		"GetRuntimeExtensionOverviewAsync(snapshot.LocalMcpUrl)",
		"UiText.Get(\"Extensions\")",
	} {
		if strings.Contains(winCode, forbidden) {
			t.Fatalf("WinUI capabilities must not fetch extension summary %q", forbidden)
		}
	}

	for _, want := range []string{
		"components.path = \"/internal/runtime/overview\"",
		"forHTTPHeaderField: \"Authorization\"",
		"RuntimeExtensionOverview",
	} {
		if !strings.Contains(macService, want) {
			t.Fatalf("macOS RuntimeOverview bridge missing %q", want)
		}
	}
	for _, want := range []string{
		"Path = \"/internal/runtime/overview\"",
		"\"Authorization\", \"Bearer \" + bearer",
		"RuntimeExtensionOverview",
	} {
		if !strings.Contains(winService, want) {
			t.Fatalf("Windows RuntimeOverview bridge missing %q", want)
		}
	}
}

func TestDesktopExtensionSummaryDoesNotCreateASecondManagementCenter(t *testing.T) {
	macView := readCapabilitySource(t, "desktop", "macos", "AgentDockApp", "Sources", "NativeControlPanelWindowController.swift")
	winView := readCapabilitySource(t, "desktop", "windows", "winui", "CapabilitiesPage.xaml")
	winCode := readCapabilitySource(t, "desktop", "windows", "winui", "CapabilitiesPage.xaml.cs")
	winWindow := readCapabilitySource(t, "desktop", "windows", "winui", "MainWindow.xaml")

	for _, forbidden := range []string{"ExtensionsNavigationItem", "SkillsNavigationItem", "PluginsNavigationItem", "McpNavigationItem"} {
		if strings.Contains(winWindow, forbidden) {
			t.Fatalf("extensions must not become a new top-level WinUI navigation item: %s", forbidden)
		}
	}
	for _, forbidden := range []string{"RuntimeSkill(", "RuntimePlugin(", "RuntimeMCPManage", "env_set", "authorize"} {
		if strings.Contains(macView, forbidden) || strings.Contains(winCode, forbidden) || strings.Contains(winView, forbidden) {
			t.Fatalf("desktop capabilities summary must stay read-only and lightweight: found %q", forbidden)
		}
	}
}

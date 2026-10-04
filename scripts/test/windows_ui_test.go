package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readWindowsNativeFile(t *testing.T, parts ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{"..", "..", "desktop", "windows"}, parts...)...)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestWindowsNativeControlPanelReplacesWPFAndCarriesPlatformLifecycle(t *testing.T) {
	app := readWindowsNativeFile(t, "winui", "App.xaml.cs")
	window := readWindowsNativeFile(t, "winui", "MainWindow.xaml.cs")
	project := readWindowsNativeFile(t, "winui", "AgentDock.WinUI.csproj")
	for _, want := range []string{
		"new Mutex(true, MutexName",
		"EventWaitHandle",
		"Forms.NotifyIcon",
		"RunElevatedNativeCommandHostAsync",
		"RunCoreStartupAsync",
		"RunTunnelStartupAsync",
		"ResumeUpdateProgressIfNeededAsync",
		"AcknowledgeUpdateUiHandoffAsync",
	} {
		if !strings.Contains(app, want) {
			t.Fatalf("WinUI app lifecycle missing %q", want)
		}
	}
	for _, want := range []string{"AppWindow.Closing", "args.Cancel = true", "AppWindow.Hide()", "ShowAndActivate", "SetForegroundWindow"} {
		if !strings.Contains(window, want) {
			t.Fatalf("WinUI window lifecycle missing %q", want)
		}
	}
	for _, want := range []string{"<AssemblyName>agentdock-tray</AssemblyName>", "Microsoft.WindowsAppSDK", "../shared/Services/RuntimeService.cs"} {
		if !strings.Contains(project, want) {
			t.Fatalf("WinUI project missing %q", want)
		}
	}
	if _, err := os.Stat(filepath.Join("..", "..", "desktop", "windows", "control-panel")); !os.IsNotExist(err) {
		t.Fatal("legacy WPF control-panel directory must be removed after WinUI parity")
	}
}

func TestWindowsNativeControlPanelCarriesSettingsParity(t *testing.T) {
	settings := readWindowsNativeFile(t, "winui", "SettingsPage.xaml.cs")
	connections := readWindowsNativeFile(t, "winui", "ConnectionsPage.xaml.cs")
	capabilities := readWindowsNativeFile(t, "winui", "CapabilitiesPage.xaml.cs")
	for _, want := range []string{
		"SetPrivilegeModeAsync", "SetStartupAsync", "ReadBearerToken", "ReadOAuthPassword",
		"CheckForUpdatesAsync", "RunUpdateAsync", "UiText.SetPreference", "SaveSettingsAsync",
	} {
		if !strings.Contains(settings, want) {
			t.Fatalf("WinUI settings parity missing %q", want)
		}
	}
	for _, want := range []string{"SetTunnelModeAsync", "RegenerateQuickTunnelAsync", "PairNexusAsync", "GetSnapshotAsync(includeNexusConnection: true)"} {
		if !strings.Contains(connections, want) {
			t.Fatalf("WinUI connections parity missing %q", want)
		}
	}
	for _, want := range []string{
		"BrowserEnabled", "BrowserReuseExistingCdp", "AcpEnabled", "AcpProfiles",
		"ResolveAcpAdapter", "AddCustomProfile_Click", "AcpDefaultProfile", "McpAppsMode",
	} {
		if !strings.Contains(capabilities, want) {
			t.Fatalf("WinUI capabilities parity missing %q", want)
		}
	}
}

func TestWindowsSharedRuntimeKeepsDiagnosticsAndDynamicLocalization(t *testing.T) {
	runtime := readWindowsNativeFile(t, "shared", "Services", "RuntimeService.cs")
	diagnostics := readWindowsNativeFile(t, "shared", "Services", "ControlPanelDiagnostics.cs")
	uiText := readWindowsNativeFile(t, "shared", "UiText.cs")
	for _, want := range []string{
		"RunRuntimeStageAsync", "--run-elevated-agentdock", "ControlPanelDiagnostics.CreateRequestLease",
		"ControlPanelDiagnostics.ReadResult", "Environment.ProcessPath",
	} {
		if !strings.Contains(runtime, want) {
			t.Fatalf("shared RuntimeService missing %q", want)
		}
	}
	for _, want := range []string{"Guid.TryParseExact", "SHA256.HashData", "CryptographicOperations.FixedTimeEquals", "control-panel.err.log"} {
		if !strings.Contains(diagnostics, want) {
			t.Fatalf("shared diagnostics missing %q", want)
		}
	}
	for _, want := range []string{"ui-language", "File.Delete(PreferencePath)", "Resources.GetString(key, _resourceCulture)"} {
		if !strings.Contains(uiText, want) {
			t.Fatalf("shared localization missing %q", want)
		}
	}
}

func TestWindowsNativePagesUseLiveRuntimeState(t *testing.T) {
	home := readWindowsNativeFile(t, "winui", "HomePage.xaml.cs")
	activity := readWindowsNativeFile(t, "winui", "ActivityPage.xaml.cs")
	connections := readWindowsNativeFile(t, "winui", "ConnectionsPage.xaml.cs")
	for _, pair := range []struct{ content, want string }{
		{home, "GetSnapshotAsync"},
		{activity, "GetSnapshotAsync"},
		{connections, "NexusConnected"},
	} {
		if !strings.Contains(pair.content, pair.want) {
			t.Fatalf("native page missing live Runtime contract %q", pair.want)
		}
	}
}

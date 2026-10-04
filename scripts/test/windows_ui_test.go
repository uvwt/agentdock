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
		"SetPrivilegeModeAsync", "SetStartupAsync",

		"CheckForUpdatesAsync", "RunUpdateAsync", "LanguagePreference_SelectionChanged", "ThemePreference_SelectionChanged", "SaveSettingsAsync",
		"SetTunnelModeAsync", "RegenerateQuickTunnelAsync", "ReadBearerToken", "ReadOAuthPassword",
	} {
		if !strings.Contains(settings, want) {
			t.Fatalf("WinUI settings parity missing %q", want)
		}
	}
	for _, want := range []string{
		"PairNexusAsync",
		"GetSnapshotAsync(includeNexusConnection: true)",
		"https://mcp.nexusdock.co/workspace/devices",
		`UiText.Get("GetPairingCodeFromNexusDock")`,
		`UiText.Get("ManageConnectedDevices")`,
		`NexusDevicesLink.Visibility = SelectedRemoteService() == "official"`,
		"IsOfficialEndpoint(_snapshot.Nexus.Endpoint)",
	} {
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

func TestWindowsPermissionsAvoidDuplicateAdministratorStatus(t *testing.T) {
	settings := readWindowsNativeFile(t, "winui", "SettingsPage.xaml.cs")
	en := readWindowsNativeFile(t, "shared", "Resources", "UiStrings.resx")
	zh := readWindowsNativeFile(t, "shared", "Resources", "UiStrings.zh-CN.resx")

	for _, want := range []string{
		`UiText.Get("RunCoreElevated")`,
		"PrivilegeToggle_Toggled",
		"SetPrivilegeModeAsync",
	} {
		if !strings.Contains(settings, want) {
			t.Fatalf("WinUI Permissions elevated toggle missing %q", want)
		}
	}
	if strings.Contains(settings, `UiText.Get("AdministratorMode")`) {
		t.Fatal("WinUI Permissions must not repeat elevated state as a separate Administrator mode row")
	}
	for _, resource := range []string{en, zh} {
		if strings.Contains(resource, `name="AdministratorMode"`) {
			t.Fatal("retired AdministratorMode localization resource must be removed")
		}
	}
}

func TestWindowsNativeLanguageAndShortcutsAreInteractive(t *testing.T) {
	app := readWindowsNativeFile(t, "winui", "App.xaml.cs")
	window := readWindowsNativeFile(t, "winui", "MainWindow.xaml.cs")
	settingsXaml := readWindowsNativeFile(t, "winui", "SettingsPage.xaml")
	settingsCode := readWindowsNativeFile(t, "winui", "SettingsPage.xaml.cs")
	homeXaml := readWindowsNativeFile(t, "winui", "HomePage.xaml")
	homeCode := readWindowsNativeFile(t, "winui", "HomePage.xaml.cs")

	for _, want := range []string{
		"ApplyLanguagePreference",
		"UiText.SetPreference(preference)",
		`new MainWindow(_runtime, "settings", "appearance")`,
	} {
		if !strings.Contains(app, want) {
			t.Fatalf("WinUI language reload missing %q", want)
		}
	}
	for _, want := range []string{
		"RuntimeNavigationItem.Content = UiText.Get(\"Runtime\")",
		"PermissionsNavigationItem.Content = UiText.Get(\"Permissions\")",
		"StartupNavigationItem.Content = UiText.Get(\"Startup\")",
		"AppearanceNavigationItem.Content = UiText.Get(\"Appearance\")",
		"LanguagePreference_SelectionChanged",
	} {
		if !strings.Contains(settingsCode, want) {
			t.Fatalf("WinUI settings localization missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"Content=\"Runtime\" Tag=\"runtime\"",
		"Content=\"Permissions\" Tag=\"permissions\"",
		"Content=\"Access Credentials\" Tag=\"credentials\"",
		"CredentialsNavigationItem",
	} {
		if strings.Contains(settingsXaml, forbidden) {
			t.Fatalf("WinUI settings still hardcodes secondary navigation label %q", forbidden)
		}
	}
	for _, want := range []string{
		"Click=\"ConnectionsShortcut_Click\"",
		"Click=\"CapabilitiesShortcut_Click\"",
	} {
		if !strings.Contains(homeXaml, want) {
			t.Fatalf("WinUI home shortcut missing click contract %q", want)
		}
	}
	for _, want := range []string{
		"ShortcutRequested?.Invoke(this, \"connections\")",
		"ShortcutRequested?.Invoke(this, \"capabilities\")",
	} {
		if !strings.Contains(homeCode, want) {
			t.Fatalf("WinUI home shortcut missing navigation request %q", want)
		}
	}
	for _, want := range []string{"ContentFrame_Navigated", "home.ShortcutRequested", "NavigateTo(tag)"} {
		if !strings.Contains(window, want) {
			t.Fatalf("WinUI host shortcut routing missing %q", want)
		}
	}
}

func TestWindowsRuntimeSettingsAvoidDuplicateServiceStatus(t *testing.T) {
	settings := readWindowsNativeFile(t, "winui", "SettingsPage.xaml.cs")

	for _, want := range []string{
		`UiText.Get("Port")`,
		`UiText.Get("LogLevel")`,
		`UiText.Get("ApplyChanges")`,
		`UiText.Get("RuntimeRestartHint")`,
		`await _runtime.SaveSettingsAsync(_snapshot.Settings)`,
		`await _runtime.RunCoreActionAsync("restart")`,
	} {
		if !strings.Contains(settings, want) {
			t.Fatalf("WinUI Runtime settings missing %q", want)
		}
	}
	for _, forbidden := range []string{
		`rows.Children.Add(Row(UiText.Get("Status")`,
		`ActionRow(UiText.Get("Service")`,
		"RuntimeAction_Click",
		`UiText.Get("SaveAndRestart")`,
	} {
		if strings.Contains(settings, forbidden) {
			t.Fatalf("WinUI Runtime settings still exposes duplicate service control %q", forbidden)
		}
	}
}

func TestWindowsThemePreferencePersistsAndAppliesLive(t *testing.T) {
	app := readWindowsNativeFile(t, "winui", "App.xaml.cs")
	window := readWindowsNativeFile(t, "winui", "MainWindow.xaml.cs")
	settings := readWindowsNativeFile(t, "winui", "SettingsPage.xaml.cs")
	theme := readWindowsNativeFile(t, "winui", "UiThemePreference.cs")

	for _, want := range []string{
		"ThemePreference_SelectionChanged",
		"UiThemePreference.ReadPreference()",
		"UiThemePreference.SetPreference(preference)",
		"Navigation.RequestedTheme = UiThemePreference.ToElementTheme(preference)",
		"LightPreference = \"light\"",
		"DarkPreference = \"dark\"",
		"ElementTheme.Light",
		"ElementTheme.Dark",
	} {
		if !strings.Contains(app+window+settings+theme, want) {
			t.Fatalf("WinUI theme preference missing %q", want)
		}
	}
}

func TestWindowsAboutLivesInSettingsSidebarAndUsesExistingVersionSource(t *testing.T) {
	mainWindowXaml := readWindowsNativeFile(t, "winui", "MainWindow.xaml")
	settingsXaml := readWindowsNativeFile(t, "winui", "SettingsPage.xaml")
	settingsCode := readWindowsNativeFile(t, "winui", "SettingsPage.xaml.cs")

	for _, want := range []string{
		`x:Name="AboutNavigationItem" Tag="about"`,
		`AboutNavigationItem.Content = UiText.Get("About")`,
		`if (tag == "about")`,
		`SettingsContent.Children.Add(BuildAbout())`,
		`_snapshot.Version`,
		`update.Click += CheckUpdate_Click`,
		`https://uvwt.github.io/agentdock-docs/`,
		`https://github.com/uvwt/agentdock`,
	} {
		if !strings.Contains(settingsXaml+settingsCode, want) {
			t.Fatalf("WinUI Settings About page missing %q", want)
		}
	}
	if strings.Contains(mainWindowXaml, `x:Name="AboutNavigationItem" Tag="about"`) {
		t.Fatal("WinUI top-level navigation must not expose About")
	}
}

func TestWindowsRemoteConnectionSupportsSelfHostedAndRoutesAdvancedSettings(t *testing.T) {

	connectionsXaml := readWindowsNativeFile(t, "winui", "ConnectionsPage.xaml")
	connectionsCode := readWindowsNativeFile(t, "winui", "ConnectionsPage.xaml.cs")
	windowCode := readWindowsNativeFile(t, "winui", "MainWindow.xaml.cs")
	settingsXaml := readWindowsNativeFile(t, "winui", "SettingsPage.xaml")
	settingsCode := readWindowsNativeFile(t, "winui", "SettingsPage.xaml.cs")

	for _, want := range []string{
		"RemoteSection",
		"RemoteServiceComboBox",
		"OfficialServiceItem",
		"SelfHostedServiceItem",
		"SelfHostedEndpointTextBox",
		"AdvancedSettingsButton",
	} {
		if !strings.Contains(connectionsXaml, want) {
			t.Fatalf("WinUI remote connection UI missing %q", want)
		}
	}
	for _, want := range []string{
		`OfficialEndpoint = "https://mcp.nexusdock.co"`,
		`SelectedRemoteService() == "self-hosted"`,
		"PairNexusAsync",
		"AdvancedSettingsRequested?.Invoke",
	} {
		if !strings.Contains(connectionsCode, want) {
			t.Fatalf("WinUI remote connection behavior missing %q", want)
		}
	}
	if !strings.Contains(settingsXaml, "AdvancedConnectionNavigationItem") {
		t.Fatal("WinUI Settings must expose the Advanced connection secondary navigation item")
	}
	for _, want := range []string{
		"BuildAdvancedConnection",
		`SetTunnelModeAsync("quick", "", "")`,
		`SetTunnelModeAsync("named", serverUrl, token)`,
		"ReadBearerToken",
		"ReadOAuthPassword",
	} {
		if !strings.Contains(settingsCode, want) {
			t.Fatalf("WinUI advanced connection settings behavior missing %q", want)
		}
	}
	if !strings.Contains(windowCode, `NavigateToSettings("advancedConnection")`) {
		t.Fatal("WinUI advanced connection entry must route directly to Settings advanced connection")
	}
	for _, forbidden := range []string{
		"AdvancedConnectionExpander",
		"TunnelModeComboBox",
		"LocalOnlyModeItem",
		"PublicModeLabel",
		"Cloudflare Tunnel",
		"ReadBearerToken",
		"SetTunnelModeAsync",
	} {
		if strings.Contains(connectionsXaml, forbidden) || strings.Contains(connectionsCode, forbidden) {
			t.Fatalf("WinUI connection page still exposes technical advanced configuration %q", forbidden)
		}
	}
	if strings.Contains(settingsXaml, "CredentialsNavigationItem") || strings.Contains(settingsCode, "BuildCredentials") {
		t.Fatal("WinUI Settings must keep credentials inside Advanced connection instead of a standalone credentials page")
	}
}

func TestWindowsHomeUsesProductStatusAndAdaptiveCapabilities(t *testing.T) {
	homeXaml := readWindowsNativeFile(t, "winui", "HomePage.xaml")
	homeCode := readWindowsNativeFile(t, "winui", "HomePage.xaml.cs")
	runtime := readWindowsNativeFile(t, "shared", "Services", "RuntimeService.cs")

	for _, want := range []string{
		`HorizontalScrollMode="Disabled"`,
		`HorizontalScrollBarVisibility="Disabled"`,
		`Source="Assets/agentdock.png"`,
		`x:Name="AgentDockState"`,
		`x:Name="SkillCount"`,
		`x:Name="McpCount"`,
		`x:Name="PluginCount"`,
		`x:Name="CoreCapabilitiesSection"`,
		`x:Name="RecentActivitySection"`,
		`x:Name="RecentActivityPanel"`,
		`x:Name="BrowserCapabilityDetail"`,
		`x:Name="BrowserCapabilityState"`,
		`x:Name="CodingAgentCapabilityDetail"`,
		`x:Name="CodingAgentCapabilityState"`,
		`x:Name="McpAppsCapabilityDetail"`,
		`x:Name="McpAppsCapabilityState"`,
		"var serviceLoaded = _snapshot.CoreRunning",
		"var serviceHealthy = serviceLoaded && _snapshot.Healthy",
		`UiText.Get("DeviceReadyForAI")`,
		"RenderCapabilities(_snapshot.Settings)",
		"GetDashboardAsync(_snapshot)",
		"dashboard.RecentCalls.Take(3)",
		"settings.BrowserEnabled",
		"settings.AcpEnabled",
		"settings.AcpProfiles.Where(profile => profile.Enabled)",
		`"off" => CapabilityState.Disabled`,
		`"full" or "compact" => CapabilityState.Enabled`,
		`!string.IsNullOrWhiteSpace(_snapshot.Nexus.Error)`,
		`UiText.Get("Unavailable")`,
		`RuntimeAction.Content = serviceLoaded ? UiText.Get("Stop") : UiText.Get("Start")`,
		`"/internal/runtime/overview"`,
		`"/internal/runtime/diagnostics"`,
		"dashboard.CountsAvailable",
		`HorizontalAlignment="Stretch"`,
	} {
		if !strings.Contains(homeXaml+homeCode+runtime, want) {
			t.Fatalf("WinUI Home missing product status contract %q", want)
		}
	}
	for _, forbidden := range []string{
		`x:Name="RuntimeState"`,
		`x:Name="McpState"`,
		`x:Name="QuickAccessSection"`,
		`x:Name="CapabilitiesSummary"`,
		"CapabilitiesAvailable",
		"CapabilitiesNeedAttention",
		`"/internal/runtime/skills"`,
		`"/internal/runtime/mcp"`,
		`"/internal/runtime/plugins"`,
		"RuntimeListCountPayload",
		`<TextBlock Text="Runtime"`,
	} {
		if strings.Contains(homeXaml+homeCode+runtime, forbidden) {
			t.Fatalf("WinUI Home still exposes implementation status or old quick-access card %q", forbidden)
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

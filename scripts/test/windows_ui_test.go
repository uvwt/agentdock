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
	tray := readWindowsNativeFile(t, "winui", "TrayIconHost.cs")
	for _, want := range []string{
		"new Mutex(true, MutexName",
		"EventWaitHandle",
		"new TrayIconHost(",
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
	for _, want := range []string{"Shell_NotifyIconW", "TrackPopupMenuEx", "TaskbarCreated"} {
		if !strings.Contains(tray, want) {
			t.Fatalf("Win32 tray host missing %q", want)
		}
	}
	for _, want := range []string{"AppWindow.Closing", "args.Cancel = true", "AppWindow.Hide()", "ShowAndActivate", "SetForegroundWindow"} {
		if !strings.Contains(window, want) {
			t.Fatalf("WinUI window lifecycle missing %q", want)
		}
	}
	for _, want := range []string{
		"<AssemblyName>agentdock-tray</AssemblyName>",
		"Microsoft.WindowsAppSDK",
		"../shared/Services/RuntimeService.cs",
		`Name="CopyAgentDockWinUIResourcesToPublish"`,
		`Include="$(TargetDir)*.xbf"`,
		`Include="$(TargetDir)$(AssemblyName).pri"`,
		`Exists('$(PublishDir)MainWindow.xbf')`,
	} {
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
		"GetNexusConnectionSnapshotAsync()",
		"https://mcp.nexusdock.co/workspace/devices",
		`UiText.Get("GetPairingCodeFromNexusDock")`,
		`UiText.Get("ManageConnectedDevices")`,
		`NexusDevicesLink.Visibility = SelectedRemoteService() == "official"`,
		"IsOfficialEndpoint(_connection!.Nexus.Endpoint)",
		"UiText.Get(\"RePair\")",
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
		"RuntimeNavigationItem",
		"Tag=\"runtime\"",
		"Content=\"Runtime\" Tag=\"runtime\"",
		"Content=\"Permissions\" Tag=\"permissions\"",
		"Content=\"Access Credentials\" Tag=\"credentials\"",
		"CredentialsNavigationItem",
	} {
		if strings.Contains(settingsXaml, forbidden) {
			t.Fatalf("WinUI settings still hardcodes secondary navigation label %q", forbidden)
		}
	}
	for _, item := range []string{
		`x:Name="PermissionsNavigationItem"`,
		`x:Name="StartupNavigationItem"`,
		`x:Name="LogsNavigationItem"`,
		`x:Name="AdvancedConnectionNavigationItem"`,
		`x:Name="AppearanceNavigationItem"`,
		`x:Name="AboutNavigationItem"`,
	} {
		if got := strings.Count(settingsXaml, item); got != 1 {
			t.Fatalf("WinUI settings navigation %q appears %d times, want exactly once", item, got)
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

func TestWindowsPortSettingsLiveUnderAdvancedConnection(t *testing.T) {
	settings := readWindowsNativeFile(t, "winui", "SettingsPage.xaml.cs")

	for _, want := range []string{
		`UiText.Get("CustomPort")`,
		`UiText.Get("ServicePort")`,
		`UiText.Get("PortRestartDetail")`,
		`UiText.Get("ApplyChanges")`,
		"SavePortSettings_Click",
		`await _runtime.SaveSettingsAsync(_snapshot.Settings)`,
		`await _runtime.RunCoreActionAsync("restart")`,
		`Render("advancedConnection")`,
	} {
		if !strings.Contains(settings, want) {
			t.Fatalf("WinUI advanced connection port settings missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"BuildRuntime()",
		"SaveRuntimeSettings_Click",
		`Render("runtime")`,
		`UiText.Get("RuntimeSettings")`,
		`UiText.Get("RuntimeSettingsDetail")`,
	} {
		if strings.Contains(settings, forbidden) {
			t.Fatalf("WinUI still exposes retired Runtime settings page %q", forbidden)
		}
	}
}

func TestWindowsThemePreferencePersistsAndAppliesLive(t *testing.T) {
	mainWindowXaml := readWindowsNativeFile(t, "winui", "MainWindow.xaml")
	app := readWindowsNativeFile(t, "winui", "App.xaml.cs")
	window := readWindowsNativeFile(t, "winui", "MainWindow.xaml.cs")
	settings := readWindowsNativeFile(t, "winui", "SettingsPage.xaml.cs")
	theme := readWindowsNativeFile(t, "winui", "UiThemePreference.cs")

	for _, want := range []string{
		"ThemePreference_SelectionChanged",
		"UiThemePreference.ReadPreference()",
		"UiThemePreference.SetPreference(preference)",
		`x:Name="Root"`,
		`Background="{ThemeResource ApplicationPageBackgroundThemeBrush}"`,
		"ApplyThemePreference(_themePreference)",
		"Root.RequestedTheme = UiThemePreference.ToElementTheme(_themePreference)",
		"Root.ActualThemeChanged += Root_ActualThemeChanged",
		"Root.ActualTheme == ElementTheme.Dark",
		"DwmSetWindowAttribute",
		"DwmwaUseImmersiveDarkMode = 20",
		"LightPreference = \"light\"",
		"DarkPreference = \"dark\"",
		"ElementTheme.Light",
		"ElementTheme.Dark",
	} {
		if !strings.Contains(mainWindowXaml+app+window+settings+theme, want) {
			t.Fatalf("WinUI theme preference missing %q", want)
		}
	}
	if strings.Contains(window, "Navigation.RequestedTheme =") {
		t.Fatal("WinUI theme must be applied at the root so transparent controls share the correct window background")
	}
}

func TestWindowsInitialFrameIsPreparedBeforeWindowIsShown(t *testing.T) {
	window := readWindowsNativeFile(t, "winui", "MainWindow.xaml.cs")
	homeXaml := readWindowsNativeFile(t, "winui", "HomePage.xaml")
	homeCode := readWindowsNativeFile(t, "winui", "HomePage.xaml.cs")

	constructorStart := strings.Index(window, "public MainWindow(RuntimeService runtime")
	showMethod := strings.Index(window, "internal void ShowAndActivate()")
	if constructorStart < 0 || showMethod < 0 {
		t.Fatal("WinUI main window lifecycle methods are missing")
	}
	constructor := window[constructorStart:showMethod]
	if !strings.Contains(constructor, "ResizeToLogicalSize(DefaultWidth, DefaultHeight)") ||
		!strings.Contains(constructor, "ApplyThemePreference(_themePreference)") {
		t.Fatal("WinUI main window must prepare size and theme before ShowAndActivate")
	}
	if strings.Contains(window, "Navigation.Loaded += Navigation_Loaded") ||
		strings.Contains(window, "Root.Loaded += Root_Loaded") {
		t.Fatal("WinUI first-frame size/theme must not be deferred until after the window is loaded")
	}

	for _, want := range []string{
		`x:Name="DashboardContent"`,
		`Visibility="Collapsed"`,
		`x:Name="InitialLoadingIndicator"`,
		`IsActive="True"`,
		"var snapshot = await _runtime.GetSnapshotAsync(includeNexusConnection: true)",
		"var dashboard = await _runtime.GetDashboardAsync(snapshot)",
		"DashboardContent.Visibility = Visibility.Visible",
		"InitialLoadingIndicator.Visibility = Visibility.Collapsed",
	} {
		if !strings.Contains(homeXaml+homeCode, want) {
			t.Fatalf("WinUI Home initial render is missing %q", want)
		}
	}
	dashboardLoaded := strings.Index(homeCode, "var dashboard = await _runtime.GetDashboardAsync(snapshot)")
	firstRender := strings.Index(homeCode, "var serviceLoaded = _snapshot.CoreRunning")
	if dashboardLoaded < 0 || firstRender < 0 || dashboardLoaded > firstRender {
		t.Fatal("WinUI Home must fetch its initial dashboard before committing runtime state to the visible UI")
	}
}

func TestWindowsDataDrivenPagesArePreparedBeforeFrameNavigation(t *testing.T) {
	window := readWindowsNativeFile(t, "winui", "MainWindow.xaml.cs")
	connections := readWindowsNativeFile(t, "winui", "ConnectionsPage.xaml.cs")
	capabilities := readWindowsNativeFile(t, "winui", "CapabilitiesPage.xaml.cs")
	runtime := readWindowsNativeFile(t, "shared", "Services", "RuntimeService.cs")
	models := readWindowsNativeFile(t, "shared", "Models", "RuntimeModels.cs")

	for _, want := range []string{
		"ConnectionsNavigationRequest",
		"CapabilitiesNavigationRequest",
		"NavigatePreparedPageAsync",
		"GetNexusConnectionSnapshotAsync(cancellation.Token)",
		"GetControlPanelSettingsAsync(cancellation.Token)",
		"ContentFrame.Navigate(PageForTag(tag), parameter)",
		"_navigationLoadCancellation?.Cancel()",
	} {
		if !strings.Contains(window, want) {
			t.Fatalf("WinUI host must preload data-driven page state before navigation: missing %q", want)
		}
	}

	for _, want := range []string{
		"e.Parameter is ConnectionsNavigationRequest request",
		"ApplyConnectionState(request.State)",
		"GetNexusConnectionSnapshotAsync()",
	} {
		if !strings.Contains(connections, want) {
			t.Fatalf("WinUI Connections first layout must consume prepared state: missing %q", want)
		}
	}
	for _, want := range []string{
		"e.Parameter is CapabilitiesNavigationRequest request",
		"ApplySettings(request.Settings)",
		"GetControlPanelSettingsAsync()",
	} {
		if !strings.Contains(capabilities, want) {
			t.Fatalf("WinUI Capabilities first layout must consume prepared settings: missing %q", want)
		}
	}

	for _, want := range []string{
		"public async Task<ControlPanelSettings> GetControlPanelSettingsAsync(",
		"public async Task<NexusConnectionSnapshot> GetNexusConnectionSnapshotAsync(",
		"ReadControlPanelSettingsAsync(manifest, cancellationToken)",
		"public sealed record NexusConnectionSnapshot(",
	} {
		if !strings.Contains(runtime+models, want) {
			t.Fatalf("Windows RuntimeService is missing lightweight navigation state contract %q", want)
		}
	}

	connectionsAwait := strings.Index(window, "await _runtime.GetNexusConnectionSnapshotAsync(cancellation.Token)")
	capabilitiesAwait := strings.Index(window, "await _runtime.GetControlPanelSettingsAsync(cancellation.Token)")
	navigate := strings.Index(window, "ContentFrame.Navigate(PageForTag(tag), parameter)")
	if connectionsAwait < 0 || capabilitiesAwait < 0 || navigate < 0 ||
		connectionsAwait > navigate || capabilitiesAwait > navigate {
		t.Fatal("data-driven WinUI pages must finish loading their layout state before Frame.Navigate")
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
		`https://nexusdock.co/`,
		`https://docs.nexusdock.co/agentdock/`,
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
	sectionCard := readWindowsNativeFile(t, "winui", "SectionCard.xaml")
	runtime := readWindowsNativeFile(t, "shared", "Services", "RuntimeService.cs")

	for _, want := range []string{
		`HorizontalScrollMode="Disabled"`,
		`HorizontalScrollBarVisibility="Disabled"`,
		`x:Name="ProductLogo"`,
		"ProductLogo.Source = LoadProductLogo()",
		`System.IO.Path.Combine(AppContext.BaseDirectory, "Assets", "agentdock.png")`,
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
		"GetDashboardAsync(snapshot)",
		"dashboard.RecentCalls.Take(3)",
		`Click="ActivityShortcut_Click"`,
		`ShortcutRequested?.Invoke(this, "activity")`,
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
	for _, want := range []string{
		`HorizontalAlignment="Stretch"`,
		`HorizontalContentAlignment="Stretch"`,
	} {
		if !strings.Contains(sectionCard, want) {
			t.Fatalf("WinUI SectionCard must stretch content horizontally: missing %q", want)
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

func TestWindowsActivityOwnsRuntimeAnalytics(t *testing.T) {
	activityXaml := readWindowsNativeFile(t, "winui", "ActivityPage.xaml")
	activityCode := readWindowsNativeFile(t, "winui", "ActivityPage.xaml.cs")
	settingsXaml := readWindowsNativeFile(t, "winui", "SettingsPage.xaml")
	settingsCode := readWindowsNativeFile(t, "winui", "SettingsPage.xaml.cs")
	runtime := readWindowsNativeFile(t, "shared", "Services", "RuntimeService.cs")

	for _, want := range []string{
		`x:Name="RecentCallsPanel"`,
		"GetRuntimeAnalyticsAsync(_snapshot)",
		`"/internal/runtime/analytics"`,
		"private const int PageSize = 20",
		"TimeSpan.FromSeconds(5)",
		"RenderRecentCalls(force: true)",
		`Content = UiText.Get("ShowMore")`,
		"CreateCallExpander",
		`Text = "›"`,
		`chevron.Text = expanding ? "⌄" : "›"`,
		"StageMcpRemoteCall",
		"RecentCallsPrivacyHint",
		`x:Name="LogsNavigationItem" Tag="logs"`,
		"BuildLogs()",
		"log.SelectionChanged += LogLevel_SelectionChanged",
		`await _runtime.RunCoreActionAsync("restart")`,
		"AdvancedDiagnostics",
		"diagnosticsContent.Visibility = Visibility.Collapsed",
		"HorizontalAlignment = HorizontalAlignment.Stretch",
		"CopyDiagnostics",
		"RecentP95",
	} {
		if !strings.Contains(activityXaml+activityCode+settingsXaml+settingsCode+runtime, want) {
			t.Fatalf("WinUI Activity/logs integration missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"OpenRuntimeAnalytics(",
		`Path = "/analytics"`,
		`x:Name="CallOverviewSection"`,
		`x:Name="CallStatisticsPanel"`,
		`x:Name="ResourcesSection"`,
		`x:Name="DiagnosticsSection"`,
		"SaveLogSettings_Click",
		"LogLevelRestartHint",
	} {
		if strings.Contains(activityXaml+activityCode+runtime, forbidden) {
			t.Fatalf("WinUI Activity retained retired analytics UI %q", forbidden)
		}
	}
}

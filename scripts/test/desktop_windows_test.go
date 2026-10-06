package scripts

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsControlPanelUsesNativeTunnelCommands(t *testing.T) {
	appData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "App.xaml.cs"))
	if err != nil {
		t.Fatalf("read App.xaml.cs: %v", err)
	}
	runtimeData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "shared", "Services", "RuntimeService.cs"))
	if err != nil {
		t.Fatalf("read RuntimeService.cs: %v", err)
	}
	app := string(appData)
	runtimeService := string(runtimeData)
	for _, want := range []string{
		`"--start-tunnel"`,
		`RunTunnelActionAsync("start",`,
		`RunTunnelStartupAsync()`,
		`RunHelperAndExit(`,
		`Task.Run(async () =>`,
		`ConfigureAwait(false)`,
		`).GetAwaiter().GetResult()`,
		`RunNativeAgentDockAsync("tunnel"`,
		`allowElevation: false`,
		`"configure"`,
		`"--token-file"`,
		`RunNativeAgentDockAsync("config"`,
		`PairNexusAsync`,
		`"nexus", "pair"`,
	} {
		if !strings.Contains(app, want) && !strings.Contains(runtimeService, want) {
			t.Fatalf("Windows control panel missing native Tunnel behavior %q", want)
		}
	}
	for _, forbidden := range []string{
		`_ = RunHelperAsync(`,
		`"--nexus-token-file"`,
		`RunManagementScriptAsync(["-Action", "start-tunnel"]`,
		`RunManagementScriptAsync(["-Action", "stop-tunnel"]`,
		`RunManagementScriptAsync(["-Action", "regenerate-quick"]`,
		`powershell.exe`,
		`manage-windows.ps1`,
	} {
		if strings.Contains(runtimeService, forbidden) {
			t.Fatalf("Windows control panel still invokes PowerShell for Tunnel lifecycle %q", forbidden)
		}
	}
}
func TestWindowsControlPanelPreservesOAuthAccessTokenTTLWithoutExposingIt(t *testing.T) {
	files := []string{
		filepath.Join("..", "..", "desktop", "windows", "shared", "Models", "RuntimeModels.cs"),
		filepath.Join("..", "..", "desktop", "windows", "shared", "Services", "RuntimeService.cs"),
		filepath.Join("..", "..", "internal", "desktopruntime", "service_environment_windows.go"),
	}
	var source strings.Builder
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		source.Write(data)
	}
	for _, want := range []string{
		`oauth_access_token_ttl`,
		`"--oauth-access-token-ttl", settings.OAuthAccessTokenTtl`,
		`OAuthAccessTokenTTL     string`,
		`effectiveOAuthAccessTokenTTL(settings.OAuthAccessTokenTTL, inheritedOAuthAccessTokenTTL)`,
	} {
		if !strings.Contains(source.String(), want) {
			t.Fatalf("Windows OAuth TTL persistence chain missing %q", want)
		}
	}
	settings, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "SettingsPage.xaml.cs"))
	if err != nil {
		t.Fatalf("read SettingsPage.xaml.cs: %v", err)
	}
	for _, forbidden := range []string{`OAuthAccessTokenTtlTextBox`, `OAuth Token TTL`, `OAuthAccessTokenTtl = _snapshot`} {
		if strings.Contains(string(settings), forbidden) {
			t.Fatalf("Windows control panel still exposes OAuth TTL setting %q", forbidden)
		}
	}
}
func TestWindowsControlPanelReadsVersionFromCoreBuildInfo(t *testing.T) {
	appData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "App.xaml.cs"))
	if err != nil {
		t.Fatalf("read App.xaml.cs: %v", err)
	}
	homeData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "HomePage.xaml.cs"))
	if err != nil {
		t.Fatalf("read HomePage.xaml.cs: %v", err)
	}
	runtimeData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "shared", "Services", "RuntimeService.cs"))
	if err != nil {
		t.Fatalf("read RuntimeService.cs: %v", err)
	}

	app := string(appData)
	home := string(homeData)
	runtimeService := string(runtimeData)
	for _, want := range []string{
		`ReadHealthAsync(localOrigin, cancellationToken)`,
		`ReadCoreVersionAsync(binaryPath, cancellationToken)`,
		`startInfo.ArgumentList.Add("version")`,
		`startInfo.ArgumentList.Add("--json")`,
	} {
		if !strings.Contains(runtimeService, want) {
			t.Fatalf("Windows control panel must read the version from the core binary BuildInfo: %q", want)
		}
	}
	if !strings.Contains(home, "_snapshot.Version") {
		t.Fatal("Windows control panel must display RuntimeSnapshot.Version on the home page")
	}
	if strings.Contains(app, `return $"运行正常 · {version}"`) || strings.Contains(app, "未知版本") {
		t.Fatal("Windows tray status must not include the AgentDock version")
	}
	for _, source := range []string{app, home, runtimeService} {
		if strings.Contains(source, "snapshot.Manifest.Version") || strings.Contains(source, "manifest.Version") {
			t.Fatal("Windows control panel must not treat runtime.json as a version source")
		}
	}
}
func TestWindowsControlPanelCanSwitchCorePrivilegeMode(t *testing.T) {
	settingsData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "SettingsPage.xaml.cs"))
	if err != nil {
		t.Fatalf("read SettingsPage.xaml.cs: %v", err)
	}
	runtimeData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "shared", "Services", "RuntimeService.cs"))
	if err != nil {
		t.Fatalf("read RuntimeService.cs: %v", err)
	}
	settings := string(settingsData)
	runtimeService := string(runtimeData)
	for _, want := range []string{
		`_snapshot?.Manifest.PrivilegeMode`,
		`UiText.Get("RunCoreElevated")`,
		`await _runtime.SetPrivilegeModeAsync(toggle.IsOn)`,
		`await RefreshAsync()`,
	} {
		if !strings.Contains(settings, want) {
			t.Fatalf("SettingsPage.xaml.cs missing privilege mode behavior %q", want)
		}
	}
	for _, want := range []string{
		"SetPrivilegeModeAsync", "prepare-elevated", "prepare-standard", `RunTaskAdminTransitionAsync("restore"`,
		`"--launcher-path", trayBinary`, "WritePrivilegeModeAsync", "SetStandardCoreStartup", "snapshot.CoreStartupEnabled", "snapshot.CoreRunning",
	} {
		if !strings.Contains(runtimeService, want) {
			t.Fatalf("RuntimeService.cs missing privilege mode behavior %q", want)
		}
	}
}
func TestDesktopAppIconAssets(t *testing.T) {
	pngData, err := os.ReadFile(filepath.Join("..", "..", "packaging", "assets", "agentdock.png"))
	if err != nil {
		t.Fatalf("read shared AgentDock PNG: %v", err)
	}
	if len(pngData) < 24 || string(pngData[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatal("shared AgentDock icon must be a valid PNG")
	}
	if width, height := binary.BigEndian.Uint32(pngData[16:20]), binary.BigEndian.Uint32(pngData[20:24]); width != 1024 || height != 1024 {
		t.Fatalf("shared AgentDock icon must be 1024x1024, got %dx%d", width, height)
	}

	icoData, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "assets", "agentdock.ico"))
	if err != nil {
		t.Fatalf("read Windows AgentDock ICO: %v", err)
	}
	if len(icoData) < 6 || binary.LittleEndian.Uint16(icoData[2:4]) != 1 {
		t.Fatal("Windows AgentDock icon must be a valid ICO")
	}
	count := int(binary.LittleEndian.Uint16(icoData[4:6]))
	expectedSizes := []int{16, 20, 24, 32, 40, 48, 64, 128, 256}
	if count != len(expectedSizes) {
		t.Fatalf("Windows AgentDock icon must include %d sizes, got %d entries", len(expectedSizes), count)
	}
	if len(icoData) < 6+16*count {
		t.Fatal("Windows AgentDock icon directory is truncated")
	}
	var frame32 []byte
	for index, expectedSize := range expectedSizes {
		entry := icoData[6+16*index : 6+16*(index+1)]
		width := int(entry[0])
		if width == 0 {
			width = 256
		}
		height := int(entry[1])
		if height == 0 {
			height = 256
		}
		if width != expectedSize || height != expectedSize {
			t.Fatalf("Windows AgentDock icon entry %d must be %dx%d, got %dx%d", index, expectedSize, expectedSize, width, height)
		}
		size := int(binary.LittleEndian.Uint32(entry[8:12]))
		offset := int(binary.LittleEndian.Uint32(entry[12:16]))
		if size <= 0 || offset < 0 || offset+size > len(icoData) {
			t.Fatalf("Windows AgentDock icon entry %d has invalid payload bounds", index)
		}
		if expectedSize == 32 {
			frame32 = icoData[offset : offset+size]
		}
	}
	if len(frame32) == 0 {
		t.Fatal("Windows AgentDock icon is missing the 32px brand frame")
	}
	brand32 := sha256.Sum256(frame32)
	if got := hex.EncodeToString(brand32[:]); got != "66eea195bdb87f69347e4c34d48e73c74f0cbd84a92733580587395fe67ab29d" {
		t.Fatalf("Windows AgentDock 32px icon frame does not match the current brand asset: %s", got)
	}
}
func TestWindowsInstallerUsesStableAppUserModelID(t *testing.T) {
	setupData, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "AgentDock.iss"))
	if err != nil {
		t.Fatalf("read AgentDock.iss: %v", err)
	}
	if !strings.Contains(string(setupData), `AppUserModelID: "com.uvwt.agentdock.controlpanel"`) {
		t.Fatal("Start menu shortcut must keep the stable AgentDock AppUserModelID")
	}
}
func TestWindowsControlPanelOmitsCopyButtons(t *testing.T) {
	xamlData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "MainWindow.xaml"))
	if err != nil {
		t.Fatalf("read MainWindow.xaml: %v", err)
	}
	codeData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "MainWindow.xaml.cs"))
	if err != nil {
		t.Fatalf("read MainWindow.xaml.cs: %v", err)
	}

	xaml := string(xamlData)
	code := string(codeData)
	for _, forbidden := range []string{
		`Content="复制"`,
		"CopyLocalMcpButton_Click",
		"CopyPublicMcpButton_Click",
		"CopyBearerButton_Click",
		"CopyOAuthButton_Click",
	} {
		if strings.Contains(xaml, forbidden) || strings.Contains(code, forbidden) {
			t.Fatalf("Windows control panel must not expose copy-button behavior %q", forbidden)
		}
	}
}
func TestDesktopTrayMenusUseNativeDismissalAndOmitCopyActions(t *testing.T) {
	windowsAppData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "App.xaml.cs"))
	if err != nil {
		t.Fatalf("read App.xaml.cs: %v", err)
	}
	windowsTrayData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "TrayIconHost.cs"))
	if err != nil {
		t.Fatalf("read TrayIconHost.cs: %v", err)
	}
	macData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "macos", "AgentDockApp", "Sources", "AppDelegate.swift"))
	if err != nil {
		t.Fatalf("read AppDelegate.swift: %v", err)
	}
	windowsApp := string(windowsAppData)
	windowsTray := string(windowsTrayData)
	macApp := string(macData)

	for _, want := range []string{
		`UiText.Get("TrayShowMainWindow")`, `UiText.Get("TrayStartAgentDock")`,
		`UiText.Get("TrayRestartAgentDock")`, `UiText.Get("Settings")`,
		`UiText.Get("TrayCheckForUpdates")`, `UiText.Get("ExitTray")`,
		"Shell_NotifyIconW", "TrackPopupMenuEx", "TaskbarCreated",
	} {
		if !strings.Contains(windowsTray, want) {
			t.Fatalf("Windows tray menu missing native behavior %q", want)
		}
	}
	if !strings.Contains(windowsApp, "new TrayIconHost(") {
		t.Fatal("Windows app must construct the native TrayIconHost")
	}

	for _, want := range []string{
		`NSMenu.popUpContextMenu(makeTrayContextMenu(), with: event, for: sender)`,
		`menu.appearance = NSApp.effectiveAppearance`,
		`L10n.text("Show main window")`, `L10n.text("Tray Start")`,
		`L10n.text("Tray Restart")`, `L10n.text("Settings")`,
		`L10n.text("Tray Check for updates")`, `L10n.text("Exit AgentDock")`,
	} {
		if !strings.Contains(macApp, want) {
			t.Fatalf("macOS tray menu missing native behavior %q", want)
		}
	}

	for _, forbidden := range []string{
		`"复制本地 MCP 地址"`, `"复制公网 MCP 地址"`, "copyLocalMCP", "copyPublicMCP",
		"Forms.Clipboard.SetText", "NSPasteboard.general", "Forms.NotifyIcon",
		"ContextMenuStrip = _trayMenu", "TrayContextMenuPopoverController",
	} {
		if strings.Contains(windowsApp, forbidden) || strings.Contains(windowsTray, forbidden) || strings.Contains(macApp, forbidden) {
			t.Fatalf("desktop tray menus must not expose retired manual/copy behavior %q", forbidden)
		}
	}
}

func TestDesktopTrayUpdateChecksUseVisibleInAppFeedback(t *testing.T) {
	windowsAppData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "App.xaml.cs"))
	if err != nil {
		t.Fatalf("read App.xaml.cs: %v", err)
	}
	windowsMainData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "MainWindow.xaml.cs"))
	if err != nil {
		t.Fatalf("read MainWindow.xaml.cs: %v", err)
	}
	windowsSettingsData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "SettingsPage.xaml.cs"))
	if err != nil {
		t.Fatalf("read SettingsPage.xaml.cs: %v", err)
	}
	macAppData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "macos", "AgentDockApp", "Sources", "AppDelegate.swift"))
	if err != nil {
		t.Fatalf("read AppDelegate.swift: %v", err)
	}
	macPopoverData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "macos", "AgentDockApp", "Sources", "TrayPopoverController.swift"))
	if err != nil {
		t.Fatalf("read TrayPopoverController.swift: %v", err)
	}

	windowsApp := string(windowsAppData)
	windowsMain := string(windowsMainData)
	windowsSettings := string(windowsSettingsData)
	macApp := string(macAppData)
	macPopover := string(macPopoverData)

	for _, want := range []string{
		"ShowAboutSettings();",
		"await _window.CheckForUpdatesAsync();",
	} {
		if !strings.Contains(windowsApp, want) {
			t.Fatalf("Windows tray update flow must reuse visible About UI %q", want)
		}
	}
	for _, want := range []string{
		"internal Task CheckForUpdatesAsync()",
		"settings.CheckForUpdatesAsync()",
	} {
		if !strings.Contains(windowsMain, want) {
			t.Fatalf("Windows main window missing update bridge %q", want)
		}
	}
	for _, want := range []string{
		"internal async Task CheckForUpdatesAsync()",
		"UiText.Get(\"CheckingForUpdates\")",
		"_updateCheckInProgress",
	} {
		if !strings.Contains(windowsSettings, want) {
			t.Fatalf("Windows About update flow missing visible check state %q", want)
		}
	}
	for _, want := range []string{
		"startUpdate(showCheckingPopover: true)",
		"trayPopover.show(relativeTo: button)",
	} {
		if !strings.Contains(macApp, want) {
			t.Fatalf("macOS tray update flow missing visible checking state %q", want)
		}
	}
	if !strings.Contains(macPopover, "func show(relativeTo button: NSStatusBarButton)") {
		t.Fatal("macOS tray popover must support idempotent visible presentation during update checks")
	}
}

func TestWindowsUpdateProgressUsesCoreByteFields(t *testing.T) {
	modelData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "shared", "Models", "RuntimeModels.cs"))
	if err != nil {
		t.Fatalf("read RuntimeModels.cs: %v", err)
	}
	runtimeData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "shared", "Services", "RuntimeService.cs"))
	if err != nil {
		t.Fatalf("read RuntimeService.cs: %v", err)
	}

	models := string(modelData)
	runtimeService := string(runtimeData)

	// Core 的进度协议字段是 bytes/total_bytes；Windows 必须与 macOS 共用同一契约。
	for _, want := range []string{
		"[JsonPropertyName(\"bytes\")]",
		"public long? Bytes",
		"[JsonPropertyName(\"total_bytes\")]",
		"public long? TotalBytes",
	} {
		if !strings.Contains(models, want) {
			t.Fatalf("Windows update progress model missing %q", want)
		}
	}
	if strings.Contains(models, "[JsonPropertyName(\"bytes_read\")]") {
		t.Fatal("Windows update progress must not use the obsolete bytes_read field")
	}

	for _, want := range []string{
		"updateEvent.Bytes is long bytesRead",
		"UpdateDownloadingProgress",
		"UpdateDownloadingUnknownSize",
		"bytesRead * 100 / totalBytes",
	} {
		if !strings.Contains(runtimeService, want) {
			t.Fatalf("Windows download progress rendering missing %q", want)
		}
	}
}
func TestWindowsUpdateFeedbackUsesUTF8AndNativeWinUIStatus(t *testing.T) {
	appData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "App.xaml.cs"))
	if err != nil {
		t.Fatalf("read App.xaml.cs: %v", err)
	}
	settingsData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "SettingsPage.xaml.cs"))
	if err != nil {
		t.Fatalf("read SettingsPage.xaml.cs: %v", err)
	}
	runtimeData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "shared", "Services", "RuntimeService.cs"))
	if err != nil {
		t.Fatalf("read RuntimeService.cs: %v", err)
	}
	app := string(appData)
	settings := string(settingsData)
	runtimeService := string(runtimeData)

	for _, want := range []string{
		`ResumeUpdateProgressIfNeededAsync`, `ReadUpdateUiHandoffTransactionAsync`, `ReadUpdateTerminalResultAsync`,
		`AcknowledgeUpdateUiHandoffAsync(transaction.TransactionId)`,
	} {
		if !strings.Contains(app, want) {
			t.Fatalf("Windows tray update handoff missing %q", want)
		}
	}
	for _, want := range []string{
		`var check = await _runtime.CheckForUpdatesAsync()`, `if (!check.UpdateAvailable)`, `new ContentDialog`,
		`PrimaryButtonText = UiText.Get("Update")`, `new Progress<UpdateProgress>`, `await _runtime.RunUpdateAsync(progress)`,
		`SetUpdateCheckButtonState()`, `_updateButton.IsEnabled = !_updateCheckInProgress`,
	} {
		if !strings.Contains(settings, want) {
			t.Fatalf("WinUI update flow missing %q", want)
		}
	}
	for _, want := range []string{
		`public async Task<UpdateCheckResult> CheckForUpdatesAsync`, `startInfo.ArgumentList.Add("--check")`,
		`JsonSerializer.Deserialize<UpdateCheckResult>`, `IProgress<UpdateProgress>? progress`,
		`startInfo.ArgumentList.Add("--progress-json")`, `startInfo.Environment[UpdateUiHandoffEnvironment] = "1"`,
		`ReadProcessLinesAsync`, `ParseUpdateProgress`, `JsonSerializer.Deserialize<UpdateProgressEvent>`,
		`StandardOutputEncoding = utf8`, `StandardErrorEncoding = utf8`,
	} {
		if !strings.Contains(runtimeService, want) {
			t.Fatalf("Windows update process handling missing %q", want)
		}
	}
	for _, forbidden := range []string{`UpdateProgressWindow`, `RunTrayActionAsync("update")`, `RunCoreActionAsync("update"`} {
		if strings.Contains(app, forbidden) || strings.Contains(settings, forbidden) {
			t.Fatalf("WinUI update flow must not depend on retired WPF update UI %q", forbidden)
		}
	}
}
func TestWindowsControlPanelUsesNativeWinUIThemeResources(t *testing.T) {
	appData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "App.xaml"))
	if err != nil {
		t.Fatalf("read App.xaml: %v", err)
	}
	cardData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "SectionCard.xaml"))
	if err != nil {
		t.Fatalf("read SectionCard.xaml: %v", err)
	}
	app := string(appData)
	card := string(cardData)
	for _, want := range []string{`<XamlControlsResources`, `ThemeResource CardBackgroundFillColorDefaultBrush`, `ThemeResource CardStrokeColorDefaultBrush`} {
		if !strings.Contains(app+card, want) {
			t.Fatalf("native WinUI theme resources missing %q", want)
		}
	}
	for _, forbidden := range []string{`x:Key="SurfaceBrush"`, `x:Key="BorderBrush"`, `<Style TargetType="TabControl">`, `PART_SelectedContentHost`} {
		if strings.Contains(app+card, forbidden) {
			t.Fatalf("WinUI shell must not retain WPF-specific styling %q", forbidden)
		}
	}
}
func TestWindowsControlPanelResolvesRuntimeRootFromExecutableDirectory(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "shared", "Services", "RuntimeService.cs"))
	if err != nil {
		t.Fatalf("read RuntimeService.cs: %v", err)
	}
	service := string(data)
	for _, want := range []string{
		"new DirectoryInfo(AppContext.BaseDirectory)",
		"executableDirectory.Parent?.FullName",
		"string.Equals(executableDirectory.Name, \"bin\"",
	} {
		if !strings.Contains(service, want) {
			t.Fatalf("RuntimeService.cs missing installed runtime root behavior %q", want)
		}
	}
	if strings.Contains(service, "Directory.GetParent(baseDirectory)?.FullName") {
		t.Fatal("RuntimeService must not resolve the parent from a trailing AppContext.BaseDirectory string")
	}
}

func TestWindowsBackgroundTrayStartupDoesNotShowExistingControlPanel(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "App.xaml.cs"))
	if err != nil {
		t.Fatalf("read App.xaml.cs: %v", err)
	}
	app := strings.ReplaceAll(string(data), "\r\n", "\n")
	backgroundDeclaration := `var background = arguments.Any(value => string.Equals(value, "--background", StringComparison.OrdinalIgnoreCase));`
	backgroundIndex := strings.Index(app, backgroundDeclaration)
	singletonIndex := strings.Index(app, "if (!createdNew)")
	if backgroundIndex < 0 || singletonIndex < 0 || backgroundIndex > singletonIndex {
		t.Fatal("Windows tray must resolve --background before handling the singleton instance")
	}
	branchEnd := strings.Index(app[singletonIndex:], "Exit();")
	if branchEnd < 0 {
		t.Fatal("Windows tray singleton branch must exit the secondary instance")
	}
	singletonBranch := app[singletonIndex : singletonIndex+branchEnd]
	if !strings.Contains(singletonBranch, "if (!background)") || !strings.Contains(singletonBranch, "show.Set();") {
		t.Fatal("only an explicit foreground launch may ask an existing tray instance to show the control panel")
	}
	if strings.Count(app, backgroundDeclaration) != 1 {
		t.Fatal("Windows tray should have one authoritative --background startup decision")
	}
	if !strings.Contains(app, "if (!background) ShowControlPanel();") {
		t.Fatal("an explicit foreground launch must still show the control panel")
	}
}

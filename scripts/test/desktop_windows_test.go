package scripts

import (
	"encoding/binary"
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
		filepath.Join("..", "..", "desktop", "windows", "winui", "CapabilitiesPage.xaml.cs"),
		filepath.Join("..", "..", "desktop", "windows", "winui", "SettingsPage.xaml.cs"),
		filepath.Join("..", "..", "desktop", "windows", "shared", "Services", "RuntimeService.cs"),
		filepath.Join("..", "..", "internal", "desktopruntime", "config_windows.go"),
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
		`_settings = snapshot.Settings`,
		`await _runtime.SaveSettingsAsync(_settings)`,
		`"--oauth-access-token-ttl", settings.OAuthAccessTokenTtl`,
		`OAuthAccessTokenTTL:     request.OAuthAccessTokenTTL`,
		`effectiveOAuthAccessTokenTTL(settings.OAuthAccessTokenTTL, inheritedOAuthAccessTokenTTL)`,
	} {
		if !strings.Contains(source.String(), want) {
			t.Fatalf("Windows OAuth TTL persistence chain missing %q", want)
		}
	}
	for _, forbidden := range []string{`OAuthAccessTokenTtlTextBox`, `OAuth Token TTL`} {
		if strings.Contains(source.String(), forbidden) {
			t.Fatalf("Windows control panel still exposes OAuth TTL setting %q", forbidden)
		}
	}
}
func TestWindowsControlPanelReadsVersionFromCoreBuildInfo(t *testing.T) {
	appData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "App.xaml.cs"))
	if err != nil {
		t.Fatalf("read App.xaml.cs: %v", err)
	}
	windowData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "HomePage.xaml.cs"))
	if err != nil {
		t.Fatalf("read HomePage.xaml.cs: %v", err)
	}
	runtimeData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "shared", "Services", "RuntimeService.cs"))
	if err != nil {
		t.Fatalf("read RuntimeService.cs: %v", err)
	}

	app := string(appData)
	window := string(windowData)
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
	if !strings.Contains(window, "_snapshot.Version") {
		t.Fatal("Windows control panel must display RuntimeSnapshot.Version on the Home page")
	}
	if strings.Contains(app, `return $"运行正常 · {version}"`) || strings.Contains(app, "未知版本") {
		t.Fatal("Windows tray status must not include the AgentDock version")
	}
	for _, source := range []string{app, window, runtimeService} {
		if strings.Contains(source, "snapshot.Manifest.Version") || strings.Contains(source, "manifest.Version") {
			t.Fatal("Windows control panel must not treat runtime.json as a version source")
		}
	}
}
func TestWindowsControlPanelCanSwitchCorePrivilegeMode(t *testing.T) {
	checks := map[string][]string{
		filepath.Join("..", "..", "desktop", "windows", "winui", "SettingsPage.xaml.cs"): {
			"_snapshot?.Manifest.PrivilegeMode",
			"PrivilegeToggle_Toggled",
			"_runtime.SetPrivilegeModeAsync(toggle.IsOn)",
			"await RefreshAsync()",
		},
		filepath.Join("..", "..", "desktop", "windows", "shared", "Services", "RuntimeService.cs"): {
			"SetPrivilegeModeAsync",
			"prepare-elevated",
			"prepare-standard",
			"RunTaskAdminTransitionAsync(\"restore\"",
			`"--launcher-path", trayBinary`,
			"WritePrivilegeModeAsync",
			"SetStandardCoreStartup",
			"snapshot.CoreStartupEnabled",
			"snapshot.CoreRunning",
		},
	}

	for path, required := range checks {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		content := string(data)
		for _, want := range required {
			if !strings.Contains(content, want) {
				t.Fatalf("%s missing privilege mode switch behavior %q", path, want)
			}
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
	if count := binary.LittleEndian.Uint16(icoData[4:6]); count < 9 {
		t.Fatalf("Windows AgentDock icon must include multiple sizes, got %d entries", count)
	}
}
func TestWindowsControlPanelUsesStableAppUserModelID(t *testing.T) {
	appData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "App.xaml.cs"))
	if err != nil {
		t.Fatalf("read App.xaml.cs: %v", err)
	}
	app := string(appData)
	for _, want := range []string{
		"com.uvwt.agentdock.controlpanel",
		"SetCurrentProcessExplicitAppUserModelID",
		"_ = SetCurrentProcessExplicitAppUserModelID(AppUserModelId)",
	} {
		if !strings.Contains(app, want) {
			t.Fatalf("App.xaml.cs missing stable taskbar identity %q", want)
		}
	}

	setupData, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "AgentDock.iss"))
	if err != nil {
		t.Fatalf("read AgentDock.iss: %v", err)
	}
	if !strings.Contains(string(setupData), "AppUserModelID: \"com.uvwt.agentdock.controlpanel\"") {
		t.Fatal("Start menu shortcut must use the same stable AppUserModelID as the control panel process")
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
	windowsData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "App.xaml.cs"))
	if err != nil {
		t.Fatalf("read App.xaml.cs: %v", err)
	}
	macData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "macos", "AgentDockApp", "Sources", "AppDelegate.swift"))
	if err != nil {
		t.Fatalf("read AppDelegate.swift: %v", err)
	}
	windowsApp := string(windowsData)
	macApp := string(macData)

	orderedItems := []string{
		`UiText.Get("OpenAgentDock")`,
		`UiText.Get("StartAgentDock")`,
		`UiText.Get("StopAgentDock")`,
		`UiText.Get("RestartAgentDock")`,
		`UiText.Get("OpenLogsFolder")`,
		`UiText.Get("OpenConfigFolder")`,
		`UiText.Get("ExitTray")`,
	}
	lastIndex := -1
	for _, item := range orderedItems {
		index := strings.Index(windowsApp, item)
		if index < 0 {
			t.Fatalf("Windows tray menu missing macOS-aligned item %q", item)
		}
		if index <= lastIndex {
			t.Fatalf("Windows tray menu item %q is out of order", item)
		}
		lastIndex = index
	}

	for _, want := range []string{
		"ContextMenuStrip = _trayMenu",
		"StartShowListener()",
		"_notifyIcon.DoubleClick",
		"RunTrayActionAsync",
		"EventWaitHandle",
	} {
		if !strings.Contains(windowsApp, want) {
			t.Fatalf("Windows tray menu missing native live behavior %q", want)
		}
	}

	for _, want := range []string{
		`L10n.text("Open AgentDock")`,
		`L10n.text("Stop AgentDock")`,
		`L10n.text("Restart AgentDock")`,
		`L10n.text("Start AgentDock")`,
		`L10n.text("Open background settings")`,
		`L10n.text("Check for updates…")`,
		`L10n.text("Open logs folder")`,
		`L10n.text("Open configuration folder")`,
		`L10n.text("Open documentation")`,
		`L10n.text("Exit menu bar app")`,
	} {
		if !strings.Contains(macApp, want) {
			t.Fatalf("macOS tray menu missing localized item %q", want)
		}
	}

	for _, forbidden := range []string{
		`"复制本地 MCP 地址"`,
		`"复制公网 MCP 地址"`,
		"copyLocalMCP",
		"copyPublicMCP",
		"Forms.Clipboard.SetText",
		"NSPasteboard.general",
	} {
		if strings.Contains(windowsApp, forbidden) || strings.Contains(macApp, forbidden) {
			t.Fatalf("desktop tray menus must not expose copy-address behavior %q", forbidden)
		}
	}

	for _, forbidden := range []string{"NotifyIcon_MouseUp", "_trayMenu.Show("} {
		if strings.Contains(windowsApp, forbidden) {
			t.Fatalf("Windows tray menu must use native NotifyIcon dismissal instead of manual popup behavior %q", forbidden)
		}
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
func TestWindowsUpdateFeedbackUsesUTF8AndImmediateStatus(t *testing.T) {
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
		`ResumeUpdateProgressIfNeededAsync`,
		`ReadUpdateUiHandoffTransactionAsync`,
		`ReadUpdateTerminalResultAsync`,
		`AcknowledgeUpdateUiHandoffAsync(transaction.TransactionId)`,
	} {
		if !strings.Contains(app, want) {
			t.Fatalf("Windows update handoff flow missing %q", want)
		}
	}

	for _, want := range []string{
		`CheckUpdate_Click`,
		`var check = await _runtime.CheckForUpdatesAsync()`,
		`if (!check.UpdateAvailable)`,
		`Title = UiText.Get("NewVersionAvailable")`,
		`new Progress<UpdateProgress>`,
		`var output = await _runtime.RunUpdateAsync(progress)`,
	} {
		if !strings.Contains(settings, want) {
			t.Fatalf("Windows About update flow missing %q", want)
		}
	}

	for _, want := range []string{
		`public async Task<UpdateCheckResult> CheckForUpdatesAsync`,
		`startInfo.ArgumentList.Add("--check")`,
		`JsonSerializer.Deserialize<UpdateCheckResult>`,
		`IProgress<UpdateProgress>? progress`,
		`startInfo.ArgumentList.Add("--progress-json")`,
		`startInfo.Environment[UpdateUiHandoffEnvironment] = "1"`,
		`"staged" or "trial" or "rolling_back" or "committed" or "rolled_back" or "failed" => transaction`,
		`Path.Combine(RuntimeRoot, "update", "ui-handoff-ack.json")`,
		`File.Move(temporaryPath, acknowledgementPath, overwrite: true)`,
		`ReadProcessLinesAsync`,
		`ParseUpdateProgress`,
		`JsonSerializer.Deserialize<UpdateProgressEvent>`,
		`StandardOutputEncoding = utf8`,
		`StandardErrorEncoding = utf8`,
	} {
		if !strings.Contains(runtimeService, want) {
			t.Fatalf("Windows update process handling missing %q", want)
		}
	}

	for _, forbidden := range []string{
		`RunTrayActionAsync("update")`,
		`RunCoreActionAsync("update"`,
		`var output = await _runtime.RunUpdateAsync();`,
	} {
		if strings.Contains(app, forbidden) || strings.Contains(settings, forbidden) {
			t.Fatalf("Windows update UI must not bypass check-and-confirm flow %q", forbidden)
		}
	}
}
func TestWindowsControlPanelUsesNativeWinUIResources(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "winui", "App.xaml"))
	if err != nil {
		t.Fatalf("read App.xaml: %v", err)
	}
	app := string(data)

	for _, want := range []string{
		`<ResourceDictionary.MergedDictionaries>`,
		`<XamlControlsResources xmlns="using:Microsoft.UI.Xaml.Controls" />`,
	} {
		if !strings.Contains(app, want) {
			t.Fatalf("App.xaml missing native WinUI resource %q", want)
		}
	}

	for _, forbidden := range []string{
		`x:Key="SurfaceBrush"`,
		`x:Key="BorderBrush"`,
		`x:Key="PanelBrush"`,
		`x:Key="ContentBrush"`,
		`<Style TargetType="TabControl">`,
	} {
		if strings.Contains(app, forbidden) {
			t.Fatalf("WinUI shell must not carry legacy WPF styling %q", forbidden)
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
		t.Fatal("Windows tray singleton branch is incomplete")
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

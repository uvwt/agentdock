package scripts

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsControlPanelUsesNativeTunnelCommands(t *testing.T) {
	appData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "App.xaml.cs"))
	if err != nil {
		t.Fatalf("read App.xaml.cs: %v", err)
	}
	runtimeData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "Services", "RuntimeService.cs"))
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
		filepath.Join("..", "..", "desktop", "windows", "control-panel", "Models", "RuntimeModels.cs"),
		filepath.Join("..", "..", "desktop", "windows", "control-panel", "MainWindow.xaml"),
		filepath.Join("..", "..", "desktop", "windows", "control-panel", "MainWindow.xaml.cs"),
		filepath.Join("..", "..", "desktop", "windows", "control-panel", "Services", "RuntimeService.cs"),
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
		`OAuthAccessTokenTtl = _snapshot?.Settings.OAuthAccessTokenTtl ?? ""`,
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
	appData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "App.xaml.cs"))
	if err != nil {
		t.Fatalf("read App.xaml.cs: %v", err)
	}
	windowData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "MainWindow.xaml.cs"))
	if err != nil {
		t.Fatalf("read MainWindow.xaml.cs: %v", err)
	}
	runtimeData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "Services", "RuntimeService.cs"))
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
	if !strings.Contains(window, "snapshot.Version") {
		t.Fatal("Windows control panel must display RuntimeSnapshot.Version in the main window")
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
		filepath.Join("..", "..", "desktop", "windows", "control-panel", "MainWindow.xaml"): {
			"ElevatedCoreCheckBox",
			"以管理员权限运行 AgentDock 核心",
			"ElevatedCoreCheckBox_Click",
		},
		filepath.Join("..", "..", "desktop", "windows", "control-panel", "MainWindow.xaml.cs"): {
			"snapshot.Manifest.PrivilegeMode",
			"_runtime.SetPrivilegeModeAsync(elevated)",
			"await RefreshAsync()",
		},
		filepath.Join("..", "..", "desktop", "windows", "control-panel", "Services", "RuntimeService.cs"): {
			"SetPrivilegeModeAsync",
			"prepare-elevated",
			"prepare-standard",
			"RunTaskAdminTransitionAsync(\"restore\"",
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
	appData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "App.xaml.cs"))
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
	xamlData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "MainWindow.xaml"))
	if err != nil {
		t.Fatalf("read MainWindow.xaml: %v", err)
	}
	codeData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "MainWindow.xaml.cs"))
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
	windowsData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "App.xaml.cs"))
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
		`AgentDock：{statusText}`,
		`"打开 AgentDock"`,
		`"停止 AgentDock"`,
		`"重启 AgentDock"`,
		`"启动 AgentDock"`,
		`"检查更新…"`,
		`"打开日志目录"`,
		`"打开配置目录"`,
		`"打开使用文档"`,
		`"退出菜单栏"`,
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
		"PopulateTrayMenu(_trayMenu, null)",
		"DispatcherTimer",
		"PopulateTrayMenu",
		"RefreshTraySnapshotAsync",
		"!_trayMenu.Visible",
		"Runtime.GetSnapshotAsync()",
		"snapshot.CoreRunning",
		"https://uvwt.github.io/agentdock-docs/",
	} {
		if !strings.Contains(windowsApp, want) {
			t.Fatalf("Windows tray menu missing native live behavior %q", want)
		}
	}

	for _, want := range []string{
		`"打开 AgentDock"`,
		`"停用 AgentDock"`,
		`"重启 AgentDock"`,
		`"启用 AgentDock"`,
		`"打开后台设置"`,
		`"检查更新…"`,
		`"打开日志目录"`,
		`"打开配置目录"`,
		`"打开使用文档"`,
		`"退出菜单栏"`,
	} {
		if !strings.Contains(macApp, want) {
			t.Fatalf("macOS tray menu missing item %q", want)
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
func TestWindowsUpdateFeedbackUsesUTF8AndImmediateStatus(t *testing.T) {
	appData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "App.xaml.cs"))
	if err != nil {
		t.Fatalf("read App.xaml.cs: %v", err)
	}
	windowData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "MainWindow.xaml.cs"))
	if err != nil {
		t.Fatalf("read MainWindow.xaml.cs: %v", err)
	}
	xamlData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "MainWindow.xaml"))
	if err != nil {
		t.Fatalf("read MainWindow.xaml: %v", err)
	}
	runtimeData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "Services", "RuntimeService.cs"))
	if err != nil {
		t.Fatalf("read RuntimeService.cs: %v", err)
	}
	progressXAMLData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "UpdateProgressWindow.xaml"))
	if err != nil {
		t.Fatalf("read UpdateProgressWindow.xaml: %v", err)
	}
	progressCodeData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "UpdateProgressWindow.xaml.cs"))
	if err != nil {
		t.Fatalf("read UpdateProgressWindow.xaml.cs: %v", err)
	}
	scriptData, err := os.ReadFile(filepath.Join("..", "install", "manage-windows.ps1"))
	if err != nil {
		t.Fatalf("read manage-windows.ps1: %v", err)
	}

	app := string(appData)
	window := string(windowData)
	xaml := string(xamlData)
	runtimeService := string(runtimeData)
	progressXAML := string(progressXAMLData)
	progressCode := string(progressCodeData)
	script := string(scriptData)

	for _, want := range []string{
		`_updateInProgress ? "正在检查更新…" : "检查更新…"`,
		`ControlPanelWindow.SetUpdateState(true, "正在检查更新，请稍候…")`,
		`var check = await Runtime.CheckForUpdatesAsync()`,
		`if (!check.UpdateAvailable)`,
		`MessageBoxButton.YesNo`,
		`new UpdateProgressWindow(check.CurrentVersion, check.LatestVersion)`,
		`var output = await Runtime.RunUpdateAsync(progress)`,
	} {
		if !strings.Contains(app, want) {
			t.Fatalf("Windows tray update flow missing %q", want)
		}
	}

	for _, want := range []string{
		`x:Name="UpdateButton"`,
		`await ((App)Application.Current).CheckForUpdatesAsync(this)`,
		`public void SetUpdateState`,
		`public void SetUpdateStatus`,
	} {
		if !strings.Contains(xaml, want) && !strings.Contains(window, want) {
			t.Fatalf("Windows control-panel update flow missing %q", want)
		}
	}

	for _, want := range []string{
		`public async Task<UpdateCheckResult> CheckForUpdatesAsync`,
		`startInfo.ArgumentList.Add("--check")`,
		`JsonSerializer.Deserialize<UpdateCheckResult>`,
		`IProgress<UpdateProgress>? progress`,
		`ReadProcessLinesAsync`,
		`MapUpdateProgress`,
		`StandardOutputEncoding = utf8`,
		`StandardErrorEncoding = utf8`,
	} {
		if !strings.Contains(runtimeService, want) {
			t.Fatalf("Windows update process handling missing %q", want)
		}
	}

	for _, want := range []string{
		`x:Class="AgentDock.ControlPanel.UpdateProgressWindow"`,
		`<ProgressBar x:Name="UpdateProgressBar"`,
		`IsEnabled="False"`,
		`if (!_canClose)`,
		`UpdateProgressBar.Value = Math.Max`,
		`public void Complete(string message)`,
		`public void Fail(string message)`,
	} {
		if !strings.Contains(progressXAML, want) && !strings.Contains(progressCode, want) {
			t.Fatalf("Windows update progress window missing %q", want)
		}
	}

	for _, want := range []string{
		`[Console]::InputEncoding = $Utf8NoBom`,
		`[Console]::OutputEncoding = $Utf8NoBom`,
		`$global:OutputEncoding = $Utf8NoBom`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("Windows management script missing UTF-8 output setup %q", want)
		}
	}

	for _, forbidden := range []string{
		`RunTrayActionAsync("update")`,
		`RunCoreActionAsync("update"`,
		`var output = await Runtime.RunUpdateAsync();`,
		`var output = await _runtime.RunUpdateAsync();`,
	} {
		if strings.Contains(app, forbidden) || strings.Contains(window, forbidden) {
			t.Fatalf("Windows update UI must not bypass check-and-confirm flow %q", forbidden)
		}
	}
}
func TestWindowsControlPanelKeepsExistingBackgroundAndStylesOnlyButtonsAndTabs(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "App.xaml"))
	if err != nil {
		t.Fatalf("read App.xaml: %v", err)
	}
	app := string(data)

	for _, want := range []string{
		`x:Key="SurfaceBrush" Color="#F5F7FA"`,
		`x:Key="BorderBrush" Color="#D8DEE8"`,
		`<Style TargetType="Button">`,
		`<Style TargetType="TabControl">`,
		`<Style TargetType="TabItem">`,
		`x:Name="PART_SelectedContentHost"`,
		`Background="White"`,
	} {
		if !strings.Contains(app, want) {
			t.Fatalf("App.xaml missing restrained Windows style %q", want)
		}
	}

	for _, forbidden := range []string{
		`x:Key="PanelBrush"`,
		`x:Key="ContentBrush"`,
		`<Style TargetType="ComboBox">`,
	} {
		if strings.Contains(app, forbidden) {
			t.Fatalf("button/tab styling must not change the existing window background or unrelated controls: %q", forbidden)
		}
	}
}
func TestWindowsControlPanelResolvesRuntimeRootFromExecutableDirectory(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "Services", "RuntimeService.cs"))
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

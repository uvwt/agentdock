package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsControlPanelPrivilegeModeCopyStaysUserFacing(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "MainWindow.xaml"))
	if err != nil {
		t.Fatalf("read MainWindow.xaml: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		`x:Name="ElevatedCoreCheckBox"`,
		`Content="{local:Loc RunCoreElevated}"`,
		`Click="ElevatedCoreCheckBox_Click"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("Windows privilege mode control missing %q", want)
		}
	}
	if strings.Contains(content, "开启时使用 Windows Highest") {
		t.Fatal("Windows privilege mode control must not expose implementation details")
	}
}

func TestWindowsControlPanelSupportsPersistentLanguagePreference(t *testing.T) {
	root := filepath.Join("..", "..", "desktop", "windows", "control-panel")
	checks := map[string][]string{
		"MainWindow.xaml": {
			`x:Name="LanguageComboBox"`,
			`Tag="system"`,
			`Tag="zh-CN"`,
			`Tag="en"`,
			`SelectionChanged="LanguageComboBox_SelectionChanged"`,
		},
		"MainWindow.xaml.cs": {
			`UiText.ReadPreference()`,
			`UiText.Get("LanguageChangeDiscardWarning")`,
			`MessageBoxButton.YesNo`,
			`SelectUiLanguage(UiText.ReadPreference())`,
			`ApplyLanguagePreferenceAsync(preference)`,
		},
		"App.xaml.cs": {
			`UiText.SetPreference(preference)`,
			`new MainWindow(Runtime)`,
			`previousWindow.CloseForReplacement()`,
		},
		filepath.Join("Localization", "UiText.cs"): {
			`"AgentDock",`,
			`"ui-language"`,
			`File.Delete(PreferencePath)`,
			`ResolveLocale(string preference, string systemCultureName)`,
		},
	}

	for relativePath, wants := range checks {
		data, err := os.ReadFile(filepath.Join(root, relativePath))
		if err != nil {
			t.Fatalf("read %s: %v", relativePath, err)
		}
		content := string(data)
		for _, want := range wants {
			if !strings.Contains(content, want) {
				t.Fatalf("Windows language preference contract missing %q in %s", want, relativePath)
			}
		}
	}
}

func TestWindowsControlPanelPersistsManagementFailureDiagnostics(t *testing.T) {
	root := filepath.Join("..", "..", "desktop", "windows", "control-panel")
	files := map[string]string{}
	for _, relative := range []string{
		"App.xaml.cs",
		"MainWindow.xaml.cs",
		filepath.Join("Services", "RuntimeService.cs"),
		filepath.Join("Services", "TaskAdminService.cs"),
		filepath.Join("Services", "ControlPanelDiagnostics.cs"),
	} {
		data, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatalf("read %s: %v", relative, err)
		}
		files[relative] = string(data)
	}

	runtimeService := files[filepath.Join("Services", "RuntimeService.cs")]
	for _, want := range []string{
		`RunRuntimeStageAsync("core", "start"`,
		`RunRuntimeStageAsync("tunnel", "start"`,
		`RunRuntimeStageAsync("core", "restart"`,
		`RunRuntimeStageAsync("tunnel", "stop"`,
		`"--run-elevated-agentdock"`,
		`"--operation-id", operationId`,
		`"--request-sha256", requestLease.Sha256`,
		`Environment.ProcessPath`,
		`ControlPanelDiagnostics.CreateRequestLease(`,
		`ControlPanelDiagnostics.ReadRequest(RuntimeRoot, operationId, requestSha256)`,
		`ControlPanelDiagnostics.ReadResult(RuntimeRoot, operationId)`,
		`"service" => action is "start" or "stop" or "restart" or "autostart"`,
		`"tunnel" => action is "start" or "stop" or "restart" or "regenerate" or "configure"`,
		`"config" => action == "update"`,
		`"ManagerFailedResultMissing"`,
		`"ManagerFailedResultInvalid"`,
		`"ManagerFailedWithDetail"`,
	} {
		if !strings.Contains(runtimeService, want) {
			t.Fatalf("RuntimeService.cs missing management diagnostic behavior %q", want)
		}
	}
	for _, forbidden := range []string{`--result-file`, `--agentdock-command`, `--agentdock-argument`} {
		if strings.Contains(runtimeService, forbidden) {
			t.Fatalf("elevated management command line must not expose payload argument %q", forbidden)
		}
	}
	readResult := strings.Index(runtimeService, `var result = ControlPanelDiagnostics.ReadResult(RuntimeRoot, operationId)`)
	if readResult < 0 {
		t.Fatal("elevated management result validation is missing")
	}
	successReturn := strings.Index(runtimeService[readResult:], `if (process.ExitCode == 0)`)
	if successReturn < 0 {
		t.Fatal("elevated management success must validate a diagnostic result before accepting exit code 0")
	}
	leaseDispose := strings.Index(runtimeService, `requestLease.Dispose()`)
	deleteFiles := strings.Index(runtimeService, `ControlPanelDiagnostics.DeleteOperationFiles(RuntimeRoot, operationId)`)
	if leaseDispose < 0 || deleteFiles < 0 || leaseDispose > deleteFiles {
		t.Fatal("elevated request lease must be released before operation files are deleted")
	}

	app := files["App.xaml.cs"]
	for _, want := range []string{
		`e.Args.Any(argument => string.Equals(argument, "--run-elevated-agentdock"`,
		`TryGetStartupRuntimeRoot(e.Args, "--run-elevated-agentdock"`,
		`ControlPanelDiagnostics.IsValidOperationId(operationId)`,
		`Environment.Exit(2)`,
		`RunElevatedNativeCommandHostAsync(startupArguments)`,
		`ControlPanelDiagnostics.RecordFailureExistingLog(`,
		`Runtime.RecordControlPanelFailure("tray", action, ex)`,
	} {
		if !strings.Contains(app, want) {
			t.Fatalf("App.xaml.cs missing management diagnostic behavior %q", want)
		}
	}
	elevatedMode := strings.Index(app, `"--run-elevated-agentdock"`)
	singleInstance := strings.Index(app, `new Mutex(true, MutexName`)
	if elevatedMode < 0 || singleInstance < 0 || elevatedMode > singleInstance {
		t.Fatal("elevated command host must be handled before the control-panel single-instance mutex")
	}

	window := files["MainWindow.xaml.cs"]
	for _, want := range []string{
		`string.IsNullOrWhiteSpace(diagnosticAction) ? "manual-action" : diagnosticAction`,
		`diagnosticAction: action`,
		`"privilege-elevated"`,
		`"privilege-standard"`,
		`ControlPanelDiagnostics.LastNonEmptyLine(ex.Message)`,
	} {
		if !strings.Contains(window, want) {
			t.Fatalf("MainWindow.xaml.cs missing manual action diagnostic behavior %q", want)
		}
	}

	taskAdmin := files[filepath.Join("Services", "TaskAdminService.cs")]
	for _, forbidden := range []string{`--operation-id`, `ControlPanelDiagnostics`} {
		if strings.Contains(taskAdmin, forbidden) {
			t.Fatalf("TaskAdminService.cs must keep its existing exit-code-only contract, found %q", forbidden)
		}
	}

	diagnostics := files[filepath.Join("Services", "ControlPanelDiagnostics.cs")]
	for _, want := range []string{
		`Guid.TryParseExact(operationId, "N"`,
		`ValidateOperationId(operationId) + ".request.json"`,
		`ValidateOperationId(operationId) + ".result.json"`,
		`FileMode.CreateNew`,
		`FileAccess.ReadWrite`,
		`FileShare.Read`,
		`FileAccess.Read`,
		`FileShare.ReadWrite`,
		`SHA256.HashData(requestBytes)`,
		`CryptographicOperations.FixedTimeEquals(actualHash, expectedHash)`,
		`FileMode.Truncate`,
		`RecordFailureCore(runtimeRoot, source, action, exception, createIfMissing: false)`,
		`Path.Combine(logsDirectory, "control-panel.err.log")`,
		`MaxResultDetailBytes = 8 * 1024`,
		`MaxLogDetailBytes = 16 * 1024`,
		`Encoding.UTF8.GetByteCount(detail)`,
	} {
		if !strings.Contains(diagnostics, want) {
			t.Fatalf("ControlPanelDiagnostics.cs missing bounded diagnostic contract %q", want)
		}
	}
	for _, forbidden := range []string{`WriteJsonAtomic(`, `File.Move(temporaryPath, path, overwrite: true)`} {
		if strings.Contains(diagnostics, forbidden) {
			t.Fatalf("elevated result must not replace the medium-integrity pre-created file, found %q", forbidden)
		}
	}
}

func TestWindowsControlPanelDynamicTextUsesSelectedResourceCulture(t *testing.T) {
	path := filepath.Join("..", "..", "desktop", "windows", "control-panel", "Localization", "UiText.cs")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read UiText.cs: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		`private static CultureInfo _resourceCulture`,
		`Resources.GetString(key, _resourceCulture)`,
		`_resourceCulture = culture;`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("Windows dynamic localization contract missing %q", want)
		}
	}
	if strings.Contains(content, `Resources.GetString(key, CultureInfo.CurrentUICulture)`) {
		t.Fatal("Windows dynamic localization must not depend on ambient CurrentUICulture")
	}
}

func TestWindowsUpdateProgressWindowSizesToContent(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "UpdateProgressWindow.xaml"))
	if err != nil {
		t.Fatalf("read UpdateProgressWindow.xaml: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		`SizeToContent="Height"`,
		`x:Name="CloseButton"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("Windows update progress window missing %q", want)
		}
	}
	if strings.Contains(content, `<RowDefinition Height="*" />`) {
		t.Fatal("Windows update progress button row must size to its content")
	}
}

func TestWindowsControlPanelShowsLiveNexusStatusInsideRuntimeStatus(t *testing.T) {
	root := filepath.Join("..", "..", "desktop", "windows", "control-panel")
	files := map[string][]string{
		"MainWindow.xaml": {
			`Text="{local:Loc HealthCheck}" Grid.Row="1"`,
			`Text="Nexus" Grid.Row="2"`,
			`x:Name="NexusStatusText" Grid.Row="2" Grid.Column="1" Text="{local:Loc NotConfigured}"`,
			`Text="{local:Loc Version}" Grid.Row="3"`,
		},
		"MainWindow.xaml.cs": {
			`NexusStatusText.Text`,
			`UiText.Get("Connected")`,
			`UiText.Get("NotConnected")`,
			`UiText.Get("NotConfigured")`,
			`UiText.Get("ConfigurationError")`,
			`snapshot.NexusConnected`,
			`GetSnapshotAsync(includeNexusConnection: true)`,
		},
		filepath.Join("Models", "RuntimeModels.cs"): {
			`bool NexusConnected`,
			`JsonPropertyName("nexus_connected")`,
		},
		filepath.Join("Services", "RuntimeService.cs"): {
			`bool includeNexusConnection = false`,
			`ReadNexusConnectionAsync`,
			`"service", "status", "--runtime-root", RuntimeRoot`,
		},
	}

	for relativePath, wants := range files {
		data, err := os.ReadFile(filepath.Join(root, relativePath))
		if err != nil {
			t.Fatalf("read %s: %v", relativePath, err)
		}
		content := string(data)
		for _, want := range wants {
			if !strings.Contains(content, want) {
				t.Fatalf("Windows Nexus status contract missing %q in %s", want, relativePath)
			}
		}
	}

	xaml, err := os.ReadFile(filepath.Join(root, "MainWindow.xaml"))
	if err != nil {
		t.Fatalf("read MainWindow.xaml: %v", err)
	}
	content := string(xaml)
	for _, forbidden := range []string{`NexusStatusDot`, `NexusHeaderStatusText`, `Nexus ·`} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("Windows Nexus status must stay plain inside runtime status; found %q", forbidden)
		}
	}
}

func TestWindowsACPSettingsUseSinglePageRowsDefaultDropdownAndCustomDialog(t *testing.T) {
	root := filepath.Join("..", "..", "desktop", "windows", "control-panel")
	xamlData, err := os.ReadFile(filepath.Join(root, "MainWindow.xaml"))
	if err != nil {
		t.Fatalf("read MainWindow.xaml: %v", err)
	}
	codeData, err := os.ReadFile(filepath.Join(root, "MainWindow.xaml.cs"))
	if err != nil {
		t.Fatalf("read MainWindow.xaml.cs: %v", err)
	}
	xaml := string(xamlData)
	code := string(codeData)
	for _, want := range []string{
		`x:Name="AcpProfileListPanel"`,
		`x:Name="AcpDefaultProfileComboBox"`,
		`SelectionChanged="AcpDefaultProfile_SelectionChanged"`,
		`Content="{local:Loc AddCustomAcp}"`,
	} {
		if !strings.Contains(xaml, want) {
			t.Fatalf("Windows ACP single-page contract missing %q", want)
		}
	}
	for _, want := range []string{
		`ShowCustomAcpProfileDialog`,
		`AcpCustomProfileEdit_Click`,
		`nameView.MouseLeftButtonUp += AcpCustomProfileEdit_Click`,
		`HorizontalAlignment = HorizontalAlignment.Stretch`,
		`AcpDefaultProfile_SelectionChanged`,
		`UniqueCustomAcpProfileId(result.Name)`,
		`profile.DisplayName = result.Name`,
		`profile.Command = result.Command`,
		`profile.Args = result.Arguments`,
		`profile.Id`,
	} {
		if !strings.Contains(code, want) {
			t.Fatalf("Windows ACP behavior contract missing %q", want)
		}
	}
	for _, forbidden := range []string{
		`Text="Profile ID"`,
		`AcpProfileIdTextBox`,
		`AcpAgentComboBox`,
		`AcpDetailPanel`,
		`AcpOverviewPanel`,
		`AcpProfileNameTextBox`,
		`AcpProfileDefaultCheckBox`,
		`AcpStatusText`,
		`ShowAcpDetail`,
		`ShowAcpOverview`,
		`Not configured`,
		`★ Default`,
		`Text = "›"`,
		`BUILT-IN AGENTS`,
		`CUSTOM AGENTS`,
		`var edit = new Button`,
	} {
		if strings.Contains(xaml, forbidden) || strings.Contains(code, forbidden) {
			t.Fatalf("Windows ACP UI still exposes old detail/status/group contract %q", forbidden)
		}
	}

	mcpAppsIndex := strings.Index(xaml, `x:Name="McpAppsEnabledCheckBox" Grid.Row="0"`)
	portIndex := strings.Index(xaml, `x:Name="PortTextBox" Grid.Row="1"`)
	if mcpAppsIndex < 0 || portIndex < 0 || mcpAppsIndex > portIndex {
		t.Fatal("Windows basic settings must place MCP Apps UI above the service port")
	}
}

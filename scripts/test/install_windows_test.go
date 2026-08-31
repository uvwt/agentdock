package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallWindowsUsesChecksumsDPAPIAndCurrentUserStartup(t *testing.T) {
	data, err := os.ReadFile("../install/install.ps1")
	if err != nil {
		t.Fatalf("read install.ps1: %v", err)
	}
	for index, value := range data {
		if value > 0x7f {
			t.Fatalf("install.ps1 must remain ASCII for Windows PowerShell 5.1; non-ASCII byte at offset %d", index)
		}
	}

	script := string(data)
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		for _, keyword := range []string{"else", "elseif", "catch", "finally"} {
			if trimmed == keyword || strings.HasPrefix(trimmed, keyword+" ") {
				t.Fatalf("install.ps1 must keep %s on the same line as the preceding closing brace: %q", keyword, line)
			}
		}
	}

	for _, want := range []string{
		"agentdock_windows_$architecture.zip",
		"[string] $OfflineArchive = ''",
		"[string] $OfflineChecksumFile = ''",
		"[string] $OfflineCloudflaredBinary = ''",
		"Using bundled AgentDock payload",
		"-SourceBinary $OfflineCloudflaredBinary",
		"agentdock-tray.exe",
		"agentdock.ico",
		"manage-windows.ps1",
		"Initialize-OAuthCredentials",
		"named-server-url.txt",
		"[switch] $ConfigurePublicAccess",
		"[string] $TunnelTokenFile = ''",
		"[switch] $DeleteTunnelTokenFile",
		"Write-RuntimeManifest",
		"Write-InstallResult",
		"runtime.json",
		"desktop-version.txt",
		"$destinationBinary version --json",
		"function Set-RunValue",
		"Unable to prepare current-user startup registry key",
		"Unable to write current-user startup registry value",
		"Set-RunValue -RegistryPath $runKey -Name $trayRunValueName",
		"cloudflared-windows-$Architecture.exe",
		"Get-Sha256Hex -Path $archivePath",
		"[System.Security.Cryptography.SHA256]::Create()",
		"-ErrorRecord $installError",
		"ErrorType=$safeErrorType",
		"ErrorStack=$safeErrorStack",
		"Stop-AgentDockForUpgrade -BinaryPath $destinationBinary",
		"Get-ProcessesByPath -ProcessName 'agentdock'",
		"Get-CimInstance Win32_Process",
		"ExecutablePath",
		"Get-AgentDockTaskState",
		"Get-InteractiveDesktopUser",
		"Start-ElevatedAgentDockTaskAction",
		"--task-admin $Action",
		"--backup-directory",
		"--launcher-path",
		"--runtime-root",
		"--user-sid",
		"--user-name",
		"prepare-elevated",
		"setup-elevated-context",
		"Start Setup normally under the signed-in account",
		"Stop-CloudflaredForUpgrade -BinaryPath $cloudflaredBinary",
		"Copy-Item -LiteralPath $destinationBinary -Destination $binaryBackup -Force",
		"Install-AgentDockBinary -SourceBinary $sourceBinary -DestinationBinary $destinationBinary",
		"Write-ProtectedText -Path $tokenPath",
		"Write-ProtectedText -Path $PasswordPath",
		"Write-ProtectedText -Path $TokenSecretPath",
		"Write-ProtectedText -Path $tunnelTokenPath",
		"Copy-Item -LiteralPath $binaryBackup -Destination $destinationBinary -Force",
		"DataProtectionScope]::CurrentUser",
		"HKCU:\\Software\\Microsoft\\Windows\\CurrentVersion\\Run",
		"Set-RunValue -RegistryPath $runKey -Name $runValueName",
		"Set-RunValue -RegistryPath $runKey -Name $cloudflaredRunValueName",
		"service launch-core --runtime-root",
		"--start-core --runtime-root",
		"& $destinationBinary service start --runtime-root $runtimeDir",
		"& $destinationBinary tunnel start --runtime-root $runtimeDir",
		"--start-tunnel --runtime-root",
		"-AdminLauncherPath $sourceTrayBinary",
		"-LauncherPath $destinationTrayBinary",
		"-FilePath $AdminLauncherPath",
		"Start-CloudflaredLauncher -LauncherPath $cloudflaredLauncherPath",
		"Wait-QuickTunnelUrl -LogPaths @($cloudflaredStdoutLogPath, $cloudflaredStderrLogPath)",
		"Wait-QuickTunnelReady -Path $quickTunnelUrlPath -ExpectedUrl $publicUrl",
		"quick-tunnel-url.txt",
		"Restart-AgentDockForQuickTunnel",
		"Update-RuntimePublicUrl -PublicUrl `$publicUrl",
		"Write-TextAtomically -Path '$escapedServerUrlPath' -Value `$publicUrl",
		"Write-TextAtomically -Path '$escapedQuickTunnelUrlPath' -Value `$publicUrl",
		"RedirectStandardOutput = '$escapedCloudflaredStdoutLogPath'",
		"RedirectStandardError = '$escapedCloudflaredStderrLogPath'",
		"RuntimeInformation]::OSArchitecture",
		"Authentication: Bearer Token and OAuth are both enabled.",
		"$coreSkillOutput = @(& $destinationBinary skill bootstrap --bundle $coreSkillBundle 2>&1)",
		"-ErrorCode $installErrorCode",
		"http://127.0.0.1:$HealthPort/healthz",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("install.ps1 missing %q", want)
		}
	}
	for _, forbidden := range []string{"[string] $RuntimeVersion", "version = $RuntimeVersion"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("install.ps1 must not persist the AgentDock version in runtime.json: %q", forbidden)
		}
	}
	for _, forbidden := range []string{
		"New-Item -Path $runKey -Force",
		"New-Item -Path $RegistryPath -Force",
		"New-ItemProperty -Path $runKey",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("install.ps1 must route current-user startup writes through Set-RunValue instead of %q", forbidden)
		}
	}
	if got := strings.Count(script, "Set-RunValue -RegistryPath $runKey"); got != 6 {
		t.Fatalf("install.ps1 must use Set-RunValue for all startup writes in install and rollback paths; got %d calls", got)
	}
	stopCall := strings.Index(script, "Stop-AgentDockForUpgrade -BinaryPath $destinationBinary")
	replaceCall := strings.Index(script, "Install-AgentDockBinary -SourceBinary $sourceBinary -DestinationBinary $destinationBinary")
	if stopCall < 0 || replaceCall < 0 || stopCall > replaceCall {
		t.Fatal("install.ps1 must stop the running instance before replacing agentdock.exe")
	}
	backupCall := strings.Index(script, "Copy-Item -LiteralPath $destinationBinary -Destination $binaryBackup -Force")
	if backupCall < stopCall || backupCall > replaceCall {
		t.Fatal("install.ps1 must back up the stopped binary before replacement")
	}
	manifestCall := strings.Index(script, "$manifestTunnelMode = $resolvedTunnelMode")
	coreStartCall := strings.Index(script, "& $destinationBinary service start --runtime-root $runtimeDir")
	tunnelStartCall := strings.Index(script, "& $destinationBinary tunnel start --runtime-root $runtimeDir")
	if manifestCall < 0 || coreStartCall < 0 || tunnelStartCall < 0 || manifestCall > coreStartCall || manifestCall > tunnelStartCall {
		t.Fatal("install.ps1 must write the runtime manifest before native core and Tunnel startup")
	}

	const securityAssemblyLoad = "Add-Type -AssemblyName System.Security"
	if got := strings.Count(script, securityAssemblyLoad); got != 2 {
		t.Fatalf("install.ps1 must load System.Security in the installer and generated tunnel launcher; got %d occurrences", got)
	}
	if !strings.Contains(script, "-Verb RunAs") {
		t.Fatal("Windows installer must elevate only the scheduled-task helper")
	}
	if strings.Contains(script, "-FilePath $powerShellPath") {
		t.Fatal("Windows installer UAC must elevate AgentDock instead of powershell.exe")
	}
	if !strings.Contains(script, "DataProtectionScope]::CurrentUser") {
		t.Fatal("Windows secrets must remain bound to the interactive user")
	}
	if strings.Contains(script, "Start-Process -FilePath $destinationBinary -Verb RunAs") {
		t.Fatal("the installer must not launch the core directly under a different administrator account")
	}
	if strings.Contains(script, "current account cannot elevate") {
		t.Fatal("scheduled-task cleanup must request UAC instead of rejecting a standard user before elevation")
	}
	if strings.Contains(script, "--token $TunnelToken") || strings.Contains(script, "--token `$env:TUNNEL_TOKEN") {
		t.Fatal("cloudflared token must be decrypted into its environment, not placed in process arguments")
	}
	if strings.Contains(script, "Get-FileHash") {
		t.Fatal("install.ps1 must compute runtime SHA-256 without depending on Get-FileHash")
	}
	for _, forbidden := range []string{
		"Set-PrivateAcl",
		"Get-Acl",
		"Set-Acl",
		"icacls.exe",
		"$icaclsArguments",
		"$AclSelfTest",
		"SetSecurityDescriptorSddlForm(",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("install.ps1 still contains removed privileged startup or ACL code %q", forbidden)
		}
	}
	for _, incompatible := range []string{
		"RandomNumberGenerator]::Fill",
		"Convert]::ToHexString",
		`Replace(\"`,
	} {
		if strings.Contains(script, incompatible) {
			t.Fatalf("install.ps1 contains Windows PowerShell 5.1 incompatible syntax %q", incompatible)
		}
	}
}
func TestWindowsManagerKeepsTunnelTransitionsAndManualTaskStartValid(t *testing.T) {
	data, err := os.ReadFile("../install/manage-windows.ps1")
	if err != nil {
		t.Fatalf("read manage-windows.ps1: %v", err)
	}
	if len(data) < 3 || data[0] != 0xef || data[1] != 0xbb || data[2] != 0xbf {
		t.Fatal("manage-windows.ps1 must use UTF-8 with BOM for Windows PowerShell 5.1")
	}

	script := string(data)
	for _, want := range []string{
		"function Start-TaskPreservingStartupState",
		"Enable-ScheduledTask -TaskName $TaskName",
		"Disable-ScheduledTask -TaskName $TaskName",
		"Update-RuntimeManifest -RuntimePort $settings.port -RuntimeMode 'none' -PublicUrl ''",
		"$Manifest.PSObject.Properties.Remove('version')",
		"function Resolve-RuntimeManagedPath",
		"-RecordedRuntimeRoot $RecordedInstallRoot",
		"Set-ObjectProperty -Object $Manifest -Name 'install_root' -Value $RuntimeRoot",
		"named-server-url.txt",
		"quick-tunnel-url.txt",
		"'regenerate-quick'",
		"'set-startup'",
		"--task-admin set-enabled",
		"-FilePath $TrayBinary",
		"'start-tunnel'",
		"'stop-tunnel'",
		"& $AgentDockBinary service launch-core --runtime-root $RuntimeRoot",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("manage-windows.ps1 missing %q", want)
		}
	}
	if strings.Contains(script, "RuntimeMode 'quick' -PublicUrl ''") {
		t.Fatal("Quick Tunnel transition must not write an invalid quick manifest without public_url")
	}
	for _, want := range []string{
		"& $AgentDockBinary tunnel $Command --runtime-root $RuntimeRoot",
		"'tunnel', 'configure'",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("manage-windows.ps1 must delegate Tunnel operations to native commands: %q", want)
		}
	}
}
func TestWindowsUninstallerCleansManagedTunnelState(t *testing.T) {
	data, err := os.ReadFile("../install/uninstall-windows.ps1")
	if err != nil {
		t.Fatalf("read uninstall-windows.ps1: %v", err)
	}
	script := string(data)
	for _, want := range []string{
		"Get-CimInstance Win32_Process",
		"Get-ProcessIdsByPath",
		"Stop-ProcessByPath -ProcessName 'agentdock-tray'",
		"[switch] $KeepInstallDir",
		"Remove-DirectoryWithRetry -Path $InstallDir",
		"Stop-ProcessByPath -ProcessName 'cloudflared'",
		"Remove-ItemProperty -LiteralPath $runKey -Name $TrayStartupValueName",
		"'runtime.json'",
		"Remove-ItemProperty -LiteralPath $runKey -Name $CloudflaredStartupValueName",
		"'start-cloudflared.ps1'",
		"'installer\\manage-windows.ps1'",
		"'named-server-url.txt'",
		"'control-panel-settings.json'",
		"'oauth-password.dpapi'",
		"'oauth-token-secret.dpapi'",
		"'oauth-access-token-ttl.txt'",
		"'cloudflared-token.dpapi'",
		"'cloudflared.out.log'",
		"'cloudflared.err.log'",
		"'quick-tunnel-url.txt'",
		"$StartupValueName -eq 'AgentDock' -and $CloudflaredStartupValueName -eq 'AgentDockCloudflared' -and $TrayStartupValueName -eq 'AgentDockTray'",
		"Remove-AgentDockScheduledTask",
		"-AdminLauncherPath $trayBinary",
		"--task-admin remove",
		"--runtime-root",
		"-Verb RunAs",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("uninstall-windows.ps1 missing %q", want)
		}
	}
}
func TestWindowsTaskAdminUsesNativeAgentDockHelper(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "Services", "TaskAdminService.cs"))
	if err != nil {
		t.Fatalf("read TaskAdminService.cs: %v", err)
	}
	source := string(data)
	for _, want := range []string{
		"WindowsPrincipal",
		"Schedule.Service",
		"TaskRunLevelHighest",
		"TaskLogonInteractiveToken",
		"--run-core-task --runtime-root",
		"SetSecurityDescriptor",
		"prepare-elevated",
		"prepare-standard",
		"restore",
		"remove",
		"set-enabled",
		"StopInstalledCore",
		"Process.GetProcessesByName(\"agentdock\")",
		"process.Kill(entireProcessTree: true)",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("TaskAdminService.cs missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"powershell.exe",
		"File.Exists(request.LauncherPath)",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("TaskAdminService.cs must not depend on %q", forbidden)
		}
	}
}
func TestWindowsElevatedCoreHostUsesKillOnCloseJob(t *testing.T) {
	jobData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "Services", "KillOnCloseJob.cs"))
	if err != nil {
		t.Fatalf("read KillOnCloseJob.cs: %v", err)
	}
	jobSource := string(jobData)
	for _, want := range []string{
		"CreateJobObject",
		"JobObjectLimitKillOnJobClose",
		"SetInformationJobObject",
		"AssignProcessToJobObject",
	} {
		if !strings.Contains(jobSource, want) {
			t.Fatalf("KillOnCloseJob.cs missing %q", want)
		}
	}

	runtimeData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "Services", "RuntimeService.cs"))
	if err != nil {
		t.Fatalf("read RuntimeService.cs: %v", err)
	}
	runtimeSource := string(runtimeData)
	for _, want := range []string{
		"KillOnCloseJob.Create()",
		"job.Assign(process)",
		"CreateNoWindow = true",
		"WindowStyle = ProcessWindowStyle.Hidden",
	} {
		if !strings.Contains(runtimeSource, want) {
			t.Fatalf("RuntimeService.cs missing elevated Core host behavior %q", want)
		}
	}
}
func TestWindowsSetupKeepsPublicAccessExplicitAndSecretsOffCommandLine(t *testing.T) {
	var setupBuilder strings.Builder
	for _, path := range []string{
		filepath.Join("..", "..", "packaging", "windows", "AgentDock.iss"),
		filepath.Join("..", "..", "packaging", "windows", "includes", "messages.iss"),
		filepath.Join("..", "..", "packaging", "windows", "includes", "code.iss"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read Windows Setup source %s: %v", path, err)
		}
		setupBuilder.Write(data)
		setupBuilder.WriteByte('\n')
	}
	setup := setupBuilder.String()
	for _, want := range []string{
		"PrivilegesRequired=lowest",
		"SetupLogging=yes",
		"#include \"includes\\messages.iss\"",
		"#include \"includes\\code.iss\"",
		"DisableDirPage=yes",
		"LanguageDetectionMethod=uilanguage",
		"AgentDock active language: ",
		"Name: \"chinesesimplified\"",
		"DetectExistingInstallation",
		"ExistingInstallSource := 'setup'",
		"ExistingInstallSource := 'powershell'",
		"LoadExistingSettings",
		"LegacyAgentDockScheduledTaskExists",
		"/Query /TN \"\\AgentDock\"",
		"AgentDock legacy scheduled task detected.",
		"cloudflared-token.dpapi",
		"-TunnelMode ",
		"-TunnelTokenFile ",
		"-DeleteTunnelTokenFile",
		"-InstallChannel setup",
		"-CorePrivilegeMode ",
		"ElevatedCoreOption",
		"UpgradeKeepSettings",
		"UpgradeChangeSettings",
		"RuntimeUsesElevatedCore",
		"ElevatedSetupUnsupported",
		"GetIniString('AgentDock', 'Code'",
		"GetIniString('AgentDock', 'ErrorType'",
		"GetIniString('AgentDock', 'ErrorLine'",
		"GetIniString('AgentDock', 'ErrorStack'",
		"AgentDock installation diagnostics: type=",
		"AgentDock installation stack: ",
		"#ifdef SignedBuild",
		"SignedUninstaller=yes",
		"PersistSetupLog",
		"ExpandConstant('{log}')",
		"{localappdata}\\AgentDock\\logs\\installer",
		"GetDateTimeString('yyyymmdd-hhnnss-zzz'",
		"CopyFile(SourceLog, PersistentLog, True)",
		"original log remains at: ",
		"DeinitializeSetup",
		"function InitializeUninstall(): Boolean",
		"procedure CurUninstallStepChanged",
		"usAppMutexCheck",
		"managed cleanup completed successfully",
		"GetUninstallParameters('')",
		"[UninstallDelete]",
		"-KeepInstallDir",
		"PurgeStateQuestion",
		"Bearer Token：",
		"AgentDockSetup-amd64",
		"AgentDockSetup-arm64",
		"agentdock_windows_{#PayloadArchitecture}.zip",
		"Source: \"{#OfflinePayloadDir}\\cloudflared.exe\"",
		"-OfflineArchive ",
		"-OfflineChecksumFile ",
		"-OfflineCloudflaredBinary ",
		"CreateOutputProgressPage",
		"OfflineProgressDescription",
		"FinishedControlPanel",
		"DesktopShortcutCheckBox",
		"DesktopShortcutCheckBox.Checked := True",
		"ApplyDesktopControlPanelShortcut",
		"CreatedShortcutPath := CreateShellLink",
		"Result := CreatedShortcutPath <> ''",
		"DesktopShortcutCheckBox.Left := WizardForm.FinishedLabel.Left",
		"{userdesktop}\\{code:GetLocalizedMessage|DesktopShortcutName}.lnk",
		"if CurPageID = wpFinished then",
		"{app}\\bin\\agentdock-tray.exe",
	} {
		if !strings.Contains(setup, want) {
			t.Fatalf("AgentDock.iss missing %q", want)
		}
	}
	if strings.Contains(setup, " -TunnelToken ") {
		t.Fatal("Setup must pass the Cloudflare Tunnel Token through a temporary file, not process arguments")
	}
	for _, forbidden := range []string{
		"ResultMemo",
		"CopyLocalButton",
		"GetIniString('AgentDock', 'BearerToken'",
		"GetIniString('AgentDock', 'OAuthPassword'",
		"完成后会自动打开控制面板",
	} {
		if strings.Contains(setup, forbidden) {
			t.Fatalf("Setup completion page must not expose connection details or credentials: %q", forbidden)
		}
	}
	for _, forbidden := range []string{
		"(recommended)",
		"（推荐）",
		"The tray stays a normal user process",
		"托盘始终使用普通用户权限",
	} {
		if strings.Contains(setup, forbidden) {
			t.Fatalf("Setup must not show recommendation or privilege implementation details: %q", forbidden)
		}
	}
}
func TestWindowsSetupLaunchesRuntimeOutsideRedirectionGuardTree(t *testing.T) {
	installData, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install", "install.ps1"))
	if err != nil {
		t.Fatalf("read install.ps1: %v", err)
	}
	brokerData, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install", "launch-windows-process.ps1"))
	if err != nil {
		t.Fatalf("read launch-windows-process.ps1: %v", err)
	}
	setupData, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "includes", "code.iss"))
	if err != nil {
		t.Fatalf("read code.iss: %v", err)
	}
	definitionData, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "AgentDock.iss"))
	if err != nil {
		t.Fatalf("read AgentDock.iss: %v", err)
	}

	installScript := strings.ReplaceAll(string(installData), "\r\n", "\n")
	brokerScript := strings.ReplaceAll(string(brokerData), "\r\n", "\n")
	setupScript := strings.ReplaceAll(string(setupData), "\r\n", "\n")
	definition := strings.ReplaceAll(string(definitionData), "\r\n", "\n")

	for _, want := range []string{
		"$setupRuntimeLauncherPath = Join-Path $PSScriptRoot 'launch-windows-process.ps1'",
		"function Invoke-SetupRuntimeProcess",
		"if ($InstallChannel -eq 'setup') {\n                Invoke-SetupRuntimeProcess `\n                    -FilePath $destinationBinary `\n                    -Arguments \"service start --runtime-root",
		"if ($InstallChannel -eq 'setup') {\n                Invoke-SetupRuntimeProcess `\n                    -FilePath $destinationBinary `\n                    -Arguments \"tunnel start --runtime-root",
		"Invoke-SetupRuntimeProcess -FilePath $BinaryPath -Arguments '--background'",
		"Invoke-SetupRuntimeProcess -FilePath (Join-Path $PSHOME 'powershell.exe') -Arguments $arguments",
	} {
		if !strings.Contains(installScript, want) {
			t.Fatalf("install.ps1 must route Setup-owned long-lived launches through the runtime broker; missing %q", want)
		}
	}

	for _, want := range []string{
		"New-ScheduledTaskAction",
		"New-ScheduledTaskPrincipal",
		"-LogonType Interactive",
		"-RunLevel Limited",
		"Register-ScheduledTask",
		"Start-ScheduledTask",
		"if ($WaitForExit) {",
		"$process.WaitForExit()",
		"$wrapperLines += 'exit 0'",
		"Unregister-ScheduledTask",
		"AGENTDOCK_HOME",
		"AGENTDOCK_DEFAULT_DIR",
	} {
		if !strings.Contains(brokerScript, want) {
			t.Fatalf("runtime launch broker missing %q", want)
		}
	}
	if !strings.Contains(brokerScript, "finally {") || !strings.Contains(brokerScript, "Unregister-ScheduledTask") {
		t.Fatal("runtime launch broker must remove its temporary task even when launch fails")
	}

	for _, want := range []string{
		"Source: \"..\\..\\scripts\\install\\launch-windows-process.ps1\"; Flags: dontcopy",
		"ExtractTemporaryFile('launch-windows-process.ps1')",
		"function LaunchRuntimeProcess(",
		"LaunchRuntimeProcess(ExpandConstant('{app}\\bin\\agentdock-tray.exe'), '')",
	} {
		if !strings.Contains(definition+"\n"+setupScript, want) {
			t.Fatalf("Windows Setup must package and use the runtime launch broker; missing %q", want)
		}
	}
	if strings.Contains(strings.ToLower(definition), "redirectionguard=no") || strings.Contains(strings.ToLower(definition), "/noredirectionguard") {
		t.Fatal("Windows Setup must keep Inno RedirectionGuard enabled instead of disabling the mitigation globally")
	}
	legacyFinishLaunch := "if not Exec(\n      ExpandConstant('{app}\\bin\\agentdock-tray.exe')"
	if strings.Contains(setupScript, legacyFinishLaunch) {
		t.Fatal("Setup finish page must not launch the long-lived tray directly from the RedirectionGuard process tree")
	}
}
func TestWindowsSetupIncludesSimplifiedChineseBaseMessages(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "languages", "ChineseSimplified.isl"))
	if err != nil {
		t.Fatalf("read ChineseSimplified.isl: %v", err)
	}
	language := string(data)
	for _, want := range []string{
		"LanguageID=$0804",
		"ButtonNext=下一步",
		"ButtonCancel=取消",
		"WelcomeLabel1=欢迎使用",
		"FinishedHeadingLabel=[name] 安装完成",
		"ConfirmUninstall=确实要完全删除",
	} {
		if !strings.Contains(language, want) {
			t.Fatalf("ChineseSimplified.isl missing %q", want)
		}
	}
}
func TestWindowsSigningPinsConfiguredSelfSignedCertificate(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "sign-windows.ps1"))
	if err != nil {
		t.Fatalf("read sign-windows.ps1: %v", err)
	}
	script := string(data)
	for _, want := range []string{
		"SignerCertificate.Thumbprint -ne $expectedCertificate.Thumbprint",
		"Test-IsExpectedSelfSignedTrustFailure",
		"@('UnknownError', 'NotTrusted')",
		"certificate chain processed",
		"$signature.Status -eq 'Valid'",
		"signtool verify failed",
		"X509KeyStorageFlags]::EphemeralKeySet",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("sign-windows.ps1 missing self-signed verification requirement %q", want)
		}
	}
	for _, forbidden := range []string{"StoreLocation]::CurrentUser", "TrustedPublisher", "TrustedPeople"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("sign-windows.ps1 must not modify Windows trust stores: %q", forbidden)
		}
	}
}

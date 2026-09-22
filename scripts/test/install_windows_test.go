package scripts

import (
	"os"
	"path/filepath"
	"strconv"
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
		"agentdock-arbiter.exe",
		"agentdock-shim.exe",
		"agentdock-tray-shim.exe",
		"agentdock.ico",
		"manage-windows.ps1",
		"Initialize-OAuthCredentials",
		"named-server-url.txt",
		"[switch] $ConfigurePublicAccess",
		"[string] $TunnelTokenFile = ''",
		"[switch] $DeleteTunnelTokenFile",
		"$runtimeAgentDockHome = [Environment]::GetEnvironmentVariable('AGENTDOCK_HOME', 'Process')",
		"$runtimeAgentDockDefaultDir = [Environment]::GetEnvironmentVariable('AGENTDOCK_DEFAULT_DIR', 'Process')",
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
		"-ErrorRecord $resultErrorRecord",
		"ErrorType=$safeErrorType",
		"ErrorStack=$safeErrorStack",
		"Stop-AgentDockForUpgrade -BinaryPath $existingGenerationCore",
		"Stop-AgentDockForUpgrade -BinaryPath $destinationBinary",
		"$processName = [IO.Path]::GetFileNameWithoutExtension($BinaryPath)",
		"Get-ProcessesByPath -ProcessName $processName -BinaryPath $BinaryPath",
		"Get-CimInstance Win32_Process",
		"ExecutablePath",
		"Get-AgentDockTaskState",
		"Conflicting = $false",
		"$state.Conflicting = $true",
		"-RuntimeRoot $runtimeDir",
		"-StableCorePath $destinationBinary",
		"-StableTrayPath $destinationTrayBinary",
		"The AgentDock scheduled task belongs to another installation root",
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
		"$tunnelSupervisorPidPath = Join-Path $runtimeDir 'tunnel-supervisor.pid'",
		"$tunnelStopOutput = @(& $existingGenerationCore tunnel stop --runtime-root $runtimeDir 2>&1)",
		"Stop-CloudflaredForUpgrade -BinaryPath $cloudflaredBinary",
		"Copy-Item -LiteralPath $destinationBinary -Destination $binaryBackup -Force",
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
		"--start-tunnel --runtime-root",
		"$tunnelStartupArguments = \"--start-tunnel --runtime-root",
		"-FilePath $destinationTrayBinary",
		"-Arguments $tunnelStartupArguments",
		"-AdminLauncherPath $sourceTrayBinary",
		"-LauncherPath $destinationTrayBinary",
		"-FilePath $AdminLauncherPath",
		"Start-CloudflaredLauncher -LauncherPath $cloudflaredLauncherPath",
		"quick-tunnel-url.txt",
		"& '$escapedBinaryPath' tunnel launch --runtime-root '$escapedRuntimeDir'",
		"RuntimeInformation]::OSArchitecture",
		"Authentication: Bearer Token and OAuth are both enabled.",
		"-ErrorCode $resultErrorCode",
		"$resultErrorCode = 'elevated-task-rollback-failed'",
		"$resultErrorCode = 'rollback-failed'",
		"$installWarningCode = 'runtime-launch-deferred'",
		"$installWarningCode = \"$installWarningCode,runtime-launch-deferred\"",
		"$installWarningCode = 'tunnel-start-deferred'",
		"$installWarningCode = \"$installWarningCode,tunnel-start-deferred\"",
		"Public access is starting in the background.",
		"Tunnel startup continues in the background; readiness is shown in the control panel and logs.",
		"-ErrorCode 'install-validation-failed'",
		"scheduled-task-recovery-",
		"Recovery files: $taskRecoveryPath",
		"$taskTransactionCommitted = $taskTransactionStarted",
		"Incomplete installer generation pointer",
		"active-version.json",
		"Name = 'active-version.json'",
		"Name = 'update-transaction.json'",
		"Name = 'update-result.json'",
		"$versionsDir = Join-Path $runtimeDir 'versions'",
		"Resolving pending AgentDock generation transaction before Setup continues",
		"$recoveryOutput = @(& $destinationBinary version --json 2>&1)",
		"Setup will not modify an unresolved generation",
		"AgentDock payload preflight failed with exit code",
		"Release archive does not contain an Installer Engine capable AgentDock binary.",
		"'--payload-dir', $extractDir",
		"$stableFilesMayBeReplaced = $true",
		"http://127.0.0.1:$HealthPort/healthz",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("install.ps1 missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"Wait-CloudflaredRunning",
		"Wait-QuickTunnelUrl",
		"Wait-QuickTunnelReady",
		"Installer Engine finished trial without a Quick Tunnel public address.",
		"& $destinationBinary tunnel start --runtime-root $runtimeDir",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("install.ps1 must not gate install/update completion on Tunnel/public readiness: %q", forbidden)
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
	if strings.Contains(script, "Stop-ProcessesForUpgrade -ProcessName 'agentdock' -BinaryPath $BinaryPath") {
		t.Fatal("generation Core stop logic must derive the process name from agentdock-core.exe instead of assuming agentdock.exe")
	}
	legacyPrepareCall := strings.Index(script, "install prepare-windows-legacy")
	stopCall := strings.Index(script, "Stop-AgentDockForUpgrade -BinaryPath $destinationBinary")
	backupCall := strings.Index(script, "Copy-Item -LiteralPath $destinationBinary -Destination $binaryBackup -Force")
	engineCall := strings.Index(script, "$engineJson = (& $sourceBinary @engineArgs")
	if legacyPrepareCall < 0 || stopCall < 0 || backupCall < 0 || engineCall < 0 ||
		legacyPrepareCall > stopCall || stopCall > backupCall || backupCall > engineCall {
		t.Fatal("legacy source generation must be prepared before old Core stop; stable backup must precede the Installer Engine payload publish")
	}
	probeCall := strings.Index(script, "install --engine-ready")
	if probeCall < 0 || probeCall > legacyPrepareCall {
		t.Fatal("Release payload must be proven Engine-ready before legacy migration or process mutation")
	}
	supervisorStopCall := strings.Index(script, "$tunnelStopOutput = @(& $existingGenerationCore tunnel stop --runtime-root $runtimeDir 2>&1)")
	cloudflaredStopCall := strings.Index(script, "[void] (Stop-CloudflaredForUpgrade -BinaryPath $cloudflaredBinary)")
	tunnelTokenWriteCall := strings.Index(script, "Write-ProtectedText -Path $tunnelTokenPath -Value $TunnelToken")
	if supervisorStopCall < 0 || cloudflaredStopCall < 0 || tunnelTokenWriteCall < 0 ||
		supervisorStopCall > cloudflaredStopCall || cloudflaredStopCall > tunnelTokenWriteCall {
		t.Fatal("managed Tunnel supervisor must stop before cloudflared replacement and protected Token mutation")
	}
	if strings.Contains(script, "$engineOwnsTargetGeneration") {
		t.Fatal("generation ownership must be decided by the Installer Engine, not by a PowerShell boolean")
	}
	if !strings.Contains(script, "install inspect --state-root $runtimeDir") {
		t.Fatal("Setup must read the generation pointer state through the Installer Engine inspect, not by parsing active-version.json")
	}
	if !strings.Contains(script, "install prepare-windows-legacy") {
		t.Fatal("pre-generation Windows installs must seed a committed legacy source before the current Engine publishes target files")
	}
	if !strings.Contains(script, "Get-AgentDockProcesses -BinaryPath $existingGenerationCore") ||
		!strings.Contains(script, "Get-AgentDockProcesses -BinaryPath $destinationBinary") {
		t.Fatal("generation retries must probe both source generation and legacy stable Core paths")
	}
	if !strings.Contains(script, "Stop-AgentDockForUpgrade -BinaryPath $existingGenerationCore") ||
		strings.Count(script, "Stop-AgentDockForUpgrade -BinaryPath $destinationBinary") < 2 {
		t.Fatal("generation retries must stop both source generation and crash-left legacy stable Core paths")
	}
	for _, forbidden := range []string{
		"function Write-RuntimeManifest",
		"generationStagingDirectory",
		"generationRepairBackupDirectory",
		"Install-AgentDockBinary",
		"$coreSkillOutput =",
		"$engineReady =",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("PowerShell must not reintroduce an Installer Engine-owned state machine: %q", forbidden)
		}
	}
	if !strings.Contains(script, "'--payload-dir', $extractDir") {
		t.Fatal("Installer Engine must be the sole payload publisher through --payload-dir")
	}
	if strings.Contains(script, "update --local-archive") || strings.Contains(script, "Delegating Setup upgrade to the AgentDock Update Engine") {
		t.Fatal("Setup must not commit a target generation through Update Engine before Installer/OS adapter commit")
	}
	if strings.Contains(script, "Install-AgentDockBinary -SourceBinary $sourceBinary -DestinationBinary $destinationBinary") {
		t.Fatal("install.ps1 must not restore the legacy in-place Core replacement path")
	}
	if strings.Contains(script, "-not $generationUpgradeHandled -and $generationLayoutDetected") {
		t.Fatal("Setup rollback must stop the target generation even after Update Engine committed")
	}
	tunnelArg := strings.Index(script, "'--tunnel-mode', $resolvedTunnelMode")
	coreStartCall := strings.Index(script, "& $destinationBinary service start --runtime-root $runtimeDir")
	tunnelProxyCall := strings.Index(script, "$tunnelStartupArguments = \"--start-tunnel --runtime-root")
	tunnelCommitCall := strings.LastIndex(script, "$commitArgs = @(")
	if tunnelArg < 0 || coreStartCall < 0 || tunnelProxyCall < 0 || tunnelCommitCall < 0 || tunnelArg > coreStartCall || tunnelCommitCall > tunnelProxyCall {
		t.Fatal("Installer must pass tunnel intent to the Engine, commit the Core transaction, then launch Tunnel asynchronously")
	}
	if strings.Contains(script, "$manifestTunnelMode = 'none'") {
		t.Fatal("Quick Tunnel must not rewrite Engine tunnel-mode to none")
	}
	if !strings.Contains(script, "'--tunnel-mode', $resolvedTunnelMode") {
		t.Fatal("Engine must receive the real resolved tunnel mode, including quick")
	}
	for _, identity := range []string{
		"'--startup-value-name', $runValueName",
		"'--tray-startup-value-name', $trayRunValueName",
		"'--cloudflared-startup-value-name', $cloudflaredRunValueName",
	} {
		if !strings.Contains(script, identity) {
			t.Fatalf("Installer Engine must receive the adapter's exact Windows startup identity: %s", identity)
		}
	}
	if !strings.Contains(script, "$engineArgs += @('--task-name', 'AgentDock')") {
		t.Fatal("elevated Engine installs must receive the real AgentDock scheduled task name")
	}
	if strings.Contains(script, "Write-ActiveVersionState") {
		t.Fatal("active-version.json is engine-owned; install.ps1 must not define or call Write-ActiveVersionState")
	}
	if !strings.Contains(script, "'--defer-commit'") {
		t.Fatal("Windows Engine install must defer commit until the adapter finishes")
	}
	commitCall := strings.Index(script, "$commitArgs = @(")
	if commitCall < 0 ||
		!strings.Contains(script, "'--transaction-id', $engineTransactionId") ||
		!strings.Contains(script, "& $sourceBinary @commitArgs") {
		t.Fatal("Windows installer must finalize a deferred Engine trial with install commit bound to the trial transaction id")
	}
	if strings.Contains(script, "if ($engineReady)") || strings.Contains(script, "if (-not $engineReady)") {
		t.Fatal("current Windows Release must require the Installer Engine instead of branching to a legacy payload fallback")
	}
	rollbackRestore := strings.LastIndex(script, "Restore-FileState")
	abandonCall := strings.LastIndex(script, "install', 'abandon'")
	if rollbackRestore < 0 || abandonCall < 0 || abandonCall < rollbackRestore {
		t.Fatal("install abandon must run after real file/registry rollback")
	}
	rollbackServiceStart := strings.LastIndex(script, "& $destinationBinary service start --runtime-root $runtimeDir")
	rollbackHealthWait := strings.LastIndex(script, "Wait-AgentDockHealth -HealthPort $Port")
	rollbackTunnelProxy := strings.LastIndex(script, "$rollbackTunnelArguments = \"--start-tunnel --runtime-root")
	if rollbackServiceStart < rollbackRestore || rollbackHealthWait < rollbackServiceStart || rollbackTunnelProxy < rollbackHealthWait {
		t.Fatal("Engine rollback must restore source Core health before scheduling best-effort Tunnel recovery")
	}
	if abandonCall < rollbackTunnelProxy {
		t.Fatal("install abandon must run after best-effort Tunnel recovery is scheduled")
	}
	if !strings.Contains(script, "--rollback-failed") {
		t.Fatal("adapter rollback failure must be recorded as failed/rollback_failed, not rolled_back")
	}
	preparedMark := strings.Index(script, "$enginePrepared = $true")
	engineJSON := strings.Index(script, "Installer Engine returned invalid JSON")
	if preparedMark < 0 || engineJSON < 0 || preparedMark > engineJSON {
		t.Fatal("Engine trial must be marked prepared before JSON handshake parsing so catch still abandons")
	}

	const securityAssemblyLoad = "Add-Type -AssemblyName System.Security"
	if got := strings.Count(script, securityAssemblyLoad); got != 1 {
		t.Fatalf("install.ps1 must load System.Security only in the installer; the native Tunnel launcher does not decrypt secrets; got %d occurrences", got)
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
func TestWindowsInstallerUsesNativeTaskStartBridge(t *testing.T) {
	installData, err := os.ReadFile("../install/install.ps1")
	if err != nil {
		t.Fatal(err)
	}
	brokerData, err := os.ReadFile("../install/launch-windows-process.ps1")
	if err != nil {
		t.Fatal(err)
	}
	combined := string(installData) + "\n" + string(brokerData)
	for _, want := range []string{
		"service task-start",
		"--task-name",
		"--expected-user-sid",
		"-AgentDockBinary $sourceBinary",
	} {
		if !strings.Contains(combined, want) {
			t.Fatalf("Windows native task bridge missing %q", want)
		}
	}
	installScript := strings.ReplaceAll(string(installData), "\r\n", "\n")
	bridgeStart := strings.Index(installScript, "function Invoke-SetupRuntimeProcess")
	bridgeEnd := strings.Index(installScript, "function Get-AgentDockArchitecture")
	if bridgeStart < 0 || bridgeEnd <= bridgeStart {
		t.Fatal("Setup runtime task-start bridge function boundary is missing")
	}
	bridge := installScript[bridgeStart:bridgeEnd]
	if !strings.Contains(bridge, "-AgentDockBinary $sourceBinary") || strings.Contains(bridge, "-AgentDockBinary $destinationBinary") {
		t.Fatal("Setup runtime task-start bridge must use the verified payload Core instead of the stable shim while Installer commit is deferred")
	}
	if strings.Contains(string(brokerData), "manage-windows.ps1") || strings.Contains(combined, "task-run-session") {
		t.Fatal("Windows runtime launch paths must not depend on the removed manage-windows compatibility shim")
	}
	if !strings.Contains(string(installData), "$legacyManagerPath = Join-Path $runtimeDir 'installer\\manage-windows.ps1'") ||
		!strings.Contains(string(installData), "Remove-Item -LiteralPath $legacyManagerPath -Force") {
		t.Fatal("install.ps1 must retain rollback-safe cleanup for manage-windows.ps1 left by older installs")
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
		"Remove-DirectoryWithRetry -Path $versionsDir",
		"Remove-DirectoryWithRetry -Path $updateDir",
		"Remove-FileIfPresent -Path $activeVersionPath",
		"Stop-ProcessByPath -ProcessName 'agentdock-core'",
		"Stop-ProcessByPath -ProcessName 'agentdock-arbiter'",
		"Stop-ProcessByPath -ProcessName 'cloudflared'",
		"Remove-RegistryValueIfPresent -Path $runKey -Name $TrayStartupValueName",
		"'runtime.json'",
		"'desktop-version.txt'",
		"Remove-RegistryValueIfPresent -Path $runKey -Name $CloudflaredStartupValueName",
		"'start-cloudflared.ps1'",
		"'named-server-url.txt'",
		"'control-panel-settings.json'",
		"'oauth-password.dpapi'",
		"'oauth-token-secret.dpapi'",
		"'credential-owner-sid.txt'",
		"'auth-token.dpapi.unreadable-*.bak'",
		"'oauth-password.dpapi.unreadable-*.bak'",
		"'oauth-token-secret.dpapi.unreadable-*.bak'",
		"'oauth-access-token-ttl.txt'",
		"'cloudflared-token.dpapi'",
		"'cloudflared.out.log'",
		"'cloudflared.err.log'",
		"'quick-tunnel-url.txt'",
		"$runtimeManifestPath = Join-Path $runtimeDir 'runtime.json'",
		"$runtimeManifest.agentdock_task_name",
		"$managedTaskName = 'AgentDock'",
		"Remove-AgentDockScheduledTask",
		"-AdminLauncherPath $trayBinary",
		"--task-admin remove",
		"--task-name",
		"--runtime-root",
		"-Verb RunAs",
		"--defer-commit",
		"install', 'commit'",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("uninstall-windows.ps1 missing %q", want)
		}
	}
	engineRunCall := strings.Index(script, "$engineUninstallJson =")
	taskCall := strings.Index(script, "Remove-AgentDockScheduledTask -AdminLauncherPath $trayBinary")
	registryCall := strings.LastIndex(script, "Remove-RegistryValueIfPresent -Path $runKey")
	commitCall := strings.Index(script, "install', 'commit'")
	fileCall := strings.Index(script, "Remove-DirectoryWithRetry -Path $InstallDir")
	purgeCall := strings.Index(script, "Remove-DirectoryWithRetry -Path (Join-Path $userHome '.agentdock')")
	if engineRunCall < 0 || taskCall < 0 || engineRunCall > taskCall {
		t.Fatal("Engine uninstall must run before Task/Registry adapter work")
	}
	if commitCall < 0 || registryCall < 0 || commitCall < registryCall {
		t.Fatal("Engine uninstall commit must wait until Task/Registry removal")
	}
	if fileCall < 0 || commitCall < fileCall {
		t.Fatal("Engine uninstall commit must happen only after install files are removed")
	}
	if purgeCall < 0 || commitCall < purgeCall {
		t.Fatal("PurgeState user data removal must complete before uninstall is committed")
	}
	if strings.Contains(script, "--purge-data") {
		t.Fatal("Windows adapter must not let Engine purge state/commit before Task/Registry/file cleanup")
	}
	if !strings.Contains(script, "$engineCommitBinary") || !strings.Contains(script, "install detach-engine --output $engineCommitBinary") {
		t.Fatal("uninstall must detach a real Engine executable so product files can be removed before commit")
	}
	detachCall := strings.LastIndex(script, "install detach-engine --output $engineCommitBinary")
	if detachCall < engineRunCall || detachCall > taskCall || detachCall > fileCall {
		t.Fatal("detached Engine helper must be prepared after the uninstall trial and before destructive adapter cleanup")
	}
	if strings.Contains(script, "Copy-Item -LiteralPath $agentDockBinary -Destination $engineCommitBinary") {
		t.Fatal("uninstall must not copy the stable shim as its detached Engine helper")
	}
	stableBinaryBranch := strings.Index(script, "if (Test-Path -LiteralPath $agentDockBinary -PathType Leaf) {")
	transactionRead := strings.Index(script, "$pendingTransaction = Get-Content -LiteralPath $installTransactionPath")
	pendingTrialGate := strings.Index(script, "$pendingState -ne 'trial'")
	if stableBinaryBranch < 0 || transactionRead < stableBinaryBranch || pendingTrialGate < transactionRead {
		t.Fatal("stable-binary uninstalls must delegate resume to the Engine transaction rebind; the durable transaction read is only the no-binary recovery bootstrap")
	}
	if !strings.Contains(script, "$engineUninstallResult.PSObject.Properties['task_name']") ||
		!strings.Contains(script, "$engineTaskNameProperty.Value") {
		t.Fatal("Windows adapter must take the managed task name from the Installer Engine uninstall result without breaking StrictMode when task_name is omitted")
	}
	if strings.Contains(script, "$engineUninstallResult.task_name") {
		t.Fatal("Windows adapter must not directly read optional task_name under Set-StrictMode")
	}
	if !strings.Contains(script, "'agentdock-uninstall-' + $engineUninstallTransactionId + '.exe'") {
		t.Fatal("detached uninstall Engine helper must use a deterministic transaction-id path so retries can find it")
	}
	if strings.Contains(script, "[Guid]::NewGuid().ToString('N')") {
		t.Fatal("uninstall helper path must not be random; retries need the same transaction-scoped helper")
	}
	if !strings.Contains(script, "both the stable binary and detached Engine helper are missing") {
		t.Fatal("pending uninstall must fail explicitly when neither stable nor detached Engine can resume it")
	}
	commitFailure := strings.Index(script, "if ($engineCommitExitCode -ne 0)")
	helperRemoval := strings.Index(script, "Remove-Item -LiteralPath $engineCommitBinary -Force -ErrorAction SilentlyContinue")
	if commitFailure < 0 || helperRemoval < commitFailure {
		t.Fatal("failed uninstall commit must retain the detached Engine helper for a later retry")
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
		"--task-core-host --runtime-root",
		"SetSecurityDescriptor",
		"prepare-elevated",
		"prepare-standard",
		"restore",
		"remove",
		"set-enabled",
		"StopInstalledCore",
		"InstalledCorePaths",
		"new[] { \"agentdock\", \"agentdock-core\" }",
		"active-version.json",
		"fallback_version",
		"--task-name",
		"request.TaskName",
		"state.WasEnabled && state.WasRunning",
		"process.Kill(entireProcessTree: true)",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("TaskAdminService.cs missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"powershell.exe",
		"File.Exists(request.LauncherPath)",
		"service launch-core --runtime-root",
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
		"StartupPage.Values[1] := False",
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

func TestWindowsSetupRepromptsUnreadableNamedTunnelToken(t *testing.T) {
	codeData, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "includes", "code.iss"))
	if err != nil {
		t.Fatalf("read code.iss: %v", err)
	}
	setupData, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "AgentDock.iss"))
	if err != nil {
		t.Fatalf("read AgentDock.iss: %v", err)
	}
	installData, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install", "install.ps1"))
	if err != nil {
		t.Fatalf("read install.ps1: %v", err)
	}
	probeData, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install", "probe-protected-text.ps1"))
	if err != nil {
		t.Fatalf("read probe-protected-text.ps1: %v", err)
	}

	setup := string(setupData) + "\n" + string(codeData)
	for _, want := range []string{
		"probe-protected-text.ps1",
		"ProtectedTextCanBeRead",
		"ExistingTunnelTokenUsable",
		"agentdock.cloudflare.tunnel.v1",
		"TokenRecoveryRequired",
		"WizardSilent",
		"silent Setup will report the missing or unreadable Tunnel Token through the installer result",
	} {
		if !strings.Contains(setup, want) {
			t.Fatalf("Windows Setup missing tunnel credential recovery contract %q", want)
		}
	}
	if strings.Contains(setup, "not FileExists(AddBackslash(ExistingInstallRoot()) + 'cloudflared-token.dpapi')") {
		t.Fatal("Windows Setup must validate the saved Tunnel Token instead of trusting file existence")
	}

	install := string(installData)
	guard := strings.Index(install, "if ($InstallChannel -eq 'setup' -and $resolvedTunnelMode -eq 'named')")
	contextCheck := strings.Index(install, "$interactiveUser = Get-InteractiveDesktopUser")
	preflight := strings.Index(install, "$setupTunnelTokenState = Resolve-AvailableTunnelToken")
	payloadMutation := strings.Index(install, "New-Item -ItemType Directory -Path $tempRoot -Force")
	prompt := strings.Index(install, "Read-Host 'Cloudflare Tunnel Token' -AsSecureString")
	if guard < 0 || prompt < 0 || guard > prompt {
		t.Fatal("install.ps1 must reject a missing/unreadable Setup Tunnel Token before the interactive Read-Host fallback")
	}
	if preflight < 0 || payloadMutation < 0 || preflight > payloadMutation {
		t.Fatal("install.ps1 must validate the Setup Tunnel Token before payload extraction or runtime mutation")
	}
	if contextCheck < 0 || contextCheck > preflight {
		t.Fatal("install.ps1 must validate the signed-in user context before attempting current-user DPAPI recovery")
	}
	if strings.Count(install, "Resolve-AvailableTunnelToken") < 3 {
		t.Fatal("install.ps1 must reuse one Tunnel Token resolution path for Setup preflight and final persistence")
	}
	if !strings.Contains(install, "$installErrorCode = 'tunnel-token-required'") {
		t.Fatal("install.ps1 must report the tunnel-token-required structured error")
	}

	probe := string(probeData)
	for _, want := range []string{
		"ProtectedData]::Unprotect",
		"DataProtectionScope]::CurrentUser",
		"exit 0",
		"exit 3",
	} {
		if !strings.Contains(probe, want) {
			t.Fatalf("probe-protected-text.ps1 missing %q", want)
		}
	}
}

func TestWindowsGeneratedCredentialRecoveryPreservesUnreadableCiphertext(t *testing.T) {
	installData, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install", "install.ps1"))
	if err != nil {
		t.Fatalf("read install.ps1: %v", err)
	}
	codeData, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "includes", "code.iss"))
	if err != nil {
		t.Fatalf("read code.iss: %v", err)
	}
	messagesData, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "includes", "messages.iss"))
	if err != nil {
		t.Fatalf("read messages.iss: %v", err)
	}

	install := string(installData)
	for _, want := range []string{
		"function Backup-UnreadableProtectedText",
		"for ($attempt = 0; $attempt -lt 3; $attempt++)",
		"Start-Sleep -Milliseconds 50",
		".unreadable-",
		"while ($backups.Count -gt 3)",
		"credential-owner-sid.txt",
		"$installErrorCode = 'credential-user-mismatch'",
		"Write-TextFile -Path $credentialOwnerSidPath -Value $taskUser.Sid",
	} {
		if !strings.Contains(install, want) {
			t.Fatalf("install.ps1 missing generated credential recovery guard %q", want)
		}
	}
	if strings.Count(install, "Backup-UnreadableProtectedText -Path") < 3 {
		t.Fatal("install.ps1 must preserve unreadable auth, OAuth password, and OAuth signing secret before replacement")
	}
	if !strings.Contains(string(codeData), "CredentialUserMismatch") || !strings.Contains(string(messagesData), "CredentialUserMismatch") {
		t.Fatal("Windows Setup must localize credential-user-mismatch failures")
	}
}

func TestWindowsSetupOwnsCoreActivationAndReadsStructuredFailure(t *testing.T) {
	installData, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install", "install.ps1"))
	if err != nil {
		t.Fatalf("read install.ps1: %v", err)
	}
	install := strings.ReplaceAll(string(installData), "\r\n", "\n")
	for _, want := range []string{
		"if ((-not $RegisterStartup) -or ($InstallChannel -eq 'setup')) {",
		"$engineOwnsActivation = $InstallChannel -ne 'setup'",
		"$commitArgs += '--healthy'",
		"function Get-InstallerEngineFailureMessage",
		"function Enter-InstallerTransactionLease",
		"[IO.FileShare]::None",
		"$installerTransactionLease = Enter-InstallerTransactionLease -RuntimeRoot $runtimeDir",
		"[IO.File]::ReadAllText($engineResultPath, [Text.Encoding]::UTF8)",
		"$engineResult.failure.message",
		"[IO.File]::WriteAllLines($Path, $lines, [Text.Encoding]::Unicode)",
	} {
		if !strings.Contains(install, want) {
			t.Fatalf("install.ps1 must keep Setup activation and structured failure handling at the adapter boundary; missing %q", want)
		}
	}
	if strings.Contains(install, "$InstallChannel -eq 'setup' -and -not $existingInstallDetected") {
		t.Fatal("existing Setup installs must not let Installer Engine start Core inside the Inno process tree")
	}
	if strings.Contains(install, "if ($enginePrepared -and -not $existingInstallDetected)") {
		t.Fatal("fresh Setup must stay in trial until adapter activation completes")
	}
	if !strings.Contains(install, "$enginePrepared -and (-not $engineCommitted -or $healthStatus -eq 'healthy')") {
		t.Fatal("Setup must commit only after adapter activation/deferred handling finishes")
	}
	leaseAcquire := strings.Index(install, "$installerTransactionLease = Enter-InstallerTransactionLease -RuntimeRoot $runtimeDir")
	leaseRelease := strings.Index(install, "Exit-InstallerTransactionLease -Lease $installerTransactionLease")
	commitCall := strings.Index(install, "$commitArgs = @(")
	if leaseAcquire < 0 || leaseRelease < 0 || commitCall < 0 || leaseAcquire > leaseRelease || leaseRelease > commitCall {
		t.Fatal("Setup must hold the Installer transaction lease through activation and release it immediately before commit")
	}

	brokerData, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install", "launch-windows-process.ps1"))
	if err != nil {
		t.Fatalf("read launch-windows-process.ps1: %v", err)
	}
	if !strings.Contains(string(brokerData), "[int] $TimeoutSeconds = 60") {
		t.Fatal("Setup runtime broker timeout must exceed the 45-second Windows Core health timeout")
	}
}
func TestWindowsSetupRuntimeBrokerTimeoutExceedsCoreStartTimeout(t *testing.T) {
	brokerData, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install", "launch-windows-process.ps1"))
	if err != nil {
		t.Fatalf("read launch-windows-process.ps1: %v", err)
	}
	coreData, err := os.ReadFile(filepath.Join("..", "..", "internal", "desktopruntime", "service_windows.go"))
	if err != nil {
		t.Fatalf("read service_windows.go: %v", err)
	}

	parseSeconds := func(content, prefix string) int {
		t.Helper()
		for _, line := range strings.Split(string(content), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 4 {
				t.Fatalf("invalid timeout declaration %q", line)
			}
			seconds, err := strconv.Atoi(fields[3])
			if err != nil {
				t.Fatalf("parse timeout declaration %q: %v", line, err)
			}
			return seconds
		}
		t.Fatalf("timeout declaration with prefix %q was not found", prefix)
		return 0
	}

	brokerSeconds := parseSeconds(string(brokerData), "[int] $TimeoutSeconds =")
	coreSeconds := parseSeconds(string(coreData), "const windowsCoreStartTimeout =")
	if brokerSeconds <= coreSeconds {
		t.Fatalf("Setup runtime broker timeout=%ds must exceed Windows Core start timeout=%ds", brokerSeconds, coreSeconds)
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
		"-Arguments \"service start --runtime-root",
		"$tunnelStartupArguments = \"--start-tunnel --runtime-root",
		"-FilePath $destinationTrayBinary",
		"-Arguments $tunnelStartupArguments",
		"Invoke-SetupRuntimeProcess -FilePath $BinaryPath -Arguments '--background'",
		"Invoke-SetupRuntimeProcess -FilePath (Join-Path $PSHOME 'powershell.exe') -Arguments $arguments",
		"-HiddenHostBinary $destinationTrayBinary",
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
		"& $AgentDockBinary service task-start",
		"--task-name $taskName",
		"--expected-user-sid $identity.User.Value",
		"[string] $HiddenHostBinary",
		"if ($WaitForExit) {",
		"ConvertTo-RuntimeHostArgument",
		"--setup-runtime-host",
		"--file-b64",
		"--wait",
		"--stdout-b64",
		"--stderr-b64",
		"--error-b64",
		"-Execute $HiddenHostBinary",
		"-Argument ($hostArguments -join ' ')",
		"Get-RuntimeFailureMessage",
		"Task Scheduler result: $rawResult",
		"Read-RuntimeDiagnosticTail",
		"Remove-Item -LiteralPath $diagnosticRoot -Recurse -Force",
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
	for _, forbidden := range []string{"-Execute $powerShellPath", "-EncodedCommand $encodedCommand"} {
		if strings.Contains(brokerScript, forbidden) {
			t.Fatalf("runtime launch broker must not use a console-subsystem PowerShell task action: %q", forbidden)
		}
	}

	for _, want := range []string{
		"Source: \"..\\..\\scripts\\install\\launch-windows-process.ps1\"; Flags: dontcopy",
		"ExtractTemporaryFile('launch-windows-process.ps1')",
		"-AgentDockBinary ",
		"-HiddenHostBinary ",
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

func TestWindowsRuntimeDiagnosticsPassesNativeTaskLauncher(t *testing.T) {
	diagnosticsData, err := os.ReadFile(filepath.Join("..", "..", "scripts", "test", "test-windows-runtime-launch-diagnostics.ps1"))
	if err != nil {
		t.Fatalf("read runtime diagnostics test: %v", err)
	}
	workflowData, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "windows-installer.yml"))
	if err != nil {
		t.Fatalf("read Windows Installer workflow: %v", err)
	}

	diagnostics := strings.ReplaceAll(string(diagnosticsData), "\r\n", "\n")
	workflow := strings.ReplaceAll(string(workflowData), "\r\n", "\n")
	for _, want := range []string{
		"[string] $AgentDockBinary",
		"[string] $HiddenHostBinary",
		"$resolvedAgentDockBinary = (Resolve-Path -LiteralPath $AgentDockBinary).Path",
		"$resolvedHiddenHostBinary = (Resolve-Path -LiteralPath $HiddenHostBinary).Path",
		"-AgentDockBinary $resolvedAgentDockBinary",
		"-HiddenHostBinary $resolvedHiddenHostBinary",
	} {
		if !strings.Contains(diagnostics, want) {
			t.Fatalf("runtime diagnostics test must pass the native task launcher; missing %q", want)
		}
	}
	for _, want := range []string{
		"$runtimeTestAgentDockBinary = Join-Path $env:RUNNER_TEMP 'agentdock-runtime-launch-test.exe'",
		"$runtimeTestHiddenHostBinary = Join-Path $env:RUNNER_TEMP 'agentdock-runtime-host-test.exe'",
		"go build -trimpath -o $runtimeTestAgentDockBinary .\\cmd\\agentdock",
		"go build -trimpath -ldflags '-H=windowsgui' -o $runtimeTestHiddenHostBinary .\\cmd\\agentdock-shim",
		"-AgentDockBinary $runtimeTestAgentDockBinary",
		"-HiddenHostBinary $runtimeTestHiddenHostBinary",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("Windows Installer workflow must build and pass the native task launcher; missing %q", want)
		}
	}
}

func TestWindowsNamedTunnelLifecycleCoversSoftFailureRecovery(t *testing.T) {
	lifecycleData, err := os.ReadFile(filepath.Join("..", "..", "scripts", "test", "test-windows-named-tunnel-lifecycle.ps1"))
	if err != nil {
		t.Fatalf("read Windows Named Tunnel lifecycle test: %v", err)
	}
	fakeData, err := os.ReadFile(filepath.Join("..", "..", "scripts", "test", "testdata", "fake-cloudflared", "main.go"))
	if err != nil {
		t.Fatalf("read fake cloudflared: %v", err)
	}
	workflowData, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "windows-installer.yml"))
	if err != nil {
		t.Fatalf("read Windows Installer workflow: %v", err)
	}

	lifecycle := strings.ReplaceAll(string(lifecycleData), "\r\n", "\n")
	for _, want := range []string{
		"-TunnelTokenFile $stableTokenFile",
		"Invoke-Installer -Archive $sourcePayload.Archive -Checksum $sourcePayload.Checksum",
		"Invoke-Installer -Archive $TargetAgentDockArchive -Checksum $TargetAgentDockChecksumFile",
		"-Archive $trialPayload.Archive",
		"-TunnelTokenFile $invalidTokenFile",
		"Invalid Named Token must not roll back a healthy Core generation",
		"Wait-TextFileContains",
		"Get-Content -LiteralPath $Path -Raw -ErrorAction Stop",
		"Provided Tunnel token is not valid.",
		"Assert-NoTunnelTokenInProcessArguments",
		"(Get-FileHash -LiteralPath $tunnelTokenPath -Algorithm SHA256).Hash -ne $ExpectedTokenHash",
		"-ExpectedVersion $trialVersion",
		"soft-failure recovery",
	} {
		if !strings.Contains(lifecycle, want) {
			t.Fatalf("Named Tunnel lifecycle test must cover install/repair/update/soft-failure recovery; missing %q", want)
		}
	}

	fake := strings.ReplaceAll(string(fakeData), "\r\n", "\n")
	for _, want := range []string{
		`os.Getenv("TUNNEL_TOKEN")`,
		`strings.Contains(argument, token)`,
		`agentdock-test-invalid-named-token`,
		`Provided Tunnel token is not valid.`,
		`Registered tunnel connection`,
	} {
		if !strings.Contains(fake, want) {
			t.Fatalf("fake cloudflared must model Named Tunnel environment/readiness without exposing the Token; missing %q", want)
		}
	}

	workflow := strings.ReplaceAll(string(workflowData), "\r\n", "\n")
	for _, want := range []string{
		"Test Named Tunnel install update and soft-failure recovery lifecycle",
		"Build-VersionedAgentDock -Version '0.0.0-named-source-e2e'",
		"Build-VersionedAgentDock -Version '999.0.0-named-trial-e2e'",
		".\\scripts\\test\\test-windows-named-tunnel-lifecycle.ps1",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("Windows Installer workflow must run the full Named Tunnel lifecycle; missing %q", want)
		}
	}
}

func TestWindowsStandardUserE2EWaitsForDirectProcessWithTimeout(t *testing.T) {
	launcherData, err := os.ReadFile(filepath.Join("..", "..", "scripts", "test", "run-windows-installer-e2e-as-standard-user.ps1"))
	if err != nil {
		t.Fatalf("read Windows standard-user E2E launcher: %v", err)
	}
	childData, err := os.ReadFile(filepath.Join("..", "..", "scripts", "test", "test-install-windows-e2e.ps1"))
	if err != nil {
		t.Fatalf("read Windows standard-user E2E child: %v", err)
	}
	launcher := strings.ReplaceAll(string(launcherData), "\r\n", "\n")
	child := strings.ReplaceAll(string(childData), "\r\n", "\n")

	for _, want := range []string{
		"$childProcessTimeoutSeconds = 600",
		"function Wait-TestProcess {",
		"$Process.WaitForExit($TimeoutSeconds * 1000)",
		"Stop-Process -Id $Process.Id -Force",
		"-Description 'Windows installer standard-user E2E'",
		"-Description 'Windows Setup user-context guard'",
		"-CompletionFile `\"$completionPath`\"",
		"Test-Path -LiteralPath $completionPath -PathType Leaf",
		"$contextSuccess -ne 'Success=false'",
	} {
		if !strings.Contains(launcher, want) {
			t.Fatalf("Windows standard-user E2E must use bounded direct-process waits; missing %q", want)
		}
	}
	if strings.Contains(launcher, "$process.ExitCode") || strings.Contains(launcher, "$contextProcess.ExitCode") {
		t.Fatal("Windows standard-user E2E must not rely on ExitCode from Start-Process -Credential under Windows PowerShell 5.1")
	}
	if strings.Contains(launcher, "-Wait `\n        -PassThru") {
		t.Fatal("Windows standard-user E2E must not use Start-Process -Wait because installer descendants are long-lived")
	}
	for _, want := range []string{
		"[string] $CompletionFile = ''",
		"New-Item -ItemType File -Path $CompletionFile -Force",
	} {
		if !strings.Contains(child, want) {
			t.Fatalf("Windows standard-user E2E child must report completion explicitly; missing %q", want)
		}
	}
}

func TestWindowsSetupE2EStagesCompleteLegacyFixture(t *testing.T) {
	testScriptData, err := os.ReadFile(filepath.Join("..", "..", "scripts", "test", "test-windows-setup-e2e.ps1"))
	if err != nil {
		t.Fatalf("read Setup E2E script: %v", err)
	}
	testScript := strings.ReplaceAll(string(testScriptData), "\r\n", "\n")
	for _, want := range []string{
		"[string] $LegacyCorePath",
		"[string] $LegacyTrayPath",
		"Copy-Item -LiteralPath $resolvedLegacyCore -Destination $binaryPath -Force",
		"Copy-Item -LiteralPath $resolvedLegacyTray -Destination $trayPath -Force",
	} {
		if !strings.Contains(testScript, want) {
			t.Fatalf("Setup E2E must stage a complete migratable legacy installation; missing %q", want)
		}
	}

	workflowData, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "windows-installer.yml"))
	if err != nil {
		t.Fatalf("read Windows Installer workflow: %v", err)
	}
	workflow := strings.ReplaceAll(string(workflowData), "\r\n", "\n")
	for _, want := range []string{
		"-LegacyCorePath .\\dist\\agentdock.exe",
		"-LegacyTrayPath .\\dist\\agentdock-tray.exe",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("Windows Installer workflow must pass a real legacy fixture binary; missing %q", want)
		}
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

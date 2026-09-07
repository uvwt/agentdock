[CmdletBinding()]
param(
    [string] $InstallerPath = '',
    [string] $ManagerPath = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

if ([string]::IsNullOrWhiteSpace($InstallerPath)) {
    $InstallerPath = Join-Path $PSScriptRoot '..\install\install.ps1'
}
if ([string]::IsNullOrWhiteSpace($ManagerPath)) {
    $ManagerPath = Join-Path $PSScriptRoot '..\install\manage-windows.ps1'
}

$resolvedInstaller = Resolve-Path -LiteralPath $InstallerPath
$tokens = $null
$errors = $null
$installerAst = [System.Management.Automation.Language.Parser]::ParseFile(
    $resolvedInstaller,
    [ref] $tokens,
    [ref] $errors
)
if ($errors.Count -gt 0) {
    $errors | ForEach-Object { Write-Error $_.Message }
    throw "$InstallerPath contains PowerShell syntax errors"
}

$resolvedManager = Resolve-Path -LiteralPath $ManagerPath
$managerTokens = $null
$managerErrors = $null
[System.Management.Automation.Language.Parser]::ParseFile(
    $resolvedManager,
    [ref] $managerTokens,
    [ref] $managerErrors
) | Out-Null
if ($managerErrors.Count -gt 0) {
    $managerErrors | ForEach-Object { Write-Error $_.Message }
    throw "$ManagerPath contains PowerShell syntax errors"
}
$managerBytes = [IO.File]::ReadAllBytes($resolvedManager)
if ($managerBytes.Length -lt 3 -or
    $managerBytes[0] -ne 0xEF -or
    $managerBytes[1] -ne 0xBB -or
    $managerBytes[2] -ne 0xBF) {
    throw "$ManagerPath must use UTF-8 with BOM for Windows PowerShell 5.1"
}

$content = Get-Content -LiteralPath $resolvedInstaller -Raw
$bytes = [IO.File]::ReadAllBytes($resolvedInstaller)
for ($index = 0; $index -lt $bytes.Length; $index++) {
    if ($bytes[$index] -gt 127) {
        throw "$InstallerPath must remain ASCII for Windows PowerShell 5.1; non-ASCII byte at offset $index"
    }
}
foreach ($line in ($content -split "`n")) {
    $trimmed = $line.Trim()
    foreach ($keyword in @('else', 'elseif', 'catch', 'finally')) {
        if ($trimmed -eq $keyword -or $trimmed.StartsWith("$keyword ")) {
            throw "$InstallerPath must keep $keyword on the same line as the preceding closing brace: $line"
        }
    }
}

foreach ($forbidden in @(
    'Set-PrivateAcl',
    'Get-Acl',
    'Set-Acl',
    'icacls.exe',
    '$icaclsArguments',
    '$AclSelfTest',
    '$sddl',
    'CanElevate',
    'Invoke-AgentDockTaskAdminAction',
    'New-ElevatedAgentDockScheduledTask',
    '--installer-admin-action',
    '-TaskAdminAction'
)) {
    if ($content.Contains($forbidden)) {
        throw "$InstallerPath still contains removed privileged startup or ACL code: $forbidden"
    }
}
if ($content.Contains('current account cannot elevate')) {
    throw "$InstallerPath must request UAC for old scheduled-task cleanup instead of rejecting a standard user early"
}

if ($content.Contains('Get-FileHash')) {
    throw "$InstallerPath must compute runtime SHA-256 without depending on Get-FileHash"
}

$resultInitializationIndex = $content.IndexOf("-Message 'AgentDock installation is initializing.'")
$protectedInitializationIndex = $content.IndexOf('$Utf8NoBom = New-Object System.Text.UTF8Encoding($false)', $resultInitializationIndex)
$architectureProbeIndex = $content.IndexOf('$architecture = Get-AgentDockArchitecture', $resultInitializationIndex)
$taskProbeIndex = $content.IndexOf('$taskState = Get-AgentDockTaskState', $resultInitializationIndex)
if ($resultInitializationIndex -lt 0 -or
    $protectedInitializationIndex -le $resultInitializationIndex -or
    $architectureProbeIndex -le $resultInitializationIndex -or
    $taskProbeIndex -le $resultInitializationIndex) {
    throw "$InstallerPath must create ResultFile before protected runtime, architecture, or scheduled-task initialization"
}
$payloadPreflightIndex = $content.IndexOf('$preflightVersionOutput = @(& $sourceBinary version --json 2>&1)')
$taskMutationIndex = $content.IndexOf('$taskActionResult = Start-ElevatedAgentDockTaskAction')
if ($payloadPreflightIndex -lt 0 -or $taskMutationIndex -le $payloadPreflightIndex) {
    throw "$InstallerPath must validate the unpacked payload before mutating an existing scheduled task"
}

$earlyResultPath = Join-Path ([IO.Path]::GetTempPath()) ('agentdock-install-early-result-' + [Guid]::NewGuid().ToString('N') + '.ini')
try {
    $previousErrorActionPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        & powershell.exe -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $resolvedInstaller -Port 0 -ResultFile $earlyResultPath *> $null
        $earlyExitCode = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $previousErrorActionPreference
        # The probe above is expected to fail. Do not leak its native exit code to the caller/CI shell.
        $global:LASTEXITCODE = 0
    }
    if ($earlyExitCode -eq 0) {
        throw 'Early installer validation probe unexpectedly succeeded'
    }
    if (-not (Test-Path -LiteralPath $earlyResultPath -PathType Leaf)) {
        throw 'Early installer failure did not create ResultFile'
    }
    $earlyResult = [IO.File]::ReadAllText($earlyResultPath, [Text.Encoding]::Unicode)
    foreach ($required in @('Success=false', 'Code=install-validation-failed', 'Message=Port must be between 1 and 65535.', 'Health=failed', 'ErrorType=')) {
        if (-not $earlyResult.Contains($required)) {
            throw "Early installer ResultFile is missing: $required"
        }
    }
} finally {
    Remove-Item -LiteralPath $earlyResultPath -Force -ErrorAction SilentlyContinue
}

foreach ($required in @(
    'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run',
    'Get-AgentDockTaskState',
    'Get-InteractiveDesktopUser',
    'Start-ElevatedAgentDockTaskAction',
    '--task-admin $Action',
    '--backup-directory',
    '--launcher-path',
    '--runtime-root',
    '--user-sid',
    '--user-name',
    '-AdminLauncherPath $sourceTrayBinary',
    '-LauncherPath $destinationTrayBinary',
    '$effectivePrivilegeMode -eq ''elevated'' -and -not $taskState.Exists',
    '$installWarningCode = ''elevated-mode-fallback''',
    '$installWarningCode = "$installWarningCode,runtime-launch-deferred"',
    'WarningCode=$WarningCode',
    '-WarningCode $installWarningCode',
    '-ErrorCode ''install-validation-failed''',
    'Administrator approval for AgentDock rollback was not completed',
    'scheduled-task-recovery-',
    'Recovery files: $taskRecoveryPath',
    'setup-elevated-context',
    'Start Setup normally under the signed-in account',
    'function Set-RunValue',
    'Unable to prepare current-user startup registry key',
    'Unable to write current-user startup registry value',
    'Set-RunValue -RegistryPath $runKey -Name $runValueName',
    'Set-RunValue -RegistryPath $runKey -Name $cloudflaredRunValueName',
    'Start-AgentDockLauncher -LauncherPath $launcherPath',
    'Start-CloudflaredLauncher -LauncherPath $cloudflaredLauncherPath',
    'Release archive does not contain manage-windows.ps1',
    'Initialize-OAuthCredentials',
    'named-server-url.txt',
    'cloudflared-windows-$Architecture.exe',
    'Wait-QuickTunnelUrl -LogPaths @($cloudflaredStdoutLogPath, $cloudflaredStderrLogPath)',
    'Wait-QuickTunnelReady -Path $quickTunnelUrlPath -ExpectedUrl $publicUrl',
    'quick-tunnel-url.txt',
    'Restart-AgentDockForQuickTunnel',
    'Update-RuntimePublicUrl -PublicUrl `$publicUrl',
    'Write-TextAtomically -Path ''$escapedServerUrlPath'' -Value `$publicUrl',
    'Write-TextAtomically -Path ''$escapedQuickTunnelUrlPath'' -Value `$publicUrl',
    'RedirectStandardOutput = ''$escapedCloudflaredStdoutLogPath''',
    'RedirectStandardError = ''$escapedCloudflaredStderrLogPath''',
    'Write-ProtectedText -Path $PasswordPath',
    'Write-ProtectedText -Path $TokenSecretPath',
    'Write-ProtectedText -Path $tunnelTokenPath',
    'Authentication: Bearer Token and OAuth are both enabled.',
    '$coreSkillOutput = @(& $destinationBinary skill bootstrap --bundle $coreSkillBundle 2>&1)',
    '-ErrorCode $resultErrorCode',
    "`$resultErrorCode = 'elevated-task-rollback-failed'",
    "`$resultErrorCode = 'rollback-failed'",
    "`$installWarningCode = 'runtime-launch-deferred'",
    '$taskTransactionCommitted = $taskTransactionStarted',
    '-ErrorRecord $resultErrorRecord',
    'Get-Sha256Hex -Path $archivePath',
    'ErrorType=$safeErrorType',
    'ErrorStack=$safeErrorStack',
    "GetEnvironmentVariable('AGENTDOCK_RELEASE_BASE_URL')"
)) {
    if (-not $content.Contains($required)) {
        throw "$InstallerPath is missing current-user startup logic: $required"
    }
}
foreach ($forbidden in @(
    'New-Item -Path $runKey -Force',
    'New-Item -Path $RegistryPath -Force',
    'New-ItemProperty -Path $runKey'
)) {
    if ($content.Contains($forbidden)) {
        throw "$InstallerPath must route current-user startup writes through Set-RunValue instead of: $forbidden"
    }
}
$setRunValueCallCount = [regex]::Matches(
    $content,
    [regex]::Escape('Set-RunValue -RegistryPath $runKey')
).Count
if ($setRunValueCallCount -ne 6) {
    throw "$InstallerPath must use Set-RunValue for all startup writes in install and rollback paths; got $setRunValueCallCount calls"
}

$sha256Function = $installerAst.Find({
    param($node)
    $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and
        $node.Name -eq 'Get-Sha256Hex'
}, $true)
if ($null -eq $sha256Function) {
    throw "$InstallerPath does not define Get-Sha256Hex"
}
$sha256ProbeAssertions = @'
$hashPath = [IO.Path]::GetTempFileName()
try {
    [IO.File]::WriteAllBytes($hashPath, [Text.Encoding]::ASCII.GetBytes('abc'))
    $actualHash = Get-Sha256Hex -Path $hashPath
    $expectedHash = 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad'
    if ($actualHash -ne $expectedHash) {
        throw "Get-Sha256Hex returned an unexpected digest: $actualHash"
    }
} finally {
    Remove-Item -LiteralPath $hashPath -Force -ErrorAction SilentlyContinue
}
'@
$sha256Probe = [scriptblock]::Create(
    $sha256Function.Extent.Text + "`r`n" + $sha256ProbeAssertions
)
& $sha256Probe

$setRunValueFunction = $installerAst.Find({
    param($node)
    $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and
        $node.Name -eq 'Set-RunValue'
}, $true)
if ($null -eq $setRunValueFunction) {
    throw "$InstallerPath does not define Set-RunValue"
}
$runValueProbeAssertions = @'
$testRegistryPath = 'HKCU:\Software\AgentDockInstallerValidation-' + [Guid]::NewGuid().ToString('N')
try {
    Set-RunValue -RegistryPath $testRegistryPath -Name 'AgentDockTest' -Value 'first'
    if (-not (Test-Path -LiteralPath $testRegistryPath)) {
        throw 'Set-RunValue did not create a missing registry key'
    }
    $actualValue = Get-ItemPropertyValue -LiteralPath $testRegistryPath -Name 'AgentDockTest' -ErrorAction Stop
    if ($actualValue -ne 'first') {
        throw "Set-RunValue did not write the initial registry value: $actualValue"
    }

    New-ItemProperty -Path $testRegistryPath -Name 'SiblingValue' -Value 'keep-me' -PropertyType String -Force | Out-Null
    Set-RunValue -RegistryPath $testRegistryPath -Name 'AgentDockTest' -Value 'second'
    $actualValue = Get-ItemPropertyValue -LiteralPath $testRegistryPath -Name 'AgentDockTest' -ErrorAction Stop
    if ($actualValue -ne 'second') {
        throw "Set-RunValue did not update the existing registry value: $actualValue"
    }
    $siblingValue = Get-ItemPropertyValue -LiteralPath $testRegistryPath -Name 'SiblingValue' -ErrorAction Stop
    if ($siblingValue -ne 'keep-me') {
        throw "Set-RunValue modified an unrelated registry value: $siblingValue"
    }
} finally {
    Remove-Item -LiteralPath $testRegistryPath -Recurse -Force -ErrorAction SilentlyContinue
}
'@
$runValueProbe = [scriptblock]::Create(
    $setRunValueFunction.Extent.Text + "`r`n" + $runValueProbeAssertions
)
& $runValueProbe

$runValueDiagnosticPreamble = @'
$script:registryPathExists = $false
function Test-Path {
    [CmdletBinding()]
    param([string] $LiteralPath)
    return $script:registryPathExists
}
function New-Item {
    [CmdletBinding()]
    param([string] $Path)
    throw [System.UnauthorizedAccessException]::new('simulated registry key denial')
}
function New-ItemProperty {
    [CmdletBinding()]
    param(
        [string] $Path,
        [string] $Name,
        [string] $Value,
        [string] $PropertyType,
        [switch] $Force
    )
    throw [System.UnauthorizedAccessException]::new('simulated registry value denial')
}
'@
$runValueDiagnosticAssertions = @'
$probePath = 'HKCU:\Software\AgentDockRegistryDiagnosticProbe'
$probeName = 'AgentDockTest'
$prepareError = ''
try {
    Set-RunValue -RegistryPath $probePath -Name $probeName -Value 'value'
} catch {
    $prepareError = $_.Exception.Message
}
if ($prepareError -notlike "*prepare current-user startup registry key*$probePath*$probeName*simulated registry key denial*") {
    throw "Set-RunValue key-creation failure did not preserve safe registry context: $prepareError"
}

$script:registryPathExists = $true
$writeError = ''
try {
    Set-RunValue -RegistryPath $probePath -Name $probeName -Value 'value'
} catch {
    $writeError = $_.Exception.Message
}
if ($writeError -notlike "*write current-user startup registry value*$probeName*$probePath*simulated registry value denial*") {
    throw "Set-RunValue value-write failure did not preserve safe registry context: $writeError"
}
'@
$runValueDiagnosticProbe = [scriptblock]::Create(
    $runValueDiagnosticPreamble + "`r`n" +
    $setRunValueFunction.Extent.Text + "`r`n" +
    $runValueDiagnosticAssertions
)
& $runValueDiagnosticProbe

$installResultValueFunction = $installerAst.Find({
    param($node)
    $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and
        $node.Name -eq 'ConvertTo-InstallResultValue'
}, $true)
$installResultFunction = $installerAst.Find({
    param($node)
    $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and
        $node.Name -eq 'Write-InstallResult'
}, $true)
if ($null -eq $installResultValueFunction -or $null -eq $installResultFunction) {
    throw "$InstallerPath does not define the install result helpers"
}
$installResultProbePreamble = @'
$Utf8NoBom = New-Object System.Text.UTF8Encoding($false)
function Invoke-DiagnosticProbe {
    param([string] $SensitiveValue)
    throw 'diagnostic probe failure'
}
'@
$installResultProbeAssertions = @'
$resultPath = Join-Path ([IO.Path]::GetTempPath()) ("agentdock-install-result-test-" + [Guid]::NewGuid().ToString('N') + '.ini')
$diagnosticSecret = 'agentdock-diagnostic-secret-value'
try {
    try {
        Invoke-DiagnosticProbe -SensitiveValue $diagnosticSecret
    } catch {
        $probeError = $_
    }
    Write-InstallResult `
        -Path $resultPath `
        -Success $false `
        -Message $probeError.Exception.Message `
        -InstalledVersion '0.0.0' `
        -LocalMCPUrl '' `
        -PublicMCPUrl '' `
        -BearerToken '' `
        -OAuthLoginPassword '' `
        -HealthStatus 'failed' `
        -PrivilegeMode 'standard' `
        -ErrorRecord $probeError
    $resultText = [IO.File]::ReadAllText($resultPath, [Text.Encoding]::Unicode)
    foreach ($requiredField in @('ErrorType=', 'ErrorId=', 'ErrorCategory=', 'ErrorLine=', 'ErrorColumn=', 'ErrorStack=')) {
        if (-not $resultText.Contains($requiredField)) {
            throw "Install result is missing diagnostic field: $requiredField"
        }
    }
    if ($resultText -notmatch 'ErrorType=.+') {
        throw 'Install result did not capture the PowerShell exception type'
    }
    if ($resultText -notmatch 'ErrorLine=[1-9][0-9]*') {
        throw 'Install result did not capture the PowerShell script line'
    }
    if ($resultText -notmatch 'ErrorStack=.+Invoke-DiagnosticProbe') {
        throw 'Install result did not capture a useful PowerShell script stack'
    }
    if ($resultText.Contains($diagnosticSecret)) {
        throw 'Install result diagnostics must not include sensitive argument values'
    }
} finally {
    Remove-Item -LiteralPath $resultPath -Force -ErrorAction SilentlyContinue
}
'@
$installResultProbe = [scriptblock]::Create(
    $installResultProbePreamble + "`r`n" +
    $installResultValueFunction.Extent.Text + "`r`n" +
    $installResultFunction.Extent.Text + "`r`n" +
    $installResultProbeAssertions
)
& $installResultProbe

$taskStateFunction = $installerAst.Find({
    param($node)
    $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and
        $node.Name -eq 'Get-AgentDockTaskState'
}, $true)
if ($null -eq $taskStateFunction) {
    throw "$InstallerPath does not define Get-AgentDockTaskState"
}
$taskStateProbePreamble = @'
function Test-AgentDockTaskEligible {
    return $true
}
function Get-ScheduledTask {
    [CmdletBinding()]
    param([string] $TaskName, [string] $TaskPath)
    throw 'simulated Task Scheduler failure'
}
'@
$taskStateProbeAssertions = @'
$state = Get-AgentDockTaskState `
    -AgentDockValueName 'AgentDock' `
    -CloudflaredValueName 'AgentDockCloudflared' `
    -TrayValueName 'AgentDockTray'
if ($state.SchedulerAvailable -ne $false -or $state.Exists -ne $false) {
    throw "Task Scheduler failure must be represented as unavailable state: $($state | Out-String)"
}
if ($state.SchedulerError -notlike '*simulated Task Scheduler failure*') {
    throw "Task Scheduler failure diagnostic was not preserved: $($state.SchedulerError)"
}
'@
$taskStateProbe = [scriptblock]::Create(
    $taskStateProbePreamble + "`r`n" +
    $taskStateFunction.Extent.Text + "`r`n" +
    $taskStateProbeAssertions
)
& $taskStateProbe

$currentTaskUserFunction = $installerAst.Find({
    param($node)
    $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and
        $node.Name -eq 'Get-CurrentTaskUser'
}, $true)
if ($null -eq $currentTaskUserFunction) {
    throw "$InstallerPath does not define Get-CurrentTaskUser"
}
$identityProbeAssertions = @'
$taskUser = Get-CurrentTaskUser
if ($taskUser.PSObject.Properties.Name -contains 'CanElevate') {
    throw 'Get-CurrentTaskUser must not pre-classify a filtered UAC token as unable to elevate'
}
if ([string]::IsNullOrWhiteSpace($taskUser.Sid) -or [string]::IsNullOrWhiteSpace($taskUser.Name)) {
    throw 'Get-CurrentTaskUser must return the signed-in Windows SID and name'
}
'@
$identityProbe = [scriptblock]::Create(
    $currentTaskUserFunction.Extent.Text + "`r`n" + $identityProbeAssertions
)
& $identityProbe

$elevationFunction = $installerAst.Find({
    param($node)
    $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and
        $node.Name -eq 'Start-ElevatedAgentDockTaskAction'
}, $true)
if ($null -eq $elevationFunction) {
    throw "$InstallerPath does not define Start-ElevatedAgentDockTaskAction"
}

$elevationProbePreamble = @'
$script:mockStartProcessMode = 'reject'
$script:mockStartProcessFilePath = ''
$script:mockStartProcessArguments = ''
function Start-Process {
    param(
        [string] $FilePath,
        [string] $ArgumentList,
        [string] $Verb,
        [string] $WindowStyle,
        [switch] $Wait,
        [switch] $PassThru
    )

    $script:mockStartProcessFilePath = $FilePath
    $script:mockStartProcessArguments = $ArgumentList
    if ($script:mockStartProcessMode -eq 'reject') {
        throw 'simulated RunAs rejection'
    }
    return [pscustomobject]@{ ExitCode = 23 }
}
'@
$elevationProbeAssertions = @'
$taskUser = [pscustomobject]@{
    Sid = 'S-1-5-21-test'
    Name = 'TEST\AgentDock'
}
$result = Start-ElevatedAgentDockTaskAction `
    -Action prepare-elevated `
    -BackupDirectory 'C:\Temp\AgentDockBackup' `
    -AdminLauncherPath 'C:\AgentDock\agentdock-tray.exe' `
    -LauncherPath 'C:\AgentDock\agentdock-tray.exe' `
    -RuntimeRoot 'C:\AgentDock' `
    -TaskUser $taskUser
if ($result.Started -ne $false -or $result.ErrorMessage -notlike '*simulated RunAs rejection*') {
    throw "RunAs pre-start rejection was not returned as a recoverable result: $($result | Out-String)"
}
if ($script:mockStartProcessFilePath -notlike '*agentdock-tray.exe' -or
    $script:mockStartProcessArguments -notlike '*--task-admin prepare-elevated*' -or
    $script:mockStartProcessArguments -like '*--script*') {
    throw "UAC must elevate the fixed AgentDock task helper instead of PowerShell: $script:mockStartProcessFilePath $script:mockStartProcessArguments"
}

$script:mockStartProcessMode = 'helper-error'
$helperResult = Start-ElevatedAgentDockTaskAction `
    -Action prepare-elevated `
    -BackupDirectory 'C:\Temp\AgentDockBackup' `
    -AdminLauncherPath 'C:\AgentDock\agentdock-tray.exe' `
    -LauncherPath 'C:\AgentDock\agentdock-tray.exe' `
    -RuntimeRoot 'C:\AgentDock' `
    -TaskUser $taskUser
if ($helperResult.Started -ne $true -or $helperResult.Succeeded -ne $false -or $helperResult.ExitCode -ne 23 -or
    $helperResult.ErrorMessage -notlike '*administrator task action failed with exit code 23*') {
    throw "Elevated helper failure must report that the privileged transaction started before it failed: $($helperResult | Out-String)"
}
'@
$elevationProbe = [scriptblock]::Create(
    $elevationProbePreamble + "`r`n" + $elevationFunction.Extent.Text + "`r`n" + $elevationProbeAssertions
)
& $elevationProbe

$repoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$taskAdminSourcePath = Join-Path $repoRoot 'desktop\windows\control-panel\Services\TaskAdminService.cs'
$appSourcePath = Join-Path $repoRoot 'desktop\windows\control-panel\App.xaml.cs'
$runtimeSourcePath = Join-Path $repoRoot 'desktop\windows\control-panel\Services\RuntimeService.cs'
$jobSourcePath = Join-Path $repoRoot 'desktop\windows\control-panel\Services\KillOnCloseJob.cs'
$taskAdminSource = Get-Content -LiteralPath $taskAdminSourcePath -Raw
$appSource = Get-Content -LiteralPath $appSourcePath -Raw
$runtimeSource = Get-Content -LiteralPath $runtimeSourcePath -Raw
$jobSource = Get-Content -LiteralPath $jobSourcePath -Raw
foreach ($required in @(
    'Type.GetTypeFromProgID("Schedule.Service")',
    'EnsureSameWindowsUser(request.UserSid)',
    'RegisterTaskDefinition(',
    'SetSecurityDescriptor(',
    '--run-core-task --runtime-root',
    'prepare-elevated',
    'prepare-standard',
    'restore',
    'remove',
    'set-enabled',
    'StopInstalledCore',
    'Process.GetProcessesByName("agentdock")',
    'process.Kill(entireProcessTree: true)'
)) {
    if (-not $taskAdminSource.Contains($required)) {
        throw "$taskAdminSourcePath is missing native task administration behavior: $required"
    }
}
foreach ($required in @('--task-admin', 'TaskAdminService.Run(e.Args)', '--run-core-task')) {
    if (-not $appSource.Contains($required)) {
        throw "$appSourcePath is missing AgentDock background helper behavior: $required"
    }
}
foreach ($required in @('RunElevatedCoreTaskAsync', 'CreateNoWindow = true', 'WindowStyle = ProcessWindowStyle.Hidden', 'KillOnCloseJob.Create()', 'job.Assign(process)', 'service', 'launch-core')) {
    if (-not $runtimeSource.Contains($required)) {
        throw "$runtimeSourcePath is missing no-console elevated core behavior: $required"
    }
}
foreach ($required in @('CreateJobObject', 'JobObjectLimitKillOnJobClose', 'SetInformationJobObject', 'AssignProcessToJobObject')) {
    if (-not $jobSource.Contains($required)) {
        throw "$jobSourcePath is missing kill-on-close Job Object behavior: $required"
    }
}
foreach ($forbidden in @('--installer-admin-action', '--script', 'powershell.exe')) {
    if ($taskAdminSource.Contains($forbidden) -or $appSource.Contains($forbidden)) {
        throw "AgentDock elevated helper must not expose arbitrary PowerShell execution: $forbidden"
    }
}

$setupCodePath = Join-Path $repoRoot 'packaging\windows\includes\code.iss'
$setupMessagesPath = Join-Path $repoRoot 'packaging\windows\includes\messages.iss'
$setupCode = Get-Content -LiteralPath $setupCodePath -Raw
$setupMessages = Get-Content -LiteralPath $setupMessagesPath -Raw
foreach ($required in @(
    "GetIniString('AgentDock', 'WarningCode', '', ResultFilePath)",
    "GetIniString('AgentDock', 'ErrorType', '', ResultFilePath)",
    "GetIniString('AgentDock', 'ErrorLine', '', ResultFilePath)",
    "GetIniString('AgentDock', 'ErrorStack', '', ResultFilePath)",
    "Log('AgentDock installation diagnostics: type=' + ErrorType",
    "Log('AgentDock installation stack: ' + ErrorStack)",
    "Pos('elevated-mode-fallback', InstallWarningCode) > 0",
    "Pos('runtime-launch-deferred', InstallWarningCode) > 0",
    "Pos('runtime-launch-deferred', InstallWarningCode) = 0",
    "GetLocalizedMessage('ElevatedModeFallbackNotice')",
    "GetLocalizedMessage('FinishedDeferredControlPanel')",
    'StartupPage.Values[1] := False',
    "FileExists(SchTasksPath)"
)) {
    if (-not $setupCode.Contains($required)) {
        throw "$setupCodePath is missing elevation fallback presentation: $required"
    }
}
foreach ($required in @(
    'english.ElevatedModeFallbackNotice=',
    'chinesesimplified.ElevatedModeFallbackNotice=',
    'english.FinishedDeferredControlPanel=',
    'chinesesimplified.FinishedDeferredControlPanel='
)) {
    if (-not $setupMessages.Contains($required)) {
        throw "$setupMessagesPath is missing elevation fallback message: $required"
    }
}

Write-Host 'Windows installer validation passed.'

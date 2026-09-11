[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string] $SetupPath,
    [string] $InstallRoot = '',
    [int] $Port = 8765,
    [switch] $AllowLegacyTaskMutation
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

if ([string]::IsNullOrWhiteSpace($InstallRoot)) {
    $tempRoot = if ([string]::IsNullOrWhiteSpace($env:RUNNER_TEMP)) { [IO.Path]::GetTempPath() } else { $env:RUNNER_TEMP }
    $InstallRoot = Join-Path $tempRoot 'agentdock-setup-e2e'
}

if (-not $AllowLegacyTaskMutation) {
    throw 'Legacy scheduled-task mutation is disabled. Pass -AllowLegacyTaskMutation only in an isolated Windows test environment.'
}

function Wait-ProcessOrThrow {
    param(
        [Diagnostics.Process] $Process,
        [int] $TimeoutSeconds,
        [string] $Description,
        [string] $LogPath
    )

    if (-not $Process.WaitForExit($TimeoutSeconds * 1000)) {
        Stop-Process -Id $Process.Id -Force -ErrorAction SilentlyContinue
        if (Test-Path -LiteralPath $LogPath -PathType Leaf) {
            Write-Host "----- $Description log -----"
            Get-Content -LiteralPath $LogPath | Write-Host
            Write-Host "----- end $Description log -----"
        }
        throw "$Description did not exit within $TimeoutSeconds seconds."
    }
    if ($Process.ExitCode -ne 0) {
        if (Test-Path -LiteralPath $LogPath -PathType Leaf) {
            Write-Host "----- $Description log -----"
            Get-Content -LiteralPath $LogPath | Write-Host
            Write-Host "----- end $Description log -----"
        }
        throw "$Description failed with exit code $($Process.ExitCode)."
    }
}

function Get-RunValue {
    param([string] $Name)

    try {
        return Get-ItemPropertyValue -LiteralPath $runKey -Name $Name -ErrorAction Stop
    } catch {
        return $null
    }
}

function Get-ActiveGenerationPaths {
    if (-not (Test-Path -LiteralPath $activeVersionPath -PathType Leaf)) {
        throw "AgentDock active-version.json is missing: $activeVersionPath"
    }
    $active = Get-Content -LiteralPath $activeVersionPath -Raw | ConvertFrom-Json
    if ([int] $active.schema_version -ne 1 -or [string]::IsNullOrWhiteSpace([string] $active.active_version)) {
        throw "AgentDock active-version.json is invalid: $activeVersionPath"
    }
    $version = 'v' + ([string] $active.active_version).Trim().TrimStart('v')
    $generationRoot = Join-Path $versionsDir $version
    return [pscustomobject]@{
        Version = $version
        State = [string] $active.state
        Root = $generationRoot
        Core = Join-Path $generationRoot 'agentdock-core.exe'
        Tray = Join-Path $generationRoot 'agentdock-tray.exe'
        Arbiter = Join-Path $generationRoot 'agentdock-arbiter.exe'
        Skills = Join-Path $generationRoot 'core-skills'
    }
}

function Assert-ElevatedAgentDockTask {
    $task = Get-ScheduledTask -TaskName 'AgentDock' -TaskPath '\' -ErrorAction Stop
    if ($task.Principal.RunLevel.ToString() -ne 'Highest') {
        throw "AgentDock task does not use the highest run level: $($task.Principal.RunLevel)"
    }
    $currentSid = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    try {
        $taskSid = (New-Object Security.Principal.NTAccount($task.Principal.UserId)).Translate(
            [Security.Principal.SecurityIdentifier]
        ).Value
    } catch {
        $taskSid = $task.Principal.UserId
    }
    if (-not [string]::Equals($taskSid, $currentSid, [StringComparison]::OrdinalIgnoreCase)) {
        throw "AgentDock task belongs to another user: $($task.Principal.UserId) / $taskSid"
    }
    $nativeActionMatch = @($task.Actions | Where-Object {
        $executeMatches = [string]::Equals(
            [IO.Path]::GetFullPath($_.Execute),
            [IO.Path]::GetFullPath($binaryPath),
            [StringComparison]::OrdinalIgnoreCase
        )
        $argumentsMatch = $_.Arguments -and
            $_.Arguments.Contains('service launch-core') -and
            $_.Arguments.Contains('--runtime-root') -and
            $_.Arguments.Contains($InstallRoot)
        $executeMatches -and $argumentsMatch
    }).Count -eq 1
    if (-not $nativeActionMatch) {
        $actions = ($task.Actions | ForEach-Object { "$($_.Execute) $($_.Arguments)" }) -join '; '
        throw "AgentDock task does not launch the stable CUI service entry: $actions"
    }
    if (@($task.Actions | Where-Object {
        $_.Execute.Contains('powershell.exe') -or
        [string]::Equals($_.Execute, $trayPath, [StringComparison]::OrdinalIgnoreCase) -or
        ($_.Arguments -and ($_.Arguments.Contains('--run-core-task') -or $_.Arguments.Contains('--start-core')))
    }).Count -gt 0) {
        throw 'AgentDock elevated task must use the stable CUI shim without PowerShell or the legacy tray host.'
    }
}

function Assert-CoreRunsWithoutConsole {
    $generation = Get-ActiveGenerationPaths
    $normalizedBinary = [IO.Path]::GetFullPath($generation.Core)
    $coreProcesses = @(Get-CimInstance Win32_Process | Where-Object {
        -not [string]::IsNullOrWhiteSpace($_.ExecutablePath) -and
        [string]::Equals([IO.Path]::GetFullPath($_.ExecutablePath), $normalizedBinary, [StringComparison]::OrdinalIgnoreCase)
    })
    if ($coreProcesses.Count -ne 1) {
        throw "Expected one active generation Core process, got $($coreProcesses.Count): $normalizedBinary"
    }

    $core = $coreProcesses[0]
    $parent = Get-CimInstance Win32_Process -Filter "ProcessId=$($core.ParentProcessId)" -ErrorAction Stop
    if ($parent.Name -ne 'agentdock.exe' -or
        [string]::IsNullOrWhiteSpace($parent.ExecutablePath) -or
        -not [string]::Equals([IO.Path]::GetFullPath($parent.ExecutablePath), [IO.Path]::GetFullPath($binaryPath), [StringComparison]::OrdinalIgnoreCase)) {
        throw "Elevated generation Core is not supervised by the stable CUI shim: $($parent.Name) $($parent.ExecutablePath)"
    }

    $consoleHosts = @(Get-CimInstance Win32_Process | Where-Object {
        $_.Name -eq 'conhost.exe' -and $_.ParentProcessId -eq $core.ProcessId
    })
    $visibleConsoleHosts = @($consoleHosts | Where-Object {
        try {
            $process = Get-Process -Id $_.ProcessId -ErrorAction Stop
            $process.MainWindowHandle -ne 0
        } catch {
            $false
        }
    })
    if ($visibleConsoleHosts.Count -gt 0) {
        throw "Elevated generation Core unexpectedly owns a visible console window: $($visibleConsoleHosts.ProcessId -join ', ')"
    }
}

function Start-AgentDockScheduledTask {
    $managerPath = Join-Path $InstallRoot 'installer\manage-windows.ps1'
    if (-not (Test-Path -LiteralPath $managerPath -PathType Leaf)) {
        throw "Installed Windows manager is missing: $managerPath"
    }
    & $managerPath -Action task-run-session -ScheduledTaskName 'AgentDock' -ScheduledTaskPath '\'
}

function Assert-TaskStopKillsCore {
    $generation = Get-ActiveGenerationPaths
    Stop-ScheduledTask -TaskName 'AgentDock' -TaskPath '\' -ErrorAction Stop
    $deadline = [DateTime]::UtcNow.AddSeconds(15)
    do {
        $remainingCore = @(Get-CimInstance Win32_Process | Where-Object {
            -not [string]::IsNullOrWhiteSpace($_.ExecutablePath) -and
            [string]::Equals([IO.Path]::GetFullPath($_.ExecutablePath), [IO.Path]::GetFullPath($generation.Core), [StringComparison]::OrdinalIgnoreCase)
        })
        if ($remainingCore.Count -eq 0) {
            break
        }
        if ([DateTime]::UtcNow -ge $deadline) {
            throw "Task Scheduler stopped the host but left AgentDock Core running: $($remainingCore.ProcessId -join ', ')"
        }
        Start-Sleep -Milliseconds 250
    } while ($true)

    Start-AgentDockScheduledTask
    $deadline = [DateTime]::UtcNow.AddSeconds(20)
    do {
        Start-Sleep -Milliseconds 250
        try {
            $health = Invoke-WebRequest -UseBasicParsing -Uri $healthUrl -TimeoutSec 2
            if ($health.StatusCode -eq 200) {
                break
            }
        } catch {
        }
        if ([DateTime]::UtcNow -ge $deadline) {
            throw 'AgentDock Core did not become healthy after restarting the elevated task.'
        }
    } while ($true)
    Assert-CoreRunsWithoutConsole
}

function Assert-ElevatedCoreLifecycle {
    $stopOutput = @(& $binaryPath service stop --runtime-root $InstallRoot 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "Elevated core stop failed: $($stopOutput -join [Environment]::NewLine)"
    }
    $stopDeadline = [DateTime]::UtcNow.AddSeconds(15)
    $generation = Get-ActiveGenerationPaths
    do {
        $remainingCore = @(Get-CimInstance Win32_Process | Where-Object {
            -not [string]::IsNullOrWhiteSpace($_.ExecutablePath) -and
            [string]::Equals([IO.Path]::GetFullPath($_.ExecutablePath), [IO.Path]::GetFullPath($generation.Core), [StringComparison]::OrdinalIgnoreCase)
        })
        if ($remainingCore.Count -eq 0) {
            break
        }
        if ([DateTime]::UtcNow -ge $stopDeadline) {
            throw "Elevated core remained running after task stop: $($remainingCore.ProcessId -join ', ')"
        }
        Start-Sleep -Milliseconds 250
    } while ($true)

    $startOutput = @(& $binaryPath service start --runtime-root $InstallRoot 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "Elevated core start failed: $($startOutput -join [Environment]::NewLine)"
    }
    $health = Invoke-WebRequest -UseBasicParsing -Uri $healthUrl -TimeoutSec 5
    if ($health.StatusCode -ne 200) {
        throw "Elevated core restart returned unexpected health status: $($health.StatusCode)"
    }
    Assert-ElevatedAgentDockTask
    Assert-CoreRunsWithoutConsole
}

function Stop-ProcessByPath {
    param([string] $ProcessName, [string] $BinaryPath)

    if (-not (Test-Path -LiteralPath $BinaryPath -PathType Leaf)) {
        return
    }
    $normalized = [IO.Path]::GetFullPath($BinaryPath)
    Get-Process -Name $ProcessName -ErrorAction SilentlyContinue | Where-Object {
        try {
            [string]::Equals(
                [IO.Path]::GetFullPath($_.Path),
                $normalized,
                [StringComparison]::OrdinalIgnoreCase
            )
        } catch {
            $false
        }
    } | Stop-Process -Force -ErrorAction SilentlyContinue
}

$resolvedSetup = (Resolve-Path -LiteralPath $SetupPath).Path
$binaryPath = Join-Path $InstallRoot 'bin\agentdock.exe'
$trayPath = Join-Path $InstallRoot 'bin\agentdock-tray.exe'
$trayIconPath = Join-Path $InstallRoot 'bin\agentdock.ico'
$manifestPath = Join-Path $InstallRoot 'runtime.json'
$activeVersionPath = Join-Path $InstallRoot 'active-version.json'
$versionsDir = Join-Path $InstallRoot 'versions'
$launcherPath = Join-Path $InstallRoot 'start-agentdock.ps1'
$uninstallerPath = Join-Path $InstallRoot 'unins000.exe'
$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$userHome = [Environment]::GetFolderPath('UserProfile')
$stateRoot = Join-Path $userHome '.agentdock'
$stateMarker = Join-Path $stateRoot ('setup-e2e-preserve-' + [Guid]::NewGuid().ToString('N') + '.txt')
$healthUrl = "http://127.0.0.1:$Port/healthz"
$setupProcess = $null
$repeatProcess = $null
$repairProcess = $null
$uninstallProcess = $null
$runtimeTempRoot = if ([string]::IsNullOrWhiteSpace($env:RUNNER_TEMP)) { [IO.Path]::GetTempPath() } else { $env:RUNNER_TEMP }
$setupLogPath = Join-Path $runtimeTempRoot 'agentdock-setup-e2e-install.log'
$repeatLogPath = Join-Path $runtimeTempRoot 'agentdock-setup-e2e-repeat.log'
$repairLogPath = Join-Path $runtimeTempRoot 'agentdock-setup-e2e-repair.log'
$uninstallLogPath = Join-Path $runtimeTempRoot 'agentdock-setup-e2e-uninstall.log'
$oldReleaseBaseUrl = $env:AGENTDOCK_RELEASE_BASE_URL
$oldCloudflaredReleaseBaseUrl = $env:AGENTDOCK_CLOUDFLARED_RELEASE_BASE_URL

try {
    Unregister-ScheduledTask -TaskName 'AgentDock' -TaskPath '\' -Confirm:$false -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $InstallRoot -Recurse -Force -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Path $InstallRoot -Force | Out-Null
    [IO.File]::WriteAllText(
        (Join-Path $InstallRoot 'start-agentdock.ps1'),
        '# legacy PowerShell install marker',
        [Text.UTF8Encoding]::new($false)
    )

    # 指向必然失败的地址，确保 Setup 的核心载荷不会偷偷退回在线下载。
    $env:AGENTDOCK_RELEASE_BASE_URL = 'http://127.0.0.1:1/agentdock-offline-e2e'
    $env:AGENTDOCK_CLOUDFLARED_RELEASE_BASE_URL = 'http://127.0.0.1:1/cloudflared-offline-e2e'
    Remove-Item -LiteralPath $setupLogPath, $repeatLogPath, $repairLogPath, $uninstallLogPath -Force -ErrorAction SilentlyContinue
    Write-Host "Starting AgentDockSetup.exe: $resolvedSetup"
    $setupProcess = Start-Process `
        -FilePath $resolvedSetup `
        -ArgumentList @(
            '/VERYSILENT',
            '/SUPPRESSMSGBOXES',
            '/LANG=chinesesimplified',
            '/NORESTART',
            "/DIR=$InstallRoot",
            "/LOG=$setupLogPath",
            "/PORT=$Port",
            '/MODE=local',
            '/AUTOSTART=1',
            '/ADMINMODE=elevated'
        ) `
        -PassThru
    Wait-ProcessOrThrow `
        -Process $setupProcess `
        -TimeoutSeconds 180 `
        -Description 'AgentDock Setup installation' `
        -LogPath $setupLogPath
    Write-Host 'AgentDock Setup installation exited successfully.'
    $initialLog = Get-Content -LiteralPath $setupLogPath -Raw
    if ($initialLog -notmatch 'AgentDock active language: chinesesimplified') {
        throw 'Setup did not use the Simplified Chinese messages.'
    }
    if ($initialLog -notmatch 'existing installation detected: source=powershell') {
        throw 'Setup did not recognize the legacy PowerShell installation.'
    }

    foreach ($path in @($binaryPath, $trayPath, $trayIconPath, $manifestPath, $uninstallerPath)) {
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
            throw "Setup did not create expected file: $path"
        }
    }
    $generation = Get-ActiveGenerationPaths
    if (-not [string]::Equals($generation.State, 'committed', [StringComparison]::OrdinalIgnoreCase)) {
        throw "Fresh Setup left active-version.json outside committed state: $($generation.State)"
    }
    foreach ($path in @(
        $activeVersionPath,
        $generation.Core,
        $generation.Tray,
        $generation.Arbiter,
        (Join-Path $generation.Skills 'manifest.json')
    )) {
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
            throw "Setup generation layout is incomplete: $path"
        }
    }
    $stableVersion = (& $binaryPath version --json | ConvertFrom-Json).version
    if ([string]::IsNullOrWhiteSpace([string] $stableVersion) -or
        -not [string]::Equals(('v' + ([string] $stableVersion).TrimStart('v')), $generation.Version, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Stable CUI shim did not resolve the committed generation: $stableVersion / $($generation.Version)"
    }
    $manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
    if (-not [string]::Equals(
        [IO.Path]::GetFullPath([string] $manifest.install_root),
        [IO.Path]::GetFullPath($InstallRoot),
        [StringComparison]::OrdinalIgnoreCase)) {
        throw "Setup runtime manifest install_root does not match the real install root: $($manifest.install_root) / $InstallRoot"
    }
    foreach ($entry in @(
        @{ Name = 'agentdock_binary'; Expected = $binaryPath },
        @{ Name = 'tray_binary'; Expected = $trayPath }
    )) {
        $recordedPath = [string] $manifest.($entry.Name)
        if (-not [string]::Equals(
            [IO.Path]::GetFullPath($recordedPath),
            [IO.Path]::GetFullPath([string] $entry.Expected),
            [StringComparison]::OrdinalIgnoreCase)) {
            throw "Setup runtime manifest $($entry.Name) does not match the installed file: $recordedPath / $($entry.Expected)"
        }
        if (-not (Test-Path -LiteralPath $recordedPath -PathType Leaf)) {
            throw "Setup runtime manifest $($entry.Name) records a missing file: $recordedPath"
        }
    }
    if ($manifest.install_channel -ne 'setup') {
        throw "Unexpected install channel: $($manifest.install_channel)"
    }
    if ($manifest.tunnel_mode -ne 'none') {
        throw "Local Setup unexpectedly enabled public access: $($manifest.tunnel_mode)"
    }
    if ($manifest.local_mcp_url -ne "http://127.0.0.1:$Port/mcp") {
        throw "Unexpected local MCP URL: $($manifest.local_mcp_url)"
    }
    if ($manifest.privilege_mode -ne 'elevated' -or $manifest.agentdock_task_name -ne 'AgentDock') {
        throw "Setup did not enable the elevated core: $($manifest.privilege_mode) / $($manifest.agentdock_task_name)"
    }

    $health = Invoke-WebRequest -UseBasicParsing -Uri $healthUrl -TimeoutSec 5
    if ($health.StatusCode -ne 200) {
        throw "Unexpected health status: $($health.StatusCode)"
    }
    if ($null -ne (Get-RunValue -Name 'AgentDock')) {
        throw 'Elevated core must not also be registered in HKCU Run.'
    }
    if ([string]::IsNullOrWhiteSpace((Get-RunValue -Name 'AgentDockTray'))) {
        throw 'Setup did not register the normal user tray in HKCU Run.'
    }
    Assert-ElevatedAgentDockTask
    Assert-CoreRunsWithoutConsole
    Assert-ElevatedCoreLifecycle
    Assert-TaskStopKillsCore

    Write-Host 'Starting AgentDockSetup.exe a second time without changing task state.'
    $repeatProcess = Start-Process `
        -FilePath $resolvedSetup `
        -ArgumentList @(
            '/VERYSILENT',
            '/SUPPRESSMSGBOXES',
            '/LANG=chinesesimplified',
            '/NORESTART',
            "/DIR=$InstallRoot",
            "/LOG=$repeatLogPath",
            "/PORT=$Port",
            '/ADMINMODE=elevated'
        ) `
        -PassThru
    Wait-ProcessOrThrow `
        -Process $repeatProcess `
        -TimeoutSeconds 180 `
        -Description 'AgentDock Setup repeated in-place upgrade' `
        -LogPath $repeatLogPath
    $repeatLog = Get-Content -LiteralPath $repeatLogPath -Raw
    if ($repeatLog -notmatch 'existing installation detected: source=setup') {
        throw 'Repeated Setup did not recognize the existing Setup-managed installation.'
    }
    $generation = Get-ActiveGenerationPaths
    if (-not [string]::Equals($generation.State, 'committed', [StringComparison]::OrdinalIgnoreCase)) {
        throw "Same-version Setup repair did not end committed: $($generation.State)"
    }
    if (@(Get-ChildItem -LiteralPath $versionsDir -Directory -Filter '.repair-backup-*' -ErrorAction SilentlyContinue).Count -ne 0) {
        throw 'Same-version Setup repair left a .repair-backup-* directory behind.'
    }
    Assert-ElevatedAgentDockTask
    Assert-CoreRunsWithoutConsole

    Write-Host 'Creating a running legacy AgentDock scheduled task for migration testing.'
    Stop-ScheduledTask -TaskName 'AgentDock' -TaskPath '\' -ErrorAction SilentlyContinue
    Unregister-ScheduledTask -TaskName 'AgentDock' -TaskPath '\' -Confirm:$false -ErrorAction Stop
    $generation = Get-ActiveGenerationPaths
    Stop-ProcessByPath -ProcessName 'agentdock-core' -BinaryPath $generation.Core
    Stop-ProcessByPath -ProcessName 'agentdock' -BinaryPath $binaryPath
    Start-Sleep -Milliseconds 500
    foreach ($name in @('AgentDock', 'AgentDockTray')) {
        Remove-ItemProperty -LiteralPath $runKey -Name $name -ErrorAction SilentlyContinue
    }
    $legacyTaskAction = New-ScheduledTaskAction `
        -Execute 'powershell.exe' `
        -Argument "-NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$launcherPath`""
    $legacyTaskTrigger = New-ScheduledTaskTrigger -AtLogOn
    Register-ScheduledTask `
        -TaskName 'AgentDock' `
        -TaskPath '\' `
        -Action $legacyTaskAction `
        -Trigger $legacyTaskTrigger `
        -Description 'AgentDock legacy migration test' `
        -Force | Out-Null
    Start-AgentDockScheduledTask
    $legacyDeadline = [DateTime]::UtcNow.AddSeconds(20)
    do {
        Start-Sleep -Milliseconds 500
        try {
            $legacyHealth = Invoke-WebRequest -UseBasicParsing -Uri $healthUrl -TimeoutSec 2
            $legacyTask = Get-ScheduledTask -TaskName 'AgentDock' -TaskPath '\' -ErrorAction Stop
            if ($legacyHealth.StatusCode -eq 200 -and $legacyTask.State -eq 'Running') {
                break
            }
        } catch {
        }
        if ([DateTime]::UtcNow -ge $legacyDeadline) {
            throw 'Legacy AgentDock scheduled task did not start a healthy server.'
        }
    } while ($true)

    Write-Host 'Starting AgentDockSetup.exe again to verify Setup-managed repair detection.'
    $repairProcess = Start-Process `
        -FilePath $resolvedSetup `
        -ArgumentList @(
            '/VERYSILENT',
            '/SUPPRESSMSGBOXES',
            '/LANG=chinesesimplified',
            '/NORESTART',
            "/DIR=$InstallRoot",
            "/LOG=$repairLogPath",
            "/PORT=$Port"
        ) `
        -PassThru
    Wait-ProcessOrThrow `
        -Process $repairProcess `
        -TimeoutSeconds 180 `
        -Description 'AgentDock Setup repair' `
        -LogPath $repairLogPath
    $repairLog = Get-Content -LiteralPath $repairLogPath -Raw
    if ($repairLog -notmatch 'existing installation detected: source=setup') {
        throw 'Setup did not recognize the existing Setup-managed installation.'
    }
    if ($repairLog -notmatch 'AgentDock legacy scheduled task detected') {
        throw 'Setup did not detect the legacy AgentDock scheduled task.'
    }
    Assert-ElevatedAgentDockTask
    Assert-CoreRunsWithoutConsole
    if ($null -ne (Get-RunValue -Name 'AgentDock')) {
        throw 'Setup repair incorrectly registered the elevated core in HKCU Run.'
    }
    if ([string]::IsNullOrWhiteSpace((Get-RunValue -Name 'AgentDockTray'))) {
        throw 'Setup repair did not preserve the normal user tray startup.'
    }
    if ((Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json).tunnel_mode -ne 'none') {
        throw 'Setup repair did not preserve local-only connection mode.'
    }

    New-Item -ItemType Directory -Path $stateRoot -Force | Out-Null
    [IO.File]::WriteAllText($stateMarker, 'preserve', [Text.UTF8Encoding]::new($false))

    Write-Host "Starting AgentDock uninstaller: $uninstallerPath"
    $uninstallProcess = Start-Process `
        -FilePath $uninstallerPath `
        -ArgumentList @(
            '/VERYSILENT',
            '/SUPPRESSMSGBOXES',
            '/NORESTART',
            "/LOG=$uninstallLogPath"
        ) `
        -PassThru
    Wait-ProcessOrThrow `
        -Process $uninstallProcess `
        -TimeoutSeconds 120 `
        -Description 'AgentDock Setup uninstall' `
        -LogPath $uninstallLogPath
    Write-Host 'AgentDock Setup uninstall exited successfully.'
    if (Test-Path -LiteralPath $binaryPath -PathType Leaf) {
        throw 'AgentDock binary remained after Setup uninstall.'
    }
    foreach ($path in @($activeVersionPath, $versionsDir, (Join-Path $InstallRoot 'update'))) {
        if (Test-Path -LiteralPath $path) {
            throw "Generation/update state remained after Setup uninstall: $path"
        }
    }
    if (-not (Test-Path -LiteralPath $stateMarker -PathType Leaf)) {
        throw 'Silent Setup uninstall unexpectedly deleted user state.'
    }
    foreach ($name in @('AgentDock', 'AgentDockTray', 'AgentDockCloudflared')) {
        if ($null -ne (Get-RunValue -Name $name)) {
            throw "HKCU startup value remained after Setup uninstall: $name"
        }
    }
    if ($null -ne (Get-ScheduledTask -TaskName 'AgentDock' -TaskPath '\' -ErrorAction SilentlyContinue)) {
        throw 'AgentDock scheduled task remained after Setup uninstall.'
    }

    Write-Host 'AgentDock Setup install, health check, and uninstall passed.'
} catch {
    foreach ($log in @($setupLogPath, $repeatLogPath, $repairLogPath, $uninstallLogPath)) {
        if (Test-Path -LiteralPath $log -PathType Leaf) {
            Write-Host "----- failure log: $log -----"
            Get-Content -LiteralPath $log | Write-Host
            Write-Host "----- end failure log -----"
        }
    }
    throw
} finally {
    $env:AGENTDOCK_RELEASE_BASE_URL = $oldReleaseBaseUrl
    $env:AGENTDOCK_CLOUDFLARED_RELEASE_BASE_URL = $oldCloudflaredReleaseBaseUrl
    Stop-ScheduledTask -TaskName 'AgentDock' -TaskPath '\' -ErrorAction SilentlyContinue
    Unregister-ScheduledTask -TaskName 'AgentDock' -TaskPath '\' -Confirm:$false -ErrorAction SilentlyContinue
    foreach ($process in @($setupProcess, $repairProcess, $uninstallProcess)) {
        if ($process -and -not $process.HasExited) {
            Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
        }
    }
    if (Test-Path -LiteralPath $versionsDir -PathType Container) {
        foreach ($generationDirectory in @(Get-ChildItem -LiteralPath $versionsDir -Directory -ErrorAction SilentlyContinue)) {
            Stop-ProcessByPath -ProcessName 'agentdock-tray' -BinaryPath (Join-Path $generationDirectory.FullName 'agentdock-tray.exe')
            Stop-ProcessByPath -ProcessName 'agentdock-core' -BinaryPath (Join-Path $generationDirectory.FullName 'agentdock-core.exe')
            Stop-ProcessByPath -ProcessName 'agentdock-arbiter' -BinaryPath (Join-Path $generationDirectory.FullName 'agentdock-arbiter.exe')
        }
    }
    Stop-ProcessByPath -ProcessName 'agentdock-tray' -BinaryPath $trayPath
    Stop-ProcessByPath -ProcessName 'agentdock' -BinaryPath $binaryPath
    foreach ($name in @('AgentDock', 'AgentDockTray', 'AgentDockCloudflared')) {
        Remove-ItemProperty -LiteralPath $runKey -Name $name -ErrorAction SilentlyContinue
    }
    Remove-Item -LiteralPath $InstallRoot -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $stateMarker -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $setupLogPath, $repeatLogPath, $repairLogPath, $uninstallLogPath -Force -ErrorAction SilentlyContinue
}

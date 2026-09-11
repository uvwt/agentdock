[CmdletBinding()]
param(
    [string] $InstallDir = (Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'AgentDock\bin'),
    [switch] $PurgeState,
    [switch] $KeepInstallDir,
    [string] $StartupValueName = 'AgentDock',
    [string] $CloudflaredStartupValueName = 'AgentDockCloudflared',
    [string] $TrayStartupValueName = 'AgentDockTray'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Get-ProcessIdsByPath {
    param(
        [string] $ProcessName,
        [string] $BinaryPath
    )

    if (-not (Test-Path -LiteralPath $BinaryPath -PathType Leaf)) {
        return @()
    }
    $normalizedBinaryPath = [IO.Path]::GetFullPath($BinaryPath)
    $processIds = @()

    # Win32_Process exposes the executable path reliably in uninstall contexts
    # where Get-Process.Path may be empty or inaccessible.
    try {
        $processIds = @(Get-CimInstance Win32_Process -Filter "Name = '$ProcessName.exe'" -ErrorAction Stop |
            Where-Object {
                $_.ExecutablePath -and
                [string]::Equals(
                    [IO.Path]::GetFullPath($_.ExecutablePath),
                    $normalizedBinaryPath,
                    [StringComparison]::OrdinalIgnoreCase
                )
            } |
            Select-Object -ExpandProperty ProcessId)
    } catch {
        $processIds = @()
    }

    if ($processIds.Count -eq 0) {
        $processIds = @(Get-Process -Name $ProcessName -ErrorAction SilentlyContinue | Where-Object {
            try {
                [string]::Equals(
                    [IO.Path]::GetFullPath($_.Path),
                    $normalizedBinaryPath,
                    [StringComparison]::OrdinalIgnoreCase
                )
            } catch {
                $false
            }
        } | Select-Object -ExpandProperty Id)
    }
    return @($processIds | Sort-Object -Unique)
}

function Stop-ProcessByPath {
    param(
        [string] $ProcessName,
        [string] $BinaryPath
    )

    $processIds = @(Get-ProcessIdsByPath -ProcessName $ProcessName -BinaryPath $BinaryPath)
    foreach ($processId in $processIds) {
        Stop-Process -Id $processId -Force -ErrorAction SilentlyContinue
    }

    $deadline = [DateTime]::UtcNow.AddSeconds(15)
    do {
        $remaining = @(Get-ProcessIdsByPath -ProcessName $ProcessName -BinaryPath $BinaryPath)
        if ($remaining.Count -eq 0) {
            return
        }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)

    throw "Process did not stop within 15 seconds: $BinaryPath"
}

function Remove-DirectoryWithRetry {
    param([string] $Path)

    if (-not (Test-Path -LiteralPath $Path)) {
        return
    }
    $deadline = [DateTime]::UtcNow.AddSeconds(15)
    do {
        Remove-Item -LiteralPath $Path -Recurse -Force -ErrorAction SilentlyContinue
        if (-not (Test-Path -LiteralPath $Path)) {
            return
        }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)

    throw "Directory could not be removed within 15 seconds: $Path"
}

function Remove-AgentDockScheduledTask {
    param(
        [string] $AdminLauncherPath,
        [string] $RuntimeRoot,
        [string] $TaskName
    )

    if ([string]::IsNullOrWhiteSpace($TaskName)) {
        return
    }
    $task = Get-ScheduledTask -TaskName $TaskName -TaskPath '\' -ErrorAction SilentlyContinue
    if ($null -eq $task) {
        return
    }
    try {
        Stop-ScheduledTask -TaskName $TaskName -TaskPath '\' -ErrorAction SilentlyContinue
        Unregister-ScheduledTask -TaskName $TaskName -TaskPath '\' -Confirm:$false -ErrorAction Stop
        return
    } catch {
        $directError = $_
    }

    if ([string]::IsNullOrWhiteSpace($AdminLauncherPath) -or
        -not (Test-Path -LiteralPath $AdminLauncherPath -PathType Leaf)) {
        throw "AgentDock scheduled task requires administrator cleanup, but the AgentDock helper is missing: $($directError.Exception.Message)"
    }
    try {
        $process = Start-Process `
            -FilePath $AdminLauncherPath `
            -ArgumentList "--task-admin remove --task-name `"$TaskName`" --runtime-root `"$RuntimeRoot`"" `
            -Verb RunAs `
            -WindowStyle Hidden `
            -Wait `
            -PassThru
    } catch {
        throw "Administrator approval for AgentDock task removal was not completed: $($_.Exception.Message)"
    }
    if ($process.ExitCode -ne 0) {
        throw "Elevated AgentDock task removal failed with exit code $($process.ExitCode)."
    }
}

$runtimeDir = Split-Path -Parent $InstallDir
$userHome = [Environment]::GetFolderPath('UserProfile')
$agentDockBinary = Join-Path $InstallDir 'agentdock.exe'
$trayBinary = Join-Path $InstallDir 'agentdock-tray.exe'
$cloudflaredBinary = Join-Path $InstallDir 'cloudflared.exe'
$versionsDir = Join-Path $runtimeDir 'versions'
$updateDir = Join-Path $runtimeDir 'update'
$activeVersionPath = Join-Path $runtimeDir 'active-version.json'
$runtimeManifestPath = Join-Path $runtimeDir 'runtime.json'
$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'

$managedTaskName = ''
if (Test-Path -LiteralPath $runtimeManifestPath -PathType Leaf) {
    try {
        $runtimeManifest = Get-Content -LiteralPath $runtimeManifestPath -Raw | ConvertFrom-Json
        if ([string]::Equals([string] $runtimeManifest.privilege_mode, 'elevated', [StringComparison]::OrdinalIgnoreCase) -and
            -not [string]::IsNullOrWhiteSpace([string] $runtimeManifest.agentdock_task_name)) {
            $managedTaskName = ([string] $runtimeManifest.agentdock_task_name).Trim()
        }
    } catch {
        Write-Warning "Unable to read runtime.json while resolving the managed scheduled task: $($_.Exception.Message)"
    }
} elseif ($StartupValueName -eq 'AgentDock' -and
    $CloudflaredStartupValueName -eq 'AgentDockCloudflared' -and
    $TrayStartupValueName -eq 'AgentDockTray') {
    # Legacy pre-manifest installs used the fixed task name. Keep that one-time cleanup path without
    # making a valid standard-mode runtime delete a task owned by another installation.
    $managedTaskName = 'AgentDock'
}

# Stop the scheduled task before touching the elevated process. New installs
# grant the desktop user task control; older administrator-owned tasks use a
# one-time UAC fallback through the installed helper.
if (-not [string]::IsNullOrWhiteSpace($managedTaskName)) {
    Remove-AgentDockScheduledTask -AdminLauncherPath $trayBinary -RuntimeRoot $runtimeDir -TaskName $managedTaskName
}

# Stable shims are short-lived and normally have no resident process. Stop every immutable
# generation explicitly so uninstall also works after an interrupted trial/rollback.
if (Test-Path -LiteralPath $versionsDir -PathType Container) {
    foreach ($generation in @(Get-ChildItem -LiteralPath $versionsDir -Directory -Force -ErrorAction SilentlyContinue)) {
        Stop-ProcessByPath -ProcessName 'agentdock-arbiter' -BinaryPath (Join-Path $generation.FullName 'agentdock-arbiter.exe')
        Stop-ProcessByPath -ProcessName 'agentdock-tray' -BinaryPath (Join-Path $generation.FullName 'agentdock-tray.exe')
        Stop-ProcessByPath -ProcessName 'agentdock-core' -BinaryPath (Join-Path $generation.FullName 'agentdock-core.exe')
    }
}
Stop-ProcessByPath -ProcessName 'agentdock-tray' -BinaryPath $trayBinary
Stop-ProcessByPath -ProcessName 'cloudflared' -BinaryPath $cloudflaredBinary
Stop-ProcessByPath -ProcessName 'agentdock' -BinaryPath $agentDockBinary

if (Test-Path -LiteralPath $runKey) {
    Remove-ItemProperty -LiteralPath $runKey -Name $StartupValueName -ErrorAction SilentlyContinue
    Remove-ItemProperty -LiteralPath $runKey -Name $CloudflaredStartupValueName -ErrorAction SilentlyContinue
    Remove-ItemProperty -LiteralPath $runKey -Name $TrayStartupValueName -ErrorAction SilentlyContinue
}

if (-not $KeepInstallDir) {
    Remove-DirectoryWithRetry -Path $InstallDir
}
Remove-DirectoryWithRetry -Path $versionsDir
Remove-DirectoryWithRetry -Path $updateDir
Remove-Item -LiteralPath $activeVersionPath -Force -ErrorAction SilentlyContinue
foreach ($name in @(
    'start-agentdock.ps1',
    'start-cloudflared.ps1',
    'installer\manage-windows.ps1',
    'auth-token.dpapi',
    'oauth-password.dpapi',
    'oauth-token-secret.dpapi',
    'oauth-access-token-ttl.txt',
    'server-url.txt',
    'named-server-url.txt',
    'control-panel-settings.json',
    'cloudflared-mode.txt',
    'cloudflared-token.dpapi',
    'cloudflared.out.log',
    'cloudflared.err.log',
    'quick-tunnel-url.txt',
    'runtime.json',
    'desktop-version.txt'
)) {
    Remove-Item -LiteralPath (Join-Path $runtimeDir $name) -Force -ErrorAction SilentlyContinue
}

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$updated = @($userPath -split ';' | Where-Object { $_ -and $_ -ne $InstallDir }) -join ';'
[Environment]::SetEnvironmentVariable('Path', $updated, 'User')

if ($PurgeState) {
    if ([string]::IsNullOrWhiteSpace($userHome)) {
        throw 'Unable to resolve the current user profile directory.'
    }
    Remove-Item -LiteralPath (Join-Path $userHome '.agentdock') -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath (Join-Path $userHome 'AgentDock') -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host 'AgentDock, its tray, and its managed Cloudflare Tunnel were uninstalled.'

[CmdletBinding()]
param(
    [string] $InstallerPath = '',
    [string] $Version = 'latest',
    [string] $ReleaseBaseUrl = '',
    [int] $Port = 18765
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

if (-not $InstallerPath) {
    $InstallerPath = Join-Path $PSScriptRoot '..\install\install.ps1'
}
$resolvedInstaller = Resolve-Path -LiteralPath $InstallerPath
$testRoot = Join-Path ([IO.Path]::GetTempPath()) ('agentdock-installer-e2e-' + [Guid]::NewGuid().ToString('N'))
$installDir = Join-Path $testRoot 'bin'
$binaryPath = Join-Path $installDir 'agentdock.exe'
$trayBinaryPath = Join-Path $installDir 'agentdock-tray.exe'
$trayIconPath = Join-Path $installDir 'agentdock.ico'
$runtimeManifestPath = Join-Path $testRoot 'runtime.json'
$desktopVersionPath = Join-Path $testRoot 'desktop-version.txt'
$tokenPath = Join-Path $testRoot 'auth-token.dpapi'
$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$runValueName = 'AgentDock'
$trayRunValueName = 'AgentDockTray'
$healthUrl = "http://127.0.0.1:$Port/healthz"
$originalUserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$originalReleaseBaseUrl = $env:AGENTDOCK_RELEASE_BASE_URL
if ($ReleaseBaseUrl) {
    $env:AGENTDOCK_RELEASE_BASE_URL = $ReleaseBaseUrl
}

function Stop-TestAgentDock {
    Get-Process -Name 'agentdock-tray' -ErrorAction SilentlyContinue | Where-Object {
        try {
            [string]::Equals([IO.Path]::GetFullPath($_.Path), [IO.Path]::GetFullPath($trayBinaryPath), [StringComparison]::OrdinalIgnoreCase)
        } catch {
            $false
        }
    } | Stop-Process -Force -ErrorAction SilentlyContinue

    Get-Process -Name 'agentdock' -ErrorAction SilentlyContinue | Where-Object {
        try {
            [string]::Equals(
                [IO.Path]::GetFullPath($_.Path),
                [IO.Path]::GetFullPath($binaryPath),
                [StringComparison]::OrdinalIgnoreCase
            )
        }
        catch {
            $false
        }
    } | Stop-Process -Force -ErrorAction SilentlyContinue
}

function Assert-AgentDockHealthy {
    $response = Invoke-WebRequest -UseBasicParsing -Uri $healthUrl -TimeoutSec 5
    if ($response.StatusCode -ne 200) {
        throw "Unexpected AgentDock health status: $($response.StatusCode)"
    }
    if (-not (Test-Path -LiteralPath $binaryPath -PathType Leaf)) {
        throw "AgentDock binary was not installed: $binaryPath"
    }
    if (-not (Test-Path -LiteralPath $trayIconPath -PathType Leaf)) {
        throw "AgentDock tray icon was not installed: $trayIconPath"
    }
    if (-not (Test-Path -LiteralPath $trayBinaryPath -PathType Leaf)) {
        throw "AgentDock tray was not installed: $trayBinaryPath"
    }
    if (-not (Test-Path -LiteralPath $runtimeManifestPath -PathType Leaf)) {
        throw "AgentDock runtime manifest was not created: $runtimeManifestPath"
    }
    if (-not (Test-Path -LiteralPath $desktopVersionPath -PathType Leaf)) {
        throw "AgentDock desktop version marker was not created: $desktopVersionPath"
    }
    $installedVersion = (& $binaryPath version --json | ConvertFrom-Json).version
    $desktopVersion = (Get-Content -LiteralPath $desktopVersionPath -Raw).Trim()
    if ($desktopVersion -ne ("v" + ([string] $installedVersion).TrimStart('v'))) {
        throw "AgentDock desktop version marker does not match the installed core: $desktopVersion / $installedVersion"
    }
    $runtimeManifest = Get-Content -LiteralPath $runtimeManifestPath -Raw | ConvertFrom-Json
    if ($runtimeManifest.tunnel_mode -ne 'none' -or $runtimeManifest.local_mcp_url -ne "http://127.0.0.1:$Port/mcp") {
        throw 'AgentDock runtime manifest contains unexpected local settings.'
    }
    if (-not [string]::Equals(
        [IO.Path]::GetFullPath([string] $runtimeManifest.install_root),
        [IO.Path]::GetFullPath($testRoot),
        [StringComparison]::OrdinalIgnoreCase)) {
        throw "AgentDock runtime manifest install_root does not match the real runtime root: $($runtimeManifest.install_root) / $testRoot"
    }
    foreach ($entry in @(
        @{ Name = 'agentdock_binary'; Expected = $binaryPath },
        @{ Name = 'tray_binary'; Expected = $trayBinaryPath }
    )) {
        $recordedPath = [string] $runtimeManifest.($entry.Name)
        if (-not [string]::Equals(
            [IO.Path]::GetFullPath($recordedPath),
            [IO.Path]::GetFullPath([string] $entry.Expected),
            [StringComparison]::OrdinalIgnoreCase)) {
            throw "AgentDock runtime manifest $($entry.Name) does not match the installed file: $recordedPath / $($entry.Expected)"
        }
        if (-not (Test-Path -LiteralPath $recordedPath -PathType Leaf)) {
            throw "AgentDock runtime manifest $($entry.Name) records a missing file: $recordedPath"
        }
    }
    if (-not (Test-Path -LiteralPath $tokenPath -PathType Leaf)) {
        throw "AgentDock DPAPI token was not created: $tokenPath"
    }
    $startupCommand = Get-ItemPropertyValue -LiteralPath $runKey -Name $runValueName -ErrorAction Stop
    $trayStartupCommand = Get-ItemPropertyValue -LiteralPath $runKey -Name $trayRunValueName -ErrorAction Stop
    if (-not $trayStartupCommand.Contains($trayBinaryPath) -or -not $trayStartupCommand.Contains('--background')) {
        throw "AgentDock tray HKCU startup command is incorrect: $trayStartupCommand"
    }
    if (-not $startupCommand.Contains($trayBinaryPath) -or
        -not $startupCommand.Contains('--start-core') -or
        -not $startupCommand.Contains('--runtime-root') -or
        -not $startupCommand.Contains($testRoot)) {
        throw "AgentDock core HKCU startup command is not native: $startupCommand"
    }
    if ($startupCommand.Contains('powershell.exe') -or $startupCommand.Contains('start-agentdock.ps1')) {
        throw "AgentDock core HKCU startup command still uses the legacy launcher: $startupCommand"
    }

    $userHome = [Environment]::GetFolderPath('UserProfile')
    $skillStore = Join-Path $userHome '.agentdock\skill-store'
    $bundledPath = Join-Path $skillStore 'bundled-skills.json'
    if (-not (Test-Path -LiteralPath $bundledPath -PathType Leaf)) {
        throw "Bundled Skill list was not created: $bundledPath"
    }
    $bundled = @((Get-Content -LiteralPath $bundledPath -Raw | ConvertFrom-Json).skills)
    foreach ($skill in @('agentdock-user-guide', 'skill-authoring', 'skill-installation', 'skill-vetter-runtime')) {
        if ($bundled -notcontains $skill) {
            throw "Bundled Skill list does not contain $skill."
        }
        $statePath = Join-Path $skillStore "state\$skill.json"
        $state = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json
        if (-not $state.active_version) {
            throw "Bundled Skill has no active version: $skill"
        }
        $documentPath = Join-Path $skillStore "installed\$skill\$($state.active_version)\SKILL.md"
        if (-not (Test-Path -LiteralPath $documentPath -PathType Leaf)) {
            throw "Bundled Skill document was not installed: $documentPath"
        }
    }
}

function Assert-RedirectedManifestRecovery {
    $managerPath = Join-Path $testRoot 'installer\manage-windows.ps1'
    if (-not (Test-Path -LiteralPath $managerPath -PathType Leaf)) {
        throw "AgentDock Windows manager was not installed: $managerPath"
    }

    $manifest = Get-Content -LiteralPath $runtimeManifestPath -Raw | ConvertFrom-Json
    $staleRoot = Join-Path ([IO.Path]::GetTempPath()) ('agentdock-msix-stale-' + [Guid]::NewGuid().ToString('N'))
    $manifest.install_root = $staleRoot
    $manifest.agentdock_binary = Join-Path $staleRoot 'bin\agentdock.exe'
    $manifest.tray_binary = Join-Path $staleRoot 'bin\agentdock-tray.exe'
    $manifest.agentdock_launcher = Join-Path $staleRoot 'start-agentdock.ps1'
    if ($null -ne $manifest.PSObject.Properties['cloudflared_binary'] -and
        -not [string]::IsNullOrWhiteSpace([string] $manifest.cloudflared_binary)) {
        $manifest.cloudflared_binary = Join-Path $staleRoot 'bin\cloudflared.exe'
    }
    if ($null -ne $manifest.PSObject.Properties['cloudflared_launcher'] -and
        -not [string]::IsNullOrWhiteSpace([string] $manifest.cloudflared_launcher)) {
        $manifest.cloudflared_launcher = Join-Path $staleRoot 'start-cloudflared.ps1'
    }
    [IO.File]::WriteAllText(
        $runtimeManifestPath,
        ($manifest | ConvertTo-Json -Depth 4),
        (New-Object System.Text.UTF8Encoding($false)))

    if (Test-Path -LiteralPath $manifest.agentdock_binary -PathType Leaf) {
        throw 'Issue #22 fixture is invalid: stale core path unexpectedly exists.'
    }
    & powershell.exe -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass `
        -File $managerPath `
        -Action restart `
        -RuntimeRoot $testRoot
    if ($LASTEXITCODE -ne 0) {
        throw "AgentDock manager could not recover a redirected runtime manifest: $LASTEXITCODE"
    }

    $response = Invoke-WebRequest -UseBasicParsing -Uri $healthUrl -TimeoutSec 5
    if ($response.StatusCode -ne 200) {
        throw "AgentDock redirected manifest recovery health check failed: $($response.StatusCode)"
    }
    $after = Get-Content -LiteralPath $runtimeManifestPath -Raw | ConvertFrom-Json
    if (-not [string]::Equals(
        [IO.Path]::GetFullPath([string] $after.install_root),
        [IO.Path]::GetFullPath($staleRoot),
        [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Issue #22 recovery test was masked by rewriting runtime.json before restart completed.'
    }
}

$identity = [System.Security.Principal.WindowsIdentity]::GetCurrent()
$principal = [System.Security.Principal.WindowsPrincipal]::new($identity)
if ($principal.IsInRole([System.Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw 'Windows installer E2E must run as a non-administrator.'
}

try {
    & $resolvedInstaller `
        -Version $Version `
        -InstallDir $installDir `
        -RegisterStartup `
        -TunnelMode none `
        -Port $Port `
        -AuthToken 'agentdock-e2e-token'

    Assert-AgentDockHealthy
    $tokenHashBeforeUpgrade = (Get-FileHash -LiteralPath $tokenPath -Algorithm SHA256).Hash

    # 第二次执行必须覆盖正在运行的二进制，并保留已有 DPAPI Token。
    & $resolvedInstaller `
        -Version $Version `
        -InstallDir $installDir `
        -RegisterStartup `
        -TunnelMode none `
        -Port $Port

    Assert-AgentDockHealthy
    $tokenHashAfterUpgrade = (Get-FileHash -LiteralPath $tokenPath -Algorithm SHA256).Hash
    if ($tokenHashBeforeUpgrade -ne $tokenHashAfterUpgrade) {
        throw 'AgentDock DPAPI token changed during an in-place upgrade.'
    }

    Assert-RedirectedManifestRecovery
    Write-Host 'AgentDock Windows full install, in-place upgrade, and redirected manifest recovery passed.'
}
finally {
    Stop-TestAgentDock
    Remove-ItemProperty -LiteralPath $runKey -Name $runValueName -ErrorAction SilentlyContinue
    Remove-ItemProperty -LiteralPath $runKey -Name $trayRunValueName -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $testRoot -Recurse -Force -ErrorAction SilentlyContinue
    [Environment]::SetEnvironmentVariable('Path', $originalUserPath, 'User')
    $env:AGENTDOCK_RELEASE_BASE_URL = $originalReleaseBaseUrl
}

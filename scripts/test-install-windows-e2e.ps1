[CmdletBinding()]
param(
    [string] $InstallerPath = '',
    [string] $Version = 'v0.2.0',
    [int] $Port = 18765
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

if (-not $InstallerPath) {
    $InstallerPath = Join-Path $PSScriptRoot 'install-windows.ps1'
}
$resolvedInstaller = Resolve-Path -LiteralPath $InstallerPath
$testRoot = Join-Path ([IO.Path]::GetTempPath()) ('agentdock-installer-e2e-' + [Guid]::NewGuid().ToString('N'))
$installDir = Join-Path $testRoot 'bin'
$binaryPath = Join-Path $installDir 'agentdock.exe'
$tokenPath = Join-Path $testRoot 'auth-token.dpapi'
$launcherPath = Join-Path $testRoot 'start-agentdock.ps1'
$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$runValueName = 'AgentDock'
$healthUrl = "http://127.0.0.1:$Port/healthz"
$originalUserPath = [Environment]::GetEnvironmentVariable('Path', 'User')

function Stop-TestAgentDock {
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
    if (-not (Test-Path -LiteralPath $tokenPath -PathType Leaf)) {
        throw "AgentDock DPAPI token was not created: $tokenPath"
    }
    $startupCommand = Get-ItemPropertyValue -LiteralPath $runKey -Name $runValueName -ErrorAction Stop
    if (-not $startupCommand.Contains($launcherPath)) {
        throw "AgentDock HKCU startup command does not reference the launcher: $startupCommand"
    }

    $skillStore = Join-Path $HOME '.agentdock\skill-store'
    $bundledPath = Join-Path $skillStore 'bundled-skills.json'
    if (-not (Test-Path -LiteralPath $bundledPath -PathType Leaf)) {
        throw "Bundled Skill list was not created: $bundledPath"
    }
    $bundled = @((Get-Content -LiteralPath $bundledPath -Raw | ConvertFrom-Json).skills)
    foreach ($skill in @('skill-authoring', 'skill-installation', 'skill-vetter-runtime')) {
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
        -Port $Port `
        -AuthToken 'agentdock-e2e-token'

    Assert-AgentDockHealthy
    $tokenHashBeforeUpgrade = (Get-FileHash -LiteralPath $tokenPath -Algorithm SHA256).Hash

    # 第二次执行必须覆盖正在运行的二进制，并保留已有 DPAPI Token。
    & $resolvedInstaller `
        -Version $Version `
        -InstallDir $installDir `
        -RegisterStartup `
        -Port $Port

    Assert-AgentDockHealthy
    $tokenHashAfterUpgrade = (Get-FileHash -LiteralPath $tokenPath -Algorithm SHA256).Hash
    if ($tokenHashBeforeUpgrade -ne $tokenHashAfterUpgrade) {
        throw 'AgentDock DPAPI token changed during an in-place upgrade.'
    }

    Write-Host 'AgentDock Windows full install and in-place upgrade passed.'
}
finally {
    Stop-TestAgentDock
    Remove-ItemProperty -LiteralPath $runKey -Name $runValueName -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $testRoot -Recurse -Force -ErrorAction SilentlyContinue
    [Environment]::SetEnvironmentVariable('Path', $originalUserPath, 'User')
}

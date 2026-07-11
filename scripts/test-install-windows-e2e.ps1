[CmdletBinding()]
param(
    [string] $InstallerPath = (Join-Path $PSScriptRoot 'install-windows.ps1'),
    [string] $Version = 'v0.2.5',
    [int] $Port = 18765
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

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

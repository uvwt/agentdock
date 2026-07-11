[CmdletBinding()]
param(
    [string] $InstallerPath = (Join-Path $PSScriptRoot 'install-windows.ps1'),
    [string] $Version = 'v0.2.4',
    [int] $Port = 18765
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$resolvedInstaller = Resolve-Path -LiteralPath $InstallerPath
$taskName = 'AgentDock'
$testRoot = Join-Path $env:RUNNER_TEMP ('agentdock-installer-e2e-' + [Guid]::NewGuid().ToString('N'))
$installDir = Join-Path $testRoot 'bin'
$binaryPath = Join-Path $installDir 'agentdock.exe'
$tokenPath = Join-Path $testRoot 'auth-token.dpapi'
$healthUrl = "http://127.0.0.1:$Port/healthz"
$originalUserPath = [Environment]::GetEnvironmentVariable('Path', 'User')

function Stop-TestAgentDock {
    Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
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
    if ($null -eq (Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue)) {
        throw 'AgentDock scheduled task was not registered.'
    }
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
    Unregister-ScheduledTask -TaskName $taskName -Confirm:$false -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $testRoot -Recurse -Force -ErrorAction SilentlyContinue
    [Environment]::SetEnvironmentVariable('Path', $originalUserPath, 'User')
}

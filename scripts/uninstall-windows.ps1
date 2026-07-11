[CmdletBinding()]
param(
    [string] $InstallDir = (Join-Path $env:LOCALAPPDATA 'AgentDock\bin'),
    [switch] $PurgeState
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$task = Get-ScheduledTask -TaskName 'AgentDock' -ErrorAction SilentlyContinue
if ($task) {
    Stop-ScheduledTask -TaskName 'AgentDock' -ErrorAction SilentlyContinue
    Unregister-ScheduledTask -TaskName 'AgentDock' -Confirm:$false
}

$runtimeDir = Split-Path -Parent $InstallDir
Remove-Item -LiteralPath $InstallDir -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath (Join-Path $runtimeDir 'start-agentdock.ps1') -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath (Join-Path $runtimeDir 'auth-token.dpapi') -Force -ErrorAction SilentlyContinue

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$updated = @($userPath -split ';' | Where-Object { $_ -and $_ -ne $InstallDir }) -join ';'
[Environment]::SetEnvironmentVariable('Path', $updated, 'User')

if ($PurgeState) {
    Remove-Item -LiteralPath (Join-Path $HOME '.agentdock') -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath (Join-Path $HOME 'AgentDock') -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host 'AgentDock uninstalled.'

[CmdletBinding()]
param(
    [string] $InstallerPath = (Join-Path $PSScriptRoot 'install-windows.ps1')
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$resolvedInstaller = Resolve-Path -LiteralPath $InstallerPath
$tokens = $null
$errors = $null
[System.Management.Automation.Language.Parser]::ParseFile(
    $resolvedInstaller,
    [ref] $tokens,
    [ref] $errors
) | Out-Null
if ($errors.Count -gt 0) {
    $errors | ForEach-Object { Write-Error $_.Message }
    throw "$InstallerPath contains PowerShell syntax errors"
}

$content = Get-Content -LiteralPath $resolvedInstaller -Raw
foreach ($forbidden in @(
    'Set-PrivateAcl',
    'Get-Acl',
    'Set-Acl',
    'icacls.exe',
    '$icaclsArguments',
    '$AclSelfTest',
    '$sddl',
    'Register-ScheduledTask',
    'Start-ScheduledTask',
    'Stop-ScheduledTask',
    'Get-ScheduledTask',
    'Unregister-ScheduledTask'
)) {
    if ($content.Contains($forbidden)) {
        throw "$InstallerPath still contains removed privileged startup or ACL code: $forbidden"
    }
}

foreach ($required in @(
    'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run',
    'New-ItemProperty -Path $runKey -Name $runValueName',
    'Start-AgentDockLauncher -LauncherPath $launcherPath'
)) {
    if (-not $content.Contains($required)) {
        throw "$InstallerPath is missing current-user startup logic: $required"
    }
}

Write-Host 'Windows installer validation passed.'

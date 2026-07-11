[CmdletBinding()]
param(
    [string] $InstallerPath = (Join-Path $PSScriptRoot 'install-windows.ps1')
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile(
    (Resolve-Path -LiteralPath $InstallerPath),
    [ref] $tokens,
    [ref] $errors
)
if ($errors.Count -gt 0) {
    $errors | ForEach-Object { Write-Error $_.Message }
    throw "$InstallerPath contains PowerShell syntax errors"
}

& (Resolve-Path -LiteralPath $InstallerPath) -AclSelfTest
if ($LASTEXITCODE -notin @(0, $null)) {
    throw "$InstallerPath ACL self-test failed with exit code $LASTEXITCODE"
}

Write-Host 'Windows installer validation passed.'

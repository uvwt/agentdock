[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string] $Path,
    [Parameter(Mandatory = $true)]
    [string] $Entropy
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
    exit 2
}

try {
    Add-Type -AssemblyName System.Security
    $encoded = [IO.File]::ReadAllText($Path).Trim()
    if ([string]::IsNullOrWhiteSpace($encoded)) {
        exit 2
    }
    $protectedBytes = [Convert]::FromBase64String($encoded)
    $plainBytes = [System.Security.Cryptography.ProtectedData]::Unprotect(
        $protectedBytes,
        [Text.Encoding]::UTF8.GetBytes($Entropy),
        [System.Security.Cryptography.DataProtectionScope]::CurrentUser
    )
    $value = [Text.Encoding]::UTF8.GetString($plainBytes)
    if ([string]::IsNullOrWhiteSpace($value)) {
        exit 2
    }
    exit 0
} catch {
    exit 3
}

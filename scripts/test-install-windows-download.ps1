[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string] $Url,
    [Parameter(Mandatory = $true)]
    [string] $ExpectedInstallerPath,
    [Parameter(Mandatory = $true)]
    [string] $OutputPath
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$resolvedExpected = Resolve-Path -LiteralPath $ExpectedInstallerPath
$downloaded = $false
for ($attempt = 1; $attempt -le 10; $attempt++) {
    try {
        Invoke-WebRequest -UseBasicParsing -Uri $Url -OutFile $OutputPath
        $downloaded = $true
        break
    } catch {
        if ($attempt -eq 10) {
            throw
        }
        Start-Sleep -Seconds 2
    }
}
if (-not $downloaded) {
    throw "Unable to download Windows installer: $Url"
}

$expectedHash = (Get-FileHash -LiteralPath $resolvedExpected -Algorithm SHA256).Hash
$downloadedHash = (Get-FileHash -LiteralPath $OutputPath -Algorithm SHA256).Hash
if ($expectedHash -ne $downloadedHash) {
    throw "Downloaded installer hash mismatch. Expected $expectedHash, got $downloadedHash."
}

& (Join-Path $PSScriptRoot 'test-install-windows.ps1') -InstallerPath $OutputPath
Write-Host "Downloaded Windows installer validation passed: $downloadedHash"

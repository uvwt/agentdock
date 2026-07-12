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

$expectedContent = [IO.File]::ReadAllText($resolvedExpected).Replace("`r`n", "`n")
$downloadedContent = [IO.File]::ReadAllText($OutputPath).Replace("`r`n", "`n")
if (-not [string]::Equals($expectedContent, $downloadedContent, [StringComparison]::Ordinal)) {
    throw 'Downloaded installer content does not match the repository version after line-ending normalization.'
}

$downloadedHash = (Get-FileHash -LiteralPath $OutputPath -Algorithm SHA256).Hash
& (Join-Path $PSScriptRoot 'test-install-windows.ps1') -InstallerPath $OutputPath
Write-Host "Downloaded Windows installer validation passed: $downloadedHash"

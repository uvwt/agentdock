[CmdletBinding()]
param(
    [string] $OutputDirectory = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
if ([string]::IsNullOrWhiteSpace($OutputDirectory)) {
    $OutputDirectory = Join-Path $repoRoot 'dist\wsl-helper'
} elseif (-not [IO.Path]::IsPathRooted($OutputDirectory)) {
    $OutputDirectory = [IO.Path]::GetFullPath((Join-Path (Get-Location) $OutputDirectory))
}

$previousGOOS = [Environment]::GetEnvironmentVariable('GOOS', 'Process')
$previousGOARCH = [Environment]::GetEnvironmentVariable('GOARCH', 'Process')
$previousCGOEnabled = [Environment]::GetEnvironmentVariable('CGO_ENABLED', 'Process')

try {
    New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null
    $helpers = [ordered]@{}

    Push-Location $repoRoot
    try {
        $env:GOOS = 'linux'
        $env:CGO_ENABLED = '0'
        foreach ($architecture in @('amd64', 'arm64')) {
            $env:GOARCH = $architecture
            $fileName = "agentdock-wsl-helper-linux-$architecture"
            $outputPath = Join-Path $OutputDirectory $fileName
            & go build -trimpath -ldflags '-s -w' -o $outputPath .\cmd\agentdock-wsl-helper
            if ($LASTEXITCODE -ne 0) {
                throw "Failed to build WSL helper for linux/$architecture."
            }
            if (-not (Test-Path -LiteralPath $outputPath -PathType Leaf)) {
                throw "WSL helper build did not produce: $outputPath"
            }
            $hash = (Get-FileHash -LiteralPath $outputPath -Algorithm SHA256).Hash.ToLowerInvariant()
            $helpers[$architecture] = [ordered]@{
                file = $fileName
                sha256 = $hash
            }
        }
    } finally {
        Pop-Location
    }

    $manifest = [ordered]@{
        protocol_version = '1'
        helpers = $helpers
    }
    $manifestPath = Join-Path $OutputDirectory 'manifest.json'
    $manifestJSON = $manifest | ConvertTo-Json -Depth 5
    [IO.File]::WriteAllText($manifestPath, $manifestJSON + "`n", [Text.UTF8Encoding]::new($false))
    Write-Host "WSL helper payload built: $OutputDirectory"
} finally {
    [Environment]::SetEnvironmentVariable('GOOS', $previousGOOS, 'Process')
    [Environment]::SetEnvironmentVariable('GOARCH', $previousGOARCH, 'Process')
    [Environment]::SetEnvironmentVariable('CGO_ENABLED', $previousCGOEnabled, 'Process')
}

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string] $Version,
    [Parameter(Mandatory = $true)]
    [ValidateSet('amd64', 'arm64')]
    [string] $Architecture,
    [Parameter(Mandatory = $true)]
    [string] $AgentDockArchive,
    [Parameter(Mandatory = $true)]
    [string] $AgentDockChecksumFile,
    [Parameter(Mandatory = $true)]
    [string] $OutputDirectory,
    [switch] $SignedBuild
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

function Resolve-RequiredFile {
    param([string] $Path, [string] $Description)

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "$Description was not found: $Path"
    }
    return (Resolve-Path -LiteralPath $Path).Path
}

function Resolve-InnoSetupCompiler {
    $command = Get-Command 'ISCC.exe' -ErrorAction SilentlyContinue
    if ($null -ne $command -and -not [string]::IsNullOrWhiteSpace([string] $command.Source)) {
        return $command.Source
    }

    $candidates = @()
    if (-not [string]::IsNullOrWhiteSpace(${env:ProgramFiles(x86)})) {
        $candidates += (Join-Path ${env:ProgramFiles(x86)} 'Inno Setup 6\ISCC.exe')
    }
    if (-not [string]::IsNullOrWhiteSpace($env:ProgramFiles)) {
        $candidates += (Join-Path $env:ProgramFiles 'Inno Setup 6\ISCC.exe')
    }
    if (-not [string]::IsNullOrWhiteSpace($env:LOCALAPPDATA)) {
        $candidates += (Join-Path $env:LOCALAPPDATA 'Programs\Inno Setup 6\ISCC.exe')
    }
    foreach ($candidate in $candidates) {
        if (Test-Path -LiteralPath $candidate -PathType Leaf) {
            return (Resolve-Path -LiteralPath $candidate).Path
        }
    }
    throw "Inno Setup compiler was not found. Checked PATH and: $($candidates -join ', ')"
}

$archivePath = Resolve-RequiredFile -Path $AgentDockArchive -Description 'AgentDock archive'
$checksumPath = Resolve-RequiredFile -Path $AgentDockChecksumFile -Description 'AgentDock checksum file'

$expectedHash = ((Get-Content -LiteralPath $checksumPath -Raw).Trim() -split '\s+')[0].ToLowerInvariant()
$actualHash = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actualHash -ne $expectedHash) {
    throw "AgentDock archive SHA-256 mismatch. Expected $expectedHash, got $actualHash."
}

Add-Type -AssemblyName System.IO.Compression.FileSystem
$archive = [IO.Compression.ZipFile]::OpenRead($archivePath)
try {
    $entryNames = @($archive.Entries | ForEach-Object { $_.FullName.Replace('\', '/') })
    foreach ($requiredEntry in @(
        'agentdock.exe',
        'agentdock-tray.exe',
        'control-panel/Microsoft.WindowsAppRuntime.Bootstrap.dll',
        'agentdock-arbiter.exe',
        'agentdock-shim.exe',
        'agentdock-tray-shim.exe',
        'agentdock.ico',
        'wsl-helper/manifest.json',
        'wsl-helper/agentdock-wsl-helper-linux-amd64',
        'wsl-helper/agentdock-wsl-helper-linux-arm64',
        'share/agentdock/core-skills/manifest.json'
    )) {
        if ($entryNames -notcontains $requiredEntry) {
            throw "AgentDock archive does not contain required entry: $requiredEntry"
        }
    }
} finally {
    $archive.Dispose()
}

$iscc = Resolve-InnoSetupCompiler

$outputRoot = [IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Path $outputRoot -Force | Out-Null
$payloadRoot = Join-Path ([IO.Path]::GetTempPath()) ('agentdock-setup-payload-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $payloadRoot -Force | Out-Null

try {
    $assetName = "agentdock_windows_$Architecture.zip"
    Copy-Item -LiteralPath $archivePath -Destination (Join-Path $payloadRoot $assetName) -Force
    Copy-Item -LiteralPath $checksumPath -Destination (Join-Path $payloadRoot "$assetName.sha256") -Force

    $arguments = @(
        "/DAppVersion=$Version",
        "/DOutputDir=$outputRoot",
        "/DPayloadDir=$payloadRoot"
    )
    if ($Architecture -eq 'arm64') {
        $arguments += '/DWindowsARM64=1'
    }
    if ($SignedBuild) {
        if ([string]::IsNullOrWhiteSpace($env:WINDOWS_SIGNING_CERT_BASE64) -or
            [string]::IsNullOrWhiteSpace($env:WINDOWS_SIGNING_CERT_PASSWORD)) {
            throw 'Windows signing secrets are required for a signed Setup.'
        }
        $signScript = Join-Path $PSScriptRoot 'sign-windows.ps1'
        $signCommand = "pwsh.exe -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File `$q$signScript`$q -Path `$q`$f`$q"
        $arguments += '/DSignedBuild=1'
        $arguments += "/Sagentdock-sign=$signCommand"
    }
    $arguments += (Join-Path $PSScriptRoot 'AgentDock.iss')

    & $iscc @arguments
    if ($LASTEXITCODE -ne 0) {
        throw "ISCC failed with exit code $LASTEXITCODE."
    }

    $setupPath = Join-Path $outputRoot "AgentDockSetup-$Architecture.exe"
    if (-not (Test-Path -LiteralPath $setupPath -PathType Leaf)) {
        throw "Windows Setup was not produced: $setupPath"
    }
    if ($SignedBuild) {
        & (Join-Path $PSScriptRoot 'sign-windows.ps1') -Path $setupPath -VerifyOnly
    }

    # Windows Setup 只封装 AgentDock 本体。cloudflared 是运行时按需安装的 optional
    # component，不能再用安装包体积间接要求它存在。
    $minimumExpectedSize = [Math]::Max(1MB, [int64] ((Get-Item -LiteralPath $archivePath).Length * 0.6))
    if ((Get-Item -LiteralPath $setupPath).Length -lt $minimumExpectedSize) {
        throw 'Windows Setup is unexpectedly small and may not contain the AgentDock payload.'
    }

    Write-Host "Windows Setup created: $setupPath"
    Write-Output $setupPath
} finally {
    Remove-Item -LiteralPath $payloadRoot -Recurse -Force -ErrorAction SilentlyContinue
}

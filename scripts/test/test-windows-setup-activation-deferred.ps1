[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string] $InstallerPath,
    [Parameter(Mandatory = $true)]
    [string] $AgentDockArchive,
    [Parameter(Mandatory = $true)]
    [string] $AgentDockChecksumFile,
    [Parameter(Mandatory = $true)]
    [string] $CloudflaredBinary,
    [int] $Port = 18795
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$resolvedInstaller = (Resolve-Path -LiteralPath $InstallerPath).Path
$resolvedArchive = (Resolve-Path -LiteralPath $AgentDockArchive).Path
$resolvedChecksum = (Resolve-Path -LiteralPath $AgentDockChecksumFile).Path
$resolvedCloudflared = (Resolve-Path -LiteralPath $CloudflaredBinary).Path
$testRoot = Join-Path ([IO.Path]::GetTempPath()) ('agentdock-setup-deferred-' + [Guid]::NewGuid().ToString('N'))
$installerRoot = Join-Path $testRoot 'installer'
$installDir = Join-Path $testRoot 'runtime\bin'
$resultPath = Join-Path $testRoot 'result.ini'
$testInstaller = Join-Path $installerRoot 'install.ps1'
$testBroker = Join-Path $installerRoot 'launch-windows-process.ps1'
$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$suffix = [Guid]::NewGuid().ToString('N')
$coreRunName = "AgentDockDeferredCore-$suffix"
$trayRunName = "AgentDockDeferredTray-$suffix"
$tunnelRunName = "AgentDockDeferredTunnel-$suffix"
$oldUserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$oldHome = $env:AGENTDOCK_HOME
$oldDefaultDir = $env:AGENTDOCK_DEFAULT_DIR

function Stop-TestProcessByPath {
    param([string] $ProcessName, [string] $BinaryPath)

    if (-not (Test-Path -LiteralPath $BinaryPath -PathType Leaf)) {
        return
    }
    $expected = [IO.Path]::GetFullPath($BinaryPath)
    Get-Process -Name $ProcessName -ErrorAction SilentlyContinue | Where-Object {
        try {
            [string]::Equals([IO.Path]::GetFullPath($_.Path), $expected, [StringComparison]::OrdinalIgnoreCase)
        } catch {
            $false
        }
    } | Stop-Process -Force -ErrorAction SilentlyContinue
}

try {
    New-Item -ItemType Directory -Path $installerRoot -Force | Out-Null
    Copy-Item -LiteralPath $resolvedInstaller -Destination $testInstaller -Force
    [IO.File]::WriteAllText(
        $testBroker,
        "throw 'simulated Setup runtime broker failure'`r`n",
        [Text.UTF8Encoding]::new($false)
    )

    # Keep all mutable AgentDock state isolated from the real Tianyi installation.
    $env:AGENTDOCK_HOME = Join-Path $testRoot '.agentdock'
    $env:AGENTDOCK_DEFAULT_DIR = Join-Path $testRoot 'workspace'

    $installOutput = @(& powershell.exe `
        -NoLogo `
        -NoProfile `
        -NonInteractive `
        -ExecutionPolicy Bypass `
        -File $testInstaller `
        -Version '0.0.0-test' `
        -OfflineArchive $resolvedArchive `
        -OfflineChecksumFile $resolvedChecksum `
        -OfflineCloudflaredBinary $resolvedCloudflared `
        -InstallDir $installDir `
        -RegisterStartup `
        -TunnelMode none `
        -InstallChannel setup `
        -CorePrivilegeMode standard `
        -Port $Port `
        -StartupValueName $coreRunName `
        -TrayStartupValueName $trayRunName `
        -CloudflaredStartupValueName $tunnelRunName `
        -ResultFile $resultPath 2>&1)
    if ($LASTEXITCODE -ne 0) {
        $safeOutput = (($installOutput | Out-String) `
            -replace '(?im)(Bearer Token:\s*)\S+', '$1[REDACTED]' `
            -replace '(?im)(OAuth login password:\s*)\S+', '$1[REDACTED]').Trim()
        throw "Fresh standard installation failed instead of deferring activation: exit code $LASTEXITCODE`n$safeOutput"
    }

    if (-not (Test-Path -LiteralPath $resultPath -PathType Leaf)) {
        throw 'Fresh standard installation did not write ResultFile.'
    }
    $result = [IO.File]::ReadAllText($resultPath, [Text.Encoding]::Unicode)
    foreach ($required in @(
        'Success=true',
        'WarningCode=runtime-launch-deferred',
        'Health=deferred',
        'PrivilegeMode=standard'
    )) {
        if (-not $result.Contains($required)) {
            throw "Deferred activation ResultFile is missing '$required':`n$result"
        }
    }

    foreach ($path in @(
        (Join-Path $installDir 'agentdock.exe'),
        (Join-Path $installDir 'agentdock-tray.exe'),
        (Join-Path $testRoot 'runtime\runtime.json')
    )) {
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
            throw "Provision was rolled back after broker failure: $path"
        }
    }
    foreach ($name in @($coreRunName, $trayRunName)) {
        $value = Get-ItemPropertyValue -LiteralPath $runKey -Name $name -ErrorAction SilentlyContinue
        if ([string]::IsNullOrWhiteSpace([string] $value)) {
            throw "Provision did not preserve startup registration after broker failure: $name"
        }
    }

    Write-Host 'Fresh standard Setup activation failure correctly deferred without rollback.'
} finally {
    $binaryPath = Join-Path $installDir 'agentdock.exe'
    $trayPath = Join-Path $installDir 'agentdock-tray.exe'
    Stop-TestProcessByPath -ProcessName 'agentdock-tray' -BinaryPath $trayPath
    Stop-TestProcessByPath -ProcessName 'agentdock' -BinaryPath $binaryPath
    foreach ($name in @($coreRunName, $trayRunName, $tunnelRunName)) {
        Remove-ItemProperty -LiteralPath $runKey -Name $name -ErrorAction SilentlyContinue
    }
    [Environment]::SetEnvironmentVariable('Path', $oldUserPath, 'User')
    $env:AGENTDOCK_HOME = $oldHome
    $env:AGENTDOCK_DEFAULT_DIR = $oldDefaultDir
    Remove-Item -LiteralPath $testRoot -Recurse -Force -ErrorAction SilentlyContinue
}

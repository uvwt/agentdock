[CmdletBinding()]
param(
    [string] $RuntimeHelperPath = '',
    [string] $RuntimeProbePath = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

if ([string]::IsNullOrWhiteSpace($RuntimeHelperPath)) {
    $RuntimeHelperPath = Join-Path $PSScriptRoot '..\..\packaging\windows\runtime-prerequisites.ps1'
}
if ([string]::IsNullOrWhiteSpace($RuntimeProbePath)) {
    $RuntimeProbePath = Join-Path $PSScriptRoot '..\..\packaging\windows\runtime-bootstrap-probe.ps1'
}
$resolvedRuntimeScript = (Resolve-Path -LiteralPath $RuntimeHelperPath).Path
$resolvedRuntimeProbe = (Resolve-Path -LiteralPath $RuntimeProbePath).Path

function Assert-PowerShellSyntax {
    param([Parameter(Mandatory = $true)][string] $Path)
    $tokens = $null
    $parseErrors = $null
    $parsed = [System.Management.Automation.Language.Parser]::ParseFile(
        $Path,
        [ref] $tokens,
        [ref] $parseErrors)
    if ($parseErrors.Count -gt 0) {
        throw "$Path has PowerShell syntax errors: $($parseErrors[0].Message)"
    }
    return $parsed
}

$ast = Assert-PowerShellSyntax -Path $resolvedRuntimeScript
$null = Assert-PowerShellSyntax -Path $resolvedRuntimeProbe
. $resolvedRuntimeProbe

$functionDefinitions = @()
foreach ($name in @(
    'Convert-ToVersion',
    'Test-WindowsAppRuntimePackageReady',
    'Get-WindowsAppRuntimeFrameworkVersion',
    'Test-WindowsAppRuntime'
)) {
    $definition = $ast.Find(
        {
            param($node)
            $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and
                $node.Name -eq $name
        },
        $true)
    if ($null -eq $definition) {
        throw "Runtime bootstrap function was not found: $name"
    }
    $functionDefinitions += $definition.Extent.Text
}
# Load only side-effect-free detection functions. The full bootstrap performs real downloads/installs.
Invoke-Expression ($functionDefinitions -join "`r`n")

$testRoot = Join-Path ([IO.Path]::GetTempPath()) ('agentdock-runtime-registration-test-' + [Guid]::NewGuid().ToString('N'))
$script:RegisteredPackages = @()

function New-MockPackage {
    param(
        [Parameter(Mandatory = $true)][string] $Name,
        [Parameter(Mandatory = $true)][string] $Version,
        [string] $Architecture = 'X64',
        [string] $Status = 'Ok',
        [string] $InstallLocation = $testRoot,
        [string] $PackageUserInformation = 'Installed'
    )
    $archToken = if ($Architecture -ieq 'Arm64') { 'arm64' } elseif ($Architecture -ieq 'X86') { 'x86' } else { 'x64' }
    return [pscustomobject]@{
        Name = $Name
        Version = [Version] $Version
        Architecture = $Architecture
        Status = $Status
        InstallLocation = $InstallLocation
        PackageFullName = "${Name}_${Version}_${archToken}__8wekyb3d8bbwe"
        PackageUserInformation = $PackageUserInformation
    }
}

function Get-AppxPackage {
    [CmdletBinding()]
    param(
        [string] $Name = '*',
        [switch] $AllUsers
    )
    if ($AllUsers) {
        throw 'Runtime version selection must not inspect other Windows users.'
    }
    return @($script:RegisteredPackages | Where-Object { $_.Name -like $Name })
}

$minimumVersion = [Version] '2.1.3.0'
$runtimeIdentityArgs = @{
    PackageName = 'Microsoft.WindowsAppRuntime.2'
    MainPackageName = 'MicrosoftCorporationII.WinAppRuntime.Main.2'
    SingletonPackageName = 'MicrosoftCorporationII.WinAppRuntime.Singleton'
    DdlmPackageNamePrefix = 'Microsoft.WinAppRuntime.DDLM.2.'
    TargetArchitecture = 'amd64'
}

try {
    New-Item -ItemType Directory -Path $testRoot -Force | Out-Null

    # Windows Compress-Archive stores directory entries with backslashes. The probe extractor must
    # normalize those names before matching the payload contract.
    $archiveSource = Join-Path $testRoot 'archive-source'
    $archiveControlPanel = Join-Path $archiveSource 'control-panel'
    New-Item -ItemType Directory -Path $archiveControlPanel -Force | Out-Null
    [IO.File]::WriteAllText((Join-Path $archiveSource 'agentdock-shim.exe'), 'shim-fixture', [Text.Encoding]::ASCII)
    [IO.File]::WriteAllText((Join-Path $archiveControlPanel 'Microsoft.WindowsAppRuntime.Bootstrap.dll'), 'bootstrap-fixture', [Text.Encoding]::ASCII)
    $archivePath = Join-Path $testRoot 'probe-fixture.zip'
    Compress-Archive `
        -Path (Join-Path $archiveSource 'agentdock-shim.exe'), $archiveControlPanel `
        -DestinationPath $archivePath `
        -Force
    $probeExtract = Join-Path $testRoot 'probe-extract'
    $probePayload = Get-WindowsAppRuntimeProbePayload -PayloadArchivePath $archivePath -DestinationDirectory $probeExtract
    if ([IO.File]::ReadAllText($probePayload.ProbeHostPath, [Text.Encoding]::ASCII) -ne 'shim-fixture' -or
        [IO.File]::ReadAllText($probePayload.BootstrapDllPath, [Text.Encoding]::ASCII) -ne 'bootstrap-fixture') {
        throw 'Windows App Runtime probe payload extraction did not preserve the expected entries.'
    }

    # Regression fixture: Framework 2.x is registered for Administrator, while matching 2.5.1
    # Main/Singleton/DDLM packages are only SYSTEM:Staged and must not count as current-user ready.
    $script:RegisteredPackages = @(
        New-MockPackage -Name 'Microsoft.WindowsAppRuntime.2' -Version '2.3.1.0'
        New-MockPackage -Name 'Microsoft.WindowsAppRuntime.2' -Version '2.4.0.0'
        New-MockPackage -Name 'Microsoft.WindowsAppRuntime.2' -Version '2.5.1.0'
        New-MockPackage -Name 'Microsoft.WinAppRuntime.DDLM.4000.1049.117.0-x6' -Version '4000.1049.117.0'
    )
    $currentVersion = Get-WindowsAppRuntimeFrameworkVersion `
        -PackageName $runtimeIdentityArgs.PackageName `
        -MinimumVersion $minimumVersion `
        -TargetArchitecture 'amd64'
    if ($currentVersion -ne [Version] '2.5.1.0') {
        throw "Highest current-user Framework version was $currentVersion, want 2.5.1.0."
    }
    if (Test-WindowsAppRuntime @runtimeIdentityArgs -RequiredVersion $currentVersion) {
        throw 'Framework-only current-user registration must not satisfy Windows App Runtime readiness.'
    }

    $repairVersion = Get-WindowsAppRuntimeFrameworkVersion `
        -PackageName $runtimeIdentityArgs.PackageName `
        -MinimumVersion $minimumVersion `
        -TargetArchitecture 'amd64'
    if ($repairVersion -ne [Version] '2.5.1.0') {
        throw "Repair version was $repairVersion, want the current user's highest Framework 2.5.1.0."
    }

    # Readiness requires all four package roles at one version to be registered for the interactive user.
    $script:RegisteredPackages += @(
        New-MockPackage -Name 'MicrosoftCorporationII.WinAppRuntime.Main.2' -Version '2.5.1.0'
        New-MockPackage -Name 'MicrosoftCorporationII.WinAppRuntime.Singleton' -Version '8002.5.1.0'
        New-MockPackage -Name 'Microsoft.WinAppRuntime.DDLM.2.5.1.0-x6' -Version '2.5.1.0'
    )
    if (-not (Test-WindowsAppRuntime @runtimeIdentityArgs -RequiredVersion ([Version] '2.5.1.0'))) {
        throw 'Complete current-user Windows App Runtime 2.5.1 registration should be ready.'
    }

    # Do not mix an older DDLM with a newer Framework/Main even if both exceed the app minimum.
    # Bootstrap requires the DDLM that belongs to the selected Framework release.
    $script:RegisteredPackages = @($script:RegisteredPackages | Where-Object {
        $_.Name -ne 'Microsoft.WinAppRuntime.DDLM.2.5.1.0-x6'
    }) + @(
        New-MockPackage -Name 'Microsoft.WinAppRuntime.DDLM.2.1.3.0-x6' -Version '2.1.3.0'
    )
    if (Test-WindowsAppRuntime @runtimeIdentityArgs -RequiredVersion ([Version] '2.5.1.0')) {
        throw 'Mixed Windows App Runtime component versions must not pass readiness.'
    }

    # Restore DDLM, then mark Main unhealthy to prove package presence alone is insufficient.
    $script:RegisteredPackages = @($script:RegisteredPackages | Where-Object {
        $_.Name -ne 'Microsoft.WinAppRuntime.DDLM.2.1.3.0-x6' -and
        $_.Name -ne 'MicrosoftCorporationII.WinAppRuntime.Main.2'
    }) + @(
        New-MockPackage -Name 'Microsoft.WinAppRuntime.DDLM.2.5.1.0-x6' -Version '2.5.1.0'
        New-MockPackage -Name 'MicrosoftCorporationII.WinAppRuntime.Main.2' -Version '2.5.1.0' -Status 'Modified'
    )
    if (Test-WindowsAppRuntime @runtimeIdentityArgs -RequiredVersion ([Version] '2.5.1.0')) {
        throw 'An unhealthy Main package must force Runtime repair.'
    }

    Write-Host 'Windows App Runtime current-user registration detection passed.'
} finally {
    Remove-Item -LiteralPath $testRoot -Recurse -Force -ErrorAction SilentlyContinue
}

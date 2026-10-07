[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('amd64', 'arm64')]
    [string] $Architecture,
    [Parameter(Mandatory = $true)]
    [string] $MetadataPath,
    [Parameter(Mandatory = $true)]
    [string] $PayloadArchivePath,
    [Parameter(Mandatory = $true)]
    [string] $ResultFile
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

function Write-AgentDockRuntimeResult {
    param(
        [string] $Status,
        [string] $Dependency,
        [string] $Message
    )
    $directory = Split-Path -Parent $ResultFile
    if (-not [string]::IsNullOrWhiteSpace($directory)) {
        New-Item -ItemType Directory -Path $directory -Force | Out-Null
    }
    $safeMessage = ([string] $Message).Replace([char] 13, ' ').Replace([char] 10, ' ')
    @(
        '[AgentDockRuntime]'
        "Status=$Status"
        "Dependency=$Dependency"
        "Message=$safeMessage"
    ) | Set-Content -LiteralPath $ResultFile -Encoding Unicode
}
$prerequisiteLibraryPath = Join-Path $PSScriptRoot 'runtime-prerequisites.ps1'
$bootstrapProbeLibraryPath = Join-Path $PSScriptRoot 'runtime-bootstrap-probe.ps1'
foreach ($libraryPath in @($prerequisiteLibraryPath, $bootstrapProbeLibraryPath)) {
    if (-not (Test-Path -LiteralPath $libraryPath -PathType Leaf)) {
        $message = "Runtime prerequisite helper was not found: $libraryPath"
        Write-AgentDockRuntimeResult -Status 'error' -Dependency 'metadata' -Message $message
        Write-Error $message
        exit 1
    }
}
. $prerequisiteLibraryPath
. $bootstrapProbeLibraryPath

function Test-WindowsDesktopRuntime {
    param(
        [Version] $MinimumVersion,
        [string] $TargetArchitecture
    )
    $dotnetArchitecture = if ($TargetArchitecture -eq 'arm64') { 'arm64' } else { 'x64' }
    $roots = @()

    # apphost prefers architecture-specific DOTNET_ROOT and then the registered architecture-specific
    # install location. Do not infer readiness from an arbitrary dotnet.exe on PATH, because an x64
    # emulation Runtime on ARM64 is not valid for the arm64 agentdock-tray apphost.
    $architectureRootName = 'DOTNET_ROOT_' + $dotnetArchitecture.ToUpperInvariant()
    $architectureRoot = [Environment]::GetEnvironmentVariable($architectureRootName)
    if (-not [string]::IsNullOrWhiteSpace($architectureRoot)) {
        $roots += $architectureRoot
    }
    if (-not [string]::IsNullOrWhiteSpace($env:DOTNET_ROOT) -and
        $roots -notcontains $env:DOTNET_ROOT) {
        $roots += $env:DOTNET_ROOT
    }
    # The .NET architecture is encoded in the registry key path. Different installer/host bitness
    # can expose the same architecture key through either registry view, so inspect both views but
    # accept only the requested architecture subkey.
    foreach ($registryView in @(
        [Microsoft.Win32.RegistryView]::Registry64,
        [Microsoft.Win32.RegistryView]::Registry32
    )) {
        $baseKey = [Microsoft.Win32.RegistryKey]::OpenBaseKey(
            [Microsoft.Win32.RegistryHive]::LocalMachine,
            $registryView)
        try {
            $architectureKey = $baseKey.OpenSubKey(
                "SOFTWARE\dotnet\Setup\InstalledVersions\$dotnetArchitecture")
            if ($null -ne $architectureKey) {
                try {
                    $installLocation = [string] $architectureKey.GetValue('InstallLocation')
                    if (-not [string]::IsNullOrWhiteSpace($installLocation) -and
                        $roots -notcontains $installLocation) {
                        $roots += $installLocation
                    }
                } finally {
                    $architectureKey.Dispose()
                }
            }
        } finally {
            $baseKey.Dispose()
        }
    }
    foreach ($root in $roots) {
        $sharedRoot = Join-Path $root 'shared\Microsoft.WindowsDesktop.App'
        if (-not (Test-Path -LiteralPath $sharedRoot -PathType Container)) {
            continue
        }
        foreach ($directory in @(Get-ChildItem -LiteralPath $sharedRoot -Directory -ErrorAction SilentlyContinue)) {
            $version = $null
            if ([Version]::TryParse($directory.Name, [ref] $version) -and
                ($version.Major -eq $MinimumVersion.Major) -and
                ($version -ge $MinimumVersion)) {
                return $true
            }
        }
    }
    return $false
}
$activeDependency = 'metadata'
try {
    if (-not (Test-Path -LiteralPath $MetadataPath -PathType Leaf)) {
        throw "Runtime dependency metadata was not found: $MetadataPath"
    }

    $metadata = Get-Content -LiteralPath $MetadataPath -Raw | ConvertFrom-Json
    if ([int] $metadata.schema_version -ne 1) {
        throw "Unsupported runtime dependency metadata schema: $($metadata.schema_version)"
    }

    $dotnetMinimum = Convert-ToVersion -Value ([string] $metadata.dotnet_windows_desktop.minimum_version) -Description '.NET minimum'
    $dotnetInstallVersion = [string] $metadata.dotnet_windows_desktop.install_version
    $activeDependency = '.NET Windows Desktop Runtime'
    if (-not (Test-WindowsDesktopRuntime -MinimumVersion $dotnetMinimum -TargetArchitecture $Architecture)) {
        $dotnetUri = Get-ArtifactUrl -Artifacts $metadata.dotnet_windows_desktop.artifacts -TargetArchitecture $Architecture -Dependency 'dotnet' -PinnedVersion $dotnetInstallVersion
        Install-Dependency -Uri $dotnetUri -Arguments @('/install', '/quiet', '/norestart') -Dependency $activeDependency -RequireElevation

        if (-not (Test-WindowsDesktopRuntime -MinimumVersion $dotnetMinimum -TargetArchitecture $Architecture)) {
            throw "$activeDependency $dotnetMinimum or newer was not detected after installation."
        }
    }

    $windowsAppMinimum = Convert-ToVersion -Value ([string] $metadata.windows_app_runtime.minimum_version) -Description 'Windows App Runtime minimum'
    $windowsAppRelease = [string] $metadata.windows_app_runtime.release
    $windowsAppPackageName = [string] $metadata.windows_app_runtime.package_name
    $windowsAppMainPackageName = [string] $metadata.windows_app_runtime.main_package_name
    $windowsAppSingletonPackageName = [string] $metadata.windows_app_runtime.singleton_package_name
    $windowsAppDdlmPackageNamePrefix = [string] $metadata.windows_app_runtime.ddlm_package_name_prefix
    foreach ($requiredName in @($windowsAppPackageName, $windowsAppMainPackageName, $windowsAppSingletonPackageName, $windowsAppDdlmPackageNamePrefix)) {
        if ([string]::IsNullOrWhiteSpace($requiredName)) {
            throw 'Windows App Runtime metadata is missing a required package identity.'
        }
    }

    $activeDependency = 'Windows App Runtime'
    $probeDirectory = Join-Path ([IO.Path]::GetTempPath()) ('agentdock-winappruntime-probe-' + [Guid]::NewGuid().ToString('N'))
    try {
        $probePayload = Get-WindowsAppRuntimeProbePayload `
            -PayloadArchivePath $PayloadArchivePath `
            -DestinationDirectory $probeDirectory
        $probe = Test-WindowsAppRuntimeBootstrap `
            -ProbeHostPath $probePayload.ProbeHostPath `
            -BootstrapDllPath $probePayload.BootstrapDllPath `
            -MinimumVersion $windowsAppMinimum
        if (-not $probe.Success) {
            # Runtime readiness is decided by Microsoft's bootstrap API above. If this user already
            # has a compatible Framework, repair that exact release so its Main/Singleton/DDLM set
            # stays coherent. Otherwise install AgentDock's pinned, release-tested baseline. Other
            # Windows users must not influence the current user's prerequisite decision.
            $repairVersion = Get-WindowsAppRuntimeFrameworkVersion `
                -PackageName $windowsAppPackageName `
                -MinimumVersion $windowsAppMinimum `
                -TargetArchitecture $Architecture
            if ($null -ne $repairVersion) {
                $windowsAppUri = Get-WindowsAppRuntimeInstallerUri -RuntimeVersion $repairVersion -TargetArchitecture $Architecture
            } else {
                $windowsAppUri = Get-ArtifactUrl `
                    -Artifacts $metadata.windows_app_runtime.artifacts `
                    -TargetArchitecture $Architecture `
                    -Dependency 'windows-app-runtime' `
                    -PinnedVersion $windowsAppRelease
            }
            Install-Dependency -Uri $windowsAppUri -Arguments @('--quiet') -Dependency $activeDependency

            $probeAfterInstall = Test-WindowsAppRuntimeBootstrap `
                -ProbeHostPath $probePayload.ProbeHostPath `
                -BootstrapDllPath $probePayload.BootstrapDllPath `
                -MinimumVersion $windowsAppMinimum
            if (-not $probeAfterInstall.Success) {
                $diagnosticVersion = Get-WindowsAppRuntimeFrameworkVersion `
                    -PackageName $windowsAppPackageName `
                    -MinimumVersion $windowsAppMinimum `
                    -TargetArchitecture $Architecture
                $coherentPackages = $false
                if ($null -ne $diagnosticVersion) {
                    $coherentPackages = Test-WindowsAppRuntime `
                        -PackageName $windowsAppPackageName `
                        -MainPackageName $windowsAppMainPackageName `
                        -SingletonPackageName $windowsAppSingletonPackageName `
                        -DdlmPackageNamePrefix $windowsAppDdlmPackageNamePrefix `
                        -RequiredVersion $diagnosticVersion `
                        -TargetArchitecture $Architecture
                }
                $probeCode = '0x{0:X8}' -f ([UInt32] $probeAfterInstall.HResult)
                throw "$activeDependency bootstrap still failed after Microsoft installer completed (HRESULT $probeCode; coherent-package-diagnostic=$coherentPackages)."
            }
        }
    } finally {
        Remove-Item -LiteralPath $probeDirectory -Recurse -Force -ErrorAction SilentlyContinue
    }

    Write-AgentDockRuntimeResult -Status 'ok' -Dependency '' -Message ''
    exit 0
} catch {
    Write-AgentDockRuntimeResult -Status 'error' -Dependency $activeDependency -Message $_.Exception.Message
    Write-Error ("AgentDock Runtime prerequisite failed for " + $activeDependency + ": " + $_.Exception.Message)
    exit 1
}

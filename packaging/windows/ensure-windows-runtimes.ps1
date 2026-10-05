[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('amd64', 'arm64')]
    [string] $Architecture,
    [Parameter(Mandatory = $true)]
    [string] $MetadataPath,
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
function Convert-ToVersion {
    param([string] $Value, [string] $Description)
    try {
        return [Version]::Parse($Value)
    } catch {
        throw "$Description has an invalid version: $Value"
    }
}
function Test-WindowsDesktopRuntime {
    param(
        [Version] $MinimumVersion,
        [string] $TargetArchitecture
    )
    $dotnetArchitecture = if ($TargetArchitecture -eq 'arm64') { 'arm64' } else { 'x64' }
    $roots = @()

    # apphost 会优先读取架构专属 DOTNET_ROOT，再读取全局注册的架构专属安装位置。
    # 不能用 PATH 中任意 dotnet.exe 判断，否则 ARM64 机器上的 x64 emulation runtime
    # 会被误判成可供 arm64 agentdock-tray 使用的 Runtime。
    $architectureRootName = 'DOTNET_ROOT_' + $dotnetArchitecture.ToUpperInvariant()
    $architectureRoot = [Environment]::GetEnvironmentVariable($architectureRootName)
    if (-not [string]::IsNullOrWhiteSpace($architectureRoot)) {
        $roots += $architectureRoot
    }
    if (-not [string]::IsNullOrWhiteSpace($env:DOTNET_ROOT) -and
        $roots -notcontains $env:DOTNET_ROOT) {
        $roots += $env:DOTNET_ROOT
    }
    # .NET 的架构名位于键路径本身；不同安装器/宿主位数可能把同一架构键写入
    # Registry64 或 Registry32 view。两边都查，但始终只接受目标架构子键。
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
function Test-WindowsAppRuntime {
    param(
        [string] $PackageName,
        [Version] $MinimumVersion,
        [string] $TargetArchitecture
    )
    $expectedArchitecture = if ($TargetArchitecture -eq 'arm64') { 'Arm64' } else { 'X64' }
    $packages = @(Get-AppxPackage -Name $PackageName -ErrorAction SilentlyContinue)
    foreach ($package in $packages) {
        $packageVersion = Convert-ToVersion -Value ([string] $package.Version) -Description 'Windows App Runtime'
        $packageArchitecture = [string] $package.Architecture
        if (($packageVersion -ge $MinimumVersion) -and
            ($packageArchitecture -ieq $expectedArchitecture)) {
            return $true
        }
    }
    return $false
}
function Assert-MicrosoftDownloadUri {
    param(
        [Uri] $Uri,
        [ValidateSet('dotnet', 'windows-app-runtime')]
        [string] $Dependency,
        [string] $PinnedVersion,
        [string] $TargetArchitecture
    )

    if ($Uri.Scheme -ne 'https') {
        throw "$Dependency download must use HTTPS: $Uri"
    }
    if ($Uri.AbsoluteUri -match '/latest(?:/|$)' -or
        $Uri.AbsoluteUri -match '[?&](?:version=)?latest(?:&|$)') {
        throw "$Dependency download must be pinned instead of using latest: $Uri"
    }

    if ($Dependency -eq 'dotnet') {
        if (($Uri.Host -ne 'builds.dotnet.microsoft.com') -or
            (-not $Uri.AbsolutePath.StartsWith("/dotnet/WindowsDesktop/$PinnedVersion/", [StringComparison]::Ordinal))) {
            throw ".NET Runtime URL is not the pinned Microsoft Windows Desktop Runtime source: $Uri"
        }
        return
    }

    $releaseVersion = Convert-ToVersion -Value $PinnedVersion -Description 'Windows App Runtime release'
    $assetArchitecture = if ($TargetArchitecture -eq 'arm64') { 'arm64' } else { 'x64' }
    $expectedPath = "/windowsappsdk/$($releaseVersion.Major).$($releaseVersion.Minor)/$PinnedVersion/windowsappruntimeinstall-$assetArchitecture.exe"
    if (($Uri.Host -ne 'aka.ms') -or ($Uri.AbsolutePath -cne $expectedPath) -or $Uri.Query -or $Uri.Fragment) {
        throw "Windows App Runtime URL is not the pinned Microsoft download source: $Uri"
    }
}

function Get-ArtifactUrl {
    param(
        [object] $Artifacts,
        [string] $TargetArchitecture,
        [string] $Dependency,
        [string] $PinnedVersion
    )

    $property = $Artifacts.PSObject.Properties[$TargetArchitecture]
    if ($null -eq $property -or $null -eq $property.Value) {
        throw "$Dependency metadata is missing the $TargetArchitecture artifact."
    }

    $url = [string] $property.Value.url
    if ([string]::IsNullOrWhiteSpace($url)) {
        throw "$Dependency metadata has an empty $TargetArchitecture URL."
    }

    $uri = [Uri] $url
    Assert-MicrosoftDownloadUri -Uri $uri -Dependency $Dependency -PinnedVersion $PinnedVersion -TargetArchitecture $TargetArchitecture
    return $uri
}

function Get-MicrosoftInstaller {
    param(
        [Uri] $Uri,
        [string] $Destination
    )

    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    $lastError = $null
    for ($attempt = 1; $attempt -le 3; $attempt++) {
        Remove-Item -LiteralPath $Destination -Force -ErrorAction SilentlyContinue
        try {
            Invoke-WebRequest -UseBasicParsing -Uri $Uri.AbsoluteUri -OutFile $Destination
            if (-not (Test-Path -LiteralPath $Destination -PathType Leaf) -or
                (Get-Item -LiteralPath $Destination).Length -le 0) {
                throw 'Downloaded installer is empty.'
            }
            return
        } catch {
            $lastError = $_
            if ($attempt -lt 3) {
                Start-Sleep -Seconds (2 * $attempt)
            }
        }
    }

    throw "Could not download Microsoft Runtime installer from $Uri after 3 attempts: $($lastError.Exception.Message)"
}

function Assert-MicrosoftAuthenticode {
    param([string] $Path)

    $signature = Get-AuthenticodeSignature -LiteralPath $Path
    if ($signature.Status -ne [System.Management.Automation.SignatureStatus]::Valid -or
        $null -eq $signature.SignerCertificate) {
        throw "Microsoft Runtime installer Authenticode signature is not valid: $($signature.Status) $($signature.StatusMessage)"
    }

    $subject = [string] $signature.SignerCertificate.Subject
    if ($subject -notmatch '(^|,\s*)O=Microsoft Corporation(,|$)' -and
        $subject -notmatch '(^|,\s*)CN=Microsoft Corporation(,|$)') {
        throw "Microsoft Runtime installer signer is not Microsoft Corporation: $subject"
    }
}

function Invoke-MicrosoftInstaller {
    param(
        [string] $Path,
        [string[]] $Arguments,
        [string] $Dependency
    )

    # Setup 本身保持按用户安装；只有系统共享 Runtime 缺失时，才为微软安装器单独请求 UAC。
    $process = Start-Process -FilePath $Path -ArgumentList $Arguments -Verb RunAs -Wait -PassThru
    if ($process.ExitCode -notin @(0, 1641, 3010)) {
        throw "$Dependency installer exited with code $($process.ExitCode)."
    }
}

function Install-Dependency {
    param(
        [Uri] $Uri,
        [string[]] $Arguments,
        [string] $Dependency
    )

    $installerPath = Join-Path ([IO.Path]::GetTempPath()) ('agentdock-runtime-' + [Guid]::NewGuid().ToString('N') + '.exe')
    try {
        Get-MicrosoftInstaller -Uri $Uri -Destination $installerPath
        Assert-MicrosoftAuthenticode -Path $installerPath
        Invoke-MicrosoftInstaller -Path $installerPath -Arguments $Arguments -Dependency $Dependency
    } finally {
        Remove-Item -LiteralPath $installerPath -Force -ErrorAction SilentlyContinue
    }
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
        Install-Dependency -Uri $dotnetUri -Arguments @('/install', '/quiet', '/norestart') -Dependency $activeDependency

        if (-not (Test-WindowsDesktopRuntime -MinimumVersion $dotnetMinimum -TargetArchitecture $Architecture)) {
            throw "$activeDependency $dotnetMinimum or newer was not detected after installation."
        }
    }

    $windowsAppMinimum = Convert-ToVersion -Value ([string] $metadata.windows_app_runtime.minimum_version) -Description 'Windows App Runtime minimum'
    $windowsAppRelease = [string] $metadata.windows_app_runtime.release
    $windowsAppPackageName = [string] $metadata.windows_app_runtime.package_name
    $activeDependency = 'Windows App Runtime'
    if (-not (Test-WindowsAppRuntime -PackageName $windowsAppPackageName -MinimumVersion $windowsAppMinimum -TargetArchitecture $Architecture)) {
        $windowsAppUri = Get-ArtifactUrl -Artifacts $metadata.windows_app_runtime.artifacts -TargetArchitecture $Architecture -Dependency 'windows-app-runtime' -PinnedVersion $windowsAppRelease
        Install-Dependency -Uri $windowsAppUri -Arguments @('--quiet') -Dependency $activeDependency

        if (-not (Test-WindowsAppRuntime -PackageName $windowsAppPackageName -MinimumVersion $windowsAppMinimum -TargetArchitecture $Architecture)) {
            throw "$activeDependency $windowsAppMinimum or newer was not detected after installation."
        }
    }

    Write-AgentDockRuntimeResult -Status 'ok' -Dependency '' -Message ''
    exit 0
} catch {
    Write-AgentDockRuntimeResult -Status 'error' -Dependency $activeDependency -Message $_.Exception.Message
    Write-Error ("AgentDock Runtime prerequisite failed for " + $activeDependency + ": " + $_.Exception.Message)
    exit 1
}

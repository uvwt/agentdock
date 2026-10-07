# Shared Windows Runtime prerequisite helpers, dot-sourced by ensure-windows-runtimes.ps1.
# This file only defines functions. Loading it must not download files or mutate package state.

function Convert-ToVersion {
    param([string] $Value, [string] $Description)
    try {
        return [Version]::Parse($Value)
    } catch {
        throw "$Description has an invalid version: $Value"
    }
}

function Test-WindowsAppRuntimePackageReady {
    param(
        [object] $Package,
        [Version] $RequiredVersion,
        [string] $ExpectedArchitecture,
        [string] $ExpectedName
    )
    if ($null -eq $Package -or
        ([string] $Package.Name -cne $ExpectedName) -or
        ([string] $Package.Architecture -ine $ExpectedArchitecture) -or
        ([string] $Package.Status -ine 'Ok') -or
        [string]::IsNullOrWhiteSpace([string] $Package.InstallLocation) -or
        ([string] $Package.PackageFullName -notmatch '__8wekyb3d8bbwe$')) {
        return $false
    }

    $packageVersion = Convert-ToVersion -Value ([string] $Package.Version) -Description 'Windows App Runtime package'
    return $packageVersion -eq $RequiredVersion
}
function Get-WindowsAppRuntimeFrameworkVersion {
    param(
        [string] $PackageName,
        [Version] $MinimumVersion,
        [string] $TargetArchitecture
    )
    $expectedArchitecture = if ($TargetArchitecture -eq 'arm64') { 'Arm64' } else { 'X64' }
    $packages = @(Get-AppxPackage -Name $PackageName -ErrorAction SilentlyContinue)

    $versions = @()
    foreach ($package in $packages) {
        if (([string] $package.Name -cne $PackageName) -or
            ([string] $package.Architecture -ine $expectedArchitecture) -or
            ([string] $package.Status -ine 'Ok') -or
            ([string] $package.PackageFullName -notmatch '__8wekyb3d8bbwe$')) {
            continue
        }
        $version = Convert-ToVersion -Value ([string] $package.Version) -Description 'Windows App Runtime framework'
        if (($version.Major -eq $MinimumVersion.Major) -and ($version -ge $MinimumVersion)) {
            $versions += $version
        }
    }
    if ($versions.Count -eq 0) {
        return $null
    }
    return @($versions | Sort-Object -Descending)[0]
}
function Test-WindowsAppRuntime {
    param(
        [string] $PackageName,
        [string] $MainPackageName,
        [string] $SingletonPackageName,
        [string] $DdlmPackageNamePrefix,
        [Version] $RequiredVersion,
        [string] $TargetArchitecture
    )
    $expectedArchitecture = if ($TargetArchitecture -eq 'arm64') { 'Arm64' } else { 'X64' }
    $ddlmArchitecture = if ($TargetArchitecture -eq 'arm64') { 'a6' } else { 'x6' }
    $singletonVersion = [Version]::new(
        8000 + $RequiredVersion.Major,
        $RequiredVersion.Minor,
        $RequiredVersion.Build,
        $RequiredVersion.Revision)
    $ddlmPackageName = $DdlmPackageNamePrefix +
        "$($RequiredVersion.Minor).$($RequiredVersion.Build).$($RequiredVersion.Revision)-$ddlmArchitecture"

    # Deliberately omit -AllUsers here. Unpackaged WinUI bootstraps from the calling user package
    # graph; SYSTEM:Staged only means machine staging and does not make the Runtime usable.
    $frameworkPackages = @(Get-AppxPackage -Name $PackageName -ErrorAction SilentlyContinue)
    $mainPackages = @(Get-AppxPackage -Name $MainPackageName -ErrorAction SilentlyContinue)
    $singletonPackages = @(Get-AppxPackage -Name $SingletonPackageName -ErrorAction SilentlyContinue)
    $ddlmPackages = @(Get-AppxPackage -Name $ddlmPackageName -ErrorAction SilentlyContinue)

    $frameworkReady = @($frameworkPackages | Where-Object {
        Test-WindowsAppRuntimePackageReady -Package $_ -RequiredVersion $RequiredVersion -ExpectedArchitecture $expectedArchitecture -ExpectedName $PackageName
    }).Count -gt 0
    $mainReady = @($mainPackages | Where-Object {
        Test-WindowsAppRuntimePackageReady -Package $_ -RequiredVersion $RequiredVersion -ExpectedArchitecture $expectedArchitecture -ExpectedName $MainPackageName
    }).Count -gt 0
    $singletonReady = @($singletonPackages | Where-Object {
        Test-WindowsAppRuntimePackageReady -Package $_ -RequiredVersion $singletonVersion -ExpectedArchitecture $expectedArchitecture -ExpectedName $SingletonPackageName
    }).Count -gt 0
    $ddlmReady = @($ddlmPackages | Where-Object {
        Test-WindowsAppRuntimePackageReady -Package $_ -RequiredVersion $RequiredVersion -ExpectedArchitecture $expectedArchitecture -ExpectedName $ddlmPackageName
    }).Count -gt 0

    return $frameworkReady -and $mainReady -and $singletonReady -and $ddlmReady
}
function Get-WindowsAppRuntimeInstallerUri {
    param(
        [Version] $RuntimeVersion,
        [string] $TargetArchitecture
    )
    $release = "$($RuntimeVersion.Major).$($RuntimeVersion.Minor).$($RuntimeVersion.Build)"
    $assetArchitecture = if ($TargetArchitecture -eq 'arm64') { 'arm64' } else { 'x64' }
    $uri = [Uri] "https://aka.ms/windowsappsdk/$($RuntimeVersion.Major).$($RuntimeVersion.Minor)/$release/windowsappruntimeinstall-$assetArchitecture.exe"
    Assert-MicrosoftDownloadUri -Uri $uri -Dependency 'windows-app-runtime' -PinnedVersion $release -TargetArchitecture $TargetArchitecture
    return $uri
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
        [string] $Dependency,
        [switch] $RequireElevation
    )

    # The official Windows App Runtime installer can register the complete MSIX set for the current
    # user. Forcing RunAs can target the wrong account when alternate admin credentials are used.
    # Only the machine-level .NET Runtime requests elevation.
    if ($RequireElevation) {
        $process = Start-Process -FilePath $Path -ArgumentList $Arguments -Verb RunAs -Wait -PassThru
    } else {
        $process = Start-Process -FilePath $Path -ArgumentList $Arguments -Wait -PassThru
    }
    if ($process.ExitCode -notin @(0, 1641, 3010)) {
        throw "$Dependency installer exited with code $($process.ExitCode)."
    }
}

function Install-Dependency {
    param(
        [Uri] $Uri,
        [string[]] $Arguments,
        [string] $Dependency,
        [switch] $RequireElevation
    )

    $installerPath = Join-Path ([IO.Path]::GetTempPath()) ('agentdock-runtime-' + [Guid]::NewGuid().ToString('N') + '.exe')
    try {
        Get-MicrosoftInstaller -Uri $Uri -Destination $installerPath
        Assert-MicrosoftAuthenticode -Path $installerPath
        Invoke-MicrosoftInstaller -Path $installerPath -Arguments $Arguments -Dependency $Dependency -RequireElevation:$RequireElevation
    } finally {
        Remove-Item -LiteralPath $installerPath -Force -ErrorAction SilentlyContinue
    }
}

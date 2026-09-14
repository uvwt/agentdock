[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string] $InstallerPath,
    [Parameter(Mandatory = $true)]
    [string] $UninstallerPath,
    [Parameter(Mandatory = $true)]
    [string] $TargetAgentDockArchive,
    [Parameter(Mandatory = $true)]
    [string] $TargetAgentDockChecksumFile,
    [Parameter(Mandatory = $true)]
    [string] $SourceCoreBinary,
    [Parameter(Mandatory = $true)]
    [string] $TrialCoreBinary,
    [Parameter(Mandatory = $true)]
    [string] $FakeCloudflaredBinary
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

function Get-FreeTcpPort {
    $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
    $listener.Start()
    try {
        return ([Net.IPEndPoint] $listener.LocalEndpoint).Port
    } finally {
        $listener.Stop()
    }
}

function Get-AgentDockVersion {
    param([string] $BinaryPath)

    $metadata = (& $BinaryPath version --json | ConvertFrom-Json)
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace([string] $metadata.version)) {
        throw "Could not read AgentDock version from: $BinaryPath"
    }
    return 'v' + ([string] $metadata.version).Trim().TrimStart('v')
}

function New-AgentDockArchiveVariant {
    param(
        [string] $BaseArchive,
        [string] $CoreBinary,
        [string] $OutputRoot,
        [string] $Name
    )

    $payloadRoot = Join-Path $OutputRoot "$Name-payload"
    $archivePath = Join-Path $OutputRoot "$Name.zip"
    $checksumPath = "$archivePath.sha256"
    Expand-Archive -LiteralPath $BaseArchive -DestinationPath $payloadRoot -Force
    $payloadCore = Join-Path $payloadRoot 'agentdock.exe'
    if (-not (Test-Path -LiteralPath $payloadCore -PathType Leaf)) {
        throw "Base AgentDock archive does not contain agentdock.exe: $BaseArchive"
    }
    Copy-Item -LiteralPath $CoreBinary -Destination $payloadCore -Force
    Compress-Archive -Path (Join-Path $payloadRoot '*') -DestinationPath $archivePath -Force
    $hash = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    [IO.File]::WriteAllText(
        $checksumPath,
        "$hash  $([IO.Path]::GetFileName($archivePath))`n",
        [Text.UTF8Encoding]::new($false)
    )
    return [pscustomobject]@{
        Archive = $archivePath
        Checksum = $checksumPath
    }
}

function Get-ProcessIdsByPath {
    param([string] $ProcessName, [string] $BinaryPath)

    if (-not (Test-Path -LiteralPath $BinaryPath -PathType Leaf)) {
        return @()
    }
    $normalizedPath = [IO.Path]::GetFullPath($BinaryPath)
    $ids = @(Get-CimInstance Win32_Process -Filter "Name = '$ProcessName.exe'" -ErrorAction SilentlyContinue |
        Where-Object {
            $_.ExecutablePath -and
            [string]::Equals(
                [IO.Path]::GetFullPath($_.ExecutablePath),
                $normalizedPath,
                [StringComparison]::OrdinalIgnoreCase
            )
        } |
        Select-Object -ExpandProperty ProcessId)
    if ($ids.Count -eq 0) {
        $ids = @(Get-Process -Name $ProcessName -ErrorAction SilentlyContinue |
            Where-Object {
                $_.Path -and
                [string]::Equals(
                    [IO.Path]::GetFullPath($_.Path),
                    $normalizedPath,
                    [StringComparison]::OrdinalIgnoreCase
                )
            } |
            Select-Object -ExpandProperty Id)
    }
    return @($ids | Sort-Object -Unique)
}

function Stop-IsolatedRuntimeProcesses {
    param([string] $RuntimeRoot)

    $prefix = [IO.Path]::GetFullPath($RuntimeRoot).TrimEnd('\') + '\'
    Get-CimInstance Win32_Process -ErrorAction SilentlyContinue | Where-Object {
        if (-not $_.ExecutablePath) { return $false }
        $name = [string] $_.Name
        if (@('agentdock.exe', 'agentdock-core.exe', 'agentdock-tray.exe', 'cloudflared.exe') -notcontains $name.ToLowerInvariant()) {
            return $false
        }
        try {
            return [IO.Path]::GetFullPath($_.ExecutablePath).StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)
        } catch {
            return $false
        }
    } | ForEach-Object {
        Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
    }
}

function Wait-Healthy {
    param([string] $Url, [int] $TimeoutSeconds = 30)

    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    do {
        Start-Sleep -Milliseconds 500
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri $Url -TimeoutSec 2
            if ($response.StatusCode -eq 200) {
                return
            }
        } catch {
        }
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "AgentDock did not become healthy: $Url"
}

function Assert-TextFile {
    param([string] $Path, [string] $Expected)

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "Expected runtime file is missing: $Path"
    }
    $actual = [IO.File]::ReadAllText($Path).Trim()
    if (-not [string]::Equals($actual, $Expected, [StringComparison]::Ordinal)) {
        throw "Unexpected runtime value in $Path; expected '$Expected', got '$actual'."
    }
}

function Assert-NoTunnelTokenInProcessArguments {
    param([string[]] $Tokens)

    foreach ($process in @(Get-CimInstance Win32_Process -ErrorAction SilentlyContinue)) {
        $commandLine = [string] $process.CommandLine
        if ([string]::IsNullOrWhiteSpace($commandLine)) {
            continue
        }
        foreach ($token in $Tokens) {
            if (-not [string]::IsNullOrWhiteSpace($token) -and $commandLine.Contains($token)) {
                throw "Tunnel Token leaked into process arguments for PID $($process.ProcessId)."
            }
        }
    }
}

function Assert-NamedRuntime {
    param(
        [string] $ExpectedVersion,
        [string] $ExpectedTokenHash,
        [string] $ExpectedAuthHash,
        [string] $ExpectedOAuthPasswordHash,
        [string] $ExpectedOAuthSecretHash
    )

    Wait-Healthy -Url $healthUrl
    Assert-TextFile -Path $serverUrlPath -Expected $fixedUrl
    Assert-TextFile -Path $namedServerUrlPath -Expected $fixedUrl
    Assert-TextFile -Path $tunnelModePath -Expected 'named'

    if (Test-Path -LiteralPath $quickUrlPath -PathType Leaf) {
        throw 'Named Tunnel runtime unexpectedly contains a Quick Tunnel ready URL.'
    }
    if (-not (Test-Path -LiteralPath $namedTokenEnvMarker -PathType Leaf)) {
        throw 'Fake cloudflared did not observe the Named Tunnel Token through its environment.'
    }

    $manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
    if ($manifest.tunnel_mode -ne 'named' -or $manifest.public_url -ne $fixedUrl) {
        throw "Named Tunnel manifest was not preserved: $($manifest | ConvertTo-Json -Compress)"
    }
    $active = Get-Content -LiteralPath $activeVersionPath -Raw | ConvertFrom-Json
    if ($active.state -ne 'committed' -or $active.active_version -ne $ExpectedVersion) {
        throw "Unexpected active generation: $($active | ConvertTo-Json -Compress)"
    }
    $activeCore = Join-Path $runtimeDir "versions\$ExpectedVersion\agentdock-core.exe"
    if (-not (Test-Path -LiteralPath $activeCore -PathType Leaf)) {
        throw "Active Named Tunnel generation is missing: $activeCore"
    }
    if ((Get-AgentDockVersion -BinaryPath $activeCore) -ne $ExpectedVersion) {
        throw "Active generation binary does not report $ExpectedVersion."
    }

    if ((Get-FileHash -LiteralPath $tunnelTokenPath -Algorithm SHA256).Hash -ne $ExpectedTokenHash) {
        throw 'Named Tunnel DPAPI Token changed unexpectedly.'
    }
    if ((Get-FileHash -LiteralPath $authPath -Algorithm SHA256).Hash -ne $ExpectedAuthHash -or
        (Get-FileHash -LiteralPath $oauthPasswordPath -Algorithm SHA256).Hash -ne $ExpectedOAuthPasswordHash -or
        (Get-FileHash -LiteralPath $oauthSecretPath -Algorithm SHA256).Hash -ne $ExpectedOAuthSecretHash) {
        throw 'Named Tunnel install/update rotated existing AgentDock credentials.'
    }

    $cloudflaredIds = @(Get-ProcessIdsByPath -ProcessName 'cloudflared' -BinaryPath $cloudflaredBinary)
    if ($cloudflaredIds.Count -ne 1) {
        throw "Expected exactly one Named Tunnel cloudflared process; got $($cloudflaredIds.Count)."
    }
    $tunnelStartupCommand = Get-ItemPropertyValue -LiteralPath $runKey -Name $cloudflaredStartupName
    if (-not $tunnelStartupCommand.Contains($trayBinary) -or -not $tunnelStartupCommand.Contains('--start-tunnel')) {
        throw "Named Tunnel startup entry is not native: $tunnelStartupCommand"
    }
    if ($tunnelStartupCommand.Contains('powershell.exe')) {
        throw 'Named Tunnel startup entry must not invoke PowerShell.'
    }
    Assert-NoTunnelTokenInProcessArguments -Tokens @($stableTunnelToken, $invalidTunnelToken)
}

function Invoke-Installer {
    param(
        [string] $Archive,
        [string] $Checksum,
        [string] $TunnelTokenFile = ''
    )

    $arguments = @{
        Version = 'latest'
        OfflineArchive = $Archive
        OfflineChecksumFile = $Checksum
        OfflineCloudflaredBinary = $FakeCloudflaredBinary
        InstallDir = $installDir
        RegisterStartup = $true
        CorePrivilegeMode = 'standard'
        Port = $port
        StartupValueName = $startupName
        CloudflaredStartupValueName = $cloudflaredStartupName
        TrayStartupValueName = $trayStartupName
    }
    if (-not [string]::IsNullOrWhiteSpace($TunnelTokenFile)) {
        $arguments['TunnelTokenFile'] = $TunnelTokenFile
    }
    & $InstallerPath @arguments
}

$testId = [Guid]::NewGuid().ToString('N')
$root = Join-Path $env:RUNNER_TEMP "agentdock-named-lifecycle-$testId"
$variantRoot = Join-Path $root 'payloads'
$installDir = Join-Path $root 'AgentDock\bin'
$runtimeDir = Split-Path -Parent $installDir
$agentDockBinary = Join-Path $installDir 'agentdock.exe'
$trayBinary = Join-Path $installDir 'agentdock-tray.exe'
$cloudflaredBinary = Join-Path $installDir 'cloudflared.exe'
$manifestPath = Join-Path $runtimeDir 'runtime.json'
$activeVersionPath = Join-Path $runtimeDir 'active-version.json'
$serverUrlPath = Join-Path $runtimeDir 'server-url.txt'
$namedServerUrlPath = Join-Path $runtimeDir 'named-server-url.txt'
$tunnelModePath = Join-Path $runtimeDir 'cloudflared-mode.txt'
$quickUrlPath = Join-Path $runtimeDir 'quick-tunnel-url.txt'
$tunnelTokenPath = Join-Path $runtimeDir 'cloudflared-token.dpapi'
$authPath = Join-Path $runtimeDir 'auth-token.dpapi'
$oauthPasswordPath = Join-Path $runtimeDir 'oauth-password.dpapi'
$oauthSecretPath = Join-Path $runtimeDir 'oauth-token-secret.dpapi'
$namedTokenEnvMarker = Join-Path $installDir 'named-token-env-ok.txt'
$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$startupName = "AgentDockNamedLifecycle-$testId"
$cloudflaredStartupName = "AgentDockCloudflaredNamedLifecycle-$testId"
$trayStartupName = "AgentDockTrayNamedLifecycle-$testId"
$port = Get-FreeTcpPort
$healthUrl = "http://127.0.0.1:$port/healthz"
$fixedUrl = 'https://named-agentdock-e2e.example.com'
$stableTunnelToken = 'agentdock-test-stable-named-token'
$invalidTunnelToken = 'agentdock-test-invalid-named-token'
$stableTokenFile = Join-Path $root 'stable-tunnel-token.txt'
$invalidTokenFile = Join-Path $root 'invalid-tunnel-token.txt'
$oldUserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$oldHome = $env:AGENTDOCK_HOME
$oldDefaultDir = $env:AGENTDOCK_DEFAULT_DIR

try {
    New-Item -ItemType Directory -Path $variantRoot -Force | Out-Null
    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
    [IO.File]::WriteAllText($stableTokenFile, $stableTunnelToken, [Text.UTF8Encoding]::new($false))
    [IO.File]::WriteAllText($invalidTokenFile, $invalidTunnelToken, [Text.UTF8Encoding]::new($false))
    $env:AGENTDOCK_HOME = Join-Path $root '.agentdock'
    $env:AGENTDOCK_DEFAULT_DIR = Join-Path $root 'workspace'

    $targetProbeRoot = Join-Path $variantRoot 'target-probe'
    Expand-Archive -LiteralPath $TargetAgentDockArchive -DestinationPath $targetProbeRoot -Force
    $targetCore = Join-Path $targetProbeRoot 'agentdock.exe'
    $sourceVersion = Get-AgentDockVersion -BinaryPath $SourceCoreBinary
    $targetVersion = Get-AgentDockVersion -BinaryPath $targetCore
    $trialVersion = Get-AgentDockVersion -BinaryPath $TrialCoreBinary
    if ($sourceVersion -eq $targetVersion -or $targetVersion -eq $trialVersion -or $sourceVersion -eq $trialVersion) {
        throw "Named lifecycle requires three distinct generations: $sourceVersion / $targetVersion / $trialVersion"
    }

    $sourcePayload = New-AgentDockArchiveVariant `
        -BaseArchive $TargetAgentDockArchive `
        -CoreBinary $SourceCoreBinary `
        -OutputRoot $variantRoot `
        -Name 'source'
    $trialPayload = New-AgentDockArchiveVariant `
        -BaseArchive $TargetAgentDockArchive `
        -CoreBinary $TrialCoreBinary `
        -OutputRoot $variantRoot `
        -Name 'trial'

    # Fresh install uses a token file so even the installer process never receives the secret in argv.
    & $InstallerPath `
        -Version latest `
        -OfflineArchive $sourcePayload.Archive `
        -OfflineChecksumFile $sourcePayload.Checksum `
        -OfflineCloudflaredBinary $FakeCloudflaredBinary `
        -InstallDir $installDir `
        -RegisterStartup `
        -TunnelMode named `
        -ServerUrl $fixedUrl `
        -TunnelTokenFile $stableTokenFile `
        -CorePrivilegeMode standard `
        -Port $port `
        -AuthToken 'stable-named-bearer-token' `
        -OAuthPassword 'stable-named-oauth-password' `
        -OAuthTokenSecret 'stable-named-oauth-secret-0123456789abcdef' `
        -StartupValueName $startupName `
        -CloudflaredStartupValueName $cloudflaredStartupName `
        -TrayStartupValueName $trayStartupName

    foreach ($path in @($tunnelTokenPath, $authPath, $oauthPasswordPath, $oauthSecretPath)) {
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
            throw "Named Tunnel install did not create protected credential: $path"
        }
    }
    $tokenHash = (Get-FileHash -LiteralPath $tunnelTokenPath -Algorithm SHA256).Hash
    $authHash = (Get-FileHash -LiteralPath $authPath -Algorithm SHA256).Hash
    $oauthPasswordHash = (Get-FileHash -LiteralPath $oauthPasswordPath -Algorithm SHA256).Hash
    $oauthSecretHash = (Get-FileHash -LiteralPath $oauthSecretPath -Algorithm SHA256).Hash
    Assert-NamedRuntime `
        -ExpectedVersion $sourceVersion `
        -ExpectedTokenHash $tokenHash `
        -ExpectedAuthHash $authHash `
        -ExpectedOAuthPasswordHash $oauthPasswordHash `
        -ExpectedOAuthSecretHash $oauthSecretHash

    # Same-version repair omits mode, origin and Token. All three must be hydrated from committed runtime state.
    Invoke-Installer -Archive $sourcePayload.Archive -Checksum $sourcePayload.Checksum
    Assert-NamedRuntime `
        -ExpectedVersion $sourceVersion `
        -ExpectedTokenHash $tokenHash `
        -ExpectedAuthHash $authHash `
        -ExpectedOAuthPasswordHash $oauthPasswordHash `
        -ExpectedOAuthSecretHash $oauthSecretHash

    # Upgrade to the real current payload, again without repeating Named configuration.
    Invoke-Installer -Archive $TargetAgentDockArchive -Checksum $TargetAgentDockChecksumFile
    Assert-NamedRuntime `
        -ExpectedVersion $targetVersion `
        -ExpectedTokenHash $tokenHash `
        -ExpectedAuthHash $authHash `
        -ExpectedOAuthPasswordHash $oauthPasswordHash `
        -ExpectedOAuthSecretHash $oauthSecretHash

    # A replacement Token that cloudflared rejects must fail the new generation trial. The installer
    # then has to restore the known-good generation plus the exact previous DPAPI/runtime bytes and
    # prove the restored Named Tunnel is ready before reporting rollback complete.
    $failureArgs = @(
        '-NoLogo', '-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass',
        '-File', $InstallerPath,
        '-Version', 'latest',
        '-OfflineArchive', $trialPayload.Archive,
        '-OfflineChecksumFile', $trialPayload.Checksum,
        '-OfflineCloudflaredBinary', $FakeCloudflaredBinary,
        '-InstallDir', $installDir,
        '-RegisterStartup',
        '-TunnelMode', 'named',
        '-ServerUrl', $fixedUrl,
        '-TunnelTokenFile', $invalidTokenFile,
        '-CorePrivilegeMode', 'standard',
        '-Port', "$port",
        '-StartupValueName', $startupName,
        '-CloudflaredStartupValueName', $cloudflaredStartupName,
        '-TrayStartupValueName', $trayStartupName
    )
    $previousErrorActionPreference = $ErrorActionPreference
    try {
        # Windows PowerShell 5.1 turns child-process stderr into NativeCommandError records.
        # This child is expected to fail, so collect the diagnostics without converting them
        # into a terminating error before we can inspect the real process exit code.
        $ErrorActionPreference = 'Continue'
        $failureOutput = @(& powershell.exe @failureArgs 2>&1)
        $failureExitCode = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $previousErrorActionPreference
        # The failed child is the fixture. Do not leak its native exit code into the parent CI step.
        $global:LASTEXITCODE = 0
    }
    if ($failureExitCode -eq 0) {
        throw 'Named Tunnel invalid-Token upgrade unexpectedly succeeded.'
    }
    $failureText = $failureOutput | Out-String
    if ($failureText.Contains($stableTunnelToken) -or $failureText.Contains($invalidTunnelToken)) {
        throw 'Named Tunnel failure diagnostics leaked a Tunnel Token.'
    }

    Assert-NamedRuntime `
        -ExpectedVersion $targetVersion `
        -ExpectedTokenHash $tokenHash `
        -ExpectedAuthHash $authHash `
        -ExpectedOAuthPasswordHash $oauthPasswordHash `
        -ExpectedOAuthSecretHash $oauthSecretHash
    if (Test-Path -LiteralPath (Join-Path $runtimeDir "versions\$trialVersion")) {
        throw "Failed Named Tunnel trial generation was not removed: $trialVersion"
    }

    Write-Host "Windows Named Tunnel lifecycle passed: $sourceVersion repair -> $targetVersion upgrade -> $trialVersion rollback"

    & $UninstallerPath `
        -InstallDir $installDir `
        -StartupValueName $startupName `
        -CloudflaredStartupValueName $cloudflaredStartupName `
        -TrayStartupValueName $trayStartupName
    if (Test-Path -LiteralPath $installDir) {
        throw 'Named Tunnel lifecycle uninstaller did not remove the install directory.'
    }
} catch {
    foreach ($path in @($manifestPath, $activeVersionPath, $serverUrlPath, $namedServerUrlPath, $tunnelModePath)) {
        Write-Host "----- diagnostic: $path -----"
        if (Test-Path -LiteralPath $path -PathType Leaf) {
            Get-Content -LiteralPath $path -Raw | Write-Host
        } else {
            Write-Host '<missing>'
        }
    }
    throw
} finally {
    Stop-IsolatedRuntimeProcesses -RuntimeRoot $runtimeDir
    foreach ($name in @($startupName, $cloudflaredStartupName, $trayStartupName)) {
        Remove-ItemProperty -LiteralPath $runKey -Name $name -ErrorAction SilentlyContinue
    }
    [Environment]::SetEnvironmentVariable('Path', $oldUserPath, 'User')
    $env:AGENTDOCK_HOME = $oldHome
    $env:AGENTDOCK_DEFAULT_DIR = $oldDefaultDir
    Remove-Item -LiteralPath $root -Recurse -Force -ErrorAction SilentlyContinue
}

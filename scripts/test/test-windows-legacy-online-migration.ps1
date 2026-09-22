param(
    [Parameter(Mandatory = $true)]
    [string] $ArchivePath,
    [Parameter(Mandatory = $true)]
    [string] $ChecksumPath,
    [string] $Version = '',
    [switch] $InjectTrayReplaceFailure
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$archive = (Resolve-Path -LiteralPath $ArchivePath).Path
$checksum = (Resolve-Path -LiteralPath $ChecksumPath).Path
$tempBase = if (-not [string]::IsNullOrWhiteSpace($env:RUNNER_TEMP)) { $env:RUNNER_TEMP } else { [IO.Path]::GetTempPath() }
$testRoot = Join-Path $tempBase ('agentdock-legacy-migration-e2e-' + [Guid]::NewGuid().ToString('N'))
$extractRoot = Join-Path $testRoot 'release'
$runtimeRoot = Join-Path $testRoot 'runtime'
$binRoot = Join-Path $runtimeRoot 'bin'
$homeRoot = Join-Path $testRoot 'home'
$defaultRoot = Join-Path $testRoot 'workspace'
$utf8NoBom = [Text.UTF8Encoding]::new($false)

function Wait-NoDesktopRepairProcess {
    param(
        [Parameter(Mandatory = $true)]
        [string] $ExecutablePath,
        [int] $TimeoutSeconds = 30
    )

    $resolvedExecutable = (Resolve-Path -LiteralPath $ExecutablePath).Path
    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    do {
        $repair = Get-CimInstance Win32_Process -ErrorAction Stop | Where-Object {
            $_.ExecutablePath -and
            [string]::Equals($_.ExecutablePath, $resolvedExecutable, [StringComparison]::OrdinalIgnoreCase) -and
            $_.CommandLine -and
            $_.CommandLine.Contains('__repair-desktop-runtime')
        }
        if ($null -eq $repair) {
            return
        }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    throw 'automatic desktop repair did not exit before the explicit migration E2E'
}

function Test-InternalAgentDockProcess {
    param(
        [Parameter(Mandatory = $true)]
        [string] $CommandMarker
    )

    $process = Get-CimInstance Win32_Process -ErrorAction Stop | Where-Object {
        $_.CommandLine -and $_.CommandLine.Contains($CommandMarker)
    } | Select-Object -First 1
    return $null -ne $process
}

$listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
$listener.Start()
$port = ([Net.IPEndPoint] $listener.LocalEndpoint).Port
$listener.Stop()

New-Item -ItemType Directory -Path $extractRoot, $binRoot, $homeRoot, $defaultRoot, (Join-Path $runtimeRoot 'installer') -Force | Out-Null
Expand-Archive -LiteralPath $archive -DestinationPath $extractRoot -Force

$core = Join-Path $binRoot 'agentdock.exe'
$tray = Join-Path $binRoot 'agentdock-tray.exe'
Copy-Item (Join-Path $extractRoot 'agentdock.exe') $core -Force
Copy-Item (Join-Path $extractRoot 'agentdock-tray.exe') $tray -Force
Copy-Item (Join-Path $extractRoot 'manage-windows.ps1') (Join-Path $runtimeRoot 'installer\manage-windows.ps1') -Force

$versionOutput = (& $core --version | Out-String).Trim()
if ([string]::IsNullOrWhiteSpace($Version)) {
    if ($versionOutput -notmatch '^AgentDock v(?<version>[0-9]+\.[0-9]+\.[0-9]+)') {
        throw "cannot derive Release version from: $versionOutput"
    }
    $Version = $Matches.version
}

$manifest = [ordered]@{
    schema_version = 1
    install_root = $runtimeRoot
    agentdock_home = $homeRoot
    agentdock_default_dir = $defaultRoot
    agentdock_binary = $core
    tray_binary = $tray
    agentdock_launcher = ''
    agentdock_task_name = ''
    privilege_mode = 'standard'
    cloudflared_binary = ''
    cloudflared_launcher = ''
    startup_value_name = 'AgentDockLegacyMigrationE2E'
    tray_startup_value_name = 'AgentDockTrayLegacyMigrationE2E'
    cloudflared_startup_value_name = 'AgentDockCloudflaredLegacyMigrationE2E'
    host = '127.0.0.1'
    port = $port
    local_mcp_url = "http://127.0.0.1:$port/mcp"
    tunnel_mode = 'none'
    public_url = ''
    install_channel = 'e2e'
}
[IO.File]::WriteAllText(
    (Join-Path $runtimeRoot 'runtime.json'),
    (($manifest | ConvertTo-Json -Depth 6) + [Environment]::NewLine),
    $utf8NoBom
)
[IO.File]::WriteAllText(
    (Join-Path $runtimeRoot 'desktop-version.txt'),
    ("v$Version" + [Environment]::NewLine),
    $utf8NoBom
)

try {
    if ($LASTEXITCODE -ne 0 -or $versionOutput -notmatch [regex]::Escape("AgentDock v$Version")) {
        throw "flat Core version mismatch: $versionOutput"
    }

    # Published v0.8.2/v0.8.3 updater starts the new flat Core and waits for this health
    # endpoint before it finishes its own commit. Reproduce that state rather than invoking
    # the migration helper against a stopped fixture.
    & $core service start --runtime-root $runtimeRoot
    if ($LASTEXITCODE -ne 0) {
        throw "flat Core start failed with exit code $LASTEXITCODE"
    }
    $flatHealth = Invoke-RestMethod -UseBasicParsing -Uri "http://127.0.0.1:$port/healthz" -TimeoutSec 3
    if ($flatHealth.ok -ne $true -or $flatHealth.version -ne $Version) {
        throw "flat Core health mismatch before migration: $($flatHealth | ConvertTo-Json -Compress)"
    }

    # The Core startup also launches the normal same-version background repair. On this
    # unreleased E2E build it exits after observing that GitHub latest is a different
    # version. Wait for it so the explicit local-archive repair owns the migration mutex.
    Wait-NoDesktopRepairProcess -ExecutablePath $core

    # The hidden repair entrypoint is the same one launched automatically by Core startup;
    # local archive flags only make the test independent from an already-published Release.
    $trayLock = $null
    try {
        if ($InjectTrayReplaceFailure) {
            # FileShare.None forces MoveFileEx(REPLACE_EXISTING) to fail only after the
            # migration has prepared its committed source generation and replaced Core.
            $trayLock = [IO.File]::Open($tray, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::None)
        }
        & $core __repair-desktop-runtime --local-archive $archive --checksum $checksum
        if ($LASTEXITCODE -ne 0) {
            throw "legacy migration repair entrypoint failed with exit code $LASTEXITCODE"
        }

        if ($InjectTrayReplaceFailure) {
            $flatCoreHash = (Get-FileHash -LiteralPath (Join-Path $extractRoot 'agentdock.exe') -Algorithm SHA256).Hash
            $flatTrayHash = (Get-FileHash -LiteralPath (Join-Path $extractRoot 'agentdock-tray.exe') -Algorithm SHA256).Hash
            $activePath = Join-Path $runtimeRoot 'active-version.json'
            $compatManagerPath = Join-Path $runtimeRoot 'installer\manage-windows.ps1'
            $deadline = [DateTime]::UtcNow.AddSeconds(90)
            $recovered = $false
            do {
                Start-Sleep -Milliseconds 250
                if (-not (Test-Path -LiteralPath $activePath -PathType Leaf)) {
                    continue
                }
                try {
                    $active = Get-Content -LiteralPath $activePath -Raw | ConvertFrom-Json
                    if ($active.state -ne 'committed' -or $active.active_version -ne "v$Version") {
                        continue
                    }
                    if (Test-InternalAgentDockProcess -CommandMarker '__migrate-windows-legacy-layout') {
                        continue
                    }
                    if ((Get-FileHash -LiteralPath $core -Algorithm SHA256).Hash -ne $flatCoreHash -or
                        (Get-FileHash -LiteralPath $tray -Algorithm SHA256).Hash -ne $flatTrayHash -or
                        -not (Test-Path -LiteralPath $compatManagerPath -PathType Leaf)) {
                        continue
                    }
                    $rollbackHealth = Invoke-RestMethod -UseBasicParsing -Uri "http://127.0.0.1:$port/healthz" -TimeoutSec 2
                    if ($rollbackHealth.ok -eq $true -and $rollbackHealth.version -eq $Version) {
                        $recovered = $true
                        break
                    }
                } catch {
                }
            } while ([DateTime]::UtcNow -lt $deadline)

            if (-not $recovered) {
                throw 'injected Tray replacement failure did not restore the healthy flat runtime'
            }
            $generation = Join-Path $runtimeRoot "versions\v$Version"
            foreach ($required in @('agentdock-core.exe', 'agentdock-tray.exe', 'agentdock-arbiter.exe')) {
                if (-not (Test-Path -LiteralPath (Join-Path $generation $required) -PathType Leaf)) {
                    throw "rollback-safe source generation is missing $required"
                }
            }
            Write-Host "Windows legacy migration rollback E2E passed on v$Version."
            return
        }
    } finally {
        if ($null -ne $trayLock) {
            $trayLock.Dispose()
        }
    }

    $deadline = [DateTime]::UtcNow.AddSeconds(90)
    $activePath = Join-Path $runtimeRoot 'active-version.json'
    $compatManagerPath = Join-Path $runtimeRoot 'installer\manage-windows.ps1'
    $coreShimHash = (Get-FileHash -LiteralPath (Join-Path $extractRoot 'agentdock-shim.exe') -Algorithm SHA256).Hash
    $trayShimHash = (Get-FileHash -LiteralPath (Join-Path $extractRoot 'agentdock-tray-shim.exe') -Algorithm SHA256).Hash
    $health = $null
    do {
        Start-Sleep -Milliseconds 500
        if (-not (Test-Path -LiteralPath $activePath -PathType Leaf)) {
            continue
        }
        try {
            $active = Get-Content -LiteralPath $activePath -Raw | ConvertFrom-Json
            if ($active.state -ne 'committed' -or $active.active_version -ne "v$Version") {
                continue
            }
            # active-version.json is committed before stable entries are replaced on purpose.
            # Wait for the entire migration terminal state, not merely the crash-safe source
            # generation checkpoint.
            if ((Get-FileHash -LiteralPath $core -Algorithm SHA256).Hash -ne $coreShimHash -or
                (Get-FileHash -LiteralPath $tray -Algorithm SHA256).Hash -ne $trayShimHash -or
                (Test-Path -LiteralPath $compatManagerPath)) {
                continue
            }
            $stableVersion = (& $core --version | Out-String).Trim()
            if ($LASTEXITCODE -ne 0 -or $stableVersion -notmatch [regex]::Escape("AgentDock v$Version")) {
                continue
            }
            $health = Invoke-RestMethod -UseBasicParsing -Uri "http://127.0.0.1:$port/healthz" -TimeoutSec 2
            if ($health.ok -eq $true -and $health.version -eq $Version) {
                break
            }
        } catch {
            $health = $null
        }
    } while ([DateTime]::UtcNow -lt $deadline)

    if ($null -eq $health -or $health.ok -ne $true -or $health.version -ne $Version) {
        throw 'generation migration did not reach committed healthy state'
    }

    $generation = Join-Path $runtimeRoot "versions\v$Version"
    foreach ($required in @(
        'agentdock-core.exe',
        'agentdock-tray.exe',
        'agentdock-arbiter.exe',
        'wsl-helper\manifest.json',
        'wsl-helper\agentdock-wsl-helper-linux-amd64',
        'wsl-helper\agentdock-wsl-helper-linux-arm64'
    )) {
        if (-not (Test-Path -LiteralPath (Join-Path $generation $required) -PathType Leaf)) {
            throw "committed generation is missing $required"
        }
    }

    $stableCoreHash = (Get-FileHash -LiteralPath $core -Algorithm SHA256).Hash
    if ($stableCoreHash -ne $coreShimHash) {
        throw 'stable Core entry was not replaced by agentdock-shim.exe'
    }
    $stableTrayHash = (Get-FileHash -LiteralPath $tray -Algorithm SHA256).Hash
    if ($stableTrayHash -ne $trayShimHash) {
        throw 'stable Tray entry was not replaced by agentdock-tray-shim.exe'
    }
    if (Test-Path -LiteralPath $compatManagerPath) {
        throw 'legacy compatibility manager remained after generation migration'
    }

    Write-Host "Windows legacy flat -> generation migration E2E passed on v$Version."
} finally {
    try {
        if (Test-Path -LiteralPath $core -PathType Leaf) {
            & $core service stop --runtime-root $runtimeRoot 2>$null | Out-Null
        }
    } catch {
    }
    Start-Sleep -Milliseconds 500
    Remove-Item -LiteralPath $testRoot -Recurse -Force -ErrorAction SilentlyContinue
}

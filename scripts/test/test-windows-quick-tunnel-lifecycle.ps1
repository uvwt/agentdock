[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string] $InstallerPath,
    [Parameter(Mandatory = $true)]
    [string] $UninstallerPath,
    [Parameter(Mandatory = $true)]
    [string] $AgentDockArchive,
    [Parameter(Mandatory = $true)]
    [string] $AgentDockChecksumFile,
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

function Get-ProcessIdsByPath {
    param([string] $ProcessName, [string] $BinaryPath)

    if (-not (Test-Path -LiteralPath $BinaryPath -PathType Leaf)) {
        return @()
    }
    $normalizedPath = [IO.Path]::GetFullPath($BinaryPath)
    $processIds = @(Get-CimInstance Win32_Process -Filter "Name = '$ProcessName.exe'" -ErrorAction SilentlyContinue |
        Where-Object {
            $_.ExecutablePath -and
            [string]::Equals(
                [IO.Path]::GetFullPath($_.ExecutablePath),
                $normalizedPath,
                [StringComparison]::OrdinalIgnoreCase
            )
        } |
        Select-Object -ExpandProperty ProcessId)
    if ($processIds.Count -eq 0) {
        $processIds = @(Get-Process -Name $ProcessName -ErrorAction SilentlyContinue |
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
    return @($processIds | Sort-Object -Unique)
}

function Stop-ProcessByPath {
    param([string] $ProcessName, [string] $BinaryPath)

    foreach ($processId in @(Get-ProcessIdsByPath -ProcessName $ProcessName -BinaryPath $BinaryPath)) {
        Stop-Process -Id $processId -Force -ErrorAction SilentlyContinue
    }
    $deadline = [DateTime]::UtcNow.AddSeconds(15)
    do {
        if (@(Get-ProcessIdsByPath -ProcessName $ProcessName -BinaryPath $BinaryPath).Count -eq 0) {
            return
        }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "Process did not stop within 15 seconds: $BinaryPath"
}

function Wait-TextFileValue {
    param([string] $Path, [string] $ExpectedValue, [int] $TimeoutSeconds = 45)

    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    do {
        Start-Sleep -Milliseconds 500
        try {
            if ((Test-Path -LiteralPath $Path -PathType Leaf) -and
                [string]::Equals(
                    [IO.File]::ReadAllText($Path).Trim(),
                    $ExpectedValue,
                    [StringComparison]::OrdinalIgnoreCase
                )) {
                return
            }
        } catch {
        }
    } while ([DateTime]::UtcNow -lt $deadline)
    $actual = if (Test-Path -LiteralPath $Path -PathType Leaf) {
        [IO.File]::ReadAllText($Path).Trim()
    } else {
        '<missing>'
    }
    throw "Timed out waiting for $Path to become $ExpectedValue; actual: $actual"
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

$testId = [Guid]::NewGuid().ToString('N')
$root = Join-Path $env:RUNNER_TEMP "agentdock-quick-lifecycle-$testId"
$installDir = Join-Path $root 'AgentDock\bin'
$runtimeDir = Split-Path -Parent $installDir
$agentDockBinary = Join-Path $installDir 'agentdock.exe'
$trayBinary = Join-Path $installDir 'agentdock-tray.exe'
$cloudflaredBinary = Join-Path $installDir 'cloudflared.exe'
$cloudflaredLauncher = Join-Path $runtimeDir 'start-cloudflared.ps1'
$managerPath = Join-Path $runtimeDir 'installer\manage-windows.ps1'
$urlSourcePath = Join-Path $installDir 'quick-url-source.txt'
$quickUrlPath = Join-Path $runtimeDir 'quick-tunnel-url.txt'
$serverUrlPath = Join-Path $runtimeDir 'server-url.txt'
$manifestPath = Join-Path $runtimeDir 'runtime.json'
$supervisorPidPath = Join-Path $runtimeDir 'tunnel-supervisor.pid'
$startCountPath = Join-Path $installDir 'start-count.txt'
$failCountPath = Join-Path $installDir 'fail-count.txt'
$authPath = Join-Path $runtimeDir 'auth-token.dpapi'
$oauthPasswordPath = Join-Path $runtimeDir 'oauth-password.dpapi'
$oauthSecretPath = Join-Path $runtimeDir 'oauth-token-secret.dpapi'
$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$startupName = "AgentDockQuickLifecycle-$testId"
$cloudflaredStartupName = "AgentDockCloudflaredQuickLifecycle-$testId"
$trayStartupName = "AgentDockTrayQuickLifecycle-$testId"
$port = Get-FreeTcpPort
$healthUrl = "http://127.0.0.1:$port/healthz"
$firstUrl = 'https://first-agentdock-test.trycloudflare.com'
$recoveredUrl = 'https://recovered-agentdock-test.trycloudflare.com'
$secondUrl = 'https://second-agentdock-test.trycloudflare.com'
$thirdUrl = 'https://third-agentdock-test.trycloudflare.com'
$oldUserPath = [Environment]::GetEnvironmentVariable('Path', 'User')

try {
    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
    [IO.File]::WriteAllText($urlSourcePath, $firstUrl, [Text.UTF8Encoding]::new($false))

    & $InstallerPath `
        -Version 'v0.0.0-test' `
        -OfflineArchive $AgentDockArchive `
        -OfflineChecksumFile $AgentDockChecksumFile `
        -OfflineCloudflaredBinary $FakeCloudflaredBinary `
        -InstallDir $installDir `
        -RegisterStartup `
        -TunnelMode quick `
        -CorePrivilegeMode standard `
        -Port $port `
        -AuthToken 'stable-quick-bearer-token' `
        -OAuthPassword 'stable-quick-oauth-password' `
        -OAuthTokenSecret 'stable-quick-oauth-secret-0123456789abcdef' `
        -StartupValueName $startupName `
        -CloudflaredStartupValueName $cloudflaredStartupName `
        -TrayStartupValueName $trayStartupName

    foreach ($path in @(
        $agentDockBinary,
        $trayBinary,
        $cloudflaredBinary,
        $cloudflaredLauncher,
        $managerPath,
        $quickUrlPath,
        $serverUrlPath,
        $manifestPath,
        $supervisorPidPath,
        $startCountPath,
        $authPath,
        $oauthPasswordPath,
        $oauthSecretPath
    )) {
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
            throw "Quick Tunnel install did not create expected file: $path"
        }
    }
    Wait-TextFileValue -Path $quickUrlPath -ExpectedValue $firstUrl
    Wait-TextFileValue -Path $serverUrlPath -ExpectedValue $firstUrl
    Wait-Healthy -Url $healthUrl

    $coreStartupCommand = Get-ItemPropertyValue -LiteralPath $runKey -Name $startupName
    $tunnelStartupCommand = Get-ItemPropertyValue -LiteralPath $runKey -Name $cloudflaredStartupName
    if (-not $coreStartupCommand.Contains($trayBinary) -or -not $coreStartupCommand.Contains('--start-core')) {
        throw "Core startup is not native: $coreStartupCommand"
    }
    if (-not $tunnelStartupCommand.Contains($trayBinary) -or -not $tunnelStartupCommand.Contains('--start-tunnel')) {
        throw "Tunnel startup is not native: $tunnelStartupCommand"
    }
    if ($coreStartupCommand.Contains('powershell.exe') -or $tunnelStartupCommand.Contains('powershell.exe')) {
        throw 'Native startup entries must not invoke PowerShell.'
    }

    $firstManifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
    if ($firstManifest.tunnel_mode -ne 'quick' -or $firstManifest.public_url -ne $firstUrl) {
        throw "Initial runtime manifest did not contain the Quick Tunnel URL: $($firstManifest | ConvertTo-Json -Compress)"
    }
    Wait-TextFileValue -Path $startCountPath -ExpectedValue '1'
    $firstAgentDockIds = @(Get-ProcessIdsByPath -ProcessName 'agentdock' -BinaryPath $agentDockBinary)
    if ($firstAgentDockIds.Count -ne 2) {
        throw "Expected Core + Tunnel supervisor after Quick Tunnel install; got $($firstAgentDockIds.Count) AgentDock processes."
    }
    $firstSupervisorId = [int] ([IO.File]::ReadAllText($supervisorPidPath).Trim())
    if ($firstAgentDockIds -notcontains $firstSupervisorId) {
        throw "Tunnel supervisor PID file points outside AgentDock processes: $firstSupervisorId"
    }
    $firstCoreIds = @($firstAgentDockIds | Where-Object { [int] $_ -ne $firstSupervisorId })
    if ($firstCoreIds.Count -ne 1) {
        throw "Expected exactly one Core process; got $($firstCoreIds.Count)."
    }
    $firstCoreId = [int] $firstCoreIds[0]
    $authHash = (Get-FileHash -LiteralPath $authPath -Algorithm SHA256).Hash
    $oauthPasswordHash = (Get-FileHash -LiteralPath $oauthPasswordPath -Algorithm SHA256).Hash
    $oauthSecretHash = (Get-FileHash -LiteralPath $oauthSecretPath -Algorithm SHA256).Hash

    # Fault injection must hit exactly one cloudflared from this isolated install.
    # Otherwise a later timeout would not prove anything about supervisor recovery.
    if ((Get-ProcessIdsByPath -ProcessName 'cloudflared' -BinaryPath $cloudflaredBinary | Measure-Object).Count -ne 1) {
        throw 'Expected exactly one cloudflared before fault injection.'
    }
    Stop-ProcessByPath -ProcessName 'cloudflared' -BinaryPath $cloudflaredBinary
    if ((Get-ProcessIdsByPath -ProcessName 'cloudflared' -BinaryPath $cloudflaredBinary | Measure-Object).Count -ne 0) {
        throw 'Fault injection did not stop the isolated cloudflared process.'
    }
    # The supervisor backs off for at least five seconds. During that window,
    # prepare a new Quick URL and make the first recovery attempt fail once.
    [IO.File]::WriteAllText($urlSourcePath, $recoveredUrl, [Text.UTF8Encoding]::new($false))
    [IO.File]::WriteAllText($failCountPath, '1', [Text.UTF8Encoding]::new($false))
    if ([IO.File]::ReadAllText($urlSourcePath).Trim() -ne $recoveredUrl) {
        throw 'Failed to prepare the recovered Quick Tunnel URL fixture.'
    }
    Wait-TextFileValue -Path $quickUrlPath -ExpectedValue $recoveredUrl -TimeoutSeconds 60
    Wait-TextFileValue -Path $serverUrlPath -ExpectedValue $recoveredUrl -TimeoutSeconds 60
    Wait-TextFileValue -Path $startCountPath -ExpectedValue '3' -TimeoutSeconds 60
    Wait-Healthy -Url $healthUrl

    $recoveredSupervisorId = [int] ([IO.File]::ReadAllText($supervisorPidPath).Trim())
    if ($recoveredSupervisorId -ne $firstSupervisorId) {
        throw "Tunnel supervisor restarted instead of supervising cloudflared: $firstSupervisorId -> $recoveredSupervisorId"
    }
    $recoveredAgentDockIds = @(Get-ProcessIdsByPath -ProcessName 'agentdock' -BinaryPath $agentDockBinary)
    if ($recoveredAgentDockIds.Count -ne 2) {
        throw "Expected Core + Tunnel supervisor after automatic recovery; got $($recoveredAgentDockIds.Count)."
    }
    $recoveredCoreIds = @($recoveredAgentDockIds | Where-Object { [int] $_ -ne $recoveredSupervisorId })
    if ($recoveredCoreIds.Count -ne 1 -or [int] $recoveredCoreIds[0] -eq $firstCoreId) {
        throw 'Core was not restarted when the recovered Quick Tunnel URL changed.'
    }
    $recoveredCoreId = [int] $recoveredCoreIds[0]
    $recoveredManifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
    if ($recoveredManifest.public_url -ne $recoveredUrl) {
        throw "Automatic recovery did not refresh runtime manifest: $($recoveredManifest | ConvertTo-Json -Compress)"
    }
    if ((Get-FileHash -LiteralPath $authPath -Algorithm SHA256).Hash -ne $authHash -or
        (Get-FileHash -LiteralPath $oauthPasswordPath -Algorithm SHA256).Hash -ne $oauthPasswordHash -or
        (Get-FileHash -LiteralPath $oauthSecretPath -Algorithm SHA256).Hash -ne $oauthSecretHash) {
        throw 'Automatic Tunnel recovery unexpectedly rotated existing credentials.'
    }

    [IO.File]::WriteAllText($urlSourcePath, $secondUrl, [Text.UTF8Encoding]::new($false))
    & $agentDockBinary tunnel regenerate --runtime-root $runtimeDir
    if ($LASTEXITCODE -ne 0) {
        throw "Native Quick Tunnel regeneration failed with exit code $LASTEXITCODE."
    }

    Wait-TextFileValue -Path $quickUrlPath -ExpectedValue $secondUrl
    Wait-TextFileValue -Path $serverUrlPath -ExpectedValue $secondUrl
    Wait-Healthy -Url $healthUrl

    $secondManifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
    if ($secondManifest.public_url -ne $secondUrl) {
        throw "Runtime manifest was not refreshed: $($secondManifest | ConvertTo-Json -Compress)"
    }
    $secondAgentDockIds = @(Get-ProcessIdsByPath -ProcessName 'agentdock' -BinaryPath $agentDockBinary)
    if ($secondAgentDockIds.Count -ne 2) {
        throw "Expected Core + Tunnel supervisor after Quick Tunnel refresh; got $($secondAgentDockIds.Count)."
    }
    $secondSupervisorId = [int] ([IO.File]::ReadAllText($supervisorPidPath).Trim())
    $secondCoreIds = @($secondAgentDockIds | Where-Object { [int] $_ -ne $secondSupervisorId })
    if ($secondCoreIds.Count -ne 1 -or [int] $secondCoreIds[0] -eq $recoveredCoreId) {
        throw 'Core was not restarted after the Quick Tunnel URL changed.'
    }
    if ((Get-FileHash -LiteralPath $authPath -Algorithm SHA256).Hash -ne $authHash -or
        (Get-FileHash -LiteralPath $oauthPasswordPath -Algorithm SHA256).Hash -ne $oauthPasswordHash -or
        (Get-FileHash -LiteralPath $oauthSecretPath -Algorithm SHA256).Hash -ne $oauthSecretHash) {
        throw 'Quick Tunnel refresh unexpectedly rotated existing credentials.'
    }

    & $agentDockBinary tunnel configure --runtime-root $runtimeDir --mode none
    if ($LASTEXITCODE -ne 0) {
        throw "Native local-only transition failed with exit code $LASTEXITCODE."
    }
    Wait-Healthy -Url $healthUrl
    Wait-TextFileValue -Path $serverUrlPath -ExpectedValue ''
    if (Test-Path -LiteralPath $quickUrlPath -PathType Leaf) {
        throw 'Switching to local-only mode did not remove the active Quick Tunnel URL.'
    }
    $noneManifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
    $nonePublicUrl = ''
    if ($noneManifest.PSObject.Properties.Name -contains 'public_url') {
        $nonePublicUrl = [string] $noneManifest.public_url
    }
    if ($noneManifest.tunnel_mode -ne 'none' -or -not [string]::IsNullOrWhiteSpace($nonePublicUrl)) {
        throw "Local-only runtime manifest is invalid: $($noneManifest | ConvertTo-Json -Compress)"
    }
    if (@(Get-ProcessIdsByPath -ProcessName 'cloudflared' -BinaryPath $cloudflaredBinary).Count -ne 0) {
        throw 'Switching to local-only mode did not stop cloudflared.'
    }
    if (Test-Path -LiteralPath $supervisorPidPath -PathType Leaf) {
        throw 'Switching to local-only mode left the Tunnel supervisor running.'
    }
    if (@(Get-ProcessIdsByPath -ProcessName 'agentdock' -BinaryPath $agentDockBinary).Count -ne 1) {
        throw 'Switching to local-only mode should leave only the Core AgentDock process.'
    }
    if ((Get-FileHash -LiteralPath $authPath -Algorithm SHA256).Hash -ne $authHash -or
        (Get-FileHash -LiteralPath $oauthPasswordPath -Algorithm SHA256).Hash -ne $oauthPasswordHash -or
        (Get-FileHash -LiteralPath $oauthSecretPath -Algorithm SHA256).Hash -ne $oauthSecretHash) {
        throw 'Switching to local-only mode unexpectedly changed existing credentials.'
    }

    [IO.File]::WriteAllText($urlSourcePath, $thirdUrl, [Text.UTF8Encoding]::new($false))
    & $agentDockBinary tunnel configure --runtime-root $runtimeDir --mode quick
    if ($LASTEXITCODE -ne 0) {
        throw "Native Quick Tunnel transition failed with exit code $LASTEXITCODE."
    }
    Wait-TextFileValue -Path $quickUrlPath -ExpectedValue $thirdUrl
    Wait-TextFileValue -Path $serverUrlPath -ExpectedValue $thirdUrl
    Wait-Healthy -Url $healthUrl
    $thirdManifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
    if ($thirdManifest.tunnel_mode -ne 'quick' -or $thirdManifest.public_url -ne $thirdUrl) {
        throw "Quick Tunnel mode was not restored: $($thirdManifest | ConvertTo-Json -Compress)"
    }
    if (-not (Test-Path -LiteralPath $supervisorPidPath -PathType Leaf) -or
        @(Get-ProcessIdsByPath -ProcessName 'agentdock' -BinaryPath $agentDockBinary).Count -ne 2) {
        throw 'Restoring Quick Tunnel mode did not restore Core + Tunnel supervisor.'
    }
    if ((Get-FileHash -LiteralPath $authPath -Algorithm SHA256).Hash -ne $authHash -or
        (Get-FileHash -LiteralPath $oauthPasswordPath -Algorithm SHA256).Hash -ne $oauthPasswordHash -or
        (Get-FileHash -LiteralPath $oauthSecretPath -Algorithm SHA256).Hash -ne $oauthSecretHash) {
        throw 'Restoring Quick Tunnel mode unexpectedly changed existing credentials.'
    }

    Write-Host "Windows Quick Tunnel lifecycle passed: $firstUrl -> auto-recovery $recoveredUrl -> $secondUrl -> local-only -> $thirdUrl"

    & $UninstallerPath `
        -InstallDir $installDir `
        -StartupValueName $startupName `
        -CloudflaredStartupValueName $cloudflaredStartupName `
        -TrayStartupValueName $trayStartupName
    if (Test-Path -LiteralPath $installDir) {
        throw 'Quick Tunnel lifecycle uninstaller did not remove the install directory.'
    }
} catch {
    foreach ($name in @(
        'start-cloudflared.ps1',
        'cloudflared.out.log',
        'cloudflared.err.log',
        'quick-tunnel-url.txt',
        'server-url.txt',
        'runtime.json'
    )) {
        $path = Join-Path $runtimeDir $name
        Write-Host "----- diagnostic: $path -----"
        if (Test-Path -LiteralPath $path -PathType Leaf) {
            Get-Content -LiteralPath $path -Raw | Write-Host
        } else {
            Write-Host '<missing>'
        }
    }
    throw
} finally {
    Stop-ProcessByPath -ProcessName 'agentdock-tray' -BinaryPath $trayBinary
    Stop-ProcessByPath -ProcessName 'cloudflared' -BinaryPath $cloudflaredBinary
    Stop-ProcessByPath -ProcessName 'agentdock' -BinaryPath $agentDockBinary
    foreach ($name in @($startupName, $cloudflaredStartupName, $trayStartupName)) {
        Remove-ItemProperty -LiteralPath $runKey -Name $name -ErrorAction SilentlyContinue
    }
    [Environment]::SetEnvironmentVariable('Path', $oldUserPath, 'User')
    Remove-Item -LiteralPath $root -Recurse -Force -ErrorAction SilentlyContinue
}

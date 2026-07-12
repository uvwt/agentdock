[CmdletBinding()]
param(
    [string] $Version = 'latest',
    [string] $InstallDir = (Join-Path $env:LOCALAPPDATA 'AgentDock\bin'),
    [switch] $RegisterStartup,
    [int] $Port = 8765,
    [string] $AuthToken = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$Utf8NoBom = New-Object System.Text.UTF8Encoding($false)

function Get-AgentDockArchitecture {
    $architecture = $env:PROCESSOR_ARCHITECTURE
    if ($env:PROCESSOR_ARCHITEW6432) {
        $architecture = $env:PROCESSOR_ARCHITEW6432
    }

    switch ($architecture.ToUpperInvariant()) {
        'AMD64' { return 'amd64' }
        'ARM64' { return 'arm64' }
        default { throw "Unsupported Windows architecture: $architecture" }
    }
}

function Get-ReleaseBaseUrl {
    param([string] $RequestedVersion)

    if ($RequestedVersion -eq 'latest') {
        return 'https://github.com/uvwt/agentdock/releases/latest/download'
    }

    $normalizedVersion = $RequestedVersion
    if (-not $normalizedVersion.StartsWith('v')) {
        $normalizedVersion = "v$normalizedVersion"
    }
    return "https://github.com/uvwt/agentdock/releases/download/$normalizedVersion"
}

function Add-UserPath {
    param([string] $Directory)

    $currentPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $pathParts = @($currentPath -split ';' | Where-Object { $_ })
    if ($pathParts -notcontains $Directory) {
        $updatedPath = (@($pathParts) + $Directory) -join ';'
        [Environment]::SetEnvironmentVariable('Path', $updatedPath, 'User')
    }
    if (($env:Path -split ';') -notcontains $Directory) {
        $env:Path = "$env:Path;$Directory"
    }
}

function New-AgentDockToken {
    $bytes = New-Object byte[] 32
    $generator = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $generator.GetBytes($bytes)
    } finally {
        $generator.Dispose()
    }
    return -join ($bytes | ForEach-Object { $_.ToString('x2') })
}

function Get-AgentDockProcesses {
    param([string] $BinaryPath)

    $normalizedBinaryPath = [IO.Path]::GetFullPath($BinaryPath)
    $matchingProcesses = @()
    $processes = @(Get-Process -Name 'agentdock' -ErrorAction SilentlyContinue)
    foreach ($process in $processes) {
        try {
            $processPath = [IO.Path]::GetFullPath($process.Path)
            if ([string]::Equals($processPath, $normalizedBinaryPath, [StringComparison]::OrdinalIgnoreCase)) {
                $matchingProcesses += $process
            }
        } catch {
        }
    }
    return @($matchingProcesses)
}

function Stop-AgentDockForUpgrade {
    param([string] $BinaryPath)

    $processWasRunning = $false
    $deadline = [DateTime]::UtcNow.AddSeconds(15)
    do {
        $runningProcesses = @(Get-AgentDockProcesses -BinaryPath $BinaryPath)
        if ($runningProcesses.Count -gt 0) {
            $processWasRunning = $true
        }
        foreach ($process in $runningProcesses) {
            Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
        }
        if ($runningProcesses.Count -eq 0) {
            break
        }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)

    $remainingProcesses = @(Get-AgentDockProcesses -BinaryPath $BinaryPath)
    if ($remainingProcesses.Count -gt 0) {
        throw "Unable to stop the running AgentDock process at $BinaryPath."
    }
    return $processWasRunning
}

function Start-AgentDockLauncher {
    param([string] $LauncherPath)

    if (-not (Test-Path -LiteralPath $LauncherPath -PathType Leaf)) {
        throw "AgentDock launcher was not found: $LauncherPath"
    }
    $arguments = "-NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$LauncherPath`""
    Start-Process -FilePath 'powershell.exe' -ArgumentList $arguments -WindowStyle Hidden | Out-Null
}

function Install-AgentDockBinary {
    param(
        [string] $SourceBinary,
        [string] $DestinationBinary
    )

    $deadline = [DateTime]::UtcNow.AddSeconds(15)
    do {
        try {
            Copy-Item -LiteralPath $SourceBinary -Destination $DestinationBinary -Force
            return
        } catch {
            if ([DateTime]::UtcNow -ge $deadline) {
                throw "Unable to replace $DestinationBinary after stopping AgentDock: $($_.Exception.Message)"
            }
            Start-Sleep -Milliseconds 250
        }
    } while ($true)
}

function Wait-AgentDockHealth {
    param([int] $HealthPort)

    $healthUrl = "http://127.0.0.1:$HealthPort/healthz"
    $deadline = [DateTime]::UtcNow.AddSeconds(20)
    do {
        Start-Sleep -Milliseconds 500
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri $healthUrl -TimeoutSec 2
            if ($response.StatusCode -eq 200) {
                return
            }
        } catch {
        }
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "AgentDock was installed, but health check failed at $healthUrl"
}

if ($Port -lt 1 -or $Port -gt 65535) {
    throw 'Port must be between 1 and 65535.'
}
if (-not $env:LOCALAPPDATA) {
    throw 'LOCALAPPDATA is required.'
}

$architecture = Get-AgentDockArchitecture
$assetName = "agentdock_windows_$architecture.zip"
$releaseBaseUrl = Get-ReleaseBaseUrl -RequestedVersion $Version
$tempRoot = Join-Path ([IO.Path]::GetTempPath()) ("agentdock-install-" + [Guid]::NewGuid().ToString('N'))
$archivePath = Join-Path $tempRoot $assetName
$checksumPath = "$archivePath.sha256"
$destinationBinary = Join-Path $InstallDir 'agentdock.exe'
$binaryBackup = Join-Path $tempRoot 'agentdock.exe.previous'
$runtimeDir = Split-Path -Parent $InstallDir
$launcherPath = Join-Path $runtimeDir 'start-agentdock.ps1'
$tokenPath = Join-Path $runtimeDir 'auth-token.dpapi'
$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$runValueName = 'AgentDock'
$processWasRunning = $false
$binaryReplacementStarted = $false
$startupRegistrationChanged = $false
$runValueExisted = $false
$previousRunValue = $null

try {
    New-Item -ItemType Directory -Path $tempRoot -Force | Out-Null
    Invoke-WebRequest -UseBasicParsing -Uri "$releaseBaseUrl/$assetName" -OutFile $archivePath
    Invoke-WebRequest -UseBasicParsing -Uri "$releaseBaseUrl/$assetName.sha256" -OutFile $checksumPath

    $expectedHash = ((Get-Content -LiteralPath $checksumPath -Raw).Trim() -split '\s+')[0].ToLowerInvariant()
    $actualHash = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actualHash -ne $expectedHash) {
        throw "SHA-256 mismatch for $assetName. Expected $expectedHash, got $actualHash."
    }

    $extractDir = Join-Path $tempRoot 'extract'
    Expand-Archive -LiteralPath $archivePath -DestinationPath $extractDir -Force
    $sourceBinary = Join-Path $extractDir 'agentdock.exe'
    if (-not (Test-Path -LiteralPath $sourceBinary -PathType Leaf)) {
        throw "Release archive does not contain agentdock.exe: $assetName"
    }

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    $processWasRunning = Stop-AgentDockForUpgrade -BinaryPath $destinationBinary
    if (Test-Path -LiteralPath $destinationBinary -PathType Leaf) {
        Copy-Item -LiteralPath $destinationBinary -Destination $binaryBackup -Force
    }

    $binaryReplacementStarted = $true
    Install-AgentDockBinary -SourceBinary $sourceBinary -DestinationBinary $destinationBinary
    Add-UserPath -Directory $InstallDir

    $agentDockHome = Join-Path $HOME '.agentdock'
    $workspace = Join-Path $HOME 'AgentDock'
    foreach ($directory in @($agentDockHome, $workspace)) {
        New-Item -ItemType Directory -Path $directory -Force | Out-Null
    }

    if ($RegisterStartup) {
        New-Item -ItemType Directory -Path $runtimeDir -Force | Out-Null
        $generatedToken = $false
        $mustWriteToken = $AuthToken -or -not (Test-Path -LiteralPath $tokenPath -PathType Leaf)
        if ($mustWriteToken) {
            if (-not $AuthToken) {
                $AuthToken = New-AgentDockToken
                $generatedToken = $true
            }
            Add-Type -AssemblyName System.Security
            $protectedToken = [System.Security.Cryptography.ProtectedData]::Protect(
                [Text.Encoding]::UTF8.GetBytes($AuthToken),
                [Text.Encoding]::UTF8.GetBytes('agentdock.startup.v1'),
                [System.Security.Cryptography.DataProtectionScope]::CurrentUser
            )
            $protectedTokenText = [Convert]::ToBase64String($protectedToken)
            [IO.File]::WriteAllText($tokenPath, $protectedTokenText, $Utf8NoBom)
        }

        $escapedTokenPath = $tokenPath.Replace("'", "''")
        $escapedBinaryPath = $destinationBinary.Replace("'", "''")
        $launcher = @"
`$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Security
`$tokenBytes = [Convert]::FromBase64String([IO.File]::ReadAllText('$escapedTokenPath').Trim())
`$plainToken = [System.Security.Cryptography.ProtectedData]::Unprotect(
    `$tokenBytes,
    [Text.Encoding]::UTF8.GetBytes('agentdock.startup.v1'),
    [System.Security.Cryptography.DataProtectionScope]::CurrentUser
)
`$env:AGENTDOCK_AUTH_TOKEN = [Text.Encoding]::UTF8.GetString(`$plainToken)
`$env:AGENTDOCK_HOST = '127.0.0.1'
`$env:AGENTDOCK_PORT = '$Port'
& '$escapedBinaryPath'
"@
        [IO.File]::WriteAllText($launcherPath, $launcher, $Utf8NoBom)

        if (Test-Path -LiteralPath $runKey) {
            try {
                $previousRunValue = Get-ItemPropertyValue -LiteralPath $runKey -Name $runValueName -ErrorAction Stop
                $runValueExisted = $true
            } catch {
                $runValueExisted = $false
            }
        }

        New-Item -Path $runKey -Force | Out-Null
        $startupCommand = "powershell.exe -NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$launcherPath`""
        New-ItemProperty -Path $runKey -Name $runValueName -Value $startupCommand -PropertyType String -Force | Out-Null
        $startupRegistrationChanged = $true
        Start-AgentDockLauncher -LauncherPath $launcherPath
        Wait-AgentDockHealth -HealthPort $Port

        if ($generatedToken) {
            Write-Host "Bearer token (shown once): $AuthToken"
        }
    }

    $mustRestartExistingProcess = (-not $RegisterStartup) -and $processWasRunning -and (Test-Path -LiteralPath $launcherPath -PathType Leaf)
    if ($mustRestartExistingProcess) {
        Start-AgentDockLauncher -LauncherPath $launcherPath
    }

    Write-Host "AgentDock installed: $destinationBinary"
    Write-Host 'Open a new terminal if the updated user PATH is not visible yet.'
} catch {
    $installError = $_
    if ($binaryReplacementStarted) {
        try {
            [void] (Stop-AgentDockForUpgrade -BinaryPath $destinationBinary)
            $backupExists = Test-Path -LiteralPath $binaryBackup -PathType Leaf
            if ($backupExists) {
                Copy-Item -LiteralPath $binaryBackup -Destination $destinationBinary -Force
            }
            if (-not $backupExists -and (Test-Path -LiteralPath $destinationBinary -PathType Leaf)) {
                Remove-Item -LiteralPath $destinationBinary -Force
            }

            if ($startupRegistrationChanged -and $runValueExisted) {
                New-Item -Path $runKey -Force | Out-Null
                New-ItemProperty -Path $runKey -Name $runValueName -Value $previousRunValue -PropertyType String -Force | Out-Null
            }
            if ($startupRegistrationChanged -and -not $runValueExisted) {
                Remove-ItemProperty -LiteralPath $runKey -Name $runValueName -ErrorAction SilentlyContinue
            }
            if ($processWasRunning -and (Test-Path -LiteralPath $launcherPath -PathType Leaf)) {
                Start-AgentDockLauncher -LauncherPath $launcherPath
            }
        } catch {
            Write-Warning "AgentDock rollback failed: $($_.Exception.Message)"
        }
    }
    if (-not $binaryReplacementStarted -and $processWasRunning -and (Test-Path -LiteralPath $launcherPath -PathType Leaf)) {
        Start-AgentDockLauncher -LauncherPath $launcherPath
    }
    throw $installError
} finally {
    Remove-Item -LiteralPath $tempRoot -Recurse -Force -ErrorAction SilentlyContinue
}

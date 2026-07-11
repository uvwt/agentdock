[CmdletBinding()]
param(
    [string] $Version = 'latest',
    [string] $InstallDir = (Join-Path $env:LOCALAPPDATA 'AgentDock\bin'),
    [switch] $RegisterStartup,
    [int] $Port = 8765,
    [string] $AuthToken = '',
    [switch] $AclSelfTest
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

function Get-AgentDockArchitecture {
    $arch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    switch ($arch.ToUpperInvariant()) {
        'AMD64' { return 'amd64' }
        'ARM64' { return 'arm64' }
        default { throw "Unsupported Windows architecture: $arch" }
    }
}

function Get-ReleaseBaseUrl([string] $RequestedVersion) {
    if ($RequestedVersion -eq 'latest') {
        return 'https://github.com/uvwt/agentdock/releases/latest/download'
    }
    $normalized = if ($RequestedVersion.StartsWith('v')) { $RequestedVersion } else { "v$RequestedVersion" }
    return "https://github.com/uvwt/agentdock/releases/download/$normalized"
}

function Set-PrivateAcl([string] $Path) {
    $item = Get-Item -LiteralPath $Path
    $currentUserSid = [System.Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    $inheritance = if ($item.PSIsContainer) { '(OI)(CI)' } else { '' }
    $grants = @(
        "*${currentUserSid}:${inheritance}F",
        "*S-1-5-18:${inheritance}F",
        "*S-1-5-32-544:${inheritance}F"
    )

    # icacls 只修改 DACL，不会读取或写回 SACL，因此普通用户无需 SeSecurityPrivilege。
    $icaclsArguments = @($item.FullName, '/inheritance:r', '/grant:r') + $grants
    & icacls.exe @icaclsArguments | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "Unable to set private ACL on $($item.FullName). icacls exited with code $LASTEXITCODE."
    }
}

function Add-UserPath([string] $Directory) {
    $current = [Environment]::GetEnvironmentVariable('Path', 'User')
    $parts = @($current -split ';' | Where-Object { $_ })
    if ($parts -notcontains $Directory) {
        $updated = (@($parts) + $Directory) -join ';'
        [Environment]::SetEnvironmentVariable('Path', $updated, 'User')
    }
    if (($env:Path -split ';') -notcontains $Directory) {
        $env:Path = "$env:Path;$Directory"
    }
}

function New-AgentDockToken {
    $bytes = [byte[]]::new(32)
    $generator = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $generator.GetBytes($bytes)
    }
    finally {
        $generator.Dispose()
    }
    return -join ($bytes | ForEach-Object { $_.ToString('x2') })
}

function Stop-AgentDockForUpgrade([string] $BinaryPath) {
    $taskName = 'AgentDock'
    $task = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    $taskWasRunning = $null -ne $task -and $task.State -eq 'Running'
    if ($null -ne $task) {
        Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    }

    $normalizedBinaryPath = [IO.Path]::GetFullPath($BinaryPath)
    $deadline = [DateTime]::UtcNow.AddSeconds(15)
    do {
        $running = @(Get-Process -Name 'agentdock' -ErrorAction SilentlyContinue | Where-Object {
            try {
                [string]::Equals(
                    [IO.Path]::GetFullPath($_.Path),
                    $normalizedBinaryPath,
                    [StringComparison]::OrdinalIgnoreCase
                )
            }
            catch {
                $false
            }
        })
        foreach ($process in $running) {
            Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
        }
        if ($running.Count -eq 0) {
            break
        }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)

    $stillRunning = @(Get-Process -Name 'agentdock' -ErrorAction SilentlyContinue | Where-Object {
        try {
            [string]::Equals(
                [IO.Path]::GetFullPath($_.Path),
                $normalizedBinaryPath,
                [StringComparison]::OrdinalIgnoreCase
            )
        }
        catch {
            $false
        }
    })
    if ($stillRunning.Count -gt 0) {
        throw "Unable to stop the running AgentDock process at $BinaryPath."
    }

    return [PSCustomObject]@{
        TaskExisted = $null -ne $task
        TaskWasRunning = $taskWasRunning
    }
}

function Install-AgentDockBinary([string] $SourceBinary, [string] $DestinationBinary) {
    $deadline = [DateTime]::UtcNow.AddSeconds(15)
    do {
        try {
            Copy-Item -LiteralPath $SourceBinary -Destination $DestinationBinary -Force
            return
        }
        catch {
            if ([DateTime]::UtcNow -ge $deadline) {
                throw "Unable to replace $DestinationBinary after stopping AgentDock: $($_.Exception.Message)"
            }
            Start-Sleep -Milliseconds 250
        }
    } while ($true)
}

if ($AclSelfTest) {
    $testRoot = Join-Path ([IO.Path]::GetTempPath()) ('agentdock-acl-' + [Guid]::NewGuid().ToString('N'))
    try {
        New-Item -ItemType Directory -Path $testRoot -Force | Out-Null
        $testFile = Join-Path $testRoot 'token.dpapi'
        [IO.File]::WriteAllText($testFile, 'test', [Text.UTF8Encoding]::new($false))
        Set-PrivateAcl $testRoot
        Set-PrivateAcl $testFile

        $expectedSids = @(
            [System.Security.Principal.WindowsIdentity]::GetCurrent().User.Value,
            'S-1-5-18',
            'S-1-5-32-544'
        ) | Sort-Object -Unique
        foreach ($target in @($testRoot, $testFile)) {
            $acl = Get-Acl -LiteralPath $target
            if (-not $acl.AreAccessRulesProtected) {
                throw "ACL inheritance is still enabled for $target"
            }
            $actualSids = @($acl.Access | ForEach-Object {
                $_.IdentityReference.Translate([System.Security.Principal.SecurityIdentifier]).Value
            } | Sort-Object -Unique)
            if (Compare-Object -ReferenceObject $expectedSids -DifferenceObject $actualSids) {
                throw "Unexpected ACL identities for ${target}: $($actualSids -join ', ')"
            }
        }
    }
    finally {
        Remove-Item -LiteralPath $testRoot -Recurse -Force -ErrorAction SilentlyContinue
    }
    Write-Host 'AgentDock Windows ACL self-test passed.'
    return
}

if ($Port -lt 1 -or $Port -gt 65535) {
    throw 'Port must be between 1 and 65535.'
}
if (-not $env:LOCALAPPDATA) {
    throw 'LOCALAPPDATA is required.'
}

$architecture = Get-AgentDockArchitecture
$asset = "agentdock_windows_$architecture.zip"
$releaseBase = Get-ReleaseBaseUrl $Version
$tempRoot = Join-Path ([IO.Path]::GetTempPath()) ("agentdock-install-" + [Guid]::NewGuid().ToString('N'))
$archive = Join-Path $tempRoot $asset
$checksumFile = "$archive.sha256"
$destinationBinary = Join-Path $InstallDir 'agentdock.exe'
$binaryBackup = Join-Path $tempRoot 'agentdock.exe.previous'
$upgradeState = $null
$binaryReplacementStarted = $false

try {
    New-Item -ItemType Directory -Path $tempRoot -Force | Out-Null
    Invoke-WebRequest -UseBasicParsing -Uri "$releaseBase/$asset" -OutFile $archive
    Invoke-WebRequest -UseBasicParsing -Uri "$releaseBase/$asset.sha256" -OutFile $checksumFile

    $expected = ((Get-Content -LiteralPath $checksumFile -Raw).Trim() -split '\s+')[0].ToLowerInvariant()
    $actual = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) {
        throw "SHA-256 mismatch for $asset. Expected $expected, got $actual."
    }

    $extractDir = Join-Path $tempRoot 'extract'
    Expand-Archive -LiteralPath $archive -DestinationPath $extractDir -Force
    $sourceBinary = Join-Path $extractDir 'agentdock.exe'
    if (-not (Test-Path -LiteralPath $sourceBinary -PathType Leaf)) {
        throw "Release archive does not contain agentdock.exe: $asset"
    }

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    $upgradeState = Stop-AgentDockForUpgrade -BinaryPath $destinationBinary
    if (Test-Path -LiteralPath $destinationBinary -PathType Leaf) {
        Copy-Item -LiteralPath $destinationBinary -Destination $binaryBackup -Force
    }
    $binaryReplacementStarted = $true
    Install-AgentDockBinary -SourceBinary $sourceBinary -DestinationBinary $destinationBinary
    Set-PrivateAcl $InstallDir
    Set-PrivateAcl $destinationBinary
    Add-UserPath $InstallDir

    $agentDockHome = Join-Path $HOME '.agentdock'
    $workspace = Join-Path $HOME 'AgentDock'
    foreach ($directory in @($agentDockHome, $workspace)) {
        New-Item -ItemType Directory -Path $directory -Force | Out-Null
        Set-PrivateAcl $directory
    }

    if ($RegisterStartup) {
        $runtimeDir = Split-Path -Parent $InstallDir
        New-Item -ItemType Directory -Path $runtimeDir -Force | Out-Null
        Set-PrivateAcl $runtimeDir
        $tokenPath = Join-Path $runtimeDir 'auth-token.dpapi'
        $generatedToken = $false
        if (-not $AuthToken -and (Test-Path -LiteralPath $tokenPath -PathType Leaf)) {
            Set-PrivateAcl $tokenPath
        }
        else {
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
            [IO.File]::WriteAllText($tokenPath, [Convert]::ToBase64String($protectedToken), [Text.UTF8Encoding]::new($false))
            Set-PrivateAcl $tokenPath
        }

        $launcherPath = Join-Path $runtimeDir 'start-agentdock.ps1'
        $escapedTokenPath = $tokenPath.Replace("'", "''")
        $binaryPath = Join-Path $InstallDir 'agentdock.exe'
        $escapedBinaryPath = $binaryPath.Replace("'", "''")
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
        [IO.File]::WriteAllText($launcherPath, $launcher, [Text.UTF8Encoding]::new($false))
        Set-PrivateAcl $launcherPath

        $taskName = 'AgentDock'
        $action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument "-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File `"$launcherPath`""
        $trigger = New-ScheduledTaskTrigger -AtLogOn -User ([System.Security.Principal.WindowsIdentity]::GetCurrent().Name)
        $settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit ([TimeSpan]::Zero) -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1)
        Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Settings $settings -Description 'AgentDock user runtime' -Force | Out-Null
        Start-ScheduledTask -TaskName $taskName

        $health = $null
        $deadline = [DateTime]::UtcNow.AddSeconds(20)
        do {
            Start-Sleep -Milliseconds 500
            try {
                $health = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$Port/healthz" -TimeoutSec 2
                if ($health.StatusCode -eq 200) { break }
            } catch {}
        } while ([DateTime]::UtcNow -lt $deadline)
        if (-not $health -or $health.StatusCode -ne 200) {
            throw "AgentDock was installed, but health check failed at http://127.0.0.1:$Port/healthz"
        }
        if ($generatedToken) {
            Write-Host "Bearer token (shown once): $AuthToken"
        }
    }
    elseif ($null -ne $upgradeState -and $upgradeState.TaskWasRunning) {
        Start-ScheduledTask -TaskName 'AgentDock'
    }

    Write-Host "AgentDock installed: $destinationBinary"
    Write-Host 'Open a new terminal if the updated user PATH is not visible yet.'
}
catch {
    $installError = $_
    if ($binaryReplacementStarted) {
        try {
            [void] (Stop-AgentDockForUpgrade -BinaryPath $destinationBinary)
            if (Test-Path -LiteralPath $binaryBackup -PathType Leaf) {
                Copy-Item -LiteralPath $binaryBackup -Destination $destinationBinary -Force
            }
            elseif (Test-Path -LiteralPath $destinationBinary -PathType Leaf) {
                Remove-Item -LiteralPath $destinationBinary -Force
            }

            if ($null -ne $upgradeState -and -not $upgradeState.TaskExisted) {
                Unregister-ScheduledTask -TaskName 'AgentDock' -Confirm:$false -ErrorAction SilentlyContinue
            }
            elseif ($null -ne $upgradeState -and $upgradeState.TaskWasRunning) {
                Start-ScheduledTask -TaskName 'AgentDock'
            }
        }
        catch {
            Write-Warning "AgentDock rollback failed: $($_.Exception.Message)"
        }
    }
    elseif ($null -ne $upgradeState -and $upgradeState.TaskWasRunning) {
        Start-ScheduledTask -TaskName 'AgentDock' -ErrorAction SilentlyContinue
    }
    throw $installError
}
finally {
    Remove-Item -LiteralPath $tempRoot -Recurse -Force -ErrorAction SilentlyContinue
}

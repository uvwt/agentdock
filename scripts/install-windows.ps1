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
    $acl = Get-Acl -LiteralPath $Path
    $acl.SetAccessRuleProtection($true, $false)
    foreach ($rule in @($acl.Access)) {
        [void] $acl.RemoveAccessRuleAll($rule)
    }
    $inheritance = if ((Get-Item -LiteralPath $Path).PSIsContainer) {
        [System.Security.AccessControl.InheritanceFlags]'ContainerInherit, ObjectInherit'
    } else {
        [System.Security.AccessControl.InheritanceFlags]::None
    }
    $propagation = [System.Security.AccessControl.PropagationFlags]::None
    foreach ($identity in @(
        [System.Security.Principal.WindowsIdentity]::GetCurrent().User,
        [System.Security.Principal.SecurityIdentifier]::new('S-1-5-18'),
        [System.Security.Principal.SecurityIdentifier]::new('S-1-5-32-544')
    )) {
        $rule = [System.Security.AccessControl.FileSystemAccessRule]::new(
            $identity,
            [System.Security.AccessControl.FileSystemRights]::FullControl,
            $inheritance,
            $propagation,
            [System.Security.AccessControl.AccessControlType]::Allow
        )
        $acl.AddAccessRule($rule)
    }
    Set-Acl -LiteralPath $Path -AclObject $acl
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
    Copy-Item -LiteralPath $sourceBinary -Destination (Join-Path $InstallDir 'agentdock.exe') -Force
    Set-PrivateAcl $InstallDir
    Set-PrivateAcl (Join-Path $InstallDir 'agentdock.exe')
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
        if (-not $AuthToken) {
            $AuthToken = New-AgentDockToken
        }
        Add-Type -AssemblyName System.Security
        $protectedToken = [System.Security.Cryptography.ProtectedData]::Protect(
            [Text.Encoding]::UTF8.GetBytes($AuthToken),
            [Text.Encoding]::UTF8.GetBytes('agentdock.startup.v1'),
            [System.Security.Cryptography.DataProtectionScope]::CurrentUser
        )
        $tokenPath = Join-Path $runtimeDir 'auth-token.dpapi'
        [IO.File]::WriteAllText($tokenPath, [Convert]::ToBase64String($protectedToken), [Text.UTF8Encoding]::new($false))
        Set-PrivateAcl $tokenPath

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
        Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
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
        Write-Host "Bearer token (shown once): $AuthToken"
    }

    Write-Host "AgentDock installed: $(Join-Path $InstallDir 'agentdock.exe')"
    Write-Host 'Open a new terminal if the updated user PATH is not visible yet.'
}
finally {
    Remove-Item -LiteralPath $tempRoot -Recurse -Force -ErrorAction SilentlyContinue
}

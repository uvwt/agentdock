[CmdletBinding()]
param(
    [string] $Version = 'latest',
    [string] $OfflineArchive = '',
    [string] $OfflineChecksumFile = '',
    [string] $OfflineCloudflaredBinary = '',
    [string] $InstallDir = (Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'AgentDock\bin'),
    [switch] $RegisterStartup,
    [switch] $ConfigurePublicAccess,
    [int] $Port = 8765,
    [string] $AuthToken = '',
    [ValidateSet('auto', 'none', 'quick', 'named')]
    [string] $TunnelMode = 'auto',
    [string] $ServerUrl = '',
    [string] $TunnelToken = '',
    [string] $TunnelTokenFile = '',
    [switch] $DeleteTunnelTokenFile,
    [string] $ResultFile = '',
    [ValidateSet('script', 'setup')]
    [string] $InstallChannel = 'script',
    [string] $OAuthPassword = '',
    [string] $OAuthTokenSecret = '',
    [string] $StartupValueName = 'AgentDock',
    [string] $CloudflaredStartupValueName = 'AgentDockCloudflared',
    [string] $TrayStartupValueName = 'AgentDockTray',
    [ValidateSet('standard', 'elevated')]
    [string] $CorePrivilegeMode = 'standard'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

function Invoke-SetupRuntimeProcess {
    param(
        [string] $FilePath,
        [string] $Arguments = '',
        [switch] $WaitForExit
    )

    if ($InstallChannel -ne 'setup') {
        throw 'The Setup runtime launcher is only valid for InstallChannel=setup.'
    }
    if (-not (Test-Path -LiteralPath $setupRuntimeLauncherPath -PathType Leaf)) {
        throw "Setup runtime launcher was not found: $setupRuntimeLauncherPath"
    }

    # Inno Setup 6.7+ enables ProcessRedirectionTrustPolicy on its process tree.
    # Task Scheduler creates the long-lived runtime from a clean user process context
    # while Setup keeps RedirectionGuard enabled for install-time filesystem work.
    & $setupRuntimeLauncherPath `
        -FilePath $FilePath `
        -Arguments $Arguments `
        -WaitForExit:$WaitForExit
}

function Get-AgentDockArchitecture {
    $architecture = $env:PROCESSOR_ARCHITECTURE
    if ($env:PROCESSOR_ARCHITEW6432) {
        $architecture = $env:PROCESSOR_ARCHITEW6432
    }
    if ([string]::IsNullOrWhiteSpace($architecture)) {
        $architecture = [Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
    }

    switch ($architecture.ToUpperInvariant()) {
        'AMD64' { return 'amd64' }
        'X64' { return 'amd64' }
        'ARM64' { return 'arm64' }
        default { throw "Unsupported Windows architecture: $architecture" }
    }
}

function Get-ReleaseBaseUrl {
    param([string] $RequestedVersion)

    $customBaseUrl = [Environment]::GetEnvironmentVariable('AGENTDOCK_RELEASE_BASE_URL')
    if (-not [string]::IsNullOrWhiteSpace($customBaseUrl)) {
        return $customBaseUrl.TrimEnd('/')
    }

    if ($RequestedVersion -eq 'latest') {
        return 'https://github.com/uvwt/agentdock/releases/latest/download'
    }

    $normalizedVersion = $RequestedVersion
    if (-not $normalizedVersion.StartsWith('v')) {
        $normalizedVersion = "v$normalizedVersion"
    }
    return "https://github.com/uvwt/agentdock/releases/download/$normalizedVersion"
}

function Get-CloudflaredReleaseBaseUrl {
    $customBaseUrl = [Environment]::GetEnvironmentVariable('AGENTDOCK_CLOUDFLARED_RELEASE_BASE_URL')
    if (-not [string]::IsNullOrWhiteSpace($customBaseUrl)) {
        return $customBaseUrl.TrimEnd('/')
    }
    return 'https://github.com/cloudflare/cloudflared/releases/latest/download'
}

function Get-Sha256Hex {
    param([string] $Path)

    $sha256 = [System.Security.Cryptography.SHA256]::Create()
    try {
        $stream = [IO.File]::OpenRead($Path)
        try {
            $hash = $sha256.ComputeHash($stream)
        } finally {
            $stream.Dispose()
        }
    } finally {
        $sha256.Dispose()
    }
    return [BitConverter]::ToString($hash).Replace('-', '').ToLowerInvariant()
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

function New-AgentDockSecret {
    param([int] $ByteCount = 32)

    $bytes = New-Object byte[] $ByteCount
    $generator = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $generator.GetBytes($bytes)
    } finally {
        $generator.Dispose()
    }
    return -join ($bytes | ForEach-Object { $_.ToString('x2') })
}

function New-AgentDockToken {
    return New-AgentDockSecret -ByteCount 32
}

function Write-ProtectedText {
    param(
        [string] $Path,
        [string] $Value,
        [string] $Entropy
    )

    $protectedBytes = [System.Security.Cryptography.ProtectedData]::Protect(
        [Text.Encoding]::UTF8.GetBytes($Value),
        [Text.Encoding]::UTF8.GetBytes($Entropy),
        [System.Security.Cryptography.DataProtectionScope]::CurrentUser
    )
    [IO.File]::WriteAllText($Path, [Convert]::ToBase64String($protectedBytes), $Utf8NoBom)
}

function Read-ProtectedText {
    param(
        [string] $Path,
        [string] $Entropy
    )

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        return ''
    }
    $protectedBytes = [Convert]::FromBase64String([IO.File]::ReadAllText($Path).Trim())
    $plainBytes = [System.Security.Cryptography.ProtectedData]::Unprotect(
        $protectedBytes,
        [Text.Encoding]::UTF8.GetBytes($Entropy),
        [System.Security.Cryptography.DataProtectionScope]::CurrentUser
    )
    return [Text.Encoding]::UTF8.GetString($plainBytes)
}

function Read-TextFile {
    param([string] $Path)

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        return ''
    }
    return [IO.File]::ReadAllText($Path).Trim()
}

function Write-TextFile {
    param(
        [string] $Path,
        [string] $Value
    )

    [IO.File]::WriteAllText($Path, $Value, $Utf8NoBom)
}

function Normalize-ServerUrl {
    param([string] $Value)

    $trimmed = $Value.Trim().TrimEnd('/')
    if ([string]::IsNullOrWhiteSpace($trimmed)) {
        throw 'A fixed Cloudflare hostname requires an HTTPS public origin.'
    }
    try {
        $uri = [Uri] $trimmed
    } catch {
        throw "Invalid public origin: $Value"
    }
    if (-not $uri.IsAbsoluteUri -or $uri.Scheme -ne 'https' -or [string]::IsNullOrWhiteSpace($uri.Host)) {
        throw "The public origin must be an absolute HTTPS URL: $Value"
    }
    if ($uri.AbsolutePath -ne '/' -or $uri.Query -or $uri.Fragment -or $uri.UserInfo) {
        throw "The public origin must not contain a path, query, fragment, or user info: $Value"
    }
    return $trimmed
}

function Resolve-TunnelMode {
    param(
        [string] $RequestedMode,
        [string] $ModePath,
        [bool] $StartupRequested,
        [bool] $PublicAccessRequested
    )

    if ($RequestedMode -ne 'auto') {
        return $RequestedMode.ToLowerInvariant()
    }

    $environmentMode = [Environment]::GetEnvironmentVariable('AGENTDOCK_TUNNEL_MODE')
    if (-not [string]::IsNullOrWhiteSpace($environmentMode)) {
        $environmentMode = $environmentMode.Trim().ToLowerInvariant()
        if (@('none', 'quick', 'named') -notcontains $environmentMode) {
            throw "AGENTDOCK_TUNNEL_MODE must be none, quick, or named: $environmentMode"
        }
        return $environmentMode
    }

    $storedMode = Read-TextFile -Path $ModePath
    if (-not [string]::IsNullOrWhiteSpace($storedMode)) {
        $storedMode = $storedMode.ToLowerInvariant()
        if (@('quick', 'named') -contains $storedMode) {
            Write-Host "Reusing public access mode: $storedMode"
            return $storedMode
        }
    }

    if (-not $StartupRequested -or -not $PublicAccessRequested) {
        return 'none'
    }

    Write-Host ''
    Write-Host 'Choose public access:'
    Write-Host '- Have a Cloudflare domain: use a fixed address for long-running clients and OAuth.'
    Write-Host '- No domain: create a temporary address for a quick trial. It changes after cloudflared restarts.'
    $answer = Read-Host 'Do you have a domain already connected to Cloudflare? [y/N]'
    if ($answer -match '^(?i:y|yes)$') {
        return 'named'
    }
    return 'quick'
}

function Read-SecretFile {
    param([string] $Path)

    if ([string]::IsNullOrWhiteSpace($Path)) {
        return ''
    }
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "Secret file was not found: $Path"
    }
    $value = (Get-Content -LiteralPath $Path -Raw).Trim()
    if ([string]::IsNullOrWhiteSpace($value)) {
        throw "Secret file is empty: $Path"
    }
    return $value
}

function Initialize-OAuthCredentials {
    param(
        [string] $PasswordPath,
        [string] $TokenSecretPath,
        [string] $RequestedPassword,
        [string] $RequestedTokenSecret
    )

    $existingPassword = Read-ProtectedText -Path $PasswordPath -Entropy 'agentdock.oauth.password.v1'
    $password = $RequestedPassword
    if ([string]::IsNullOrWhiteSpace($password)) {
        $password = [Environment]::GetEnvironmentVariable('AGENTDOCK_OAUTH_PASSWORD')
    }
    if ([string]::IsNullOrWhiteSpace($password)) {
        $password = $existingPassword
    }
    if ([string]::IsNullOrWhiteSpace($password)) {
        $password = New-AgentDockSecret -ByteCount 12
    }
    if ($password.Length -lt 12) {
        throw 'OAuth password must contain at least 12 characters.'
    }
    if (-not [string]::Equals($password, $existingPassword, [StringComparison]::Ordinal)) {
        Write-ProtectedText -Path $PasswordPath -Value $password -Entropy 'agentdock.oauth.password.v1'
    }

    $existingTokenSecret = Read-ProtectedText -Path $TokenSecretPath -Entropy 'agentdock.oauth.secret.v1'
    $tokenSecret = $RequestedTokenSecret
    if ([string]::IsNullOrWhiteSpace($tokenSecret)) {
        $tokenSecret = [Environment]::GetEnvironmentVariable('AGENTDOCK_OAUTH_TOKEN_SECRET')
    }
    if ([string]::IsNullOrWhiteSpace($tokenSecret)) {
        $tokenSecret = $existingTokenSecret
    }
    if ([string]::IsNullOrWhiteSpace($tokenSecret)) {
        $tokenSecret = New-AgentDockSecret -ByteCount 32
    }
    if ([Text.Encoding]::UTF8.GetByteCount($tokenSecret) -lt 32) {
        throw 'OAuth token secret must contain at least 32 bytes.'
    }
    if (-not [string]::Equals($tokenSecret, $existingTokenSecret, [StringComparison]::Ordinal)) {
        Write-ProtectedText -Path $TokenSecretPath -Value $tokenSecret -Entropy 'agentdock.oauth.secret.v1'
    }

    return [pscustomobject]@{
        Password = $password
        TokenSecret = $tokenSecret
    }
}

# runtime.json is owned by agentdock install. Generation pointer is owned by the
# Update Engine on upgrades, and by the Installer Engine on first publish.
function Write-RuntimeManifest {
    param(
        [string] $Path,
        [string] $InstallRoot,
        [string] $AgentDockHome,
        [string] $AgentDockDefaultDir,
        [string] $AgentDockBinary,
        [string] $TrayBinary,
        [string] $AgentDockLauncher,
        [string] $AgentDockTaskName,
        [string] $PrivilegeMode,
        [string] $CloudflaredBinary,
        [string] $CloudflaredLauncher,
        [string] $CoreStartupValueName,
        [string] $TrayStartupValueName,
        [string] $TunnelStartupValueName,
        [int] $RuntimePort,
        [string] $RuntimeTunnelMode,
        [string] $RuntimePublicUrl,
        [string] $Channel
    )

    $manifest = [ordered]@{
        schema_version = 1
        install_root = $InstallRoot
        agentdock_home = $AgentDockHome
        agentdock_default_dir = $AgentDockDefaultDir
        agentdock_binary = $AgentDockBinary
        tray_binary = $TrayBinary
        agentdock_launcher = $AgentDockLauncher
        agentdock_task_name = $AgentDockTaskName
        privilege_mode = $PrivilegeMode
        cloudflared_binary = $CloudflaredBinary
        cloudflared_launcher = $CloudflaredLauncher
        startup_value_name = $CoreStartupValueName
        tray_startup_value_name = $TrayStartupValueName
        cloudflared_startup_value_name = $TunnelStartupValueName
        host = '127.0.0.1'
        port = $RuntimePort
        local_mcp_url = "http://127.0.0.1:$RuntimePort/mcp"
        tunnel_mode = $RuntimeTunnelMode
        public_url = $RuntimePublicUrl
        install_channel = $Channel
    }
    [IO.File]::WriteAllText($Path, ($manifest | ConvertTo-Json -Depth 3), $Utf8NoBom)
}

function Write-ActiveVersionState {
    param(
        [string] $Path,
        [string] $ActiveVersion,
        [string] $FallbackVersion = ''
    )

    $state = [ordered]@{
        schema_version = 1
        active_version = $ActiveVersion
        fallback_version = $FallbackVersion
        state = 'committed'
        updated_at = [DateTime]::UtcNow.ToString('o')
    }
    $directory = Split-Path -Parent $Path
    New-Item -ItemType Directory -Path $directory -Force | Out-Null
    $tempPath = Join-Path $directory ('.active-version.' + [Guid]::NewGuid().ToString('N') + '.tmp')
    try {
        $bytes = $Utf8NoBom.GetBytes(($state | ConvertTo-Json -Depth 3) + "`n")
        $stream = [IO.File]::Open($tempPath, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
        try {
            $stream.Write($bytes, 0, $bytes.Length)
            $stream.Flush($true)
        } finally {
            $stream.Dispose()
        }
        if (Test-Path -LiteralPath $Path -PathType Leaf) {
            [IO.File]::Replace($tempPath, $Path, $null, $true)
        } else {
            [IO.File]::Move($tempPath, $Path)
        }
    } finally {
        Remove-Item -LiteralPath $tempPath -Force -ErrorAction SilentlyContinue
    }
}

function ConvertTo-InstallResultValue {
    param(
        [string] $Value,
        [int] $MaxLength = 0
    )

    if ([string]::IsNullOrEmpty($Value)) {
        return ''
    }
    $normalized = $Value.Replace("`r", ' ').Replace("`n", ' ')
    if (($MaxLength -gt 0) -and ($normalized.Length -gt $MaxLength)) {
        return $normalized.Substring(0, $MaxLength)
    }
    return $normalized
}

function Write-InstallResult {
    param(
        [string] $Path,
        [bool] $Success,
        [string] $Message,
        [string] $InstalledVersion,
        [string] $LocalMCPUrl,
        [string] $PublicMCPUrl,
        [string] $BearerToken,
        [string] $OAuthLoginPassword,
        [string] $HealthStatus,
        [string] $PrivilegeMode,
        [string] $ErrorCode = '',
        [System.Management.Automation.ErrorRecord] $ErrorRecord = $null,
        [string] $WarningCode = '',
        [string] $WarningMessage = ''
    )

    if ([string]::IsNullOrWhiteSpace($Path)) {
        return
    }
    $parent = Split-Path -Parent $Path
    if (-not [string]::IsNullOrWhiteSpace($parent)) {
        New-Item -ItemType Directory -Path $parent -Force | Out-Null
    }

    $errorType = ''
    $errorId = ''
    $errorCategory = ''
    $errorScript = ''
    $errorLine = 0
    $errorColumn = 0
    $errorStack = ''
    if ($null -ne $ErrorRecord) {
        if ($null -ne $ErrorRecord.Exception) {
            $errorType = $ErrorRecord.Exception.GetType().FullName
        }
        $errorId = [string] $ErrorRecord.FullyQualifiedErrorId
        $errorCategory = [string] $ErrorRecord.CategoryInfo.Category
        if ($null -ne $ErrorRecord.InvocationInfo) {
            $errorScript = [string] $ErrorRecord.InvocationInfo.ScriptName
            $errorLine = $ErrorRecord.InvocationInfo.ScriptLineNumber
            $errorColumn = $ErrorRecord.InvocationInfo.OffsetInLine
        }
        $errorStack = [string] $ErrorRecord.ScriptStackTrace
    }

    $safeMessage = ConvertTo-InstallResultValue -Value $Message
    $safeWarningMessage = ConvertTo-InstallResultValue -Value $WarningMessage
    $safeErrorType = ConvertTo-InstallResultValue -Value $errorType
    $safeErrorId = ConvertTo-InstallResultValue -Value $errorId
    $safeErrorCategory = ConvertTo-InstallResultValue -Value $errorCategory
    $safeErrorScript = ConvertTo-InstallResultValue -Value $errorScript
    $safeErrorStack = ConvertTo-InstallResultValue -Value $errorStack -MaxLength 2048
    $lines = @(
        '[AgentDock]',
        "Success=$($Success.ToString().ToLowerInvariant())",
        "Code=$ErrorCode",
        "Message=$safeMessage",
        "ErrorType=$safeErrorType",
        "ErrorId=$safeErrorId",
        "ErrorCategory=$safeErrorCategory",
        "ErrorScript=$safeErrorScript",
        "ErrorLine=$errorLine",
        "ErrorColumn=$errorColumn",
        "ErrorStack=$safeErrorStack",
        "WarningCode=$WarningCode",
        "WarningMessage=$safeWarningMessage",
        "Version=$InstalledVersion",
        "LocalMCPUrl=$LocalMCPUrl",
        "PublicMCPUrl=$PublicMCPUrl",
        "BearerToken=$BearerToken",
        "OAuthPassword=$OAuthLoginPassword",
        "Health=$HealthStatus",
        "PrivilegeMode=$PrivilegeMode"
    )
    # Windows INI APIs read BOM-prefixed UTF-16 reliably, including localized errors.
    [IO.File]::WriteAllLines($Path, $lines, [Text.Encoding]::Unicode)
}

function Get-ProcessesByPath {
    param(
        [string] $ProcessName,
        [string] $BinaryPath
    )

    $normalizedBinaryPath = [IO.Path]::GetFullPath($BinaryPath)
    $matchingProcessIds = @()

    # Win32_Process exposes ExecutablePath reliably in installer contexts where
    # Get-Process.Path may be empty even for a process owned by the current user.
    try {
        $matchingProcessIds = @(Get-CimInstance Win32_Process -Filter "Name = '$ProcessName.exe'" -ErrorAction Stop |
            Where-Object {
                $_.ExecutablePath -and
                [string]::Equals(
                    [IO.Path]::GetFullPath($_.ExecutablePath),
                    $normalizedBinaryPath,
                    [StringComparison]::OrdinalIgnoreCase
                )
            } |
            Select-Object -ExpandProperty ProcessId)
    } catch {
        $matchingProcessIds = @()
    }

    if ($matchingProcessIds.Count -eq 0) {
        $matchingProcessIds = @(Get-Process -Name $ProcessName -ErrorAction SilentlyContinue | Where-Object {
            try {
                [string]::Equals(
                    [IO.Path]::GetFullPath($_.Path),
                    $normalizedBinaryPath,
                    [StringComparison]::OrdinalIgnoreCase
                )
            } catch {
                $false
            }
        } | Select-Object -ExpandProperty Id)
    }

    return @($matchingProcessIds | Sort-Object -Unique | ForEach-Object {
        [pscustomobject]@{ Id = [int] $_ }
    })
}

function Get-AgentDockProcesses {
    param([string] $BinaryPath)
    $processName = [IO.Path]::GetFileNameWithoutExtension($BinaryPath)
    if ([string]::IsNullOrWhiteSpace($processName)) {
        throw "Unable to resolve AgentDock process name from path: $BinaryPath"
    }
    return @(Get-ProcessesByPath -ProcessName $processName -BinaryPath $BinaryPath)
}

function Get-CloudflaredProcesses {
    param([string] $BinaryPath)
    return @(Get-ProcessesByPath -ProcessName 'cloudflared' -BinaryPath $BinaryPath)
}

function Get-AgentDockTrayProcesses {
    param([string] $BinaryPath)
    return @(Get-ProcessesByPath -ProcessName 'agentdock-tray' -BinaryPath $BinaryPath)
}

function Test-AgentDockTaskEligible {
    param(
        [string] $AgentDockValueName,
        [string] $CloudflaredValueName,
        [string] $TrayValueName
    )

    return $AgentDockValueName -eq 'AgentDock' -and
        $CloudflaredValueName -eq 'AgentDockCloudflared' -and
        $TrayValueName -eq 'AgentDockTray'
}

function Get-AgentDockTaskState {
    param(
        [string] $AgentDockValueName,
        [string] $CloudflaredValueName,
        [string] $TrayValueName,
        [string] $RuntimeRoot,
        [string] $StableCorePath,
        [string] $StableTrayPath,
        [string] $LegacyLauncherPath
    )

    $state = [pscustomobject]@{
        Eligible = $false
        Exists = $false
        Conflicting = $false
        WasEnabled = $false
        WasRunning = $false
        SchedulerAvailable = $true
        SchedulerError = ''
    }
    $state.Eligible = Test-AgentDockTaskEligible `
        -AgentDockValueName $AgentDockValueName `
        -CloudflaredValueName $CloudflaredValueName `
        -TrayValueName $TrayValueName
    if (-not $state.Eligible) {
        return $state
    }

    try {
        $task = Get-ScheduledTask -TaskName 'AgentDock' -TaskPath '\' -ErrorAction SilentlyContinue
    } catch {
        # fresh standard installs do not require Task Scheduler. Record capability failure instead of
        # letting ScheduledTasks/WMI/COM terminate the installer before it can write diagnostics.
        $state.SchedulerAvailable = $false
        $state.SchedulerError = $_.Exception.Message
        return $state
    }
    if ($null -eq $task) {
        return $state
    }
    $owned = $false
    $normalizedRoot = [IO.Path]::GetFullPath($RuntimeRoot).TrimEnd('\')
    $normalizedCore = [IO.Path]::GetFullPath($StableCorePath)
    $normalizedTray = [IO.Path]::GetFullPath($StableTrayPath)
    $normalizedLegacyLauncher = [IO.Path]::GetFullPath($LegacyLauncherPath)
    foreach ($action in @($task.Actions)) {
        $executePath = [string] $action.Execute
        $arguments = [string] $action.Arguments
        try {
            if (-not [string]::IsNullOrWhiteSpace($executePath)) {
                $normalizedExecute = [IO.Path]::GetFullPath($executePath)
                if ([string]::Equals($normalizedExecute, $normalizedCore, [StringComparison]::OrdinalIgnoreCase) -or
                    [string]::Equals($normalizedExecute, $normalizedTray, [StringComparison]::OrdinalIgnoreCase)) {
                    $owned = $true
                    break
                }
            }
        } catch {
        }
        if (-not [string]::IsNullOrWhiteSpace($arguments) -and
            ($arguments.IndexOf($normalizedRoot, [StringComparison]::OrdinalIgnoreCase) -ge 0 -or
             $arguments.IndexOf($normalizedLegacyLauncher, [StringComparison]::OrdinalIgnoreCase) -ge 0)) {
            $owned = $true
            break
        }
    }
    if (-not $owned) {
        $state.Conflicting = $true
        return $state
    }
    $state.Exists = $true
    $state.WasEnabled = $task.State -ne 'Disabled'
    $state.WasRunning = $task.State -eq 'Running'
    return $state
}

function Get-CurrentTaskUser {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    return [pscustomobject]@{
        Sid = $identity.User.Value
        Name = $identity.Name
    }
}

function Get-InteractiveDesktopUser {
    try {
        $userName = (Get-CimInstance Win32_ComputerSystem -ErrorAction Stop).UserName
        if ([string]::IsNullOrWhiteSpace($userName)) {
            return $null
        }
        $sid = (New-Object Security.Principal.NTAccount($userName)).Translate(
            [Security.Principal.SecurityIdentifier]
        ).Value
        return [pscustomobject]@{
            Sid = $sid
            Name = $userName
        }
    } catch {
        return $null
    }
}

function Start-ElevatedAgentDockTaskAction {
    param(
        [ValidateSet('prepare-elevated', 'prepare-standard', 'restore', 'remove')]
        [string] $Action,
        [string] $BackupDirectory,
        [string] $AdminLauncherPath,
        [string] $LauncherPath,
        [string] $RuntimeRoot,
        [pscustomobject] $TaskUser
    )

    $arguments = "--task-admin $Action"
    if (-not [string]::IsNullOrWhiteSpace($BackupDirectory)) {
        $arguments += " --backup-directory `"$BackupDirectory`""
    }
    if (-not [string]::IsNullOrWhiteSpace($LauncherPath)) {
        $arguments += " --launcher-path `"$LauncherPath`""
    }
    if (-not [string]::IsNullOrWhiteSpace($RuntimeRoot)) {
        $arguments += " --runtime-root `"$RuntimeRoot`""
    }
    if ($null -ne $TaskUser) {
        $arguments += " --user-sid `"$($TaskUser.Sid)`" --user-name `"$($TaskUser.Name)`""
    }

    try {
        $process = Start-Process `
            -FilePath $AdminLauncherPath `
            -ArgumentList $arguments `
            -Verb RunAs `
            -WindowStyle Hidden `
            -Wait `
            -PassThru
    } catch {
        # Windows or enterprise policy can reject RunAs before the UAC helper starts.
        # The main install flow decides whether that pre-start failure is safe to downgrade.
        return [pscustomobject]@{
            Started = $false
            ErrorMessage = $_.Exception.Message
        }
    }
    $succeeded = $process.ExitCode -eq 0
    return [pscustomobject]@{
        Started = $true
        Succeeded = $succeeded
        ExitCode = $process.ExitCode
        ErrorMessage = $(if ($succeeded) { '' } else { "AgentDock administrator task action failed with exit code $($process.ExitCode)." })
    }
}

function Enable-AgentDockTask {
    Enable-ScheduledTask -TaskName 'AgentDock' -TaskPath '\' -ErrorAction Stop | Out-Null
}

function Start-AgentDockTask {
    param([Parameter(Mandatory = $true)][string] $ManagerScriptPath)

    if (-not (Test-Path -LiteralPath $ManagerScriptPath -PathType Leaf)) {
        throw "Windows manager was not found: $ManagerScriptPath"
    }
    & $ManagerScriptPath `
        -Action task-run-session `
        -ScheduledTaskName 'AgentDock' `
        -ScheduledTaskPath '\'
}

function Stop-ProcessesForUpgrade {
    param(
        [string] $ProcessName,
        [string] $BinaryPath
    )

    $processWasRunning = $false
    $deadline = [DateTime]::UtcNow.AddSeconds(15)
    do {
        $runningProcesses = @(Get-ProcessesByPath -ProcessName $ProcessName -BinaryPath $BinaryPath)
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

    $remainingProcesses = @(Get-ProcessesByPath -ProcessName $ProcessName -BinaryPath $BinaryPath)
    if ($remainingProcesses.Count -gt 0) {
        throw "Unable to stop $ProcessName at $BinaryPath."
    }
    return $processWasRunning
}

function Stop-AgentDockForUpgrade {
    param([string] $BinaryPath)
    $processName = [IO.Path]::GetFileNameWithoutExtension($BinaryPath)
    if ([string]::IsNullOrWhiteSpace($processName)) {
        throw "Unable to resolve AgentDock process name from path: $BinaryPath"
    }
    return Stop-ProcessesForUpgrade -ProcessName $processName -BinaryPath $BinaryPath
}

function Stop-CloudflaredForUpgrade {
    param([string] $BinaryPath)
    return Stop-ProcessesForUpgrade -ProcessName 'cloudflared' -BinaryPath $BinaryPath
}

function Stop-AgentDockTrayForUpgrade {
    param([string] $BinaryPath)
    return Stop-ProcessesForUpgrade -ProcessName 'agentdock-tray' -BinaryPath $BinaryPath
}

function Start-HiddenPowerShellScript {
    param([string] $ScriptPath)

    if (-not (Test-Path -LiteralPath $ScriptPath -PathType Leaf)) {
        throw "Launcher was not found: $ScriptPath"
    }
    $arguments = "-NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$ScriptPath`""
    if ($InstallChannel -eq 'setup') {
        Invoke-SetupRuntimeProcess -FilePath (Join-Path $PSHOME 'powershell.exe') -Arguments $arguments
        return
    }
    Start-Process -FilePath 'powershell.exe' -ArgumentList $arguments -WindowStyle Hidden | Out-Null
}

function Start-AgentDockLauncher {
    param([string] $LauncherPath)
    Start-HiddenPowerShellScript -ScriptPath $LauncherPath
}

function Start-CloudflaredLauncher {
    param([string] $LauncherPath)
    Start-HiddenPowerShellScript -ScriptPath $LauncherPath
}

function Start-AgentDockTray {
    param([string] $BinaryPath)

    if (-not (Test-Path -LiteralPath $BinaryPath -PathType Leaf)) {
        throw "AgentDock tray was not found: $BinaryPath"
    }
    if ($InstallChannel -eq 'setup') {
        Invoke-SetupRuntimeProcess -FilePath $BinaryPath -Arguments '--background'
        return
    }
    Start-Process -FilePath $BinaryPath -ArgumentList '--background' -WindowStyle Hidden | Out-Null
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

function Test-CloudflaredBinary {
    param([string] $BinaryPath)

    if (-not (Test-Path -LiteralPath $BinaryPath -PathType Leaf)) {
        return $false
    }
    try {
        & $BinaryPath --version | Out-Null
        return $LASTEXITCODE -eq 0
    } catch {
        return $false
    }
}

function Install-CloudflaredBinary {
    param(
        [string] $DestinationBinary,
        [string] $Architecture,
        [string] $TempDirectory,
        [string] $SourceBinary = ''
    )

    $sourceOverride = $SourceBinary
    if ([string]::IsNullOrWhiteSpace($sourceOverride)) {
        $sourceOverride = [Environment]::GetEnvironmentVariable('AGENTDOCK_CLOUDFLARED_BINARY')
    }
    if (-not [string]::IsNullOrWhiteSpace($sourceOverride)) {
        if (-not (Test-CloudflaredBinary -BinaryPath $sourceOverride)) {
            throw "The supplied cloudflared binary is invalid: $sourceOverride"
        }
        $stagedPath = "$DestinationBinary.tmp.$PID"
        Copy-Item -LiteralPath $sourceOverride -Destination $stagedPath -Force
        Move-Item -LiteralPath $stagedPath -Destination $DestinationBinary -Force
        return
    }

    if (Test-CloudflaredBinary -BinaryPath $DestinationBinary) {
        return
    }

    $assetName = "cloudflared-windows-$Architecture.exe"
    $downloadPath = Join-Path $TempDirectory $assetName
    $downloadUrl = "$(Get-CloudflaredReleaseBaseUrl)/$assetName"
    Write-Host "Downloading cloudflared: $downloadUrl"
    Invoke-WebRequest -UseBasicParsing -Uri $downloadUrl -OutFile $downloadPath
    if (-not (Test-CloudflaredBinary -BinaryPath $downloadPath)) {
        throw "Downloaded cloudflared executable is invalid: $downloadUrl"
    }
    $stagedPath = "$DestinationBinary.tmp.$PID"
    Copy-Item -LiteralPath $downloadPath -Destination $stagedPath -Force
    Move-Item -LiteralPath $stagedPath -Destination $DestinationBinary -Force
}

function Wait-AgentDockHealth {
    param([int] $HealthPort)

    $healthUrl = "http://127.0.0.1:$HealthPort/healthz"
    $deadline = [DateTime]::UtcNow.AddSeconds(45)
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

function Wait-CloudflaredRunning {
    param([string] $BinaryPath)

    $deadline = [DateTime]::UtcNow.AddSeconds(20)
    do {
        Start-Sleep -Milliseconds 500
        if (@(Get-CloudflaredProcesses -BinaryPath $BinaryPath).Count -gt 0) {
            return
        }
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "cloudflared did not stay running: $BinaryPath"
}

function Wait-QuickTunnelUrl {
    param([string[]] $LogPaths)

    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    do {
        Start-Sleep -Milliseconds 500
        foreach ($logPath in $LogPaths) {
            try {
                if (Test-Path -LiteralPath $logPath -PathType Leaf) {
                    $content = Get-Content -LiteralPath $logPath -Raw -ErrorAction Stop
                    # Provisioning failures also print the trycloudflare API URL; require the creation marker first.
                    $match = [Regex]::Match(
                        $content,
                        '(?s)Your quick Tunnel has been created! Visit it at.*?(https://[A-Za-z0-9-]+\.trycloudflare\.com)'
                    )
                    if ($match.Success) {
                        return $match.Groups[1].Value
                    }
                }
            } catch {
            }
        }
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "cloudflared started, but no temporary trycloudflare.com URL appeared in: $($LogPaths -join ', ')"
}

function Wait-QuickTunnelReady {
    param(
        [string] $Path,
        [string] $ExpectedUrl
    )

    $deadline = [DateTime]::UtcNow.AddSeconds(35)
    do {
        Start-Sleep -Milliseconds 500
        try {
            if ((Test-Path -LiteralPath $Path -PathType Leaf) -and
                [string]::Equals(
                    [IO.File]::ReadAllText($Path).Trim(),
                    $ExpectedUrl,
                    [StringComparison]::OrdinalIgnoreCase
                )) {
                return
            }
        } catch {
        }
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "Quick Tunnel generated $ExpectedUrl, but AgentDock did not finish adopting it."
}

function Backup-FileState {
    param(
        [string] $Path,
        [string] $Name,
        [string] $BackupDirectory
    )

    if (Test-Path -LiteralPath $Path) {
        if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
            throw "Managed runtime path must be a regular file: $Path"
        }
        Copy-Item -LiteralPath $Path -Destination (Join-Path $BackupDirectory $Name) -Force
        New-Item -ItemType File -Path (Join-Path $BackupDirectory "$Name.present") -Force | Out-Null
    }
}

function Restore-FileState {
    param(
        [string] $Path,
        [string] $Name,
        [string] $BackupDirectory
    )

    $marker = Join-Path $BackupDirectory "$Name.present"
    $backup = Join-Path $BackupDirectory $Name
    if (Test-Path -LiteralPath $marker -PathType Leaf) {
        New-Item -ItemType Directory -Path (Split-Path -Parent $Path) -Force | Out-Null
        Copy-Item -LiteralPath $backup -Destination $Path -Force
        return
    }
    Remove-Item -LiteralPath $Path -Force -ErrorAction SilentlyContinue
}

function Get-RunValue {
    param(
        [string] $RegistryPath,
        [string] $Name
    )

    if (-not (Test-Path -LiteralPath $RegistryPath)) {
        return $null
    }
    try {
        return Get-ItemPropertyValue -LiteralPath $RegistryPath -Name $Name -ErrorAction Stop
    } catch {
        return $null
    }
}

function Set-RunValue {
    param(
        [string] $RegistryPath,
        [string] $Name,
        [string] $Value
    )

    try {
        # The standard Run key usually exists; recreating it with -Force can fail in restricted environments.
        if (-not (Test-Path -LiteralPath $RegistryPath -ErrorAction Stop)) {
            New-Item -Path $RegistryPath -ErrorAction Stop | Out-Null
        }
    } catch {
        throw "Unable to prepare current-user startup registry key '$RegistryPath' for value '$Name': $($_.Exception.Message)"
    }

    try {
        New-ItemProperty `
            -Path $RegistryPath `
            -Name $Name `
            -Value $Value `
            -PropertyType String `
            -Force `
            -ErrorAction Stop | Out-Null
    } catch {
        throw "Unable to write current-user startup registry value '$Name' at '$RegistryPath': $($_.Exception.Message)"
    }
}

$effectivePrivilegeMode = $CorePrivilegeMode
$installWarningCode = ''
$installWarningMessage = ''
$installErrorCode = ''
$resolvedTunnelMode = 'none'
$existingInstallDetected = $false
$taskUser = $null
$taskState = [pscustomobject]@{
    Eligible = $false
    Exists = $false
    WasEnabled = $false
    WasRunning = $false
    SchedulerAvailable = $false
    SchedulerError = ''
}

# Initialize the result file before parameter, architecture, user, or ScheduledTasks probing.
# Inno Setup can therefore always show a concrete PowerShell failure instead of a bare exit code.
Write-InstallResult `
    -Path $ResultFile `
    -Success $false `
    -Message 'AgentDock installation is initializing.' `
    -InstalledVersion $Version `
    -LocalMCPUrl "http://127.0.0.1:$Port/mcp" `
    -PublicMCPUrl '' `
    -BearerToken '' `
    -OAuthLoginPassword '' `
    -HealthStatus 'initializing' `
    -PrivilegeMode $effectivePrivilegeMode

try {
    $Utf8NoBom = New-Object System.Text.UTF8Encoding($false)
    Add-Type -AssemblyName System.Security
    $setupRuntimeLauncherPath = Join-Path $PSScriptRoot 'launch-windows-process.ps1'

    if ($Port -lt 1 -or $Port -gt 65535) {
        throw 'Port must be between 1 and 65535.'
    }
    if ([string]::IsNullOrWhiteSpace($InstallDir)) {
        throw 'InstallDir is required.'
    }
    $userHome = [Environment]::GetFolderPath('UserProfile')
    if ([string]::IsNullOrWhiteSpace($userHome)) {
        throw 'Unable to resolve the current user profile directory.'
    }
    $architecture = Get-AgentDockArchitecture
} catch {
    Write-InstallResult `
        -Path $ResultFile `
        -Success $false `
        -Message $_.Exception.Message `
        -InstalledVersion $Version `
        -LocalMCPUrl "http://127.0.0.1:$Port/mcp" `
        -PublicMCPUrl '' `
        -BearerToken '' `
        -OAuthLoginPassword '' `
        -HealthStatus 'failed' `
        -PrivilegeMode $effectivePrivilegeMode `
        -ErrorCode 'install-validation-failed' `
        -ErrorRecord $_
    throw
}

$assetName = "agentdock_windows_$architecture.zip"
$releaseBaseUrl = ''
$tempRoot = Join-Path ([IO.Path]::GetTempPath()) ("agentdock-install-" + [Guid]::NewGuid().ToString('N'))
$archivePath = Join-Path $tempRoot $assetName
$checksumPath = "$archivePath.sha256"
$destinationBinary = Join-Path $InstallDir 'agentdock.exe'
$destinationTrayBinary = Join-Path $InstallDir 'agentdock-tray.exe'
$destinationTrayIcon = Join-Path $InstallDir 'agentdock.ico'
$cloudflaredBinary = Join-Path $InstallDir 'cloudflared.exe'
$binaryBackup = Join-Path $tempRoot 'agentdock.exe.previous'
$trayBackup = Join-Path $tempRoot 'agentdock-tray.exe.previous'
$trayIconBackup = Join-Path $tempRoot 'agentdock.ico.previous'
$cloudflaredBackup = Join-Path $tempRoot 'cloudflared.exe.previous'
$runtimeDir = Split-Path -Parent $InstallDir
$versionsDir = Join-Path $runtimeDir 'versions'
$activeVersionPath = Join-Path $runtimeDir 'active-version.json'
$installerDir = Join-Path $runtimeDir 'installer'
$managerScriptPath = Join-Path $installerDir 'manage-windows.ps1'
$launcherPath = Join-Path $runtimeDir 'start-agentdock.ps1'
$cloudflaredLauncherPath = Join-Path $runtimeDir 'start-cloudflared.ps1'
$tokenPath = Join-Path $runtimeDir 'auth-token.dpapi'
$oauthPasswordPath = Join-Path $runtimeDir 'oauth-password.dpapi'
$oauthTokenSecretPath = Join-Path $runtimeDir 'oauth-token-secret.dpapi'
$serverUrlPath = Join-Path $runtimeDir 'server-url.txt'
$namedServerUrlPath = Join-Path $runtimeDir 'named-server-url.txt'
$controlPanelSettingsPath = Join-Path $runtimeDir 'control-panel-settings.json'
$tunnelModePath = Join-Path $runtimeDir 'cloudflared-mode.txt'
$tunnelTokenPath = Join-Path $runtimeDir 'cloudflared-token.dpapi'
$runtimeManifestPath = Join-Path $runtimeDir 'runtime.json'
$runtimeAgentDockHome = [Environment]::GetEnvironmentVariable('AGENTDOCK_HOME', 'Process')
$runtimeAgentDockDefaultDir = [Environment]::GetEnvironmentVariable('AGENTDOCK_DEFAULT_DIR', 'Process')
if (([string]::IsNullOrWhiteSpace($runtimeAgentDockHome) -or [string]::IsNullOrWhiteSpace($runtimeAgentDockDefaultDir)) -and
    (Test-Path -LiteralPath $runtimeManifestPath -PathType Leaf)) {
    try {
        $existingRuntimeManifest = Get-Content -LiteralPath $runtimeManifestPath -Raw -Encoding UTF8 | ConvertFrom-Json
        if ([string]::IsNullOrWhiteSpace($runtimeAgentDockHome) -and
            $null -ne $existingRuntimeManifest.PSObject.Properties['agentdock_home']) {
            $runtimeAgentDockHome = [string] $existingRuntimeManifest.agentdock_home
        }
        if ([string]::IsNullOrWhiteSpace($runtimeAgentDockDefaultDir) -and
            $null -ne $existingRuntimeManifest.PSObject.Properties['agentdock_default_dir']) {
            $runtimeAgentDockDefaultDir = [string] $existingRuntimeManifest.agentdock_default_dir
        }
    } catch {
        # Runtime manifest validation later reports malformed state. Do not make path recovery
        # depend on a best-effort compatibility read here.
    }
}
if ([string]::IsNullOrWhiteSpace($runtimeAgentDockHome)) {
    $runtimeAgentDockHome = Join-Path $userHome '.agentdock'
}
if ([string]::IsNullOrWhiteSpace($runtimeAgentDockDefaultDir)) {
    $runtimeAgentDockDefaultDir = Join-Path $userHome 'AgentDock'
}
if (-not [IO.Path]::IsPathRooted($runtimeAgentDockHome) -or -not [IO.Path]::IsPathRooted($runtimeAgentDockDefaultDir)) {
    throw 'AGENTDOCK_HOME and AGENTDOCK_DEFAULT_DIR must resolve to absolute paths.'
}
$runtimeAgentDockHome = [IO.Path]::GetFullPath($runtimeAgentDockHome)
$runtimeAgentDockDefaultDir = [IO.Path]::GetFullPath($runtimeAgentDockDefaultDir)
$desktopVersionPath = Join-Path $runtimeDir 'desktop-version.txt'
$quickTunnelUrlPath = Join-Path $runtimeDir 'quick-tunnel-url.txt'
$cloudflaredStdoutLogPath = Join-Path $runtimeDir 'cloudflared.out.log'
$cloudflaredStderrLogPath = Join-Path $runtimeDir 'cloudflared.err.log'
$runtimeBackupDir = Join-Path $tempRoot 'runtime-backup'
$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$runValueName = $StartupValueName
$cloudflaredRunValueName = $CloudflaredStartupValueName
$trayRunValueName = $TrayStartupValueName
$processWasRunning = $false
$trayProcessWasRunning = $false
$cloudflaredProcessWasRunning = $false
$agentDockStopAttempted = $false
$trayStopAttempted = $false
$cloudflaredStopAttempted = $false
$rollbackStateCaptured = $false
$engineCommitted = $false
$enginePrepared = $false
$engineReady = $false
$engineTransactionId = ''
$binaryReplacementStarted = $false
$trayReplacementStarted = $false
$cloudflaredReplacementStarted = $false
$startupRegistrationChanged = $false
$trayStartupRegistrationChanged = $false
$tunnelStartupRegistrationChanged = $false
$previousRunValue = $null
$previousTrayRunValue = $null
$previousTunnelRunValue = $null
$taskBackupDirectory = Join-Path $tempRoot 'scheduled-task-backup'
$taskTransactionStarted = $false
$taskTransactionCommitted = $false
$taskRestored = $false
$generationLayoutDetected = $false
$engineOwnsTargetGeneration = $false
$generationBootstrapDirectory = ''
$generationRepairBackupDirectory = ''
$generationBootstrapPublished = $false
$activeVersionCreatedByBootstrap = $false

$managedRuntimeFiles = @(
    @{ Path = $managerScriptPath; Name = 'manage-windows.ps1' },
    @{ Path = $launcherPath; Name = 'start-agentdock.ps1' },
    @{ Path = $cloudflaredLauncherPath; Name = 'start-cloudflared.ps1' },
    @{ Path = $tokenPath; Name = 'auth-token.dpapi' },
    @{ Path = $oauthPasswordPath; Name = 'oauth-password.dpapi' },
    @{ Path = $oauthTokenSecretPath; Name = 'oauth-token-secret.dpapi' },
    @{ Path = $serverUrlPath; Name = 'server-url.txt' },
    @{ Path = $namedServerUrlPath; Name = 'named-server-url.txt' },
    @{ Path = $controlPanelSettingsPath; Name = 'control-panel-settings.json' },
    @{ Path = $tunnelModePath; Name = 'cloudflared-mode.txt' },
    @{ Path = $tunnelTokenPath; Name = 'cloudflared-token.dpapi' },
    @{ Path = $runtimeManifestPath; Name = 'runtime.json' },
    @{ Path = $desktopVersionPath; Name = 'desktop-version.txt' },
    @{ Path = $quickTunnelUrlPath; Name = 'quick-tunnel-url.txt' },
    @{ Path = $activeVersionPath; Name = 'active-version.json' },
    @{ Path = (Join-Path $runtimeDir 'update\transaction.json'); Name = 'update-transaction.json' },
    @{ Path = (Join-Path $runtimeDir 'update\result.json'); Name = 'update-result.json' }
)

try {
    $existingInstallDetected =
        (Test-Path -LiteralPath $destinationBinary -PathType Leaf) -or
        (Test-Path -LiteralPath $runtimeManifestPath -PathType Leaf) -or
        (Test-Path -LiteralPath $launcherPath -PathType Leaf)
    $taskUser = Get-CurrentTaskUser
    $taskState = Get-AgentDockTaskState `
        -AgentDockValueName $runValueName `
        -CloudflaredValueName $cloudflaredRunValueName `
        -TrayValueName $trayRunValueName `
        -RuntimeRoot $runtimeDir `
        -StableCorePath $destinationBinary `
        -StableTrayPath $destinationTrayBinary `
        -LegacyLauncherPath $launcherPath
    if ($effectivePrivilegeMode -eq 'elevated' -and -not $taskState.Eligible) {
        throw 'Elevated AgentDock mode requires the default Windows startup names.'
    }
    if ($effectivePrivilegeMode -eq 'elevated' -and $taskState.Conflicting) {
        throw 'The AgentDock scheduled task belongs to another installation root. Remove or repair that installation before enabling elevated mode here.'
    }

    $existingPrivilegeMode = ''
    if (Test-Path -LiteralPath $runtimeManifestPath -PathType Leaf) {
        try {
            $existingManifest = Get-Content -LiteralPath $runtimeManifestPath -Raw | ConvertFrom-Json
            $existingPrivilegeMode = [string] $existingManifest.privilege_mode
        } catch {
            $existingPrivilegeMode = ''
        }
    }
    if (-not $taskState.SchedulerAvailable -and
        ($effectivePrivilegeMode -eq 'elevated' -or $existingPrivilegeMode -eq 'elevated')) {
        $installErrorCode = 'task-scheduler-unavailable'
        throw "Windows Task Scheduler is required to preserve administrator-enhanced AgentDock mode: $($taskState.SchedulerError)"
    }

    $resolvedTunnelMode = Resolve-TunnelMode `
        -RequestedMode $TunnelMode `
        -ModePath $tunnelModePath `
        -StartupRequested ([bool] $RegisterStartup) `
        -PublicAccessRequested ([bool] $ConfigurePublicAccess)
    if ($resolvedTunnelMode -ne 'none' -or (Test-Path -LiteralPath $tunnelModePath -PathType Leaf)) {
        $RegisterStartup = $true
    }

    # Setup must stay in the signed-in desktop user's context so HKCU,
    # current-user DPAPI, and per-user Skill state remain on the right account.
    if ($InstallChannel -eq 'setup') {
        $interactiveUser = Get-InteractiveDesktopUser
        if ($null -ne $interactiveUser -and
            -not [string]::Equals(
                $interactiveUser.Sid,
                $taskUser.Sid,
                [StringComparison]::OrdinalIgnoreCase
            )) {
            $installErrorCode = 'setup-elevated-context'
            throw "AgentDock Setup is running as $($taskUser.Name), but the signed-in desktop user is $($interactiveUser.Name). Start Setup normally under the signed-in account; it requests administrator approval only for scheduled-task operations."
        }
    }

    New-Item -ItemType Directory -Path $tempRoot -Force | Out-Null
    New-Item -ItemType Directory -Path $runtimeBackupDir -Force | Out-Null
    foreach ($item in $managedRuntimeFiles) {
        Backup-FileState -Path $item.Path -Name $item.Name -BackupDirectory $runtimeBackupDir
    }
    $previousRunValue = Get-RunValue -RegistryPath $runKey -Name $runValueName
    $previousTrayRunValue = Get-RunValue -RegistryPath $runKey -Name $trayRunValueName
    $previousTunnelRunValue = Get-RunValue -RegistryPath $runKey -Name $cloudflaredRunValueName
    $rollbackStateCaptured = $true

    $existingTunnelMode = (Read-TextFile -Path $tunnelModePath).ToLowerInvariant()
    $existingActiveServerUrl = Read-TextFile -Path $serverUrlPath
    if ($existingTunnelMode -eq 'named' -and -not [string]::IsNullOrWhiteSpace($existingActiveServerUrl)) {
        Write-TextFile -Path $namedServerUrlPath -Value $existingActiveServerUrl
    }

    if (-not [string]::IsNullOrWhiteSpace($OfflineArchive)) {
        if (-not (Test-Path -LiteralPath $OfflineArchive -PathType Leaf)) {
            throw "Offline AgentDock archive was not found: $OfflineArchive"
        }
        if ([string]::IsNullOrWhiteSpace($OfflineChecksumFile) -or
            -not (Test-Path -LiteralPath $OfflineChecksumFile -PathType Leaf)) {
            throw "Offline AgentDock checksum file was not found: $OfflineChecksumFile"
        }
        Write-Host "Using bundled AgentDock payload: $OfflineArchive"
        Copy-Item -LiteralPath $OfflineArchive -Destination $archivePath -Force
        Copy-Item -LiteralPath $OfflineChecksumFile -Destination $checksumPath -Force
    } else {
        $releaseBaseUrl = Get-ReleaseBaseUrl -RequestedVersion $Version
        Invoke-WebRequest -UseBasicParsing -Uri "$releaseBaseUrl/$assetName" -OutFile $archivePath
        Invoke-WebRequest -UseBasicParsing -Uri "$releaseBaseUrl/$assetName.sha256" -OutFile $checksumPath
    }

    $expectedHash = ((Get-Content -LiteralPath $checksumPath -Raw).Trim() -split '\s+')[0].ToLowerInvariant()
    $actualHash = Get-Sha256Hex -Path $archivePath
    if ($actualHash -ne $expectedHash) {
        throw "SHA-256 mismatch for $assetName. Expected $expectedHash, got $actualHash."
    }

    $extractDir = Join-Path $tempRoot 'extract'
    Expand-Archive -LiteralPath $archivePath -DestinationPath $extractDir -Force
    $sourceBinary = Join-Path $extractDir 'agentdock.exe'
    if (-not (Test-Path -LiteralPath $sourceBinary -PathType Leaf)) {
        throw "Release archive does not contain agentdock.exe: $assetName"
    }
    $sourceTrayBinary = Join-Path $extractDir 'agentdock-tray.exe'
    $sourceTrayIcon = Join-Path $extractDir 'agentdock.ico'
    $sourceManagerScript = Join-Path $extractDir 'manage-windows.ps1'
    $sourceArbiter = Join-Path $extractDir 'agentdock-arbiter.exe'
    $sourceCoreShim = Join-Path $extractDir 'agentdock-shim.exe'
    $sourceTrayShim = Join-Path $extractDir 'agentdock-tray-shim.exe'
    if (-not (Test-Path -LiteralPath $sourceTrayBinary -PathType Leaf)) {
        throw "Release archive does not contain agentdock-tray.exe: $assetName"
    }
    if (-not (Test-Path -LiteralPath $sourceTrayIcon -PathType Leaf)) {
        throw "Release archive does not contain agentdock.ico: $assetName"
    }
    if (-not (Test-Path -LiteralPath $sourceManagerScript -PathType Leaf)) {
        throw "Release archive does not contain manage-windows.ps1: $assetName"
    }
    foreach ($generationFile in @($sourceArbiter, $sourceCoreShim, $sourceTrayShim)) {
        if (-not (Test-Path -LiteralPath $generationFile -PathType Leaf)) {
            throw "Release archive does not contain generation update component: $generationFile"
        }
    }
    $coreSkillBundle = Join-Path $extractDir 'share\agentdock\core-skills'
    $coreSkillManifest = Join-Path $coreSkillBundle 'manifest.json'
    if (-not (Test-Path -LiteralPath $coreSkillBundle -PathType Container) -or
        -not (Test-Path -LiteralPath $coreSkillManifest -PathType Leaf)) {
        throw "Release archive does not contain a valid core Skill Bundle: $assetName"
    }
    $sourceWSLHelperDir = Join-Path $extractDir 'wsl-helper'
    $sourceWSLHelperManifestPath = Join-Path $sourceWSLHelperDir 'manifest.json'
    $sourceWSLHelperAMD64 = Join-Path $sourceWSLHelperDir 'agentdock-wsl-helper-linux-amd64'
    $sourceWSLHelperARM64 = Join-Path $sourceWSLHelperDir 'agentdock-wsl-helper-linux-arm64'
    if (-not (Test-Path -LiteralPath $sourceWSLHelperDir -PathType Container) -or
        -not (Test-Path -LiteralPath $sourceWSLHelperManifestPath -PathType Leaf) -or
        -not (Test-Path -LiteralPath $sourceWSLHelperAMD64 -PathType Leaf) -or
        -not (Test-Path -LiteralPath $sourceWSLHelperARM64 -PathType Leaf)) {
        throw "Release archive does not contain a valid WSL helper payload: $assetName"
    }
    try {
        $wslHelperManifest = Get-Content -LiteralPath $sourceWSLHelperManifestPath -Raw | ConvertFrom-Json
        if ([string] $wslHelperManifest.protocol_version -ne '1') {
            throw 'WSL helper manifest protocol_version must be 1.'
        }
        $helperChecks = @(
            @{
                Architecture = 'amd64'
                ExpectedName = 'agentdock-wsl-helper-linux-amd64'
                Path = $sourceWSLHelperAMD64
                Entry = $wslHelperManifest.helpers.amd64
            },
            @{
                Architecture = 'arm64'
                ExpectedName = 'agentdock-wsl-helper-linux-arm64'
                Path = $sourceWSLHelperARM64
                Entry = $wslHelperManifest.helpers.arm64
            }
        )
        foreach ($helperCheck in $helperChecks) {
            if ([string] $helperCheck.Entry.file -ne [string] $helperCheck.ExpectedName) {
                throw "WSL helper manifest file mismatch for $($helperCheck.Architecture)."
            }
            $expectedHelperHash = ([string] $helperCheck.Entry.sha256).Trim().ToLowerInvariant()
            $actualHelperHash = Get-Sha256Hex -Path $helperCheck.Path
            if ($expectedHelperHash -notmatch '^[0-9a-f]{64}$' -or $actualHelperHash -ne $expectedHelperHash) {
                throw "WSL helper SHA-256 mismatch for $($helperCheck.Architecture)."
            }
        }
    } catch {
        throw "Release archive contains an invalid WSL helper manifest: $($_.Exception.Message)"
    }

    # Validate the unpacked payload before stopping processes or touching an existing scheduled task.
    # This catches architecture/runtime incompatibility while the previous installation is still intact.
    $preflightVersionOutput = @(& $sourceBinary version --json 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "AgentDock payload preflight failed with exit code $LASTEXITCODE."
    }
    try {
        $preflightVersionInfo = ($preflightVersionOutput | Out-String) | ConvertFrom-Json
    } catch {
        throw "AgentDock payload preflight returned invalid version metadata: $($_.Exception.Message)"
    }
    if ($null -eq $preflightVersionInfo -or [string]::IsNullOrWhiteSpace([string] $preflightVersionInfo.version)) {
        throw 'AgentDock payload preflight did not return a version.'
    }

    $payloadVersion = 'v' + ([string] $preflightVersionInfo.version).TrimStart('v')
    if (Test-Path -LiteralPath $sourceBinary -PathType Leaf) {
        $engineReadyOutput = & $sourceBinary install --engine-ready 2>$null
        if ($LASTEXITCODE -eq 0 -and ("$engineReadyOutput" -like '*agentdock-installer-engine*')) {
            $engineReady = $true
        }
    }
    $generationBootstrapDirectory = Join-Path $versionsDir $payloadVersion
    $generationCorePath = Join-Path $generationBootstrapDirectory 'agentdock-core.exe'
    $generationTrayPath = Join-Path $generationBootstrapDirectory 'agentdock-tray.exe'
    $generationArbiterPath = Join-Path $generationBootstrapDirectory 'agentdock-arbiter.exe'
    $generationSkillsPath = Join-Path $generationBootstrapDirectory 'core-skills'

    $existingActiveVersion = ''
    if (Test-Path -LiteralPath $activeVersionPath -PathType Leaf) {
        try {
            $activeState = Get-Content -LiteralPath $activeVersionPath -Raw | ConvertFrom-Json
            if ([int] $activeState.schema_version -ne 1 -or [string]::IsNullOrWhiteSpace([string] $activeState.active_version)) {
                throw 'active-version.json has an unsupported schema.'
            }

            # Setup must never repair or replace a generation while an update trial is unresolved.
            # Running the stable CUI entry is also the crash-recovery trigger: a live Arbiter leaves
            # the state in trial and Setup stops safely; an abandoned trial is rolled back first.
            # Fresh installer trials use install/transaction.json. The shim only recovers
            # update/transaction.json, so an installer trial is not a committed generation layout.
            $activeStateName = ([string] $activeState.state).Trim().ToLowerInvariant()
            if ($activeStateName -ne 'committed') {
                $updateTransactionPath = Join-Path $runtimeDir 'update\transaction.json'
                if (-not (Test-Path -LiteralPath $updateTransactionPath -PathType Leaf)) {
                    Write-Host 'Incomplete installer generation pointer; Setup will let the Installer Engine recover.'
                    $existingActiveVersion = ''
                    $generationLayoutDetected = $false
                } else {
                    if (-not (Test-Path -LiteralPath $destinationBinary -PathType Leaf)) {
                        throw "AgentDock generation state is '$activeStateName', but the stable recovery entry is missing: $destinationBinary"
                    }
                    Write-Host "Resolving pending AgentDock generation transaction before Setup continues..."
                    $recoveryOutput = @(& $destinationBinary version --json 2>&1)
                    $recoveryExitCode = $LASTEXITCODE
                    if ($recoveryExitCode -ne 0) {
                        $recoveryText = (($recoveryOutput | Out-String).Trim())
                        throw "AgentDock generation recovery failed with exit code $recoveryExitCode. $recoveryText"
                    }
                    $activeState = Get-Content -LiteralPath $activeVersionPath -Raw | ConvertFrom-Json
                    $activeStateName = ([string] $activeState.state).Trim().ToLowerInvariant()
                    if ([int] $activeState.schema_version -ne 1 -or
                        [string]::IsNullOrWhiteSpace([string] $activeState.active_version) -or
                        $activeStateName -ne 'committed') {
                        throw "AgentDock generation transaction is still '$activeStateName'; Setup will not modify an unresolved generation."
                    }
                    $existingActiveVersion = 'v' + ([string] $activeState.active_version).TrimStart('v')
                    $generationLayoutDetected = $true
                }
            } else {
                $existingActiveVersion = 'v' + ([string] $activeState.active_version).TrimStart('v')
                $generationLayoutDetected = $true
            }
        } catch {
            throw "Unable to read the existing AgentDock generation state: $($_.Exception.Message)"
        }
    }

    if ($generationLayoutDetected) {
        $existingGenerationDirectory = Join-Path $versionsDir $existingActiveVersion
        $existingGenerationCore = Join-Path $existingGenerationDirectory 'agentdock-core.exe'
        $existingGenerationTray = Join-Path $existingGenerationDirectory 'agentdock-tray.exe'
        $processWasRunning = @(Get-AgentDockProcesses -BinaryPath $existingGenerationCore).Count -gt 0
        $trayProcessWasRunning = @(Get-AgentDockTrayProcesses -BinaryPath $existingGenerationTray).Count -gt 0
    }

    # Setup upgrades and fresh installs share one Installer transaction. Do not pre-commit
    # the target generation through Update Engine before Task/Registry/runtime adapter work.

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    if (-not $generationLayoutDetected) {
        $processWasRunning = @(Get-AgentDockProcesses -BinaryPath $destinationBinary).Count -gt 0
    }
    if ($effectivePrivilegeMode -eq 'elevated' -or $taskState.Exists) {
        $taskAction = if ($effectivePrivilegeMode -eq 'elevated') { 'prepare-elevated' } else { 'prepare-standard' }
        $taskActionResult = Start-ElevatedAgentDockTaskAction `
            -Action $taskAction `
            -BackupDirectory $taskBackupDirectory `
            -AdminLauncherPath $sourceTrayBinary `
            -LauncherPath $destinationBinary `
            -RuntimeRoot $runtimeDir `
            -TaskUser $taskUser
        if (-not $taskActionResult.Started) {
            if ($effectivePrivilegeMode -eq 'elevated' -and -not $taskState.Exists) {
                # No scheduled-task state changed because RunAs never started. A fresh install can
                # therefore continue safely without administrator-enhanced core mode.
                $effectivePrivilegeMode = 'standard'
                $installWarningCode = 'elevated-mode-fallback'
                $installWarningMessage = 'Windows did not start the administrator approval request. AgentDock continued in standard user mode.'
                Write-Warning "$installWarningMessage Details: $($taskActionResult.ErrorMessage)"
            } else {
                throw "Administrator approval for AgentDock was not completed: $($taskActionResult.ErrorMessage)"
            }
        } else {
            # Once the elevated helper actually started, task state may have changed even if the helper
            # later reports failure. Mark the transaction before checking its exit status so rollback runs.
            $taskTransactionStarted = $true
            if (-not $taskActionResult.Succeeded) {
                throw $taskActionResult.ErrorMessage
            }
            Write-Host "Prepared AgentDock scheduled task transaction: $taskAction"
        }
    }
    $agentDockStopAttempted = $true
    $coreToStop = $(if ($generationLayoutDetected) { $existingGenerationCore } else { $destinationBinary })
    [void] (Stop-AgentDockForUpgrade -BinaryPath $coreToStop)

    # Stable entries remain the rollback boundary for both legacy bootstrap and same-version repair.
    # Preserve them before replacement even when active-version.json already exists.
    if (Test-Path -LiteralPath $destinationBinary -PathType Leaf) {
        Copy-Item -LiteralPath $destinationBinary -Destination $binaryBackup -Force
    }
    if (Test-Path -LiteralPath $destinationTrayBinary -PathType Leaf) {
        Copy-Item -LiteralPath $destinationTrayBinary -Destination $trayBackup -Force
    }

    if (-not $generationLayoutDetected) {
        $trayProcessWasRunning = @(Get-AgentDockTrayProcesses -BinaryPath $destinationTrayBinary).Count -gt 0
    }

    $trayStopAttempted = $true
    $trayToStop = $(if ($generationLayoutDetected) { $existingGenerationTray } else { $destinationTrayBinary })
    [void] (Stop-AgentDockTrayForUpgrade -BinaryPath $trayToStop)
    if (Test-Path -LiteralPath $destinationTrayIcon -PathType Leaf) {
        Copy-Item -LiteralPath $destinationTrayIcon -Destination $trayIconBackup -Force
    }

    # Engine-ready fresh installs and cross-version upgrades have one generation owner: Go Installer Engine.
    # PowerShell only publishes for legacy payloads and same-version repair of an already committed layout.
    $engineOwnsTargetGeneration = $engineReady -and (
        (-not $generationLayoutDetected) -or
        (-not [string]::Equals($existingActiveVersion, $payloadVersion, [StringComparison]::OrdinalIgnoreCase))
    )
    if (-not $engineOwnsTargetGeneration) {
        # Stage the complete immutable generation first. The stable entries are only replaced after
        # core/tray/arbiter/skills are all present, so bootstrap never points at a partial generation.
        $generationStagingDirectory = Join-Path $versionsDir ('.bootstrap-' + [Guid]::NewGuid().ToString('N'))
        New-Item -ItemType Directory -Path $generationStagingDirectory -Force | Out-Null
        try {
            Copy-Item -LiteralPath $sourceBinary -Destination (Join-Path $generationStagingDirectory 'agentdock-core.exe') -Force
            Copy-Item -LiteralPath $sourceTrayBinary -Destination (Join-Path $generationStagingDirectory 'agentdock-tray.exe') -Force
            Copy-Item -LiteralPath $sourceArbiter -Destination (Join-Path $generationStagingDirectory 'agentdock-arbiter.exe') -Force
            Copy-Item -LiteralPath $coreSkillBundle -Destination (Join-Path $generationStagingDirectory 'core-skills') -Recurse -Force
            Copy-Item -LiteralPath $sourceWSLHelperDir -Destination (Join-Path $generationStagingDirectory 'wsl-helper') -Recurse -Force

            if (Test-Path -LiteralPath $generationBootstrapDirectory) {
                if (-not $generationLayoutDetected) {
                    # Leftover from a crashed fresh bootstrap. No committed pointer exists, so this
                    # directory is not a known-good generation and must not block retry.
                    Remove-Item -LiteralPath $generationBootstrapDirectory -Recurse -Force
                } else {
                    # Same-version Setup is a repair transaction. Move the active generation aside instead
                    # of deleting it so any later configuration/activation failure can restore it exactly.
                    $generationRepairBackupDirectory = Join-Path $versionsDir ('.repair-backup-' + [Guid]::NewGuid().ToString('N'))
                    Move-Item -LiteralPath $generationBootstrapDirectory -Destination $generationRepairBackupDirectory
                }
            }
            Move-Item -LiteralPath $generationStagingDirectory -Destination $generationBootstrapDirectory
            if (-not $generationLayoutDetected) {
                # Record publication immediately. A later shim/configuration failure must clean the
                # generation even when active-version.json has not been written yet.
                $generationBootstrapPublished = $true
            }
        } finally {
            Remove-Item -LiteralPath $generationStagingDirectory -Recurse -Force -ErrorAction SilentlyContinue
        }
    }

    # The two fixed entries intentionally have different PE subsystems. The CUI entry owns CLI/
    # Task Scheduler semantics; the GUI entry owns tray/Start-menu semantics without console flash.
    $binaryReplacementStarted = $true
    Install-AgentDockBinary -SourceBinary $sourceCoreShim -DestinationBinary $destinationBinary
    $trayReplacementStarted = $true
    Install-AgentDockBinary -SourceBinary $sourceTrayShim -DestinationBinary $destinationTrayBinary
    Copy-Item -LiteralPath $sourceTrayIcon -Destination $destinationTrayIcon -Force

    if (-not $generationLayoutDetected -and -not $engineOwnsTargetGeneration) {
        # Legacy first-publish still records the bootstrap so rollback can delete the
        # generation it just moved. Engine fresh path leaves this false: abandon owns it.
        $activeVersionCreatedByBootstrap = $true
        $generationLayoutDetected = $true
    }

    New-Item -ItemType Directory -Path $installerDir -Force | Out-Null
    Copy-Item -LiteralPath $sourceManagerScript -Destination $managerScriptPath -Force

    if ($engineOwnsTargetGeneration) {
        # The stable shim intentionally refuses a trial pointer, so pre-commit version reporting must
        # use the payload version already verified above rather than executing the shim too early.
        Write-TextFile -Path $desktopVersionPath -Value $payloadVersion
    } else {
        $installedVersionJson = & $destinationBinary version --json
        if ($LASTEXITCODE -ne 0) {
            throw 'Unable to read the installed AgentDock version after replacing the Windows payload.'
        }
        $installedVersionInfo = $installedVersionJson | ConvertFrom-Json
        if ($null -eq $installedVersionInfo -or [string]::IsNullOrWhiteSpace([string] $installedVersionInfo.version)) {
            throw 'Unable to read the installed AgentDock version after replacing the Windows payload.'
        }
        Write-TextFile -Path $desktopVersionPath -Value ("v" + ([string] $installedVersionInfo.version).TrimStart('v'))
    }
    Add-UserPath -Directory $InstallDir

    $agentDockHome = Join-Path $userHome '.agentdock'
    $workspace = Join-Path $userHome 'AgentDock'
    foreach ($directory in @($agentDockHome, $workspace)) {
        New-Item -ItemType Directory -Path $directory -Force | Out-Null
    }

    $cloudflaredProcessWasRunning = @(Get-CloudflaredProcesses -BinaryPath $cloudflaredBinary).Count -gt 0
    $cloudflaredStopAttempted = $true
    [void] (Stop-CloudflaredForUpgrade -BinaryPath $cloudflaredBinary)
    if (Test-Path -LiteralPath $cloudflaredBinary -PathType Leaf) {
        Copy-Item -LiteralPath $cloudflaredBinary -Destination $cloudflaredBackup -Force
    }
    $cloudflaredReplacementStarted = $true
    Install-CloudflaredBinary `
        -DestinationBinary $cloudflaredBinary `
        -Architecture $architecture `
        -TempDirectory $tempRoot `
        -SourceBinary $OfflineCloudflaredBinary

    $publicUrl = ''
    $manifestPublicUrl = ''
    $engineCommitted = $false
    $enginePrepared = $false
    $engineTransactionId = ''
    if ($RegisterStartup) {
        New-Item -ItemType Directory -Path $runtimeDir -Force | Out-Null

        $existingAuthToken = Read-ProtectedText -Path $tokenPath -Entropy 'agentdock.startup.v1'
        if ([string]::IsNullOrWhiteSpace($AuthToken)) {
            $AuthToken = $existingAuthToken
        }
        if ([string]::IsNullOrWhiteSpace($AuthToken)) {
            $AuthToken = New-AgentDockToken
        }
        if (-not [string]::Equals($AuthToken, $existingAuthToken, [StringComparison]::Ordinal)) {
            Write-ProtectedText -Path $tokenPath -Value $AuthToken -Entropy 'agentdock.startup.v1'
        }

        $oauthCredentials = Initialize-OAuthCredentials `
            -PasswordPath $oauthPasswordPath `
            -TokenSecretPath $oauthTokenSecretPath `
            -RequestedPassword $OAuthPassword `
            -RequestedTokenSecret $OAuthTokenSecret
        $OAuthPassword = $oauthCredentials.Password
        $OAuthTokenSecret = $oauthCredentials.TokenSecret

        if ($resolvedTunnelMode -ne 'none') {
            $existingServerUrl = Read-TextFile -Path $serverUrlPath
            if ([string]::IsNullOrWhiteSpace($ServerUrl)) {
                $ServerUrl = [Environment]::GetEnvironmentVariable('AGENTDOCK_SERVER_URL')
            }
            if ([string]::IsNullOrWhiteSpace($ServerUrl)) {
                $ServerUrl = $existingServerUrl
            }
            if ($resolvedTunnelMode -eq 'named' -and [string]::IsNullOrWhiteSpace($ServerUrl)) {
                $ServerUrl = Read-TextFile -Path $namedServerUrlPath
            }
            if ($resolvedTunnelMode -eq 'quick') {
                $ServerUrl = ''
            }
            if ($resolvedTunnelMode -eq 'named') {
                if ([string]::IsNullOrWhiteSpace($ServerUrl)) {
                    $ServerUrl = Read-Host 'Fixed HTTPS public origin, for example https://agent.example.com'
                }
                $ServerUrl = Normalize-ServerUrl -Value $ServerUrl
                Write-TextFile -Path $namedServerUrlPath -Value $ServerUrl

                $existingTunnelToken = Read-ProtectedText -Path $tunnelTokenPath -Entropy 'agentdock.cloudflare.tunnel.v1'
                if ([string]::IsNullOrWhiteSpace($TunnelToken) -and -not [string]::IsNullOrWhiteSpace($TunnelTokenFile)) {
                    $TunnelToken = Read-SecretFile -Path $TunnelTokenFile
                }
                if ([string]::IsNullOrWhiteSpace($TunnelToken)) {
                    $TunnelToken = [Environment]::GetEnvironmentVariable('AGENTDOCK_CLOUDFLARE_TUNNEL_TOKEN')
                }
                if ([string]::IsNullOrWhiteSpace($TunnelToken)) {
                    $TunnelToken = $existingTunnelToken
                }
                if ([string]::IsNullOrWhiteSpace($TunnelToken)) {
                    $secureTunnelToken = Read-Host 'Cloudflare Tunnel Token' -AsSecureString
                    $credential = New-Object System.Management.Automation.PSCredential('token', $secureTunnelToken)
                    $TunnelToken = $credential.GetNetworkCredential().Password
                }
                if ([string]::IsNullOrWhiteSpace($TunnelToken)) {
                    throw 'A fixed Cloudflare hostname requires a Tunnel Token.'
                }
                if (-not [string]::Equals($TunnelToken, $existingTunnelToken, [StringComparison]::Ordinal)) {
                    Write-ProtectedText -Path $tunnelTokenPath -Value $TunnelToken -Entropy 'agentdock.cloudflare.tunnel.v1'
                }
            }
            Write-TextFile -Path $serverUrlPath -Value $ServerUrl
            Write-TextFile -Path $tunnelModePath -Value $resolvedTunnelMode
        } else {
            Write-TextFile -Path $serverUrlPath -Value ''
            Write-TextFile -Path $tunnelModePath -Value 'none'
            Remove-Item -LiteralPath $quickTunnelUrlPath -Force -ErrorAction SilentlyContinue
        }

        $escapedBinaryPath = $destinationBinary.Replace("'", "''")
        $escapedRuntimeDir = $runtimeDir.Replace("'", "''")
        # Keep the compatibility launcher only for legacy updates and rollback. New startup entries use the WinExe tray proxy.
        $launcher = @"
`$ErrorActionPreference = 'Stop'
& '$escapedBinaryPath' service launch-core --runtime-root '$escapedRuntimeDir'
exit `$LASTEXITCODE
"@
        [IO.File]::WriteAllText($launcherPath, $launcher, $Utf8NoBom)

        $manifestTunnelMode = $resolvedTunnelMode
        $manifestPublicUrl = ''
        if ($resolvedTunnelMode -eq 'named') {
            $manifestPublicUrl = $ServerUrl
        }
        $publicUrl = $manifestPublicUrl
        $localMCPUrl = "http://127.0.0.1:$Port/mcp"
        if ($effectivePrivilegeMode -eq 'elevated') {
            Remove-ItemProperty -LiteralPath $runKey -Name $runValueName -ErrorAction SilentlyContinue
            Enable-AgentDockTask
        } else {
            $startupCommand = "`"$destinationTrayBinary`" --start-core --runtime-root `"$runtimeDir`""
            Set-RunValue -RegistryPath $runKey -Name $runValueName -Value $startupCommand
        }
        $startupRegistrationChanged = $true
        $trayStartupCommand = "`"$destinationTrayBinary`" --background"
        Set-RunValue -RegistryPath $runKey -Name $trayRunValueName -Value $trayStartupCommand
        $trayStartupRegistrationChanged = $true

        if ($resolvedTunnelMode -ne 'none') {
            # Compatibility launcher delegates to the native Tunnel supervisor.
            # Normal startup still uses the WinExe tray proxy.
            $cloudflaredLauncher = @"
`$ErrorActionPreference = 'Stop'
& '$escapedBinaryPath' tunnel launch --runtime-root '$escapedRuntimeDir'
exit `$LASTEXITCODE
"@
            [IO.File]::WriteAllText($cloudflaredLauncherPath, $cloudflaredLauncher, $Utf8NoBom)
            [IO.File]::WriteAllText($cloudflaredStdoutLogPath, '', $Utf8NoBom)
            [IO.File]::WriteAllText($cloudflaredStderrLogPath, '', $Utf8NoBom)

            $cloudflaredStartupCommand = "`"$destinationTrayBinary`" --start-tunnel --runtime-root `"$runtimeDir`""
            Set-RunValue -RegistryPath $runKey -Name $cloudflaredRunValueName -Value $cloudflaredStartupCommand
            $tunnelStartupRegistrationChanged = $true
        } else {
            Remove-ItemProperty -LiteralPath $runKey -Name $cloudflaredRunValueName -ErrorAction SilentlyContinue
            $tunnelStartupRegistrationChanged = $true
            Write-TextFile -Path $tunnelModePath -Value 'none'
            Write-TextFile -Path $serverUrlPath -Value ''
            Remove-Item -LiteralPath $quickTunnelUrlPath -Force -ErrorAction SilentlyContinue
        }

        if (-not $engineReady) {
            Write-RuntimeManifest `
                -Path $runtimeManifestPath `
                -InstallRoot $runtimeDir `
                -AgentDockHome $runtimeAgentDockHome `
                -AgentDockDefaultDir $runtimeAgentDockDefaultDir `
                -AgentDockBinary $destinationBinary `
                -TrayBinary $destinationTrayBinary `
                -AgentDockLauncher $launcherPath `
                -AgentDockTaskName $(if ($effectivePrivilegeMode -eq 'elevated') { 'AgentDock' } else { '' }) `
                -PrivilegeMode $effectivePrivilegeMode `
                -CloudflaredBinary $cloudflaredBinary `
                -CloudflaredLauncher $cloudflaredLauncherPath `
                -CoreStartupValueName $runValueName `
                -TrayStartupValueName $trayRunValueName `
                -TunnelStartupValueName $cloudflaredRunValueName `
                -RuntimePort $Port `
                -RuntimeTunnelMode $manifestTunnelMode `
                -RuntimePublicUrl $manifestPublicUrl `
                -Channel $InstallChannel
        }
    }

    if ($engineReady) {
        # HKCU/Task (if any) are already written. Engine owns runtime.json/skills/start.
        # committed is written only after this script finishes adapter work and calls install commit.
        # A fresh or different-version generation is published here from --payload-dir; PowerShell does not pre-commit it.
        New-Item -ItemType Directory -Path $runtimeDir -Force | Out-Null
        $engineArgs = @(
            'install',
            '--install-root', $runtimeDir,
            '--payload-dir', $extractDir,
            '--host', '127.0.0.1',
            '--port', "$Port",
            '--tunnel-mode', $resolvedTunnelMode,
            '--privilege-mode', $effectivePrivilegeMode,
            '--agentdock-home', $runtimeAgentDockHome,
            '--agentdock-default-dir', $runtimeAgentDockDefaultDir,
            '--channel', $InstallChannel,
            '--defer-commit'
        )
        if ((-not $RegisterStartup) -or ($InstallChannel -eq 'setup' -and -not $existingInstallDetected)) {
            $engineArgs += @('--no-start', '--skip-health')
        }
        if (-not [string]::IsNullOrWhiteSpace($payloadVersion)) {
            $engineArgs += @('--version', $payloadVersion)
        }
        if (-not [string]::IsNullOrWhiteSpace($coreSkillBundle)) {
            $engineArgs += @('--skill-bundle', $coreSkillBundle)
        }
        if (-not [string]::IsNullOrWhiteSpace($manifestPublicUrl)) {
            $engineArgs += @('--server-url', $manifestPublicUrl)
        }
        $engineJson = (& $sourceBinary @engineArgs 2>$null | Out-String).Trim()
        if ($LASTEXITCODE -ne 0) {
            throw 'Installer Engine failed to write the runtime generation and manifest.'
        }
        # Engine already left a trial. Catch must abandon even if the JSON handshake is unreadable.
        $enginePrepared = $true
        try {
            $engineResult = $engineJson | ConvertFrom-Json
        } catch {
            throw "Installer Engine returned invalid JSON: $($_.Exception.Message)"
        }
        $engineTransactionId = [string] $engineResult.transaction_id
        if ([string]::IsNullOrWhiteSpace($engineTransactionId)) {
            throw 'Installer Engine did not return a transaction id.'
        }
    }

    if (-not $RegisterStartup) {
        Remove-ItemProperty -LiteralPath $runKey -Name $trayRunValueName -ErrorAction SilentlyContinue
        $trayStartupRegistrationChanged = $true
    }

    $mustRestartExistingProcess = (-not $RegisterStartup) -and $processWasRunning

    $localMCPUrl = "http://127.0.0.1:$Port/mcp"
    if ((-not $RegisterStartup) -and (-not $engineReady)) {
        Write-RuntimeManifest `
            -Path $runtimeManifestPath `
            -InstallRoot $runtimeDir `
            -AgentDockHome $runtimeAgentDockHome `
            -AgentDockDefaultDir $runtimeAgentDockDefaultDir `
            -AgentDockBinary $destinationBinary `
            -TrayBinary $destinationTrayBinary `
            -AgentDockLauncher $launcherPath `
            -AgentDockTaskName $(if ($effectivePrivilegeMode -eq 'elevated') { 'AgentDock' } else { '' }) `
            -PrivilegeMode $effectivePrivilegeMode `
            -CloudflaredBinary $cloudflaredBinary `
            -CloudflaredLauncher $cloudflaredLauncherPath `
            -CoreStartupValueName $runValueName `
            -TrayStartupValueName $trayRunValueName `
            -TunnelStartupValueName $cloudflaredRunValueName `
            -RuntimePort $Port `
            -RuntimeTunnelMode $resolvedTunnelMode `
            -RuntimePublicUrl $publicUrl `
            -Channel $InstallChannel
    }

    if ($engineReady) {
        Write-Host 'Core Skills were installed by the Installer Engine.'
        $coreSkillExitCode = 0
        $coreSkillOutputText = ''
    } else {
    Write-Host 'Installing official core Skills...'
    $coreSkillOutput = @(& $destinationBinary skill bootstrap --bundle $coreSkillBundle 2>&1)
    $coreSkillExitCode = $LASTEXITCODE
    $coreSkillOutputText = (($coreSkillOutput | Out-String).Trim())
    if (-not [string]::IsNullOrWhiteSpace($coreSkillOutputText)) {
        Write-Host $coreSkillOutputText
    }
    if ($coreSkillExitCode -ne 0) {
        $installErrorCode = 'core-skill-bootstrap'
        if ($coreSkillOutputText.Length -gt 2000) {
            $coreSkillOutputText = $coreSkillOutputText.Substring($coreSkillOutputText.Length - 2000)
        }
        if ([string]::IsNullOrWhiteSpace($coreSkillOutputText)) {
            throw "Core Skill bootstrap failed with exit code $coreSkillExitCode."
        }
        throw "Core Skill bootstrap failed with exit code $coreSkillExitCode`: $coreSkillOutputText"
    }
    }

    # Provision is complete here. A fresh Installer-owned generation must become committed before
    # the stable shim can be used for optional immediate activation; outer rollback can still abandon
    # this transaction because the committed pointer keeps the Installer transaction id.
    if ($enginePrepared -and -not $existingInstallDetected) {
        & $sourceBinary install commit --install-root $runtimeDir --runtime-root $runtimeDir --transaction-id $engineTransactionId 1>$null
        if ($LASTEXITCODE -ne 0) {
            throw 'Installer Engine failed to commit the fresh install transaction.'
        }
        $engineCommitted = $true
    }

    # Immediate activation is a separate phase; only a fresh standard install may defer activation,
    # because an upgrade must still be able to roll back to its prior runtime.
    $healthStatus = 'not-started'
    $engineOwnsActivation = $engineReady -and -not ($InstallChannel -eq 'setup' -and -not $existingInstallDetected)
    try {
        if ($InstallChannel -eq 'setup' -and -not $taskState.SchedulerAvailable -and
            ($RegisterStartup -or $mustRestartExistingProcess -or $trayProcessWasRunning)) {
            throw "Windows Task Scheduler is unavailable for immediate Setup activation: $($taskState.SchedulerError)"
        }

        if ($engineOwnsActivation -and $RegisterStartup) {
            $healthStatus = 'healthy'
            if ($resolvedTunnelMode -eq 'quick') {
                $publicUrl = Read-TextFile -Path $quickTunnelUrlPath
                if ([string]::IsNullOrWhiteSpace($publicUrl)) {
                    throw 'Installer Engine finished trial without a Quick Tunnel public address.'
                }
            } elseif ($resolvedTunnelMode -eq 'named') {
                $publicUrl = $ServerUrl
            }
        } elseif ($RegisterStartup) {
            if ($effectivePrivilegeMode -eq 'elevated') {
                Start-AgentDockTask -ManagerScriptPath $managerScriptPath
            } elseif ($InstallChannel -eq 'setup') {
                Invoke-SetupRuntimeProcess `
                    -FilePath $destinationBinary `
                    -Arguments "service start --runtime-root `"$runtimeDir`"" `
                    -WaitForExit
            } else {
                & $destinationBinary service start --runtime-root $runtimeDir
                if ($LASTEXITCODE -ne 0) {
                    throw "AgentDock native service start failed with exit code $LASTEXITCODE."
                }
            }
            Wait-AgentDockHealth -HealthPort $Port
            $healthStatus = 'healthy'

            if ($resolvedTunnelMode -ne 'none') {
                if ($InstallChannel -eq 'setup') {
                    Invoke-SetupRuntimeProcess `
                        -FilePath $destinationBinary `
                        -Arguments "tunnel start --runtime-root `"$runtimeDir`"" `
                        -WaitForExit
                } else {
                    & $destinationBinary tunnel start --runtime-root $runtimeDir
                    if ($LASTEXITCODE -ne 0) {
                        throw "AgentDock native Tunnel start failed with exit code $LASTEXITCODE."
                    }
                }

                if ($resolvedTunnelMode -eq 'quick') {
                    $publicUrl = Read-TextFile -Path $quickTunnelUrlPath
                    if ([string]::IsNullOrWhiteSpace($publicUrl)) {
                        $publicUrl = Wait-QuickTunnelUrl -LogPaths @($cloudflaredStdoutLogPath, $cloudflaredStderrLogPath)
                    }
                    Wait-QuickTunnelReady -Path $quickTunnelUrlPath -ExpectedUrl $publicUrl
                } else {
                    $publicUrl = $ServerUrl
                    Wait-CloudflaredRunning -BinaryPath $cloudflaredBinary
                }
            }
        } elseif ($mustRestartExistingProcess) {
            if ($InstallChannel -eq 'setup') {
                Invoke-SetupRuntimeProcess `
                    -FilePath $destinationBinary `
                    -Arguments "service start --runtime-root `"$runtimeDir`"" `
                    -WaitForExit
            } else {
                & $destinationBinary service start --runtime-root $runtimeDir
                if ($LASTEXITCODE -ne 0) {
                    throw "AgentDock native service restart failed with exit code $LASTEXITCODE."
                }
            }
            Wait-AgentDockHealth -HealthPort $Port
            $healthStatus = 'healthy'
        }

        if ($RegisterStartup -or $trayProcessWasRunning) {
            Start-AgentDockTray -BinaryPath $destinationTrayBinary
        }
    } catch {
        if ($existingInstallDetected -or $effectivePrivilegeMode -ne 'standard') {
            throw
        }

        $healthStatus = 'deferred'
        $activationWarningMessage = 'AgentDock was installed and startup was configured, but immediate runtime activation or verification did not complete. Start AgentDock from the Start menu or sign in again to retry.'
        if ([string]::IsNullOrWhiteSpace($installWarningMessage)) {
            $installWarningMessage = $activationWarningMessage
        } else {
            $installWarningMessage = ($installWarningMessage + ' ' + $activationWarningMessage).Trim()
        }
        if ([string]::IsNullOrWhiteSpace($installWarningCode)) {
            $installWarningCode = 'runtime-launch-deferred'
        } else {
            $installWarningCode = "$installWarningCode,runtime-launch-deferred"
        }
        Write-Warning "$activationWarningMessage Details: $($_.Exception.Message)"
    }

    if ($enginePrepared -and -not $engineCommitted) {
        & $sourceBinary install commit --install-root $runtimeDir --runtime-root $runtimeDir --transaction-id $engineTransactionId 1>$null
        if ($LASTEXITCODE -ne 0) {
            throw 'Installer Engine failed to commit the install transaction.'
        }
        $engineCommitted = $true
    }

    $taskTransactionCommitted = $taskTransactionStarted
    if (-not [string]::IsNullOrWhiteSpace($generationRepairBackupDirectory) -and
        (Test-Path -LiteralPath $generationRepairBackupDirectory -PathType Container)) {
        try {
            Remove-Item -LiteralPath $generationRepairBackupDirectory -Recurse -Force
            $generationRepairBackupDirectory = ''
        } catch {
            Write-Warning "AgentDock could not clean the repaired generation backup: $($_.Exception.Message)"
        }
    }
    $publicMCPUrl = ''
    if (-not [string]::IsNullOrWhiteSpace($publicUrl)) {
        $publicMCPUrl = "$publicUrl/mcp"
    }
    Write-InstallResult `
        -Path $ResultFile `
        -Success $true `
        -Message 'AgentDock installation completed.' `
        -InstalledVersion $Version `
        -LocalMCPUrl $localMCPUrl `
        -PublicMCPUrl $publicMCPUrl `
        -BearerToken $AuthToken `
        -OAuthLoginPassword $OAuthPassword `
        -HealthStatus $healthStatus `
        -PrivilegeMode $effectivePrivilegeMode `
        -WarningCode $installWarningCode `
        -WarningMessage $installWarningMessage

    Write-Host "AgentDock installed: $destinationBinary"
    Write-Host "Local MCP address: $localMCPUrl"
    Write-Host 'Open a new terminal if the updated user PATH is not visible yet.'
    if ($RegisterStartup) {
        Write-Host "Bearer Token: $AuthToken"
    }
    if ($resolvedTunnelMode -ne 'none') {
        Write-Host ''
        Write-Host 'AgentDock public installation complete'
        Write-Host "Public mode: $resolvedTunnelMode"
        Write-Host "Public address: $publicUrl"
        Write-Host "MCP address: $publicUrl/mcp"
        Write-Host "Bearer Token: $AuthToken"
        Write-Host "OAuth login password: $OAuthPassword"
        Write-Host 'Authentication: Bearer Token and OAuth are both enabled.'
        Write-Host "cloudflared stdout log: $cloudflaredStdoutLogPath"
        Write-Host "cloudflared stderr log: $cloudflaredStderrLogPath"
        if ($resolvedTunnelMode -eq 'quick') {
            Write-Host 'The temporary address changes after cloudflared restarts.'
            Write-Host 'Run the same installer command again to refresh the address; credentials are preserved.'
            Write-Host 'Then replace the MCP URL in the client and complete OAuth again.'
        } else {
            Write-Host "Cloudflare Public Hostname service target: http://127.0.0.1:$Port"
        }
    }
} catch {
    $installError = $_
    $taskRollbackError = $null
    $rollbackError = $null
    $taskRecoveryPath = ''
    try {
        if ($generationLayoutDetected -or $enginePrepared) {
            # Target Core runs as agentdock-core.exe after both bootstrap and Update Engine.
            # Stopping the CUI shim would miss the running generation and leave the new pointer live.
            $rollbackGenerationTray = Join-Path $generationBootstrapDirectory 'agentdock-tray.exe'
            $rollbackGenerationCore = Join-Path $generationBootstrapDirectory 'agentdock-core.exe'
            [void] (Stop-AgentDockTrayForUpgrade -BinaryPath $rollbackGenerationTray)
            [void] (Stop-AgentDockForUpgrade -BinaryPath $rollbackGenerationCore)
        } else {
            if ($trayStopAttempted -or $trayReplacementStarted -or $trayStartupRegistrationChanged) {
                [void] (Stop-AgentDockTrayForUpgrade -BinaryPath $destinationTrayBinary)
            }
            if ($agentDockStopAttempted -or $binaryReplacementStarted -or $startupRegistrationChanged) {
                [void] (Stop-AgentDockForUpgrade -BinaryPath $destinationBinary)
            }
        }
        if ($cloudflaredStopAttempted -or $cloudflaredReplacementStarted -or $tunnelStartupRegistrationChanged) {
            [void] (Stop-CloudflaredForUpgrade -BinaryPath $cloudflaredBinary)
        }
        if ($effectivePrivilegeMode -eq 'elevated') {
            Stop-ScheduledTask -TaskName 'AgentDock' -TaskPath '\' -ErrorAction SilentlyContinue
            Start-Sleep -Milliseconds 500
        }

        if (-not [string]::IsNullOrWhiteSpace($generationRepairBackupDirectory) -and
            (Test-Path -LiteralPath $generationRepairBackupDirectory -PathType Container)) {
            Remove-Item -LiteralPath $generationBootstrapDirectory -Recurse -Force -ErrorAction SilentlyContinue
            Move-Item -LiteralPath $generationRepairBackupDirectory -Destination $generationBootstrapDirectory
            $generationRepairBackupDirectory = ''
        } elseif ($activeVersionCreatedByBootstrap -or $generationBootstrapPublished) {
            Remove-Item -LiteralPath $activeVersionPath -Force -ErrorAction SilentlyContinue
            Remove-Item -LiteralPath $generationBootstrapDirectory -Recurse -Force -ErrorAction SilentlyContinue
            $activeVersionCreatedByBootstrap = $false
            $generationBootstrapPublished = $false
        }

        if ($cloudflaredReplacementStarted) {
            $cloudflaredBackupExists = Test-Path -LiteralPath $cloudflaredBackup -PathType Leaf
            if ($cloudflaredBackupExists) {
                Copy-Item -LiteralPath $cloudflaredBackup -Destination $cloudflaredBinary -Force
            }
            if (-not $cloudflaredBackupExists) {
                Remove-Item -LiteralPath $cloudflaredBinary -Force -ErrorAction SilentlyContinue
            }
        }

        if ($trayReplacementStarted) {
            $trayBackupExists = Test-Path -LiteralPath $trayBackup -PathType Leaf
            if ($trayBackupExists) {
                Copy-Item -LiteralPath $trayBackup -Destination $destinationTrayBinary -Force
            }
            if (-not $trayBackupExists) {
                Remove-Item -LiteralPath $destinationTrayBinary -Force -ErrorAction SilentlyContinue
            }
            $trayIconBackupExists = Test-Path -LiteralPath $trayIconBackup -PathType Leaf
            if ($trayIconBackupExists) {
                Copy-Item -LiteralPath $trayIconBackup -Destination $destinationTrayIcon -Force
            }
            if (-not $trayIconBackupExists) {
                Remove-Item -LiteralPath $destinationTrayIcon -Force -ErrorAction SilentlyContinue
            }
        }

        if ($binaryReplacementStarted) {
            $backupExists = Test-Path -LiteralPath $binaryBackup -PathType Leaf
            if ($backupExists) {
                Copy-Item -LiteralPath $binaryBackup -Destination $destinationBinary -Force
            }
            if (-not $backupExists) {
                Remove-Item -LiteralPath $destinationBinary -Force -ErrorAction SilentlyContinue
            }
        }

        if ($rollbackStateCaptured) {
            foreach ($item in $managedRuntimeFiles) {
                Restore-FileState -Path $item.Path -Name $item.Name -BackupDirectory $runtimeBackupDir
            }

            if ($null -ne $previousRunValue) {
                Set-RunValue -RegistryPath $runKey -Name $runValueName -Value $previousRunValue
            } else {
                Remove-ItemProperty -LiteralPath $runKey -Name $runValueName -ErrorAction SilentlyContinue
            }
            if ($null -ne $previousTrayRunValue) {
                Set-RunValue -RegistryPath $runKey -Name $trayRunValueName -Value $previousTrayRunValue
            } else {
                Remove-ItemProperty -LiteralPath $runKey -Name $trayRunValueName -ErrorAction SilentlyContinue
            }
            if ($null -ne $previousTunnelRunValue) {
                Set-RunValue -RegistryPath $runKey -Name $cloudflaredRunValueName -Value $previousTunnelRunValue
            } else {
                Remove-ItemProperty -LiteralPath $runKey -Name $cloudflaredRunValueName -ErrorAction SilentlyContinue
            }
        }

        if ($taskTransactionStarted -and -not $taskTransactionCommitted) {
            try {
                $restoreTaskActionResult = Start-ElevatedAgentDockTaskAction `
                    -Action restore `
                    -BackupDirectory $taskBackupDirectory `
                    -AdminLauncherPath $sourceTrayBinary `
                    -LauncherPath '' `
                    -TaskUser $taskUser
                if (-not $restoreTaskActionResult.Started) {
                    throw "Administrator approval for AgentDock rollback was not completed: $($restoreTaskActionResult.ErrorMessage)"
                }
                if (-not $restoreTaskActionResult.Succeeded) {
                    throw $restoreTaskActionResult.ErrorMessage
                }
                $taskRestored = $true
            } catch {
                $taskRollbackError = $_
                try {
                    # Preserve the original task definition when automatic restore fails so temp cleanup does not destroy manual recovery material.
                    $taskRecoveryPath = Join-Path `
                        (Join-Path $runtimeDir 'logs\installer') `
                        ('scheduled-task-recovery-' + [Guid]::NewGuid().ToString('N'))
                    New-Item -ItemType Directory -Path $taskRecoveryPath -Force | Out-Null
                    $taskBackupStatePath = Join-Path $taskBackupDirectory 'state.json'
                    if (-not (Test-Path -LiteralPath $taskBackupStatePath -PathType Leaf)) {
                        throw "AgentDock scheduled-task backup state is missing: $taskBackupStatePath"
                    }
                    Copy-Item -LiteralPath $taskBackupStatePath -Destination $taskRecoveryPath -Force
                    $taskBackupXmlPath = Join-Path $taskBackupDirectory 'task.xml'
                    if (Test-Path -LiteralPath $taskBackupXmlPath -PathType Leaf) {
                        Copy-Item -LiteralPath $taskBackupXmlPath -Destination $taskRecoveryPath -Force
                    }
                    Write-Warning "AgentDock scheduled-task recovery files were preserved at: $taskRecoveryPath"
                } catch {
                    Write-Warning "AgentDock could not preserve scheduled-task recovery files: $($_.Exception.Message)"
                    $taskRecoveryPath = ''
                }
                Write-Warning "AgentDock scheduled-task rollback failed: $($taskRollbackError.Exception.Message)"
            }
        }

        $taskWillRestartAgentDock = $false
        if ($taskRestored -and $taskState.WasRunning) {
            & $sourceManagerScript `
                -Action task-run-session `
                -ScheduledTaskName 'AgentDock' `
                -ScheduledTaskPath '\' `
                -ExpectedUserSid $taskUser.Sid
            $taskWillRestartAgentDock = $true
        }
        if ($processWasRunning -and -not $taskWillRestartAgentDock -and
            (Test-Path -LiteralPath $launcherPath -PathType Leaf)) {
            Start-AgentDockLauncher -LauncherPath $launcherPath
        }
        if ($cloudflaredProcessWasRunning -and (Test-Path -LiteralPath $cloudflaredLauncherPath -PathType Leaf)) {
            Start-CloudflaredLauncher -LauncherPath $cloudflaredLauncherPath
        }
        if ($trayProcessWasRunning -and (Test-Path -LiteralPath $destinationTrayBinary -PathType Leaf)) {
            Start-AgentDockTray -BinaryPath $destinationTrayBinary
        }
    } catch {
        $rollbackError = $_
        Write-Warning "AgentDock rollback failed: $($_.Exception.Message)"
    }

    if (($enginePrepared -or $engineCommitted) -and (Test-Path -LiteralPath $sourceBinary -PathType Leaf)) {
        $abandonArgs = @(
            'install', 'abandon',
            '--install-root', $runtimeDir,
            '--runtime-root', $runtimeDir
        )
        if (-not [string]::IsNullOrWhiteSpace($engineTransactionId)) {
            $abandonArgs += @('--transaction-id', $engineTransactionId)
        }
        if ($null -ne $rollbackError -or $null -ne $taskRollbackError) {
            $abandonArgs += '--rollback-failed'
        }
        & $sourceBinary @abandonArgs 1>$null
        if ($LASTEXITCODE -ne 0 -and $null -eq $rollbackError) {
            try {
                throw "Installer Engine could not record rollback state (exit $LASTEXITCODE)."
            } catch {
                $rollbackError = $_
            }
        }
    }

    $resultErrorCode = $installErrorCode
    $resultMessage = $installError.Exception.Message
    $resultErrorRecord = $installError
    if ($null -ne $taskRollbackError) {
        $resultErrorCode = 'elevated-task-rollback-failed'
        $resultMessage = "AgentDock installation failed and the previous administrator-enhanced task could not be restored automatically. Original error: $($installError.Exception.Message) Rollback error: $($taskRollbackError.Exception.Message)"
        if (-not [string]::IsNullOrWhiteSpace($taskRecoveryPath)) {
            $resultMessage += " Recovery files: $taskRecoveryPath"
        }
        $resultErrorRecord = $taskRollbackError
    } elseif ($null -ne $rollbackError) {
        $resultErrorCode = 'rollback-failed'
        $resultMessage = "AgentDock installation failed and rollback did not complete. Original error: $($installError.Exception.Message) Rollback error: $($rollbackError.Exception.Message)"
        $resultErrorRecord = $rollbackError
    }

    Write-InstallResult `
        -Path $ResultFile `
        -Success $false `
        -Message $resultMessage `
        -InstalledVersion $Version `
        -LocalMCPUrl "http://127.0.0.1:$Port/mcp" `
        -PublicMCPUrl '' `
        -BearerToken '' `
        -OAuthLoginPassword '' `
        -HealthStatus 'failed' `
        -PrivilegeMode $effectivePrivilegeMode `
        -ErrorCode $resultErrorCode `
        -ErrorRecord $resultErrorRecord
    throw $installError
} finally {
    if ($DeleteTunnelTokenFile -and -not [string]::IsNullOrWhiteSpace($TunnelTokenFile)) {
        Remove-Item -LiteralPath $TunnelTokenFile -Force -ErrorAction SilentlyContinue
    }
    Remove-Item -LiteralPath $tempRoot -Recurse -Force -ErrorAction SilentlyContinue
}

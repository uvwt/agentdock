[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet(
        'start',
        'stop',
        'restart',
        'start-tunnel',
        'stop-tunnel',
        'update',
        'set-mode',
        'regenerate-quick',
        'set-startup',
        'launch-core',
        'launch-tunnel',
        'set-task-startup',
        'task-start',
        'task-stop'
    )]
    [string] $Action,
    [string] $RuntimeRoot = (Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'AgentDock'),
    [ValidateSet('none', 'quick', 'named')]
    [string] $Mode = 'none',
    [string] $ServerUrl = '',
    [string] $TunnelTokenFile = '',
    [ValidateSet('core', 'tray')]
    [string] $Component = 'core',
    [ValidateSet('true', 'false')]
    [string] $Enabled = 'false'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$Utf8NoBom = New-Object System.Text.UTF8Encoding($false)
[Console]::InputEncoding = $Utf8NoBom
[Console]::OutputEncoding = $Utf8NoBom
$global:OutputEncoding = $Utf8NoBom
Add-Type -AssemblyName System.Security

function Convert-ToBoolean {
    param(
        [object] $Value,
        [bool] $Default = $false
    )

    if ($Value -is [bool]) {
        return [bool] $Value
    }
    if ($null -eq $Value) {
        return $Default
    }
    switch ([string] $Value) {
        'true' { return $true }
        'false' { return $false }
        default { return $Default }
    }
}

function Get-ObjectProperty {
    param(
        [object] $Object,
        [string] $Name,
        [object] $Default = $null
    )

    if ($null -eq $Object) {
        return $Default
    }
    $property = $Object.PSObject.Properties[$Name]
    if ($null -eq $property -or $null -eq $property.Value) {
        return $Default
    }
    return $property.Value
}

function Set-ObjectProperty {
    param(
        [object] $Object,
        [string] $Name,
        [object] $Value
    )

    if ($null -ne $Object.PSObject.Properties[$Name]) {
        $Object.$Name = $Value
        return
    }
    $Object | Add-Member -NotePropertyName $Name -NotePropertyValue $Value
}

function Test-SameFullPath {
    param(
        [string] $Left,
        [string] $Right
    )

    try {
        return [string]::Equals(
            [IO.Path]::GetFullPath($Left).TrimEnd([char[]]'\/'),
            [IO.Path]::GetFullPath($Right).TrimEnd([char[]]'\/'),
            [StringComparison]::OrdinalIgnoreCase)
    } catch {
        return $false
    }
}

function Test-PathWithinRoot {
    param(
        [string] $Root,
        [string] $Path
    )

    if ([string]::IsNullOrWhiteSpace($Root) -or [string]::IsNullOrWhiteSpace($Path)) {
        return $false
    }
    try {
        $fullRoot = [IO.Path]::GetFullPath($Root).TrimEnd([char[]]'\/')
        $fullPath = [IO.Path]::GetFullPath($Path)
        $rootPrefix = $fullRoot + [IO.Path]::DirectorySeparatorChar
        return $fullPath.StartsWith($rootPrefix, [StringComparison]::OrdinalIgnoreCase)
    } catch {
        return $false
    }
}

function Resolve-RuntimeManagedPath {
    param(
        [string] $CurrentRuntimeRoot,
        [string] $RecordedRuntimeRoot,
        [string] $RecordedPath,
        [string] $FallbackRelativePath,
        [switch] $Required
    )

    $fallbackPath = [IO.Path]::GetFullPath((Join-Path $CurrentRuntimeRoot $FallbackRelativePath))
    $candidate = $RecordedPath.Trim()
    if ([string]::IsNullOrWhiteSpace($candidate)) {
        if ($Required -or (Test-Path -LiteralPath $fallbackPath -PathType Leaf)) {
            return $fallbackPath
        }
        return ''
    }

    $managedPath = (Test-PathWithinRoot -Root $CurrentRuntimeRoot -Path $candidate) -or
        (-not [string]::IsNullOrWhiteSpace($RecordedRuntimeRoot) -and
            (Test-PathWithinRoot -Root $RecordedRuntimeRoot -Path $candidate))

    # runtime.json 所在目录是当前安装根；若清单路径仍属于旧根目录，就保持相对布局重定位。
    if (-not [string]::IsNullOrWhiteSpace($RecordedRuntimeRoot) -and
        (Test-PathWithinRoot -Root $RecordedRuntimeRoot -Path $candidate) -and
        -not (Test-SameFullPath -Left $RecordedRuntimeRoot -Right $CurrentRuntimeRoot)) {
        try {
            $fullRecordedRoot = [IO.Path]::GetFullPath($RecordedRuntimeRoot).TrimEnd([char[]]'\/')
            $fullCandidate = [IO.Path]::GetFullPath($candidate)
            $relativePath = $fullCandidate.Substring($fullRecordedRoot.Length + 1)
            $candidate = [IO.Path]::GetFullPath((Join-Path $CurrentRuntimeRoot $relativePath))
        } catch {
        }
    }

    if (Test-Path -LiteralPath $candidate -PathType Leaf) {
        return [IO.Path]::GetFullPath($candidate)
    }
    if ($managedPath -and (Test-Path -LiteralPath $fallbackPath -PathType Leaf)) {
        return $fallbackPath
    }
    return $candidate
}

function Read-JsonFile {
    param([string] $Path)

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        return [pscustomobject]@{}
    }
    try {
        return Get-Content -LiteralPath $Path -Raw -Encoding UTF8 | ConvertFrom-Json
    } catch {
        throw "无法读取 JSON 文件 $Path：$($_.Exception.Message)"
    }
}

function Write-TextAtomically {
    param(
        [string] $Path,
        [string] $Value
    )

    $parent = Split-Path -Parent $Path
    if (-not [string]::IsNullOrWhiteSpace($parent)) {
        New-Item -ItemType Directory -Path $parent -Force | Out-Null
    }
    $temporaryPath = "$Path.tmp.$PID"
    [IO.File]::WriteAllText($temporaryPath, $Value, $Utf8NoBom)
    Move-Item -LiteralPath $temporaryPath -Destination $Path -Force
}

function Write-JsonAtomically {
    param(
        [string] $Path,
        [object] $Value
    )

    Write-TextAtomically -Path $Path -Value ($Value | ConvertTo-Json -Depth 8)
}

function Read-TextFile {
    param([string] $Path)

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        return ''
    }
    try {
        return [IO.File]::ReadAllText($Path, [Text.Encoding]::UTF8).Trim()
    } catch {
        return ''
    }
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
    Write-TextAtomically -Path $Path -Value ([Convert]::ToBase64String($protectedBytes))
}

function Read-ProtectedText {
    param(
        [string] $Path,
        [string] $Entropy
    )

    $encoded = Read-TextFile -Path $Path
    if ([string]::IsNullOrWhiteSpace($encoded)) {
        return ''
    }
    try {
        $plainBytes = [System.Security.Cryptography.ProtectedData]::Unprotect(
            [Convert]::FromBase64String($encoded),
            [Text.Encoding]::UTF8.GetBytes($Entropy),
            [System.Security.Cryptography.DataProtectionScope]::CurrentUser
        )
        return [Text.Encoding]::UTF8.GetString($plainBytes)
    } catch {
        throw "无法读取当前 Windows 用户保存的凭据：$Path"
    }
}

function New-RandomHex {
    param([int] $ByteCount)

    $bytes = New-Object byte[] $ByteCount
    $generator = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $generator.GetBytes($bytes)
    } finally {
        $generator.Dispose()
    }
    return -join ($bytes | ForEach-Object { $_.ToString('x2') })
}

function Read-SecretFile {
    param([string] $Path)

    if ([string]::IsNullOrWhiteSpace($Path)) {
        return ''
    }
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "找不到临时凭据文件：$Path"
    }
    $value = (Get-Content -LiteralPath $Path -Raw -Encoding UTF8).Trim()
    if ([string]::IsNullOrWhiteSpace($value)) {
        throw "临时凭据文件为空：$Path"
    }
    return $value
}

if ([string]::IsNullOrWhiteSpace($RuntimeRoot)) {
    throw 'RuntimeRoot 不能为空。'
}
$RuntimeRoot = [IO.Path]::GetFullPath($RuntimeRoot)
$ManifestPath = Join-Path $RuntimeRoot 'runtime.json'
$SettingsPath = Join-Path $RuntimeRoot 'control-panel-settings.json'
$AuthTokenPath = Join-Path $RuntimeRoot 'auth-token.dpapi'
$OAuthPasswordPath = Join-Path $RuntimeRoot 'oauth-password.dpapi'
$OAuthSecretPath = Join-Path $RuntimeRoot 'oauth-token-secret.dpapi'
$TunnelTokenPath = Join-Path $RuntimeRoot 'cloudflared-token.dpapi'
$TunnelModePath = Join-Path $RuntimeRoot 'cloudflared-mode.txt'
$ServerUrlPath = Join-Path $RuntimeRoot 'server-url.txt'
$NamedServerUrlPath = Join-Path $RuntimeRoot 'named-server-url.txt'
$QuickTunnelUrlPath = Join-Path $RuntimeRoot 'quick-tunnel-url.txt'
$RunKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'

$Manifest = Read-JsonFile -Path $ManifestPath
$RecordedInstallRoot = [string] (Get-ObjectProperty -Object $Manifest -Name 'install_root' -Default '')
$AgentDockBinary = Resolve-RuntimeManagedPath `
    -CurrentRuntimeRoot $RuntimeRoot `
    -RecordedRuntimeRoot $RecordedInstallRoot `
    -RecordedPath ([string] (Get-ObjectProperty -Object $Manifest -Name 'agentdock_binary' -Default '')) `
    -FallbackRelativePath 'bin\agentdock.exe' `
    -Required
$TrayBinary = Resolve-RuntimeManagedPath `
    -CurrentRuntimeRoot $RuntimeRoot `
    -RecordedRuntimeRoot $RecordedInstallRoot `
    -RecordedPath ([string] (Get-ObjectProperty -Object $Manifest -Name 'tray_binary' -Default '')) `
    -FallbackRelativePath 'bin\agentdock-tray.exe'
$AgentDockLauncher = Resolve-RuntimeManagedPath `
    -CurrentRuntimeRoot $RuntimeRoot `
    -RecordedRuntimeRoot $RecordedInstallRoot `
    -RecordedPath ([string] (Get-ObjectProperty -Object $Manifest -Name 'agentdock_launcher' -Default '')) `
    -FallbackRelativePath 'start-agentdock.ps1'
$CloudflaredLauncher = Resolve-RuntimeManagedPath `
    -CurrentRuntimeRoot $RuntimeRoot `
    -RecordedRuntimeRoot $RecordedInstallRoot `
    -RecordedPath ([string] (Get-ObjectProperty -Object $Manifest -Name 'cloudflared_launcher' -Default '')) `
    -FallbackRelativePath 'start-cloudflared.ps1'
$CloudflaredBinary = Resolve-RuntimeManagedPath `
    -CurrentRuntimeRoot $RuntimeRoot `
    -RecordedRuntimeRoot $RecordedInstallRoot `
    -RecordedPath ([string] (Get-ObjectProperty -Object $Manifest -Name 'cloudflared_binary' -Default '')) `
    -FallbackRelativePath 'bin\cloudflared.exe'
$PrivilegeMode = [string] (Get-ObjectProperty -Object $Manifest -Name 'privilege_mode' -Default 'standard')
$TaskName = [string] (Get-ObjectProperty -Object $Manifest -Name 'agentdock_task_name' -Default 'AgentDock')
$CoreStartupValueName = [string] (Get-ObjectProperty -Object $Manifest -Name 'startup_value_name' -Default 'AgentDock')
$TrayStartupValueName = [string] (Get-ObjectProperty -Object $Manifest -Name 'tray_startup_value_name' -Default 'AgentDockTray')
$CloudflaredStartupValueName = [string] (Get-ObjectProperty -Object $Manifest -Name 'cloudflared_startup_value_name' -Default 'AgentDockCloudflared')
$InstalledManagerPath = Join-Path $RuntimeRoot 'installer\manage-windows.ps1'
$ManagerPath = if (Test-Path -LiteralPath $InstalledManagerPath -PathType Leaf) { $InstalledManagerPath } else { $PSCommandPath }

function Get-ControlPanelSettings {
    $stored = Read-JsonFile -Path $SettingsPath
    $manifestPort = [int] (Get-ObjectProperty -Object $Manifest -Name 'port' -Default 8765)
    $storedPort = [int] (Get-ObjectProperty -Object $stored -Name 'port' -Default $manifestPort)
    if ($storedPort -lt 1 -or $storedPort -gt 65535) {
        $storedPort = 8765
    }
    $storedLogLevel = [string] (Get-ObjectProperty -Object $stored -Name 'log_level' -Default 'info')
    if (@('debug', 'info', 'warn', 'error') -notcontains $storedLogLevel) {
        $storedLogLevel = 'info'
    }
    $storedACPAgent = ([string] (Get-ObjectProperty -Object $stored -Name 'acp_agent' -Default 'codex')).Trim().ToLowerInvariant()
    if (@('codex', 'claude', 'grok', 'custom') -notcontains $storedACPAgent) {
        throw "不支持的 Coding Agent: $storedACPAgent"
    }
    return [pscustomobject][ordered]@{
        port = $storedPort
        log_level = $storedLogLevel
        browser_enabled = Convert-ToBoolean -Value (Get-ObjectProperty -Object $stored -Name 'browser_enabled' -Default $false)
        acp_enabled = Convert-ToBoolean -Value (Get-ObjectProperty -Object $stored -Name 'acp_enabled' -Default $false)
        acp_agent = $storedACPAgent
        acp_command = [string] (Get-ObjectProperty -Object $stored -Name 'acp_command' -Default '')
        acp_args = @((Get-ObjectProperty -Object $stored -Name 'acp_args' -Default @()))
    }
}

function Update-RuntimeManifest {
    param(
        [int] $RuntimePort,
        [string] $RuntimeMode,
        [string] $PublicUrl
    )

    # 版本由 agentdock.exe BuildInfo 唯一提供；清理旧清单残留，避免再次形成第二版本真值。
    $Manifest.PSObject.Properties.Remove('version')
    Set-ObjectProperty -Object $Manifest -Name 'schema_version' -Value 1
    Set-ObjectProperty -Object $Manifest -Name 'install_root' -Value $RuntimeRoot
    Set-ObjectProperty -Object $Manifest -Name 'agentdock_binary' -Value $AgentDockBinary
    Set-ObjectProperty -Object $Manifest -Name 'tray_binary' -Value $TrayBinary
    Set-ObjectProperty -Object $Manifest -Name 'agentdock_launcher' -Value $AgentDockLauncher
    Set-ObjectProperty -Object $Manifest -Name 'cloudflared_binary' -Value $CloudflaredBinary
    Set-ObjectProperty -Object $Manifest -Name 'cloudflared_launcher' -Value $CloudflaredLauncher
    Set-ObjectProperty -Object $Manifest -Name 'startup_value_name' -Value $CoreStartupValueName
    Set-ObjectProperty -Object $Manifest -Name 'tray_startup_value_name' -Value $TrayStartupValueName
    Set-ObjectProperty -Object $Manifest -Name 'cloudflared_startup_value_name' -Value $CloudflaredStartupValueName
    Set-ObjectProperty -Object $Manifest -Name 'host' -Value '127.0.0.1'
    Set-ObjectProperty -Object $Manifest -Name 'port' -Value $RuntimePort
    Set-ObjectProperty -Object $Manifest -Name 'local_mcp_url' -Value "http://127.0.0.1:$RuntimePort/mcp"
    Set-ObjectProperty -Object $Manifest -Name 'tunnel_mode' -Value $RuntimeMode
    Set-ObjectProperty -Object $Manifest -Name 'public_url' -Value $PublicUrl
    Write-JsonAtomically -Path $ManifestPath -Value $Manifest
}

function Ensure-Credentials {
    if ([string]::IsNullOrWhiteSpace((Read-TextFile -Path $AuthTokenPath))) {
        Write-ProtectedText -Path $AuthTokenPath -Value (New-RandomHex -ByteCount 32) -Entropy 'agentdock.startup.v1'
    }
    if ([string]::IsNullOrWhiteSpace((Read-TextFile -Path $OAuthPasswordPath))) {
        Write-ProtectedText -Path $OAuthPasswordPath -Value (New-RandomHex -ByteCount 12) -Entropy 'agentdock.oauth.password.v1'
    }
    if ([string]::IsNullOrWhiteSpace((Read-TextFile -Path $OAuthSecretPath))) {
        Write-ProtectedText -Path $OAuthSecretPath -Value (New-RandomHex -ByteCount 32) -Entropy 'agentdock.oauth.secret.v1'
    }
}

function Get-ProcessesAtPath {
    param(
        [string] $ProcessName,
        [string] $BinaryPath
    )

    if ([string]::IsNullOrWhiteSpace($BinaryPath)) {
        return @()
    }
    $normalizedPath = [IO.Path]::GetFullPath($BinaryPath)
    return @(Get-CimInstance Win32_Process -Filter "Name = '$($ProcessName).exe'" -ErrorAction SilentlyContinue | Where-Object {
        $_.ExecutablePath -and
        [string]::Equals(
            [IO.Path]::GetFullPath($_.ExecutablePath),
            $normalizedPath,
            [StringComparison]::OrdinalIgnoreCase
        )
    })
}

function Stop-ProcessesAtPath {
    param(
        [string] $ProcessName,
        [string] $BinaryPath
    )

    $deadline = [DateTime]::UtcNow.AddSeconds(15)
    do {
        $processes = @(Get-ProcessesAtPath -ProcessName $ProcessName -BinaryPath $BinaryPath)
        foreach ($process in $processes) {
            Stop-Process -Id $process.ProcessId -Force -ErrorAction SilentlyContinue
        }
        if ($processes.Count -eq 0) {
            return
        }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "无法停止 $ProcessName：$BinaryPath"
}

function Escape-SingleQuoted {
    param([string] $Value)
    return $Value.Replace("'", "''")
}

function Test-IsAdministrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Invoke-ElevatedManagerAction {
    param([string] $InternalEnabled = 'false')

    if (-not (Test-Path -LiteralPath $TrayBinary -PathType Leaf)) {
        throw "找不到 AgentDock 管理程序：$TrayBinary"
    }
    $currentSid = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    $arguments = "--task-admin set-enabled --enabled $InternalEnabled --user-sid `"$currentSid`""
    $process = Start-Process `
        -FilePath $TrayBinary `
        -ArgumentList $arguments `
        -Verb RunAs `
        -Wait `
        -PassThru
    if ($process.ExitCode -ne 0) {
        throw "需要管理员权限的操作失败，退出码：$($process.ExitCode)"
    }
}

function Write-Launchers {
    $settings = Get-ControlPanelSettings
    $escapedManager = Escape-SingleQuoted -Value $ManagerPath
    $escapedRoot = Escape-SingleQuoted -Value $RuntimeRoot
    $escapedAgentDockBinary = Escape-SingleQuoted -Value $AgentDockBinary

    # 旧版本升级仍可能由自更新逻辑识别该文件，因此暂时保留兼容启动器。
    # 新安装和桌面端均直接调用 agentdock service；支持的旧版本淘汰后可删除此文件。
    $coreLauncher = @"
`$ErrorActionPreference = 'Stop'
`$env:AGENTDOCK_PORT = '$($settings.port)'
`$agentDockBinary = '$escapedAgentDockBinary'
& '$escapedAgentDockBinary' service launch-core --runtime-root '$escapedRoot'
exit `$LASTEXITCODE
"@
    Write-TextAtomically -Path $AgentDockLauncher -Value $coreLauncher

    $tunnelLauncher = @"
`$ErrorActionPreference = 'Stop'
& '$escapedManager' -Action launch-tunnel -RuntimeRoot '$escapedRoot'
exit `$LASTEXITCODE
"@
    Write-TextAtomically -Path $CloudflaredLauncher -Value $tunnelLauncher
}

function Start-TaskPreservingStartupState {
    $task = Get-ScheduledTask -TaskName $TaskName -TaskPath '\' -ErrorAction Stop
    $wasEnabled = [bool] $task.Settings.Enabled

    # 用户关闭开机启动后仍应允许手动启动。这里临时启用任务，启动后恢复原状态。
    if (-not $wasEnabled) {
        Enable-ScheduledTask -TaskName $TaskName -TaskPath '\' -ErrorAction Stop | Out-Null
    }
    try {
        Start-ScheduledTask -TaskName $TaskName -TaskPath '\' -ErrorAction Stop
    } finally {
        if (-not $wasEnabled) {
            Disable-ScheduledTask -TaskName $TaskName -TaskPath '\' -ErrorAction Stop | Out-Null
        }
    }
}

function Set-TaskStartupState {
    param([bool] $ShouldEnable)

    if (-not (Test-IsAdministrator)) {
        Invoke-ElevatedManagerAction -InternalEnabled $ShouldEnable.ToString().ToLowerInvariant()
        return
    }
    if ($ShouldEnable) {
        Enable-ScheduledTask -TaskName $TaskName -TaskPath '\' -ErrorAction Stop | Out-Null
    } else {
        Disable-ScheduledTask -TaskName $TaskName -TaskPath '\' -ErrorAction Stop | Out-Null
    }
}

function Invoke-LaunchCore {
    if (-not (Test-Path -LiteralPath $AgentDockBinary -PathType Leaf)) {
        throw "找不到 AgentDock 核心程序：$AgentDockBinary"
    }

    Ensure-Credentials
    # 兼容入口也统一交给原生 launch-core 恢复设置和 DPAPI 凭据，避免形成第二套启动配置逻辑。
    & $AgentDockBinary service launch-core --runtime-root $RuntimeRoot
    return $LASTEXITCODE
}

function Invoke-NativeCoreCommand {
    param([ValidateSet('start', 'stop', 'restart')] [string] $Command)

    & $AgentDockBinary service $Command --runtime-root $RuntimeRoot
    if ($LASTEXITCODE -ne 0) {
        throw "AgentDock 原生服务命令执行失败：$Command，退出码：$LASTEXITCODE"
    }
}

function Invoke-NativeTunnelCommand {
    param([ValidateSet('start', 'stop', 'restart', 'regenerate')] [string] $Command)

    & $AgentDockBinary tunnel $Command --runtime-root $RuntimeRoot
    if ($LASTEXITCODE -ne 0) {
        throw "AgentDock 原生 Tunnel 命令执行失败：$Command，退出码：$LASTEXITCODE"
    }
}

function Start-Core {
    Invoke-NativeCoreCommand -Command start
}

function Stop-Core {
    Invoke-NativeCoreCommand -Command stop
}

function Restart-Core {
    Invoke-NativeCoreCommand -Command restart
}

function Invoke-LaunchTunnel {
    # 兼容启动器也只进入原生 Tunnel 生命周期；cloudflared 输出由 AgentDock 监督进程轮转。
    & $AgentDockBinary tunnel start --runtime-root $RuntimeRoot
    return $LASTEXITCODE
}

function Start-Tunnel {
    Invoke-NativeTunnelCommand -Command start
}

function Stop-Tunnel {
    Invoke-NativeTunnelCommand -Command stop
}

function Wait-QuickTunnelReady {
    $deadline = [DateTime]::UtcNow.AddSeconds(45)
    do {
        $publicUrl = Read-TextFile -Path $QuickTunnelUrlPath
        if (-not [string]::IsNullOrWhiteSpace($publicUrl)) {
            return $publicUrl
        }
        Start-Sleep -Milliseconds 500
    } while ([DateTime]::UtcNow -lt $deadline)
    throw '新的 Quick Tunnel 地址未在 45 秒内准备完成。'
}

function Clear-ActivePublicUrl {
    Write-TextAtomically -Path $ServerUrlPath -Value ''
    Remove-Item -LiteralPath $QuickTunnelUrlPath -Force -ErrorAction SilentlyContinue
}

function Set-TunnelMode {
    param(
        [string] $RequestedMode,
        [string] $RequestedServerUrl,
        [string] $RequestedTokenFile
    )

    $arguments = @(
        'tunnel', 'configure',
        '--runtime-root', $RuntimeRoot,
        '--mode', $RequestedMode,
        '--server-url', $RequestedServerUrl
    )
    if (-not [string]::IsNullOrWhiteSpace($RequestedTokenFile)) {
        $arguments += @('--token-file', $RequestedTokenFile)
    }
    & $AgentDockBinary @arguments
    if ($LASTEXITCODE -ne 0) {
        throw "AgentDock 原生 Tunnel 配置失败，退出码：$LASTEXITCODE"
    }
}

function Regenerate-QuickTunnel {
    Invoke-NativeTunnelCommand -Command regenerate
}

function Set-ComponentStartup {
    param(
        [string] $TargetComponent,
        [bool] $ShouldEnable
    )

    $enabledValue = $ShouldEnable.ToString().ToLowerInvariant()
    & $AgentDockBinary service autostart `
        --runtime-root $RuntimeRoot `
        --component $TargetComponent `
        --enabled $enabledValue
    if ($LASTEXITCODE -ne 0) {
        throw "AgentDock 原生开机启动命令执行失败，退出码：$LASTEXITCODE"
    }
}

function Start-AgentDockRuntime {
    Start-Core
    Start-Tunnel
}

function Stop-AgentDockRuntime {
    Stop-Tunnel
    Stop-Core
}

function Restart-AgentDockRuntime {
    $modeValue = (Read-TextFile -Path $TunnelModePath).ToLowerInvariant()
    Stop-Tunnel
    Stop-Core
    if ($modeValue -eq 'quick') {
        Clear-ActivePublicUrl
        $settings = Get-ControlPanelSettings
        Update-RuntimeManifest -RuntimePort $settings.port -RuntimeMode 'none' -PublicUrl ''
    }
    Start-Core
    Start-Tunnel
    if ($modeValue -eq 'quick') {
        [void] (Wait-QuickTunnelReady)
    }
}

switch ($Action) {
    'launch-core' {
        exit (Invoke-LaunchCore)
    }
    'launch-tunnel' {
        exit (Invoke-LaunchTunnel)
    }
    'set-task-startup' {
        if (-not (Test-IsAdministrator)) {
            throw '设置最高权限计划任务需要管理员权限。'
        }
        Set-TaskStartupState -ShouldEnable (Convert-ToBoolean -Value $Enabled)
        exit 0
    }
    'task-start' {
        if (-not (Test-IsAdministrator)) {
            throw '启动最高权限计划任务需要管理员权限。'
        }
        Start-TaskPreservingStartupState
        exit 0
    }
    'task-stop' {
        if (-not (Test-IsAdministrator)) {
            throw '停止最高权限计划任务需要管理员权限。'
        }
        Stop-ScheduledTask -TaskName $TaskName -TaskPath '\' -ErrorAction SilentlyContinue
        Start-Sleep -Milliseconds 500
        Stop-ProcessesAtPath -ProcessName 'agentdock' -BinaryPath $AgentDockBinary
        exit 0
    }
    'start' {
        Start-AgentDockRuntime
    }
    'stop' {
        Stop-AgentDockRuntime
    }
    'restart' {
        Restart-AgentDockRuntime
    }
    'start-tunnel' {
        Start-Tunnel
    }
    'stop-tunnel' {
        Stop-Tunnel
    }
    'update' {
        if (-not (Test-Path -LiteralPath $AgentDockBinary -PathType Leaf)) {
            throw "找不到 AgentDock 核心程序：$AgentDockBinary"
        }
        & $AgentDockBinary update
        if ($LASTEXITCODE -ne 0) {
            exit $LASTEXITCODE
        }
    }
    'set-mode' {
        Set-TunnelMode -RequestedMode $Mode -RequestedServerUrl $ServerUrl -RequestedTokenFile $TunnelTokenFile
    }
    'regenerate-quick' {
        Regenerate-QuickTunnel
    }
    'set-startup' {
        Set-ComponentStartup -TargetComponent $Component -ShouldEnable (Convert-ToBoolean -Value $Enabled)
    }
}

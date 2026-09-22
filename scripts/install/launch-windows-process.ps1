[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string] $FilePath,
    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string] $AgentDockBinary,
    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string] $HiddenHostBinary,
    [string] $Arguments = '',
    [switch] $WaitForExit,
    [ValidateRange(1, 120)]
    [int] $TimeoutSeconds = 60
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Read-RuntimeDiagnosticTail {
    param(
        [string] $Path,
        [int] $MaxChars = 4000
    )

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        return ''
    }
    try {
        $lines = @(Get-Content -LiteralPath $Path -Encoding UTF8 -Tail 40 -ErrorAction Stop)
        $text = (($lines -join [Environment]::NewLine).Trim())
    } catch {
        return ''
    }
    if ($text.Length -gt $MaxChars) {
        return '...' + $text.Substring($text.Length - $MaxChars)
    }
    return $text
}

function Get-RuntimeFailureMessage {
    param(
        [string] $Action,
        $TaskResult,
        [string] $WrapperErrorPath,
        [string] $StdoutPath,
        [string] $StderrPath
    )

    [int64] $rawResult = [int64] $TaskResult
    [int64] $signedResult = $rawResult
    if ($rawResult -lt 0) {
        $rawResult += 4294967296
    } elseif ($rawResult -gt [int32]::MaxValue) {
        $signedResult -= 4294967296
    }

    $message = "$Action with exit code $signedResult (Task Scheduler result: $rawResult)."
    foreach ($diagnostic in @(
        @{ Label = 'launcher error'; Path = $WrapperErrorPath },
        @{ Label = 'stderr'; Path = $StderrPath },
        @{ Label = 'stdout'; Path = $StdoutPath }
    )) {
        $text = Read-RuntimeDiagnosticTail -Path $diagnostic.Path
        if (-not [string]::IsNullOrWhiteSpace($text)) {
            $message += "`r`n$($diagnostic.Label): $text"
        }
    }
    return $message
}

if (-not (Test-Path -LiteralPath $FilePath -PathType Leaf)) {
    throw "Runtime executable was not found: $FilePath"
}

$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
if ($null -eq $identity -or $null -eq $identity.User -or [string]::IsNullOrWhiteSpace($identity.Name)) {
    throw 'Unable to resolve the current Windows identity for runtime launch.'
}
if (-not (Test-Path -LiteralPath $AgentDockBinary -PathType Leaf)) {
    throw "AgentDock native task launcher was not found: $AgentDockBinary"
}
if (-not (Test-Path -LiteralPath $HiddenHostBinary -PathType Leaf)) {
    throw "AgentDock hidden runtime host was not found: $HiddenHostBinary"
}

$taskName = 'AgentDock Setup Runtime ' + [Guid]::NewGuid().ToString('N')
$diagnosticRoot = ''
$stdoutPath = ''
$stderrPath = ''
$wrapperErrorPath = ''
if ($WaitForExit) {
    $diagnosticRoot = Join-Path ([IO.Path]::GetTempPath()) ('agentdock-setup-runtime-' + [Guid]::NewGuid().ToString('N'))
    $stdoutPath = Join-Path $diagnosticRoot 'stdout.log'
    $stderrPath = Join-Path $diagnosticRoot 'stderr.log'
    $wrapperErrorPath = Join-Path $diagnosticRoot 'launcher-error.log'
    New-Item -ItemType Directory -Path $diagnosticRoot -Force | Out-Null
}

function ConvertTo-RuntimeHostArgument {
    param([AllowEmptyString()][string] $Value)
    return [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($Value))
}

# The scheduled task itself must be a GUI-subsystem process. Starting powershell.exe as the
# interactive task action can create a console before -WindowStyle Hidden takes effect.
# The WinExe tray shim is therefore a narrow host that creates the real child with CREATE_NO_WINDOW.
$hostArguments = @(
    '--setup-runtime-host',
    '--file-b64', (ConvertTo-RuntimeHostArgument -Value $FilePath)
)
if (-not [string]::IsNullOrWhiteSpace($Arguments)) {
    $hostArguments += @('--args-b64', (ConvertTo-RuntimeHostArgument -Value $Arguments))
}
foreach ($environment in @(
    @{ Name = 'AGENTDOCK_HOME'; Flag = '--agentdock-home-b64' },
    @{ Name = 'AGENTDOCK_DEFAULT_DIR'; Flag = '--agentdock-default-dir-b64' }
)) {
    $value = [Environment]::GetEnvironmentVariable($environment.Name, 'Process')
    if ($null -ne $value) {
        $hostArguments += @($environment.Flag, (ConvertTo-RuntimeHostArgument -Value $value))
    }
}
if ($WaitForExit) {
    $hostArguments += @(
        '--wait',
        '--stdout-b64', (ConvertTo-RuntimeHostArgument -Value $stdoutPath),
        '--stderr-b64', (ConvertTo-RuntimeHostArgument -Value $stderrPath),
        '--error-b64', (ConvertTo-RuntimeHostArgument -Value $wrapperErrorPath)
    )
}
$action = New-ScheduledTaskAction `
    -Execute $HiddenHostBinary `
    -Argument ($hostArguments -join ' ')
$principal = New-ScheduledTaskPrincipal `
    -UserId $identity.Name `
    -LogonType Interactive `
    -RunLevel Limited
$settings = New-ScheduledTaskSettingsSet `
    -AllowStartIfOnBatteries `
    -DontStopIfGoingOnBatteries

$registered = $false
$startedAt = Get-Date
try {
    Register-ScheduledTask `
        -TaskName $taskName `
        -Action $action `
        -Principal $principal `
        -Settings $settings `
        -Force | Out-Null
    $registered = $true
    & $AgentDockBinary service task-start `
        --task-name $taskName `
        --expected-user-sid $identity.User.Value | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "AgentDock native task-start failed with exit code $LASTEXITCODE."
    }

    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    do {
        $task = Get-ScheduledTask -TaskName $taskName -TaskPath '\' -ErrorAction Stop
        $info = Get-ScheduledTaskInfo -TaskName $taskName -TaskPath '\' -ErrorAction Stop
        $hasRun = $info.LastRunTime -ge $startedAt.AddSeconds(-1)
        if ($hasRun) {
            if (-not $WaitForExit) {
                if ($task.State -eq 'Ready' -and $info.LastTaskResult -ne 0) {
                    throw "Runtime process failed to launch, Task Scheduler result: $($info.LastTaskResult)."
                }
                return
            }
            if ($task.State -notin @('Running', 'Queued')) {
                if ($info.LastTaskResult -ne 0) {
                    throw (Get-RuntimeFailureMessage `
                        -Action 'Runtime process exited' `
                        -TaskResult $info.LastTaskResult `
                        -WrapperErrorPath $wrapperErrorPath `
                        -StdoutPath $stdoutPath `
                        -StderrPath $stderrPath)
                }
                return
            }
        }
        Start-Sleep -Milliseconds 100
    } while ([DateTime]::UtcNow -lt $deadline)

    if ($WaitForExit) {
        throw "Runtime process did not finish within $TimeoutSeconds seconds."
    }
    throw "Runtime process did not start within $TimeoutSeconds seconds."
} finally {
    if ($registered) {
        Unregister-ScheduledTask -TaskName $taskName -TaskPath '\' -Confirm:$false -ErrorAction SilentlyContinue
    }
    if (-not [string]::IsNullOrWhiteSpace($diagnosticRoot)) {
        Remove-Item -LiteralPath $diagnosticRoot -Recurse -Force -ErrorAction SilentlyContinue
    }
}

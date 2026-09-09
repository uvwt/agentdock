[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string] $FilePath,
    [string] $Arguments = '',
    [switch] $WaitForExit,
    [ValidateRange(1, 120)]
    [int] $TimeoutSeconds = 30
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
if ($null -eq $identity -or [string]::IsNullOrWhiteSpace($identity.Name)) {
    throw 'Unable to resolve the current Windows identity for runtime launch.'
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

$wrapperLines = @("`$ErrorActionPreference = 'Stop'")
foreach ($name in @('AGENTDOCK_HOME', 'AGENTDOCK_DEFAULT_DIR')) {
    $value = [Environment]::GetEnvironmentVariable($name, 'Process')
    if ($null -ne $value) {
        $encodedValue = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($value))
        $wrapperLines += "`$env:$name = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('$encodedValue'))"
    }
}
$encodedFilePath = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($FilePath))
$wrapperLines += "`$filePath = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('$encodedFilePath'))"
if (-not [string]::IsNullOrWhiteSpace($Arguments)) {
    $encodedArguments = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($Arguments))
    $wrapperLines += "`$arguments = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('$encodedArguments'))"
}
if ($WaitForExit) {
    $encodedStdoutPath = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($stdoutPath))
    $encodedStderrPath = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($stderrPath))
    $encodedWrapperErrorPath = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($wrapperErrorPath))
    $wrapperLines += "`$stdoutPath = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('$encodedStdoutPath'))"
    $wrapperLines += "`$stderrPath = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('$encodedStderrPath'))"
    $wrapperLines += "`$wrapperErrorPath = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('$encodedWrapperErrorPath'))"
    $wrapperLines += 'try {'

    # Windows PowerShell 5.1 loses ExitCode when Start-Process combines PassThru with redirected streams
    # unless -Wait is used. -Wait can also follow descendant processes, which would change the runtime lifecycle.
    # ProcessStartInfo keeps the original direct-child wait semantics while capturing both diagnostic streams.
    $wrapperLines += '    $startInfo = New-Object Diagnostics.ProcessStartInfo'
    $wrapperLines += '    $startInfo.FileName = $filePath'
    if (-not [string]::IsNullOrWhiteSpace($Arguments)) {
        $wrapperLines += '    $startInfo.Arguments = $arguments'
    }
    $wrapperLines += '    $startInfo.UseShellExecute = $false'
    $wrapperLines += '    $startInfo.CreateNoWindow = $true'
    $wrapperLines += '    $startInfo.RedirectStandardOutput = $true'
    $wrapperLines += '    $startInfo.RedirectStandardError = $true'
    $wrapperLines += '    $process = New-Object Diagnostics.Process'
    $wrapperLines += '    $process.StartInfo = $startInfo'
    $wrapperLines += "    if (-not `$process.Start()) { throw 'Runtime process could not be started.' }"
    $wrapperLines += '    $stdoutTask = $process.StandardOutput.ReadToEndAsync()'
    $wrapperLines += '    $stderrTask = $process.StandardError.ReadToEndAsync()'
    $wrapperLines += '    $process.WaitForExit()'
    $wrapperLines += '    $stdout = $stdoutTask.GetAwaiter().GetResult()'
    $wrapperLines += '    $stderr = $stderrTask.GetAwaiter().GetResult()'
    $wrapperLines += '    [IO.File]::WriteAllText($stdoutPath, $stdout, (New-Object Text.UTF8Encoding($false)))'
    $wrapperLines += '    [IO.File]::WriteAllText($stderrPath, $stderr, (New-Object Text.UTF8Encoding($false)))'
    $wrapperLines += '    exit $process.ExitCode'
    $wrapperLines += '} catch {'
    $wrapperLines += '    [IO.File]::WriteAllText($wrapperErrorPath, ($_ | Out-String), (New-Object Text.UTF8Encoding($false)))'
    $wrapperLines += '    exit 1'
    $wrapperLines += '}'
} else {
    if ([string]::IsNullOrWhiteSpace($Arguments)) {
        $wrapperLines += '$process = Start-Process -FilePath $filePath -PassThru'
    } else {
        $wrapperLines += '$process = Start-Process -FilePath $filePath -ArgumentList $arguments -PassThru'
    }
    $wrapperLines += 'exit 0'
}
$encodedCommand = [Convert]::ToBase64String(
    [Text.Encoding]::Unicode.GetBytes(($wrapperLines -join "`r`n"))
)
$powerShellPath = Join-Path $PSHOME 'powershell.exe'
$action = New-ScheduledTaskAction `
    -Execute $powerShellPath `
    -Argument "-NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -EncodedCommand $encodedCommand"
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
    Start-ScheduledTask -TaskName $taskName -TaskPath '\' -ErrorAction Stop

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

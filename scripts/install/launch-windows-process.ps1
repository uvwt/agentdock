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

if (-not (Test-Path -LiteralPath $FilePath -PathType Leaf)) {
    throw "Runtime executable was not found: $FilePath"
}

$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
if ($null -eq $identity -or [string]::IsNullOrWhiteSpace($identity.Name)) {
    throw 'Unable to resolve the current Windows identity for runtime launch.'
}

$taskName = 'AgentDock Setup Runtime ' + [Guid]::NewGuid().ToString('N')
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
if ([string]::IsNullOrWhiteSpace($Arguments)) {
    $wrapperLines += '$process = Start-Process -FilePath $filePath -PassThru'
} else {
    $encodedArguments = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($Arguments))
    $wrapperLines += "`$arguments = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('$encodedArguments'))"
    $wrapperLines += '$process = Start-Process -FilePath $filePath -ArgumentList $arguments -PassThru'
}
if ($WaitForExit) {
    $wrapperLines += '$process.WaitForExit()'
    $wrapperLines += 'exit $process.ExitCode'
} else {
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
                    throw "Runtime process exited with Task Scheduler result: $($info.LastTaskResult)."
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
}

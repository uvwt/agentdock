[CmdletBinding()]
param(
    [string] $LauncherPath = (Join-Path $PSScriptRoot '..\install\launch-windows-process.ps1')
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$resolvedLauncher = (Resolve-Path -LiteralPath $LauncherPath).Path
$testRoot = Join-Path ([IO.Path]::GetTempPath()) ('agentdock runtime diagnostics test ' + [Guid]::NewGuid().ToString('N'))
$childScript = Join-Path $testRoot 'child.ps1'
$taskPrefix = 'AgentDock Setup Runtime '
$tempPrefix = 'agentdock-setup-runtime-'
$beforeTasks = @(Get-ScheduledTask -ErrorAction Stop | Where-Object { $_.TaskName.StartsWith($taskPrefix) } | ForEach-Object TaskName)
$beforeTempDirs = @(Get-ChildItem -LiteralPath ([IO.Path]::GetTempPath()) -Directory -Filter "$tempPrefix*" -ErrorAction SilentlyContinue | ForEach-Object FullName)

try {
    New-Item -ItemType Directory -Path $testRoot -Force | Out-Null
    [IO.File]::WriteAllText(
        $childScript,
        "[Console]::Out.WriteLine('runtime-diagnostic-stdout')`r`n" +
            "[Console]::Error.WriteLine('runtime-diagnostic-stderr')`r`n" +
            "exit -1`r`n",
        [Text.UTF8Encoding]::new($false)
    )

    $arguments = "-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File `"$childScript`""
    $failureMessage = ''
    try {
        & $resolvedLauncher `
            -FilePath (Join-Path $PSHOME 'powershell.exe') `
            -Arguments $arguments `
            -WaitForExit `
            -TimeoutSeconds 30
    } catch {
        $failureMessage = $_.Exception.Message
    }

    if ([string]::IsNullOrWhiteSpace($failureMessage)) {
        throw 'Runtime launcher unexpectedly reported success for exit code -1.'
    }
    foreach ($expected in @(
        'Runtime process exited with exit code -1',
        'Task Scheduler result: 4294967295',
        'stderr: runtime-diagnostic-stderr',
        'stdout: runtime-diagnostic-stdout'
    )) {
        if (-not $failureMessage.Contains($expected)) {
            throw "Runtime launcher diagnostics are missing '$expected':`n$failureMessage"
        }
    }

    [IO.File]::WriteAllText(
        $childScript,
        "[Console]::Out.WriteLine('runtime-success')`r`nexit 0`r`n",
        [Text.UTF8Encoding]::new($false)
    )
    & $resolvedLauncher `
        -FilePath (Join-Path $PSHOME 'powershell.exe') `
        -Arguments $arguments `
        -WaitForExit `
        -TimeoutSeconds 30

    $afterTasks = @(Get-ScheduledTask -ErrorAction Stop | Where-Object { $_.TaskName.StartsWith($taskPrefix) } | ForEach-Object TaskName)
    $newTasks = @($afterTasks | Where-Object { $_ -notin $beforeTasks })
    if ($newTasks.Count -gt 0) {
        throw "Runtime launcher left temporary scheduled tasks behind: $($newTasks -join ', ')"
    }

    $afterTempDirs = @(Get-ChildItem -LiteralPath ([IO.Path]::GetTempPath()) -Directory -Filter "$tempPrefix*" -ErrorAction SilentlyContinue | ForEach-Object FullName)
    $newTempDirs = @($afterTempDirs | Where-Object { $_ -notin $beforeTempDirs })
    if ($newTempDirs.Count -gt 0) {
        throw "Runtime launcher left temporary diagnostic directories behind: $($newTempDirs -join ', ')"
    }

    Write-Host 'Windows Setup runtime diagnostic launcher validation passed.'
} finally {
    Remove-Item -LiteralPath $testRoot -Recurse -Force -ErrorAction SilentlyContinue
}

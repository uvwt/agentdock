[CmdletBinding()]
param(
    [string] $LauncherPath = '',
    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string] $AgentDockBinary,
    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string] $HiddenHostBinary
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

if ([string]::IsNullOrWhiteSpace($LauncherPath)) {
    $LauncherPath = Join-Path $PSScriptRoot '..\install\launch-windows-process.ps1'
}

$resolvedLauncher = (Resolve-Path -LiteralPath $LauncherPath).Path
$resolvedAgentDockBinary = (Resolve-Path -LiteralPath $AgentDockBinary).Path
$resolvedHiddenHostBinary = (Resolve-Path -LiteralPath $HiddenHostBinary).Path

function Get-PeSubsystem {
    param([Parameter(Mandatory = $true)][string] $Path)

    $stream = [IO.File]::OpenRead($Path)
    $reader = New-Object IO.BinaryReader($stream)
    try {
        $stream.Position = 0x3c
        $peOffset = $reader.ReadInt32()
        $stream.Position = $peOffset
        if ($reader.ReadUInt32() -ne 0x00004550) {
            throw "Invalid PE signature: $Path"
        }
        # IMAGE_OPTIONAL_HEADER.Subsystem is at offset 68 for both PE32 and PE32+.
        $stream.Position = $peOffset + 4 + 20 + 68
        return $reader.ReadUInt16()
    } finally {
        $reader.Dispose()
        $stream.Dispose()
    }
}

if ((Get-PeSubsystem -Path $resolvedHiddenHostBinary) -ne 2) {
    throw 'Setup runtime hidden host must use the Windows GUI subsystem so Task Scheduler cannot create a console window.'
}

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
            -AgentDockBinary $resolvedAgentDockBinary `
            -HiddenHostBinary $resolvedHiddenHostBinary `
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
        -AgentDockBinary $resolvedAgentDockBinary `
        -HiddenHostBinary $resolvedHiddenHostBinary `
        -Arguments $arguments `
        -WaitForExit `
        -TimeoutSeconds 30

    # The non-wait path is used for Tray/background launch. The child must survive after the
    # temporary Task action exits and must not own a console window of its own.
    $detachedMarker = Join-Path $testRoot 'detached-marker.txt'
    $encodedMarker = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($detachedMarker))
    [IO.File]::WriteAllText(
        $childScript,
        "Add-Type -TypeDefinition 'using System; using System.Runtime.InteropServices; public static class AgentDockConsoleProbe { [DllImport(`"kernel32.dll`")] public static extern IntPtr GetConsoleWindow(); }'`r`n" +
            "`$marker = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('$encodedMarker'))`r`n" +
            "`$state = if ([AgentDockConsoleProbe]::GetConsoleWindow() -eq [IntPtr]::Zero) { 'hidden' } else { 'visible' }`r`n" +
            "[IO.File]::WriteAllText(`$marker, `$state, [Text.UTF8Encoding]::new(`$false))`r`n" +
            "Start-Sleep -Milliseconds 750`r`nexit 0`r`n",
        [Text.UTF8Encoding]::new($false)
    )
    & $resolvedLauncher `
        -FilePath (Join-Path $PSHOME 'powershell.exe') `
        -AgentDockBinary $resolvedAgentDockBinary `
        -HiddenHostBinary $resolvedHiddenHostBinary `
        -Arguments $arguments `
        -TimeoutSeconds 30

    $markerDeadline = [DateTime]::UtcNow.AddSeconds(5)
    while (-not (Test-Path -LiteralPath $detachedMarker -PathType Leaf) -and [DateTime]::UtcNow -lt $markerDeadline) {
        Start-Sleep -Milliseconds 100
    }
    if (-not (Test-Path -LiteralPath $detachedMarker -PathType Leaf)) {
        throw 'Detached runtime child did not survive the temporary Task host.'
    }
    $detachedState = (Get-Content -LiteralPath $detachedMarker -Raw).Trim()
    if ($detachedState -ne 'hidden') {
        throw "Detached runtime child unexpectedly owns a console window: $detachedState"
    }

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

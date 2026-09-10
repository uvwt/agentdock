[CmdletBinding()]
param(
    [string] $ManagerPath = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

if ([string]::IsNullOrWhiteSpace($ManagerPath)) {
    $ManagerPath = Join-Path $PSScriptRoot '..\install\manage-windows.ps1'
}
. $ManagerPath -Action task-run-session

function Assert-Equal {
    param($Actual, $Expected, [string] $Message)
    if ($Actual -ne $Expected) {
        throw "$Message Expected=$Expected Actual=$Actual"
    }
}

function Assert-ThrowsLike {
    param([scriptblock] $Action, [string] $ExpectedText)
    try {
        & $Action
    } catch {
        if ($_.Exception.Message -notlike "*$ExpectedText*") {
            throw "Expected error containing '$ExpectedText', got: $($_.Exception.Message)"
        }
        return
    }
    throw "Expected an error containing '$ExpectedText', but no error was thrown."
}

$targetSid = 'S-1-5-21-1000-1000-1000-1001'
$otherSid = 'S-1-5-18'

$currentSession = @(
    [pscustomobject]@{ SessionId = 2; State = 0; UserSid = $targetSid },
    [pscustomobject]@{ SessionId = 4; State = 0; UserSid = $targetSid }
)
Assert-Equal `
    (Select-InteractiveTaskSessionId -Sessions $currentSession -ExpectedUserSid $targetSid -CurrentSessionId 2 -CurrentUserSid $targetSid -ConsoleSessionId 4) `
    2 `
    'Current active caller session should win over another active session.'

$sessionZero = @(
    [pscustomobject]@{ SessionId = 2; State = 0; UserSid = $targetSid },
    [pscustomobject]@{ SessionId = 3; State = 4; UserSid = $targetSid }
)
Assert-Equal `
    (Select-InteractiveTaskSessionId -Sessions $sessionZero -ExpectedUserSid $targetSid -CurrentSessionId 0 -CurrentUserSid $otherSid -ConsoleSessionId 2) `
    2 `
    'Session 0 caller should resolve the unique active target-user session.'

$consoleChoice = @(
    [pscustomobject]@{ SessionId = 2; State = 0; UserSid = $targetSid },
    [pscustomobject]@{ SessionId = 4; State = 0; UserSid = $targetSid }
)
Assert-Equal `
    (Select-InteractiveTaskSessionId -Sessions $consoleChoice -ExpectedUserSid $targetSid -CurrentSessionId 0 -CurrentUserSid $otherSid -ConsoleSessionId 4) `
    4 `
    'Matching active console session should disambiguate multiple active sessions.'

Assert-ThrowsLike {
    Select-InteractiveTaskSessionId `
        -Sessions $consoleChoice `
        -ExpectedUserSid $targetSid `
        -CurrentSessionId 0 `
        -CurrentUserSid $otherSid `
        -ConsoleSessionId 9
} 'Multiple active interactive Windows sessions'

$noActiveSession = @(
    [pscustomobject]@{ SessionId = 2; State = 4; UserSid = $targetSid },
    [pscustomobject]@{ SessionId = 0; State = 0; UserSid = $otherSid }
)
Assert-ThrowsLike {
    Select-InteractiveTaskSessionId `
        -Sessions $noActiveSession `
        -ExpectedUserSid $targetSid `
        -CurrentSessionId 0 `
        -CurrentUserSid $otherSid `
        -ConsoleSessionId ([uint32]::MaxValue)
} 'No active interactive Windows session'

Write-Host 'Windows Task Scheduler session-selection tests passed.'

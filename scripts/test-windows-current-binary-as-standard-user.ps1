[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string] $BinaryPath,
    [int] $Port = 18766
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$resolvedBinary = Resolve-Path -LiteralPath $BinaryPath
$userName = 'adockbin' + [Guid]::NewGuid().ToString('N').Substring(0, 8)
$passwordText = 'AgentDock-Binary-' + [Guid]::NewGuid().ToString('N') + 'Aa1!'
$password = ConvertTo-SecureString $passwordText -AsPlainText -Force
$credential = New-Object System.Management.Automation.PSCredential(".\$userName", $password)
$testRoot = Join-Path $env:PUBLIC ('agentdock-binary-e2e-' + [Guid]::NewGuid().ToString('N'))
$testBinary = Join-Path $testRoot 'agentdock.exe'
$launcherPath = Join-Path $testRoot 'start-current-agentdock.ps1'
$stdoutPath = Join-Path $env:RUNNER_TEMP 'agentdock-current-binary.stdout.log'
$stderrPath = Join-Path $env:RUNNER_TEMP 'agentdock-current-binary.stderr.log'
$healthUrl = "http://127.0.0.1:$Port/healthz"
$launcherProcess = $null

function Stop-TestAgentDock {
    Get-Process -Name 'agentdock' -ErrorAction SilentlyContinue | Where-Object {
        try {
            [string]::Equals(
                [IO.Path]::GetFullPath($_.Path),
                [IO.Path]::GetFullPath($testBinary),
                [StringComparison]::OrdinalIgnoreCase
            )
        } catch {
            $false
        }
    } | Stop-Process -Force -ErrorAction SilentlyContinue
}

try {
    New-LocalUser `
        -Name $userName `
        -Password $password `
        -PasswordNeverExpires `
        -UserMayNotChangePassword | Out-Null

    New-Item -ItemType Directory -Path $testRoot -Force | Out-Null
    Copy-Item -LiteralPath $resolvedBinary -Destination $testBinary -Force
    $escapedBinary = $testBinary.Replace("'", "''")
    $launcher = @"
`$ErrorActionPreference = 'Stop'
`$identity = [System.Security.Principal.WindowsIdentity]::GetCurrent()
`$principal = New-Object System.Security.Principal.WindowsPrincipal(`$identity)
if (`$principal.IsInRole([System.Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw 'Current binary smoke test must run as a non-administrator.'
}
`$env:AGENTDOCK_AUTH_TOKEN = 'agentdock-current-binary-e2e'
`$env:AGENTDOCK_HOST = '127.0.0.1'
`$env:AGENTDOCK_PORT = '$Port'
& '$escapedBinary'
"@
    [IO.File]::WriteAllText($launcherPath, $launcher, (New-Object System.Text.UTF8Encoding($false)))

    $arguments = "-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File `"$launcherPath`""
    $launcherProcess = Start-Process `
        -FilePath 'powershell.exe' `
        -Credential $credential `
        -LoadUserProfile `
        -WorkingDirectory $env:SystemRoot `
        -ArgumentList $arguments `
        -RedirectStandardOutput $stdoutPath `
        -RedirectStandardError $stderrPath `
        -PassThru

    $healthy = $false
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    do {
        Start-Sleep -Milliseconds 500
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri $healthUrl -TimeoutSec 2
            if ($response.StatusCode -eq 200) {
                $healthy = $true
                break
            }
        } catch {
        }
        if ($launcherProcess.HasExited) {
            break
        }
    } while ([DateTime]::UtcNow -lt $deadline)

    if (-not $healthy) {
        if (Test-Path -LiteralPath $stdoutPath) {
            Write-Host '--- current binary stdout ---'
            Get-Content -LiteralPath $stdoutPath | Write-Host
        }
        if (Test-Path -LiteralPath $stderrPath) {
            Write-Host '--- current binary stderr ---'
            Get-Content -LiteralPath $stderrPath | Write-Host
        }
        throw "Current AgentDock binary failed health check at $healthUrl"
    }

    Write-Host 'AgentDock current Windows binary standard-user smoke test passed.'
} finally {
    Stop-TestAgentDock
    if ($null -ne $launcherProcess -and -not $launcherProcess.HasExited) {
        Stop-Process -Id $launcherProcess.Id -Force -ErrorAction SilentlyContinue
    }
    Remove-LocalUser -Name $userName -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $testRoot -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $stdoutPath, $stderrPath -Force -ErrorAction SilentlyContinue
}

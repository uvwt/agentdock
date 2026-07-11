[CmdletBinding()]
param(
    [string] $Version = 'v0.2.5'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$userName = 'adocke2e' + [Guid]::NewGuid().ToString('N').Substring(0, 8)
$passwordText = 'AgentDock-E2E-' + [Guid]::NewGuid().ToString('N') + 'Aa1!'
$password = ConvertTo-SecureString $passwordText -AsPlainText -Force
$credential = [PSCredential]::new(".\$userName", $password)
$testScriptDir = Join-Path $env:PUBLIC ('agentdock-installer-e2e-' + [Guid]::NewGuid().ToString('N'))
$stdoutPath = Join-Path $env:RUNNER_TEMP 'agentdock-installer-e2e.stdout.log'
$stderrPath = Join-Path $env:RUNNER_TEMP 'agentdock-installer-e2e.stderr.log'

try {
    New-LocalUser `
        -Name $userName `
        -Password $password `
        -PasswordNeverExpires `
        -UserMayNotChangePassword | Out-Null

    # New-LocalUser 默认只创建普通本地账户；子进程还会再次验证自身不是管理员。
    New-Item -ItemType Directory -Path $testScriptDir -Force | Out-Null
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'install-windows.ps1') -Destination $testScriptDir -Force
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'test-install-windows-e2e.ps1') -Destination $testScriptDir -Force

    $testScript = Join-Path $testScriptDir 'test-install-windows-e2e.ps1'
    $arguments = "-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File `"$testScript`" -Version $Version"
    $process = Start-Process `
        -FilePath 'powershell.exe' `
        -Credential $credential `
        -LoadUserProfile `
        -WorkingDirectory $env:SystemRoot `
        -ArgumentList $arguments `
        -RedirectStandardOutput $stdoutPath `
        -RedirectStandardError $stderrPath `
        -Wait `
        -PassThru

    if (Test-Path -LiteralPath $stdoutPath) {
        Get-Content -LiteralPath $stdoutPath
    }
    if (Test-Path -LiteralPath $stderrPath) {
        Get-Content -LiteralPath $stderrPath | Write-Host
    }
    if ($process.ExitCode -ne 0) {
        throw "Windows installer E2E failed as standard user with exit code $($process.ExitCode)."
    }
}
finally {
    Remove-LocalUser -Name $userName -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $testScriptDir -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $stdoutPath, $stderrPath -Force -ErrorAction SilentlyContinue
}

Write-Host 'AgentDock Windows standard-user E2E passed.'

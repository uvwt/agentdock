[CmdletBinding()]
param(
    [string] $InstallerPath = '',
    [string] $Version = 'latest',
    [string] $ReleaseBaseUrl = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

if (-not $InstallerPath) {
    $InstallerPath = Join-Path $PSScriptRoot '..\install\install.ps1'
}
$resolvedInstaller = Resolve-Path -LiteralPath $InstallerPath

$userName = 'adocke2e' + [Guid]::NewGuid().ToString('N').Substring(0, 8)
$passwordText = 'AgentDock-E2E-' + [Guid]::NewGuid().ToString('N') + 'Aa1!'
$password = ConvertTo-SecureString $passwordText -AsPlainText -Force
$credential = [PSCredential]::new(".\$userName", $password)
$testScriptDir = Join-Path $env:PUBLIC ('agentdock-installer-e2e-' + [Guid]::NewGuid().ToString('N'))
$stdoutPath = Join-Path $env:RUNNER_TEMP 'agentdock-installer-e2e.stdout.log'
$stderrPath = Join-Path $env:RUNNER_TEMP 'agentdock-installer-e2e.stderr.log'
$completionPath = Join-Path $testScriptDir 'completed.ok'
$contextResultPath = Join-Path $env:PUBLIC ('agentdock-setup-context-' + [Guid]::NewGuid().ToString('N') + '.ini')
$contextInstallDir = Join-Path $env:PUBLIC ('agentdock-setup-context-' + [Guid]::NewGuid().ToString('N') + '\bin')
$contextStdoutPath = Join-Path $env:RUNNER_TEMP 'agentdock-setup-context.stdout.log'
$contextStderrPath = Join-Path $env:RUNNER_TEMP 'agentdock-setup-context.stderr.log'
$childProcessTimeoutSeconds = 600

function Wait-TestProcess {
    param(
        [Parameter(Mandatory = $true)]
        [System.Diagnostics.Process] $Process,
        [Parameter(Mandatory = $true)]
        [string] $Description,
        [Parameter(Mandatory = $true)]
        [string] $StdoutPath,
        [Parameter(Mandatory = $true)]
        [string] $StderrPath,
        [Parameter(Mandatory = $true)]
        [int] $TimeoutSeconds
    )

    # Start-Process -Wait 会等待整个进程树；安装器会启动长期运行的 AgentDock，
    # 因而即使测试 PowerShell 已退出，CI 也可能一直等到 GitHub 的 6 小时上限。
    # Process.WaitForExit(timeout) 只等待这个直接子进程，并给异常路径一个明确上限。
    if (-not $Process.WaitForExit($TimeoutSeconds * 1000)) {
        Write-Host "--- $Description stdout ---"
        if (Test-Path -LiteralPath $StdoutPath) {
            Get-Content -LiteralPath $StdoutPath | Write-Host
        }
        Write-Host "--- $Description stderr ---"
        if (Test-Path -LiteralPath $StderrPath) {
            Get-Content -LiteralPath $StderrPath | Write-Host
        }
        Stop-Process -Id $Process.Id -Force -ErrorAction SilentlyContinue
        throw "$Description timed out after $TimeoutSeconds seconds."
    }
}

try {
    New-LocalUser `
        -Name $userName `
        -Password $password `
        -PasswordNeverExpires `
        -UserMayNotChangePassword | Out-Null

    # New-LocalUser 默认只创建普通本地账户；子进程还会再次验证自身不是管理员。
    New-Item -ItemType Directory -Path $testScriptDir -Force | Out-Null
    Copy-Item -LiteralPath $resolvedInstaller -Destination (Join-Path $testScriptDir 'install.ps1') -Force
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'test-install-windows-e2e.ps1') -Destination $testScriptDir -Force

    $testScript = Join-Path $testScriptDir 'test-install-windows-e2e.ps1'
    $installerScript = Join-Path $testScriptDir 'install.ps1'
    $arguments = "-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File `"$testScript`" -InstallerPath `"$installerScript`" -Version $Version"
    if ($ReleaseBaseUrl) {
        $arguments += " -ReleaseBaseUrl `"$ReleaseBaseUrl`""
    }
    $arguments += " -CompletionFile `"$completionPath`""
    $process = Start-Process `
        -FilePath 'powershell.exe' `
        -Credential $credential `
        -LoadUserProfile `
        -WorkingDirectory $env:SystemRoot `
        -ArgumentList $arguments `
        -RedirectStandardOutput $stdoutPath `
        -RedirectStandardError $stderrPath `
        -PassThru
    Wait-TestProcess `
        -Process $process `
        -Description 'Windows installer standard-user E2E' `
        -StdoutPath $stdoutPath `
        -StderrPath $stderrPath `
        -TimeoutSeconds $childProcessTimeoutSeconds

    if (Test-Path -LiteralPath $stdoutPath) {
        Get-Content -LiteralPath $stdoutPath
    }
    if (Test-Path -LiteralPath $stderrPath) {
        Get-Content -LiteralPath $stderrPath | Write-Host
    }
    # Windows PowerShell 5.1 may leave ExitCode null for Start-Process
    # -Credential even after the direct process exits. The child writes this
    # sentinel only after its complete success path, including finally cleanup.
    if (-not (Test-Path -LiteralPath $completionPath -PathType Leaf)) {
        throw 'Windows installer E2E process exited without reporting successful completion.'
    }

    # Setup must never continue when an over-the-shoulder administrator or any
    # other account owns the process instead of the signed-in desktop user.
    $contextArguments =
        "-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File `"$installerScript`"" +
        " -InstallChannel setup -InstallDir `"$contextInstallDir`"" +
        " -ResultFile `"$contextResultPath`""
    $contextProcess = Start-Process `
        -FilePath 'powershell.exe' `
        -Credential $credential `
        -LoadUserProfile `
        -WorkingDirectory $env:SystemRoot `
        -ArgumentList $contextArguments `
        -RedirectStandardOutput $contextStdoutPath `
        -RedirectStandardError $contextStderrPath `
        -PassThru
    Wait-TestProcess `
        -Process $contextProcess `
        -Description 'Windows Setup user-context guard' `
        -StdoutPath $contextStdoutPath `
        -StderrPath $contextStderrPath `
        -TimeoutSeconds $childProcessTimeoutSeconds

    if (-not (Test-Path -LiteralPath $contextResultPath -PathType Leaf)) {
        throw 'Setup user-context guard did not write its structured result.'
    }
    $contextSuccess = Get-Content -LiteralPath $contextResultPath |
        Where-Object { $_ -like 'Success=*' } |
        Select-Object -First 1
    if ($contextSuccess -ne 'Success=false') {
        throw "Setup user-context guard unexpectedly succeeded: $contextSuccess"
    }
    $contextCode = Get-Content -LiteralPath $contextResultPath |
        Where-Object { $_ -like 'Code=*' } |
        Select-Object -First 1
    if ($contextCode -ne 'Code=setup-elevated-context') {
        throw "Unexpected Setup user-context result: $contextCode"
    }
    if (Test-Path -LiteralPath $contextInstallDir) {
        throw 'Setup user-context guard wrote installation files before rejecting the wrong account.'
    }
    Write-Host 'AgentDock Setup user-context guard passed.'
}
finally {
    Remove-LocalUser -Name $userName -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $testScriptDir -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $stdoutPath, $stderrPath, $contextResultPath, $contextStdoutPath, $contextStderrPath -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath (Split-Path -Parent $contextInstallDir) -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host 'AgentDock Windows standard-user E2E passed.'

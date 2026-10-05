[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string] $AgentDockBinary,
    [Parameter(Mandatory = $true)]
    [string] $SignedCloudflaredBinary,
    [Parameter(Mandatory = $true)]
    [string] $ArtifactUrl,
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9a-fA-F]{64}$')]
    [string] $ExpectedDigest
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

function Get-FreeTcpPort {
    $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
    $listener.Start()
    try { return ([Net.IPEndPoint] $listener.LocalEndpoint).Port } finally { $listener.Stop() }
}

$agentdock = (Resolve-Path -LiteralPath $AgentDockBinary).Path
$cloudflared = (Resolve-Path -LiteralPath $SignedCloudflaredBinary).Path
$signature = Get-AuthenticodeSignature -LiteralPath $cloudflared
if ($signature.Status -ne [Management.Automation.SignatureStatus]::Valid) {
    throw "cloudflared fixture is not Authenticode-valid: $($signature.StatusMessage)"
}
$versionLine = (& $cloudflared --version | Select-Object -First 1).Trim()
$parts = @($versionLine -split '\s+')
if ($parts.Count -lt 3 -or $parts[0] -ne 'cloudflared' -or $parts[1] -ne 'version') {
    throw "Unexpected cloudflared version output: $versionLine"
}
$version = $parts[2]
$digest = (Get-FileHash -LiteralPath $cloudflared -Algorithm SHA256).Hash.ToLowerInvariant()
$expectedDigestNormalized = $ExpectedDigest.ToLowerInvariant()
if ($digest -ne $expectedDigestNormalized) {
    throw "cloudflared fixture digest mismatch: got $digest, want $expectedDigestNormalized"
}

function Invoke-AgentDockJson {
    param([Parameter(Mandatory = $true)][string[]] $Arguments)

    $rawOutput = @(& $agentdock @Arguments)
    $exitCode = $LASTEXITCODE
    if ($exitCode -ne 0) {
        throw "agentdock $($Arguments -join ' ') failed with exit code $exitCode."
    }
    $json = ($rawOutput -join [Environment]::NewLine).Trim()
    if ([string]::IsNullOrWhiteSpace($json)) {
        throw "agentdock $($Arguments -join ' ') returned no JSON output."
    }
    try {
        return ($json | ConvertFrom-Json)
    } catch {
        throw "agentdock $($Arguments -join ' ') returned invalid JSON: $json"
    }
}

$testRoot = Join-Path $env:RUNNER_TEMP ('agentdock-component-lifecycle-' + [Guid]::NewGuid().ToString('N'))
$runtimeRoot = Join-Path $testRoot 'runtime'
$catalogRoot = Join-Path $testRoot 'catalog'
$catalogPath = Join-Path $catalogRoot 'agentdock-component-catalog.json'
New-Item -ItemType Directory -Path $catalogRoot -Force | Out-Null

$catalog = @{
    schema_version = 2
    revision = 1
    components = @(
        @{
            component = 'cloudflared'
            version = $version
            status = 'supported'
            agentdock = @{
                min_version = '0.9.1'
                max_version_exclusive = '2.0.0'
            }
            upstream_version = $version
            upstream_source = "https://github.com/cloudflare/cloudflared/releases/tag/$version"
            artifacts = @(
                @{
                    os = 'windows'
                    arch = 'amd64'
                    format = 'binary'
                    url = $ArtifactUrl
                    sha256 = $expectedDigestNormalized
                }
            )
        }
    )
}
[IO.File]::WriteAllText(
    $catalogPath,
    ($catalog | ConvertTo-Json -Depth 8),
    [Text.UTF8Encoding]::new($false)
)

$port = Get-FreeTcpPort
$catalogUrl = "http://127.0.0.1:$port/agentdock-component-catalog.json"
$server = Start-Process -FilePath 'python' `
    -ArgumentList @('-m', 'http.server', $port, '--bind', '127.0.0.1', '--directory', $catalogRoot) `
    -WindowStyle Hidden -PassThru
try {
    $deadline = [DateTime]::UtcNow.AddSeconds(15)
    do {
        Start-Sleep -Milliseconds 250
        try {
            Invoke-WebRequest -UseBasicParsing -Uri $catalogUrl -TimeoutSec 2 | Out-Null
            break
        } catch {
            if ([DateTime]::UtcNow -ge $deadline) { throw }
        }
    } while ($true)

    $initial = Invoke-AgentDockJson -Arguments @(
        'component', 'status', 'cloudflared',
        '--runtime-root', $runtimeRoot,
        '--json'
    )
    if ($initial.state -ne 'not_installed' -or $initial.ready) {
        throw "Fresh component status is not not_installed: $($initial | ConvertTo-Json -Compress)"
    }

    $installed = Invoke-AgentDockJson -Arguments @(
        'component', 'install', 'cloudflared',
        '--runtime-root', $runtimeRoot,
        '--catalog-url', $catalogUrl,
        '--json'
    )
    if (-not $installed.ready -or $installed.version -ne $version) {
        throw "Catalog install failed: $($installed | ConvertTo-Json -Compress)"
    }
    $managedBinary = [string] $installed.path
    if (-not (Test-Path -LiteralPath $managedBinary -PathType Leaf)) {
        throw "Managed cloudflared missing: $managedBinary"
    }
    $managedSignature = Get-AuthenticodeSignature -LiteralPath $managedBinary
    if ($managedSignature.Status -ne [Management.Automation.SignatureStatus]::Valid) {
        throw "Managed cloudflared lost Authenticode validity: $($managedSignature.StatusMessage)"
    }

    [IO.File]::WriteAllText($managedBinary, 'corrupt', [Text.UTF8Encoding]::new($false))
    $broken = Invoke-AgentDockJson -Arguments @(
        'component', 'status', 'cloudflared',
        '--runtime-root', $runtimeRoot,
        '--json'
    )
    if ($broken.state -ne 'broken' -or $broken.ready) {
        throw "Corrupt component did not enter broken state: $($broken | ConvertTo-Json -Compress)"
    }
    $repaired = Invoke-AgentDockJson -Arguments @(
        'component', 'install', 'cloudflared',
        '--runtime-root', $runtimeRoot,
        '--catalog-url', $catalogUrl,
        '--json'
    )
    if (-not $repaired.ready -or $repaired.version -ne $version) {
        throw "Component repair failed: $($repaired | ConvertTo-Json -Compress)"
    }
    if ((Get-FileHash -LiteralPath $repaired.path -Algorithm SHA256).Hash.ToLowerInvariant() -ne $digest) {
        throw 'Repaired component digest does not match the catalog.'
    }

    $updated = Invoke-AgentDockJson -Arguments @(
        'component', 'update', 'cloudflared',
        '--runtime-root', $runtimeRoot,
        '--catalog-url', $catalogUrl,
        '--json'
    )
    if (-not $updated.ready -or $updated.version -ne $version) {
        throw "Component update failed: $($updated | ConvertTo-Json -Compress)"
    }

    Invoke-AgentDockJson -Arguments @(
        'component', 'uninstall', 'cloudflared',
        '--runtime-root', $runtimeRoot,
        '--json'
    ) | Out-Null
    $removed = Invoke-AgentDockJson -Arguments @(
        'component', 'status', 'cloudflared',
        '--runtime-root', $runtimeRoot,
        '--json'
    )
    if ($removed.state -ne 'not_installed' -or $removed.ready) {
        throw "Component uninstall failed: $($removed | ConvertTo-Json -Compress)"
    }

    $imported = Invoke-AgentDockJson -Arguments @(
        'component', '__import-legacy', 'cloudflared',
        '--runtime-root', $runtimeRoot,
        '--source', $cloudflared,
        '--json'
    )
    if (-not $imported.ready -or $imported.version -ne $version) {
        throw "Legacy component import failed: $($imported | ConvertTo-Json -Compress)"
    }
} finally {
    Stop-Process -Id $server.Id -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $testRoot -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host 'Windows cloudflared component lifecycle test passed.'

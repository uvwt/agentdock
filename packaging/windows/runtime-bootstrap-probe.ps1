# Windows App Runtime bootstrap capability probe helpers.
# Loading this file only defines functions and does not mutate package state.

function Export-AgentDockPayloadEntry {
    param(
        [Parameter(Mandatory = $true)] [object] $Archive,
        [Parameter(Mandatory = $true)] [string[]] $CandidateNames,
        [Parameter(Mandatory = $true)] [string] $Destination
    )
    $entry = $null
    foreach ($candidateName in $CandidateNames) {
        $entry = @($Archive.Entries | Where-Object {
            $_.FullName.Replace('\', '/') -ceq $candidateName
        }) | Select-Object -First 1
        if ($null -ne $entry) {
            break
        }
    }
    if ($null -eq $entry) {
        throw "AgentDock payload does not contain required Runtime probe entry: $($CandidateNames -join ', ')"
    }
    $inputStream = $entry.Open()
    try {
        $outputStream = [IO.File]::Open($Destination, [IO.FileMode]::Create, [IO.FileAccess]::Write, [IO.FileShare]::None)
        try {
            $inputStream.CopyTo($outputStream)
        } finally {
            $outputStream.Dispose()
        }
    } finally {
        $inputStream.Dispose()
    }
    if (-not (Test-Path -LiteralPath $Destination -PathType Leaf) -or
        (Get-Item -LiteralPath $Destination).Length -le 0) {
        throw "Runtime probe extraction produced an empty file: $Destination"
    }
}

function Get-WindowsAppRuntimeProbePayload {
    param(
        [string] $PayloadArchivePath,
        [string] $DestinationDirectory
    )
    if (-not (Test-Path -LiteralPath $PayloadArchivePath -PathType Leaf)) {
        throw "AgentDock payload archive was not found: $PayloadArchivePath"
    }
    New-Item -ItemType Directory -Path $DestinationDirectory -Force | Out-Null
    $bootstrapDllPath = Join-Path $DestinationDirectory 'Microsoft.WindowsAppRuntime.Bootstrap.dll'
    $probeHostPath = Join-Path $DestinationDirectory 'agentdock-shim.exe'

    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $archive = [IO.Compression.ZipFile]::OpenRead($PayloadArchivePath)
    try {
        Export-AgentDockPayloadEntry `
            -Archive $archive `
            -CandidateNames @('control-panel/Microsoft.WindowsAppRuntime.Bootstrap.dll', 'Microsoft.WindowsAppRuntime.Bootstrap.dll') `
            -Destination $bootstrapDllPath
        Export-AgentDockPayloadEntry `
            -Archive $archive `
            -CandidateNames @('agentdock-shim.exe') `
            -Destination $probeHostPath
    } finally {
        $archive.Dispose()
    }
    return [pscustomobject]@{
        BootstrapDllPath = $bootstrapDllPath
        ProbeHostPath = $probeHostPath
    }
}

function Test-WindowsAppRuntimeBootstrap {
    param(
        [string] $ProbeHostPath,
        [string] $BootstrapDllPath,
        [Version] $MinimumVersion
    )
    foreach ($requiredPath in @($ProbeHostPath, $BootstrapDllPath)) {
        if (-not (Test-Path -LiteralPath $requiredPath -PathType Leaf)) {
            throw "Windows App Runtime probe file was not found: $requiredPath"
        }
    }

    $probeOutput = @(& $ProbeHostPath '--windows-app-runtime-probe' $BootstrapDllPath $MinimumVersion.ToString() 2>&1)
    $exitCode = $LASTEXITCODE
    $outputText = (($probeOutput | ForEach-Object { [string] $_ }) -join "`n").Trim()
    if ($exitCode -notin @(0, 3)) {
        throw "Windows App Runtime probe host failed with exit code ${exitCode}: $outputText"
    }

    [Int32] $hresult = 0
    if (-not [Int32]::TryParse(
        $outputText,
        [Globalization.NumberStyles]::Integer,
        [Globalization.CultureInfo]::InvariantCulture,
        [ref] $hresult)) {
        throw "Windows App Runtime probe host returned an invalid HRESULT: $outputText"
    }
    $success = $hresult -ge 0
    if (($exitCode -eq 0) -ne $success) {
        throw "Windows App Runtime probe host exit code $exitCode disagrees with HRESULT $hresult."
    }
    return [pscustomobject]@{
        Success = $success
        HResult = $hresult
    }
}

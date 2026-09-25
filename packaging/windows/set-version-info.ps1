param(
    [Parameter(Mandatory = $true)]
    [string] $Version,

    [Parameter(Mandatory = $true)]
    [string[]] $Path
)

$ErrorActionPreference = 'Stop'

function Normalize-WindowsVersion {
    param([Parameter(Mandatory = $true)][string] $Value)

    $parts = @($Value.Trim().TrimStart('v').Split('.'))
    if ($parts.Count -lt 1 -or $parts.Count -gt 4) {
        throw "Windows version must contain between one and four numeric components: $Value"
    }

    foreach ($part in $parts) {
        if ($part -notmatch '^\d+$') {
            throw "Windows version contains a non-numeric component: $Value"
        }
        if ([int64] $part -gt 65535) {
            throw "Windows version component exceeds 65535: $Value"
        }
    }

    while ($parts.Count -lt 4) {
        $parts += '0'
    }
    return ($parts -join '.')
}

$windowsVersion = Normalize-WindowsVersion -Value $Version
$descriptions = @{
    'agentdock.exe' = 'AgentDock'
    'agentdock-tray.exe' = 'AgentDock Control Panel'
    'agentdock-arbiter.exe' = 'AgentDock Arbiter'
    'agentdock-shim.exe' = 'AgentDock Shim'
    'agentdock-tray-shim.exe' = 'AgentDock Tray Shim'
}
$copyright = 'Copyright AgentDock contributors'
$originalFilenames = @{
    'agentdock-tray.exe' = 'agentdock-tray.dll'
}
$tempRoot = Join-Path ([IO.Path]::GetTempPath()) ('agentdock-winres-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tempRoot -Force | Out-Null

try {
    foreach ($inputPath in $Path) {
        $resolved = (Resolve-Path -LiteralPath $inputPath).Path
        $fileName = [IO.Path]::GetFileName($resolved)
        if (-not $descriptions.ContainsKey($fileName)) {
            throw "Unsupported AgentDock Windows executable for VersionInfo: $fileName"
        }

        $signature = Get-AuthenticodeSignature -LiteralPath $resolved
        if ($signature.Status -ne [Management.Automation.SignatureStatus]::NotSigned) {
            throw "VersionInfo must be applied before Authenticode signing: $fileName ($($signature.Status))"
        }

        $expectedOriginalFilename = if ($originalFilenames.ContainsKey($fileName)) {
            $originalFilenames[$fileName]
        } else {
            $fileName
        }
        $expected = @{
            CompanyName = 'AgentDock'
            FileVersion = $windowsVersion
            LegalCopyright = $copyright
            OriginalFilename = $expectedOriginalFilename
            ProductName = 'AgentDock'
            ProductVersion = $windowsVersion
        }

        $existingInfo = [Diagnostics.FileVersionInfo]::GetVersionInfo($resolved)
        $existingValues = @(
            [string] $existingInfo.CompanyName,
            [string] $existingInfo.FileVersion,
            [string] $existingInfo.LegalCopyright,
            [string] $existingInfo.OriginalFilename,
            [string] $existingInfo.ProductName,
            [string] $existingInfo.ProductVersion
        )
        $hasExistingVersionInfo = @($existingValues | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }).Count -gt 0
        if ($hasExistingVersionInfo) {
            foreach ($entry in $expected.GetEnumerator()) {
                $actual = [string] $existingInfo.($entry.Key)
                if ($actual -ne [string] $entry.Value) {
                    throw "Existing VersionInfo mismatch for $fileName $($entry.Key): '$actual' != '$($entry.Value)'. Build this executable with the required metadata before signing."
                }
            }
            Write-Host "Verified existing AgentDock Windows VersionInfo on $fileName."
            continue
        }

        $internalName = [IO.Path]::GetFileNameWithoutExtension($fileName)
        $resource = [ordered]@{
            RT_VERSION = [ordered]@{
                '#1' = [ordered]@{
                    '0000' = [ordered]@{
                        fixed = [ordered]@{
                            file_version = $windowsVersion
                            product_version = $windowsVersion
                        }
                        info = [ordered]@{
                            '0409' = [ordered]@{
                                CompanyName = 'AgentDock'
                                FileDescription = $descriptions[$fileName]
                                FileVersion = $windowsVersion
                                InternalName = $internalName
                                LegalCopyright = $copyright
                                OriginalFilename = $expectedOriginalFilename
                                ProductName = 'AgentDock'
                                ProductVersion = $windowsVersion
                            }
                        }
                    }
                }
            }
        }

        $jsonPath = Join-Path $tempRoot ($internalName + '.winres.json')
        $json = $resource | ConvertTo-Json -Depth 12
        [IO.File]::WriteAllText($jsonPath, $json, [Text.UTF8Encoding]::new($false))

        & go run github.com/tc-hib/go-winres@v0.3.3 patch `
            --in $jsonPath `
            --product-version $windowsVersion `
            --file-version $windowsVersion `
            --no-backup `
            $resolved
        if ($LASTEXITCODE -ne 0) {
            throw "go-winres failed for ${fileName}: exit code $LASTEXITCODE"
        }

        $info = [Diagnostics.FileVersionInfo]::GetVersionInfo($resolved)
        foreach ($entry in $expected.GetEnumerator()) {
            $actual = [string] $info.($entry.Key)
            if ($actual -ne [string] $entry.Value) {
                throw "VersionInfo mismatch for $fileName $($entry.Key): '$actual' != '$($entry.Value)'"
            }
        }
    }
} finally {
    Remove-Item -LiteralPath $tempRoot -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host "Applied AgentDock Windows VersionInfo $windowsVersion to $($Path.Count) executable(s)."

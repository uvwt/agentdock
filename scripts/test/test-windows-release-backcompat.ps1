param(
    [Parameter(Mandatory = $true)]
    [string] $ArchivePath,
    [string] $TargetVersion = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$archive = (Resolve-Path -LiteralPath $ArchivePath).Path
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$tempBase = if (-not [string]::IsNullOrWhiteSpace($env:RUNNER_TEMP)) { $env:RUNNER_TEMP } else { [IO.Path]::GetTempPath() }
$utf8NoBom = [Text.UTF8Encoding]::new($false)

if ([string]::IsNullOrWhiteSpace($TargetVersion)) {
    $versionProbeRoot = Join-Path $tempBase ('agentdock-backcompat-version-' + [Guid]::NewGuid().ToString('N'))
    try {
        Expand-Archive -LiteralPath $archive -DestinationPath $versionProbeRoot -Force
        $versionOutput = (& (Join-Path $versionProbeRoot 'agentdock.exe') --version | Out-String).Trim()
        if ($LASTEXITCODE -ne 0 -or $versionOutput -notmatch '^AgentDock v(?<version>[0-9]+\.[0-9]+\.[0-9]+)') {
            throw "cannot derive Windows Release version from: $versionOutput"
        }
        $TargetVersion = $Matches.version
    } finally {
        Remove-Item -LiteralPath $versionProbeRoot -Recurse -Force -ErrorAction SilentlyContinue
    }
}

$testSource = @'
package selfupdate

import (
    "context"
    "os"
    "path/filepath"
    "testing"
)

func TestCurrentWindowsReleaseAcceptedByPublishedUpdater(t *testing.T) {
    archivePath := os.Getenv("AGENTDOCK_BACKCOMPAT_ARCHIVE")
    targetVersion := os.Getenv("AGENTDOCK_BACKCOMPAT_VERSION")
    archive, err := os.ReadFile(archivePath)
    if err != nil {
        t.Fatal(err)
    }
    root, err := extractDesktopUpdateArchive(context.Background(), archive, t.TempDir(), targetVersion)
    if err != nil {
        t.Fatalf("published updater rejected current Release ZIP: %v", err)
    }
    if _, err := os.Stat(filepath.Join(root, "manage-windows.ps1")); err != nil {
        t.Fatalf("published updater did not stage compatibility manager: %v", err)
    }
}
'@

foreach ($tag in @('v0.8.2', 'v0.8.3')) {
    $worktree = Join-Path $tempBase ("agentdock-backcompat-" + $tag.Replace('.', '-') + '-' + [Guid]::NewGuid().ToString('N'))
    try {
        & git -C $repoRoot worktree add --detach $worktree $tag
        if ($LASTEXITCODE -ne 0) {
            throw "git worktree add failed for $tag with exit code $LASTEXITCODE"
        }
        $testPath = Join-Path $worktree 'internal\selfupdate\zz_release_backcompat_test.go'
        [IO.File]::WriteAllText($testPath, $testSource, $utf8NoBom)
        $env:AGENTDOCK_BACKCOMPAT_ARCHIVE = $archive
        $env:AGENTDOCK_BACKCOMPAT_VERSION = $TargetVersion
        Push-Location $worktree
        try {
            & go test ./internal/selfupdate -run '^TestCurrentWindowsReleaseAcceptedByPublishedUpdater$' -count=1
            if ($LASTEXITCODE -ne 0) {
                throw "$tag updater compatibility test failed with exit code $LASTEXITCODE"
            }
        } finally {
            Pop-Location
        }
        Write-Host "$tag updater accepted current Windows Release ZIP."
    } finally {
        Remove-Item Env:\AGENTDOCK_BACKCOMPAT_ARCHIVE -ErrorAction SilentlyContinue
        Remove-Item Env:\AGENTDOCK_BACKCOMPAT_VERSION -ErrorAction SilentlyContinue
        & git -C $repoRoot worktree remove --force $worktree 2>$null
        Remove-Item -LiteralPath $worktree -Recurse -Force -ErrorAction SilentlyContinue
    }
}

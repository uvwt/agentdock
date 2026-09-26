[CmdletBinding()]
param([string]$ReportDirectory = (Join-Path ([IO.Path]::GetTempPath()) ('agentdock-ui-checks-' + [Guid]::NewGuid().ToString('N'))))
$ErrorActionPreference = 'Stop'
if ($env:OS -ne 'Windows_NT') { throw 'The WPF and tray checks require an interactive Windows desktop.' }
$uiProject = Join-Path $PSScriptRoot '../control-panel/AgentDock.ControlPanel.csproj'
$testProject = Join-Path $PSScriptRoot 'AgentDock.ControlPanel.Tests.csproj'
$runner = Join-Path $PSScriptRoot 'bin/Release/net8.0-windows10.0.19041.0/agentdock-ui-tests.dll'
$ReportDirectory = [IO.Path]::GetFullPath($ReportDirectory)
& dotnet build $uiProject -c Release -p:SelfContained=false -p:PublishSingleFile=false --nologo
if ($LASTEXITCODE -ne 0) { throw 'Control-panel build failed.' }
& dotnet build $testProject -c Release --nologo
if ($LASTEXITCODE -ne 0) { throw 'UI test build failed.' }
& dotnet $runner --verify --output $ReportDirectory
if ($LASTEXITCODE -ne 0) { throw 'Chinese UI checks failed.' }
& dotnet $runner --verify --english --output $ReportDirectory
if ($LASTEXITCODE -ne 0) { throw 'English UI checks failed.' }
foreach ($name in @('ui-verification.json', 'english/ui-verification.json')) {
    $report = Get-Content (Join-Path $ReportDirectory $name) -Raw -Encoding UTF8 | ConvertFrom-Json
    if (-not $report.passed) { throw 'UI verification contains failed checks.' }
    [pscustomobject]@{ Report = $name; Passed = $report.passed; Checks = @($report.tests.PSObject.Properties).Count }
}

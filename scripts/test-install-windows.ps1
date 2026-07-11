[CmdletBinding()]
param(
    [string] $InstallerPath = (Join-Path $PSScriptRoot 'install-windows.ps1')
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile(
    (Resolve-Path -LiteralPath $InstallerPath),
    [ref] $tokens,
    [ref] $errors
)
if ($errors.Count -gt 0) {
    $errors | ForEach-Object { Write-Error $_.Message }
    throw "$InstallerPath contains PowerShell syntax errors"
}

$aclFunction = $ast.Find({
    param($node)
    $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and
        $node.Name -eq 'Set-PrivateAcl'
}, $true)
if ($null -eq $aclFunction) {
    throw "Set-PrivateAcl was not found in $InstallerPath"
}
Invoke-Expression $aclFunction.Extent.Text

$testRoot = Join-Path ([IO.Path]::GetTempPath()) ('agentdock-acl-' + [Guid]::NewGuid().ToString('N'))
try {
    New-Item -ItemType Directory -Path $testRoot -Force | Out-Null
    $testFile = Join-Path $testRoot 'token.dpapi'
    [IO.File]::WriteAllText($testFile, 'test', [Text.UTF8Encoding]::new($false))

    Set-PrivateAcl $testRoot
    Set-PrivateAcl $testFile

    $expectedSids = @(
        [System.Security.Principal.WindowsIdentity]::GetCurrent().User.Value,
        'S-1-5-18',
        'S-1-5-32-544'
    ) | Sort-Object -Unique

    foreach ($target in @($testRoot, $testFile)) {
        $acl = Get-Acl -LiteralPath $target
        if (-not $acl.AreAccessRulesProtected) {
            throw "ACL inheritance is still enabled for $target"
        }

        $rules = @($acl.Access)
        $actualSids = @($rules | ForEach-Object {
            $_.IdentityReference.Translate([System.Security.Principal.SecurityIdentifier]).Value
        } | Sort-Object -Unique)
        if (Compare-Object -ReferenceObject $expectedSids -DifferenceObject $actualSids) {
            throw "Unexpected ACL identities for ${target}: $($actualSids -join ', ')"
        }
        foreach ($rule in $rules) {
            $hasFullControl = ($rule.FileSystemRights -band [System.Security.AccessControl.FileSystemRights]::FullControl) -eq
                [System.Security.AccessControl.FileSystemRights]::FullControl
            if ($rule.AccessControlType -ne [System.Security.AccessControl.AccessControlType]::Allow -or -not $hasFullControl) {
                throw "Unexpected ACL rule for ${target}: $rule"
            }
        }
    }
}
finally {
    Remove-Item -LiteralPath $testRoot -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host 'Windows installer validation passed.'

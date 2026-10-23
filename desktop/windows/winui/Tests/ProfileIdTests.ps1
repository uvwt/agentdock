Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$sourcePath = Join-Path $PSScriptRoot '../AcpProfileId.cs'
$source = [IO.File]::ReadAllText($sourcePath)
$tests = @'
public static class AgentDockProfileIdRegression
{
    public static void Run()
    {
        Check("My Agent", new string[0], "my-agent");
        Check("\u4e2d\u6587\u52a9\u624b", new string[0], "custom");
        Check("\u4e2d\u6587 Codex", new string[0], "codex");
        Check("\ud83d\ude80", new string[0], "custom");
        Check("\u4e2d\u6587", new[] { "custom", "custom-2" }, "custom-3");
        Check(new string('a', 100), new string[0], new string('a', 64));
        Check(new string('a', 100), new[] { new string('a', 64), new string('a', 62) + "-2" }, new string('a', 62) + "-3");
        Check("Already.Valid_1", new string[0], "already.valid_1");
    }

    private static void Check(string name, string[] existing, string expected)
    {
        var actual = AgentDock.ControlPanel.AcpProfileId.Create(name, existing);
        if (actual != expected)
            throw new System.Exception("Unexpected profile identifier: " + actual + "; expected: " + expected);
        if (!System.Text.RegularExpressions.Regex.IsMatch(actual, "^[a-z0-9._-]{1,64}$"))
            throw new System.Exception("Identifier violates the runtime contract: " + actual);
        if (actual != AgentDock.ControlPanel.AcpProfileId.Create(name, existing))
            throw new System.Exception("Identifier generation is not deterministic.");
    }
}
'@

Add-Type -TypeDefinition ($source + [Environment]::NewLine + $tests) -Language CSharp
[AgentDockProfileIdRegression]::Run()
Write-Output 'Custom profile identifier regression: 8 cases passed.'

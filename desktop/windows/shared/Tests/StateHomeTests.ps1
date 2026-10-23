$ErrorActionPreference = 'Stop'
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../../..'))
$modelsPath = Join-Path $repoRoot 'desktop/windows/shared/Models/RuntimeModels.cs'
$modelsSource = [IO.File]::ReadAllText($modelsPath)
$testSource = @'
public static class StateHomeRegressionTests
{
    public static void Run()
    {
        var userHome = System.IO.Path.Combine(System.IO.Path.GetTempPath(), "agentdock-home-review");
        var configuredHome = System.IO.Path.Combine(userHome, "configured-state");
        var environmentHome = System.IO.Path.Combine(userHome, "environment-state");
        var manifest = System.Text.Json.JsonSerializer.Deserialize<RuntimeManifest>(
            "{\"agentdock_home\":" + System.Text.Json.JsonSerializer.Serialize(configuredHome) + "}")!;
        Expect(configuredHome, AgentDockStatePaths.ResolveHome(manifest, environmentHome, userHome));
        Expect(environmentHome, AgentDockStatePaths.ResolveHome(new RuntimeManifest(), environmentHome, userHome));
        Expect(System.IO.Path.Combine(userHome, ".agentdock"), AgentDockStatePaths.ResolveHome(new RuntimeManifest(), " ", userHome));
        Expect(System.IO.Path.Combine(configuredHome, "nexus", "device.json"),
            AgentDockStatePaths.NexusIdentityPath(manifest, environmentHome, userHome));
        System.Console.WriteLine("Windows state home: manifest, environment, legacy default and identity path passed.");
    }

    private static void Expect(string expected, string actual)
    {
        if (!System.String.Equals(expected, actual, System.StringComparison.OrdinalIgnoreCase))
            throw new System.InvalidOperationException("Expected " + expected + ", got " + actual);
    }
}
'@
$source = "using System;`nusing System.IO;`nusing System.Collections.Generic;`n" + $modelsSource + "`n" + $testSource
$references = @(Get-ChildItem (Join-Path $PSHOME 'ref') -Filter '*.dll' | ForEach-Object FullName)
Add-Type -TypeDefinition $source -ReferencedAssemblies $references -CompilerOptions '/nullable:enable' -WarningAction SilentlyContinue
[AgentDock.ControlPanel.StateHomeRegressionTests]::Run()

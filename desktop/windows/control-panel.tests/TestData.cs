using System.IO;
using AgentDock.ControlPanel;

internal static class TestData
{
    internal static string RepositoryRoot
    {
        get
        {
            for (var dir = new DirectoryInfo(AppContext.BaseDirectory); dir is not null; dir = dir.Parent)
                if (File.Exists(Path.Combine(dir.FullName, "go.mod"))) return dir.FullName;
            throw new DirectoryNotFoundException("Run the checks from a repository build output.");
        }
    }

    internal static RuntimeSnapshot CreateSnapshot() => new(
        new RuntimeManifest { ListenPort = 8765, PrivilegeMode = "standard" },
        new ControlPanelSettings { Port = 8765, LogLevel = "info", McpAppsMode = "full", BrowserEnabled = false, AcpEnabled = false },
        "0.9.1", true, true, false,
        "http://127.0.0.1:8765/mcp", "https://example.invalid", "https://example.invalid/mcp", "https://example.invalid",
        "none", false, false, false,
        new NexusDeviceStatus(false, "", "", "", false), false,
        new DateTimeOffset(2026, 9, 26, 12, 0, 0, TimeSpan.Zero));
}

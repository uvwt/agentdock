using System.Text.Json.Serialization;

namespace AgentDock.ControlPanel;

public sealed class RuntimeManifest
{
    [JsonPropertyName("install_root")]
    public string InstallRoot { get; set; } = "";

    [JsonPropertyName("agentdock_binary")]
    public string BinaryPath { get; set; } = "";

    [JsonPropertyName("agentdock_launcher")]
    public string LauncherPath { get; set; } = "";

    [JsonPropertyName("local_mcp_url")]
    public string LocalMcpUrl { get; set; } = "";

    [JsonPropertyName("public_mcp_url")]
    public string PublicMcpUrl { get; set; } = "";

    [JsonPropertyName("public_url")]
    public string PublicUrl { get; set; } = "";

    [JsonPropertyName("port")]
    public int ListenPort { get; set; } = 8765;

    [JsonPropertyName("privilege_mode")]
    public string PrivilegeMode { get; set; } = "standard";

    [JsonPropertyName("agentdock_task_name")]
    public string AgentDockTaskName { get; set; } = "AgentDock";

    [JsonPropertyName("startup_value_name")]
    public string StartupValueName { get; set; } = "AgentDock";

    [JsonPropertyName("tray_binary")]
    public string TrayBinaryPath { get; set; } = "";

    [JsonPropertyName("tray_startup_value_name")]
    public string TrayStartupValueName { get; set; } = "AgentDockTray";

    [JsonPropertyName("tunnel_mode")]
    public string TunnelMode { get; set; } = "none";

    [JsonPropertyName("cloudflared_binary")]
    public string CloudflaredBinary { get; set; } = "";

    [JsonPropertyName("cloudflared_launcher")]
    public string CloudflaredLauncher { get; set; } = "";

    [JsonPropertyName("cloudflared_startup_value_name")]
    public string CloudflaredStartupValueName { get; set; } = "AgentDockCloudflared";
}

public sealed class ControlPanelSettings
{
    [JsonPropertyName("port")]
    public int Port { get; set; } = 8765;

    [JsonPropertyName("log_level")]
    public string LogLevel { get; set; } = "info";

    [JsonPropertyName("oauth_access_token_ttl")]
    public string OAuthAccessTokenTtl { get; set; } = "";

    [JsonPropertyName("mcp_apps_enabled")]
    public bool McpAppsEnabled { get; set; } = true;

    [JsonPropertyName("browser_enabled")]
    public bool BrowserEnabled { get; set; }

    [JsonPropertyName("browser_cdp_url")]
    public string BrowserCdpUrl { get; set; } = "";

    [JsonPropertyName("browser_reuse_existing_cdp")]
    public bool BrowserReuseExistingCdp { get; set; }

    [JsonPropertyName("acp_enabled")]
    public bool AcpEnabled { get; set; }

    [JsonPropertyName("acp_agent")]
    public string AcpAgent { get; set; } = "codex";

    [JsonPropertyName("acp_command")]
    public string AcpCommand { get; set; } = "";

    [JsonPropertyName("acp_args")]
    public List<string> AcpArgs { get; set; } = [];
}

public sealed class CoreVersionInfo
{
    [JsonPropertyName("version")]
    public string Version { get; set; } = "";
}

internal sealed class NativeServiceStatus
{
    [JsonPropertyName("nexus_connected")]
    public bool NexusConnected { get; set; }
}

public sealed record RuntimeSnapshot(
    RuntimeManifest Manifest,
    ControlPanelSettings Settings,
    string Version,
    bool CoreRunning,
    bool Healthy,
    bool CloudflaredRunning,
    string LocalMcpUrl,
    string PublicOrigin,
    string PublicMcpUrl,
    string SavedNamedOrigin,
    string TunnelMode,
    bool CoreStartupEnabled,
    bool TrayStartupEnabled,
    bool TunnelTokenStored,
    NexusDeviceStatus Nexus,
    bool NexusConnected,
    DateTimeOffset CheckedAt);

public sealed record NexusDeviceStatus(
    bool Paired,
    string Endpoint,
    string NodeId,
    string DeviceId,
    bool DeviceTokenStored,
    string Error = "");

internal sealed class NexusDeviceIdentity
{
    [JsonPropertyName("endpoint")]
    public string Endpoint { get; set; } = "";

    [JsonPropertyName("node_id")]
    public string NodeId { get; set; } = "";

    [JsonPropertyName("device_id")]
    public string DeviceId { get; set; } = "";

    [JsonPropertyName("device_token")]
    public string DeviceToken { get; set; } = "";
}

public sealed record UrlTestResult(bool Success, int? StatusCode, TimeSpan Elapsed, string Message);

public sealed class UpdateCheckResult
{
    [JsonPropertyName("current_version")]
    public string CurrentVersion { get; set; } = "";

    [JsonPropertyName("latest_version")]
    public string LatestVersion { get; set; } = "";

    [JsonPropertyName("update_available")]
    public bool UpdateAvailable { get; set; }

    [JsonPropertyName("message")]
    public string Message { get; set; } = "";
}

public sealed record UpdateProgress(int Percentage, string Message);

public sealed record AcpAdapterResolution(bool Available, string Command, IReadOnlyList<string> Arguments, string Message);

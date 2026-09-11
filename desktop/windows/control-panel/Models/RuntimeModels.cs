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
    [JsonPropertyName("running")]
    public bool Running { get; set; }

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

public sealed record UpdateProgress(int? Percentage, bool IsIndeterminate, string Message);

internal sealed class UpdateProgressEvent
{
    [JsonPropertyName("schema_version")]
    public int SchemaVersion { get; set; }

    [JsonPropertyName("type")]
    public string Type { get; set; } = "";

    [JsonPropertyName("stage")]
    public string Stage { get; set; } = "";

    [JsonPropertyName("asset")]
    public string Asset { get; set; } = "";

    [JsonPropertyName("bytes_read")]
    public long? BytesRead { get; set; }

    [JsonPropertyName("total_bytes")]
    public long? TotalBytes { get; set; }

    [JsonPropertyName("error")]
    public string Error { get; set; } = "";
}

internal sealed class UpdateTransactionState
{
    [JsonPropertyName("schema_version")]
    public int SchemaVersion { get; set; }

    [JsonPropertyName("transaction_id")]
    public string TransactionId { get; set; } = "";

    [JsonPropertyName("platform")]
    public string Platform { get; set; } = "";

    [JsonPropertyName("source_version")]
    public string SourceVersion { get; set; } = "";

    [JsonPropertyName("target_version")]
    public string TargetVersion { get; set; } = "";

    [JsonPropertyName("state")]
    public string State { get; set; } = "";

    [JsonPropertyName("phase")]
    public string Phase { get; set; } = "";

    [JsonPropertyName("windows")]
    public WindowsUpdatePlan? Windows { get; set; }
}

internal sealed class WindowsUpdatePlan
{
    [JsonPropertyName("progress_ui_handoff")]
    public bool ProgressUiHandoff { get; set; }
}

internal sealed class UpdateTerminalResult
{
    [JsonPropertyName("schema_version")]
    public int SchemaVersion { get; set; }

    [JsonPropertyName("transaction_id")]
    public string TransactionId { get; set; } = "";

    [JsonPropertyName("platform")]
    public string Platform { get; set; } = "";

    [JsonPropertyName("state")]
    public string State { get; set; } = "";

    [JsonPropertyName("failure")]
    public UpdateFailure? Failure { get; set; }

    [JsonPropertyName("warnings")]
    public string[] Warnings { get; set; } = [];
}

internal sealed class UpdateFailure
{
    [JsonPropertyName("message")]
    public string Message { get; set; } = "";
}

internal sealed class UpdateUiHandoffAck
{
    [JsonPropertyName("schema_version")]
    public int SchemaVersion { get; set; } = 1;

    [JsonPropertyName("transaction_id")]
    public string TransactionId { get; set; } = "";
}

public sealed record AcpAdapterResolution(bool Available, string Command, IReadOnlyList<string> Arguments, string Message);

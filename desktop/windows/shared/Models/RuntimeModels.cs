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

public sealed class AcpProfileSettings
{
    [JsonPropertyName("id")]
    public string Id { get; set; } = "";

    [JsonPropertyName("display_name")]
    public string DisplayName { get; set; } = "";

    [JsonPropertyName("kind")]
    public string Kind { get; set; } = "custom";

    [JsonPropertyName("command")]
    public string Command { get; set; } = "";

    [JsonPropertyName("args")]
    public List<string> Args { get; set; } = [];

    [JsonPropertyName("env_from_env")]
    public Dictionary<string, string>? EnvFromEnv { get; set; }

    [JsonPropertyName("enabled")]
    public bool Enabled { get; set; } = true;
}

public sealed class ControlPanelSettings
{
    [JsonPropertyName("port")]
    public int Port { get; set; } = 8765;

    [JsonPropertyName("log_level")]
    public string LogLevel { get; set; } = "info";

    [JsonPropertyName("oauth_access_token_ttl")]
    public string OAuthAccessTokenTtl { get; set; } = "";

    [JsonPropertyName("mcp_apps_mode")]
    public string McpAppsMode { get; set; } = "";

    [JsonPropertyName("mcp_apps_enabled")]
    [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
    public bool? LegacyMcpAppsEnabled { get; set; }

    [JsonPropertyName("browser_enabled")]
    public bool BrowserEnabled { get; set; }

    [JsonPropertyName("browser_cdp_url")]
    public string BrowserCdpUrl { get; set; } = "";

    [JsonPropertyName("browser_reuse_existing_cdp")]
    public bool BrowserReuseExistingCdp { get; set; }

    [JsonPropertyName("acp_enabled")]
    public bool AcpEnabled { get; set; }

    [JsonPropertyName("acp_profiles")]
    public List<AcpProfileSettings> AcpProfiles { get; set; } = [];

    [JsonPropertyName("acp_default_profile")]
    public string AcpDefaultProfile { get; set; } = "";
}


internal sealed class LegacyAcpControlPanelSettings
{
    [JsonPropertyName("acp_agent")]
    public string AcpAgent { get; set; } = "";

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

internal sealed class NativeTunnelStatus
{
    [JsonPropertyName("mode")]
    public string Mode { get; set; } = "none";

    [JsonPropertyName("running")]
    public bool Running { get; set; }

    [JsonPropertyName("ready")]
    public bool Ready { get; set; }

    [JsonPropertyName("public_url")]
    public string PublicUrl { get; set; } = "";

    [JsonPropertyName("dependency_state")]
    public string DependencyState { get; set; } = "";

    [JsonPropertyName("component_version")]
    public string ComponentVersion { get; set; } = "";
}

public sealed class ComponentStatus
{
    [JsonPropertyName("component")]
    public string Component { get; set; } = "";

    [JsonPropertyName("state")]
    public string State { get; set; } = "not_installed";

    [JsonPropertyName("installed")]
    public bool Installed { get; set; }

    [JsonPropertyName("ready")]
    public bool Ready { get; set; }

    [JsonPropertyName("version")]
    public string Version { get; set; } = "";

    [JsonPropertyName("detail")]
    public string Detail { get; set; } = "";
}

public sealed record ComponentProgress(string Stage, long Bytes, long Total);

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
    string CloudflaredComponentState,
    string CloudflaredComponentVersion,
    bool CoreStartupEnabled,
    bool TrayStartupEnabled,
    bool TunnelTokenStored,
    NexusDeviceStatus Nexus,
    bool NexusConnected,
    DateTimeOffset CheckedAt);

public sealed record PublicEndpointCheckResult(
    bool IsReachable,
    string Message,
    long? LatencyMilliseconds,
    int? HttpStatusCode = null);

public sealed record RuntimeExtensionOverview(
    bool Available,
    int SkillCount,
    int PluginCount,
    bool PluginsAvailable,
    int McpCount)
{
    public static RuntimeExtensionOverview Unavailable { get; } = new(false, 0, 0, false, 0);
}

internal sealed class RuntimeOverviewCountPayload
{
    [JsonPropertyName("count")]
    public int Count { get; set; }
}

internal sealed class RuntimeOverviewPluginsPayload
{
    [JsonPropertyName("count")]
    public int Count { get; set; }

    [JsonPropertyName("available")]
    public bool Available { get; set; }
}

internal sealed class RuntimeOverviewPayload
{
    [JsonPropertyName("skills")]
    public RuntimeOverviewCountPayload Skills { get; set; } = new();

    [JsonPropertyName("plugins")]
    public RuntimeOverviewPluginsPayload Plugins { get; set; } = new();

    [JsonPropertyName("mcp")]
    public RuntimeOverviewCountPayload Mcp { get; set; } = new();
}

public sealed record NexusDeviceStatus(
    bool Paired,
    string Endpoint,
    string NodeId,
    string DeviceId,
    bool DeviceTokenStored,
    string Error = "");

public sealed record NexusConnectionSnapshot(
    NexusDeviceStatus Nexus,
    bool NexusConnected);

public sealed class RuntimeRecentCall
{
    [JsonPropertyName("id")]
    public string Id { get; set; } = "";

    [JsonPropertyName("tool")]
    public string Tool { get; set; } = "";

    [JsonPropertyName("source")]
    public string Source { get; set; } = "";

    [JsonPropertyName("started_at")]
    public DateTimeOffset StartedAt { get; set; }

    [JsonPropertyName("duration_ms")]
    public double DurationMs { get; set; }

    [JsonPropertyName("success")]
    public bool Success { get; set; }

    [JsonPropertyName("error_code")]
    public string ErrorCode { get; set; } = "";
}

public sealed class RuntimeDiagnosticsPayload
{
    [JsonPropertyName("recent_calls")]
    public List<RuntimeRecentCall> RecentCalls { get; set; } = [];
}

public sealed class RuntimeAnalyticsStage
{
    [JsonPropertyName("name")]
    public string Name { get; set; } = "";

    [JsonPropertyName("started_offset_ms")]
    public double StartedOffsetMs { get; set; }

    [JsonPropertyName("duration_ms")]
    public double DurationMs { get; set; }

    [JsonPropertyName("success")]
    public bool Success { get; set; }
}

public sealed class RuntimeAnalyticsCall
{
    [JsonPropertyName("id")]
    public ulong Id { get; set; }

    [JsonPropertyName("tool")]
    public string Tool { get; set; } = "";

    [JsonPropertyName("source")]
    public string Source { get; set; } = "";

    [JsonPropertyName("started_at")]
    public DateTimeOffset StartedAt { get; set; }

    [JsonPropertyName("duration_ms")]
    public double DurationMs { get; set; }

    [JsonPropertyName("success")]
    public bool Success { get; set; }

    [JsonPropertyName("error_code")]
    public string ErrorCode { get; set; } = "";

    [JsonPropertyName("error_category")]
    public string ErrorCategory { get; set; } = "";

    [JsonPropertyName("stages")]
    public List<RuntimeAnalyticsStage> Stages { get; set; } = [];
}

public sealed class RuntimeToolStats
{
    [JsonPropertyName("tool")]
    public string Tool { get; set; } = "";

    [JsonPropertyName("count")]
    public int Count { get; set; }

    [JsonPropertyName("error_count")]
    public int ErrorCount { get; set; }

    [JsonPropertyName("error_rate")]
    public double ErrorRate { get; set; }

    [JsonPropertyName("p50_duration_ms")]
    public double P50DurationMs { get; set; }

    [JsonPropertyName("p95_duration_ms")]
    public double P95DurationMs { get; set; }

    [JsonPropertyName("p99_duration_ms")]
    public double P99DurationMs { get; set; }
}

public sealed class RuntimeProcessSnapshot
{
    [JsonPropertyName("goroutines")]
    public int Goroutines { get; set; }

    [JsonPropertyName("heap_alloc_bytes")]
    public ulong HeapAllocBytes { get; set; }

    [JsonPropertyName("heap_inuse_bytes")]
    public ulong HeapInuseBytes { get; set; }

    [JsonPropertyName("heap_sys_bytes")]
    public ulong HeapSysBytes { get; set; }

    [JsonPropertyName("gc_cycles")]
    public uint GcCycles { get; set; }

    [JsonPropertyName("uptime_ms")]
    public long UptimeMs { get; set; }
}

public sealed class RuntimeAnalyticsPayload
{
    [JsonPropertyName("recent_capacity")]
    public int RecentCapacity { get; set; }

    [JsonPropertyName("window_calls")]
    public int WindowCalls { get; set; }

    [JsonPropertyName("total_calls")]
    public ulong TotalCalls { get; set; }

    [JsonPropertyName("total_errors")]
    public ulong TotalErrors { get; set; }

    [JsonPropertyName("active_calls")]
    public int ActiveCalls { get; set; }

    [JsonPropertyName("tool_stats")]
    public List<RuntimeToolStats> ToolStats { get; set; } = [];

    [JsonPropertyName("recent_calls")]
    public List<RuntimeAnalyticsCall> RecentCalls { get; set; } = [];

    [JsonPropertyName("process")]
    public RuntimeProcessSnapshot Process { get; set; } = new();
}

public sealed record RuntimeDashboardSnapshot(
    bool CountsAvailable,
    bool DiagnosticsAvailable,
    int SkillCount,
    int McpCount,
    int PluginCount,
    IReadOnlyList<RuntimeRecentCall> RecentCalls)
{
    public static RuntimeDashboardSnapshot Empty { get; } =
        new(false, false, 0, 0, 0, []);
}

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

    [JsonPropertyName("bytes")]
    public long? Bytes { get; set; }

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

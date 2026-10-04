using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Navigation;

namespace AgentDock.ControlPanel;

public sealed partial class HomePage : Page
{
    private enum CapabilityState
    {
        Enabled,
        Disabled,
        Attention
    }

    private RuntimeService? _runtime;
    private RuntimeSnapshot? _snapshot;

    internal event EventHandler<string>? ShortcutRequested;

    public HomePage()
    {
        InitializeComponent();
        PageTitle.Text = UiText.Get("Home");
        ConnectionSection.Title = UiText.Get("Connection");
        RemoteConnectionLabel.Text = UiText.Get("RemoteConnection");
        CapabilitiesSection.Title = UiText.Get("Capabilities");
        ConnectionsShortcutLabel.Text = UiText.Get("Connections");
        CapabilitiesShortcutLabel.Text = UiText.Get("Capabilities");
        ActivityShortcutLabel.Text = UiText.Get("Activity");
    }

    protected override async void OnNavigatedTo(NavigationEventArgs e)
    {
        _runtime = e.Parameter as RuntimeService;
        await RefreshAsync();
    }

    private async Task RefreshAsync()
    {
        if (_runtime is null) return;

        _snapshot = await _runtime.GetSnapshotAsync(includeNexusConnection: true);
        var serviceLoaded = _snapshot.CoreRunning;
        var serviceHealthy = serviceLoaded && _snapshot.Healthy;

        if (!serviceLoaded)
        {
            RuntimeDescription.Text = UiText.Get("StartAgentDockForClients");
            AgentDockState.Text = UiText.Get("Stopped");
        }
        else if (serviceHealthy)
        {
            RuntimeDescription.Text = UiText.Get("DeviceReadyForAI");
            AgentDockState.Text = UiText.Get("Running") + " ●";
        }
        else
        {
            RuntimeDescription.Text = UiText.Get("AgentDockNeedsAttentionDetail");
            AgentDockState.Text = UiText.Get("NeedsAttention");
        }

        RemoteConnectionState.Text = !string.IsNullOrWhiteSpace(_snapshot.Nexus.Error)
            ? UiText.Get("Unavailable")
            : _snapshot.NexusConnected
                ? UiText.Get("Connected") + " ●"
                : _snapshot.Nexus.Paired ? UiText.Get("NotConnected") : UiText.Get("NotConfigured");
        RemoteConnectionDetail.Text = RemoteServiceDetail(_snapshot);
        RuntimeAction.Content = serviceLoaded ? UiText.Get("Stop") : UiText.Get("Start");

        RenderCapabilities(_snapshot.Settings);
    }

    private static string RemoteServiceDetail(RuntimeSnapshot snapshot)
    {
        if (!string.IsNullOrWhiteSpace(snapshot.Nexus.Error)) return UiText.Get("Unavailable");
        if (!snapshot.Nexus.Paired) return UiText.Get("NotConfigured");

        var endpoint = snapshot.Nexus.Endpoint.Trim().TrimEnd('/');
        if (string.Equals(endpoint, "https://mcp.nexusdock.co", StringComparison.OrdinalIgnoreCase))
        {
            return $"{UiText.Get("OfficialService")} · nexusdock.co";
        }
        return $"{UiText.Get("SelfHostedService")} · {snapshot.Nexus.Endpoint}";
    }

    private void RenderCapabilities(ControlPanelSettings settings)
    {
        var browserState = settings.BrowserEnabled
            ? CapabilityState.Enabled
            : CapabilityState.Disabled;

        var enabledProfiles = settings.AcpProfiles.Where(profile => profile.Enabled).ToList();
        var codingAgentState = !settings.AcpEnabled
            ? CapabilityState.Disabled
            : enabledProfiles.Count == 0 ||
              !enabledProfiles.Any(profile => string.Equals(profile.Id, settings.AcpDefaultProfile, StringComparison.Ordinal))
                ? CapabilityState.Attention
                : CapabilityState.Enabled;

        var mode = (settings.McpAppsMode ?? "").Trim().ToLowerInvariant();
        var mcpAppsState = mode switch
        {
            "off" => CapabilityState.Disabled,
            "full" or "compact" => CapabilityState.Enabled,
            _ => CapabilityState.Attention
        };

        ApplyCapabilityState(BrowserCapabilityState, UiText.Get("Browser"), browserState);
        ApplyCapabilityState(CodingAgentCapabilityState, UiText.Get("CodingAgent"), codingAgentState);
        ApplyCapabilityState(McpAppsCapabilityState, UiText.Get("McpApps"), mcpAppsState);

        var states = new[] { browserState, codingAgentState, mcpAppsState };
        var enabledCount = states.Count(state => state == CapabilityState.Enabled);
        var attentionCount = states.Count(state => state == CapabilityState.Attention);
        CapabilitiesSummary.Text = attentionCount > 0
            ? UiText.Format("CapabilitiesNeedAttention", attentionCount)
            : enabledCount == states.Length
                ? UiText.Get("AllAvailable")
                : enabledCount == 0
                    ? UiText.Get("NotEnabledYet")
                    : UiText.Format("CapabilitiesAvailable", enabledCount);
    }

    private static void ApplyCapabilityState(TextBlock target, string title, CapabilityState state)
    {
        target.Text = state switch
        {
            CapabilityState.Enabled => $"● {title}",
            CapabilityState.Disabled => $"○ {title}",
            CapabilityState.Attention => $"! {title}",
            _ => title
        };
        target.Opacity = state == CapabilityState.Disabled ? 0.62 : 1.0;
    }

    private async void RuntimeAction_Click(object sender, RoutedEventArgs e)
    {
        if (_runtime is null || _snapshot is null) return;

        RuntimeAction.IsEnabled = false;
        try
        {
            await _runtime.RunCoreActionAsync(_snapshot.CoreRunning ? "stop" : "start");
            await RefreshAsync();
        }
        finally
        {
            RuntimeAction.IsEnabled = true;
        }
    }

    private void ConnectionsShortcut_Click(object sender, RoutedEventArgs e) =>
        ShortcutRequested?.Invoke(this, "connections");

    private void CapabilitiesShortcut_Click(object sender, RoutedEventArgs e) =>
        ShortcutRequested?.Invoke(this, "capabilities");

    private void ActivityShortcut_Click(object sender, RoutedEventArgs e) =>
        ShortcutRequested?.Invoke(this, "activity");
}

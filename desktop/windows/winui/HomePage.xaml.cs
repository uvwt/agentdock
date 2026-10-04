using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Media.Imaging;
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
        ProductLogo.Source = LoadProductLogo();
        PageTitle.Text = UiText.Get("Home");
        RemoteConnectionLabel.Text = UiText.Get("RemoteConnection");
        SkillsLabel.Text = UiText.Get("Skills");
        SkillsDetail.Text = UiText.Get("DashboardInstalled");
        McpDetail.Text = UiText.Get("DashboardConfigured");
        PluginsLabel.Text = UiText.Get("Plugins");
        PluginsDetail.Text = UiText.Get("DashboardInstalled");
        CoreCapabilitiesSection.Title = UiText.Get("CoreCapabilities");
        BrowserCapabilityTitle.Text = UiText.Get("Browser");
        CodingAgentCapabilityTitle.Text = UiText.Get("CodingAgent");
        McpAppsCapabilityTitle.Text = UiText.Get("McpApps");
        RecentActivitySection.Title = UiText.Get("RecentActivity");
    }

    private static BitmapImage? LoadProductLogo()
    {
        var path = System.IO.Path.Combine(AppContext.BaseDirectory, "Assets", "agentdock.png");
        return File.Exists(path) ? new BitmapImage(new System.Uri(path, System.UriKind.Absolute)) : null;
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

        VersionText.Text = string.IsNullOrWhiteSpace(_snapshot.Version) ? "" : $"v{_snapshot.Version.TrimStart('v')}";
        if (!serviceLoaded)
        {
            RuntimeDescription.Text = UiText.Get("StartAgentDockForClients");
            ApplyStatus(AgentDockStateDot, AgentDockState, UiText.Get("Stopped"), CapabilityState.Disabled);
        }
        else if (serviceHealthy)
        {
            RuntimeDescription.Text = UiText.Get("DeviceReadyForAI");
            ApplyStatus(AgentDockStateDot, AgentDockState, UiText.Get("Running"), CapabilityState.Enabled);
        }
        else
        {
            RuntimeDescription.Text = UiText.Get("AgentDockNeedsAttentionDetail");
            ApplyStatus(AgentDockStateDot, AgentDockState, UiText.Get("NeedsAttention"), CapabilityState.Attention);
        }

        RemoteConnectionState.Text = !string.IsNullOrWhiteSpace(_snapshot.Nexus.Error)
            ? UiText.Get("Unavailable")
            : _snapshot.NexusConnected
                ? UiText.Get("Connected")
                : _snapshot.Nexus.Paired ? UiText.Get("NotConnected") : UiText.Get("NotConfigured");
        RemoteConnectionDetail.Text = RemoteServiceDetail(_snapshot);
        RuntimeAction.Content = serviceLoaded ? UiText.Get("Stop") : UiText.Get("Start");

        RenderCapabilities(_snapshot.Settings);
        var dashboard = await _runtime.GetDashboardAsync(_snapshot);
        RenderDashboard(dashboard);
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
        var browserState = settings.BrowserEnabled ? CapabilityState.Enabled : CapabilityState.Disabled;
        BrowserCapabilityDetail.Text = BrowserCapabilityDetailText(settings);
        ApplyCapabilityState(BrowserCapabilityDot, BrowserCapabilityState, browserState);

        var enabledProfiles = settings.AcpProfiles.Where(profile => profile.Enabled).ToList();
        var codingAgentState = !settings.AcpEnabled
            ? CapabilityState.Disabled
            : enabledProfiles.Count == 0 ||
              !enabledProfiles.Any(profile => string.Equals(profile.Id, settings.AcpDefaultProfile, StringComparison.Ordinal))
                ? CapabilityState.Attention
                : CapabilityState.Enabled;
        CodingAgentCapabilityDetail.Text = CodingAgentDetailText(settings, enabledProfiles, codingAgentState);
        ApplyCapabilityState(CodingAgentCapabilityDot, CodingAgentCapabilityState, codingAgentState);

        var mode = (settings.McpAppsMode ?? "").Trim().ToLowerInvariant();
        var mcpAppsState = mode switch
        {
            "off" => CapabilityState.Disabled,
            "full" or "compact" => CapabilityState.Enabled,
            _ => CapabilityState.Attention
        };
        McpAppsCapabilityDetail.Text = mode switch
        {
            "full" => UiText.Get("Full"),
            "compact" => UiText.Get("Compact"),
            "off" => UiText.Get("Off"),
            _ => UiText.Get("NeedsAttention")
        };
        ApplyCapabilityState(McpAppsCapabilityDot, McpAppsCapabilityState, mcpAppsState);
    }

    private static string BrowserCapabilityDetailText(ControlPanelSettings settings)
    {
        if (!settings.BrowserEnabled) return UiText.Get("NotEnabledYet");
        if (!string.IsNullOrWhiteSpace(settings.BrowserCdpUrl)) return UiText.Get("SpecifiedCdp");
        return settings.BrowserReuseExistingCdp
            ? UiText.Get("ReuseLocalBrowser")
            : UiText.Get("IsolatedBrowser");
    }

    private static string CodingAgentDetailText(
        ControlPanelSettings settings,
        IReadOnlyList<AcpProfileSettings> enabledProfiles,
        CapabilityState state)
    {
        if (state == CapabilityState.Disabled) return UiText.Get("NotEnabledYet");
        if (state == CapabilityState.Attention) return UiText.Get("NeedsAttention");

        var profile = enabledProfiles.First(profile =>
            string.Equals(profile.Id, settings.AcpDefaultProfile, StringComparison.Ordinal));
        var name = string.IsNullOrWhiteSpace(profile.DisplayName) ? profile.Id : profile.DisplayName;
        return UiText.Format("DefaultProfileSummary", name);
    }

    private static void ApplyCapabilityState(
        Microsoft.UI.Xaml.Shapes.Ellipse dot,
        TextBlock target,
        CapabilityState state)
    {
        var text = state switch
        {
            CapabilityState.Enabled => UiText.Get("Enabled"),
            CapabilityState.Disabled => UiText.Get("Disabled"),
            CapabilityState.Attention => UiText.Get("NeedsAttention"),
            _ => UiText.Get("Unavailable")
        };
        ApplyStatus(dot, target, text, state);
    }

    private static void ApplyStatus(
        Microsoft.UI.Xaml.Shapes.Ellipse dot,
        TextBlock target,
        string text,
        CapabilityState state)
    {
        target.Text = text;
        target.Opacity = state == CapabilityState.Disabled ? 0.62 : 1.0;
        dot.Opacity = state == CapabilityState.Disabled ? 0.72 : 1.0;
        dot.Fill = new SolidColorBrush(state switch
        {
            CapabilityState.Enabled => Microsoft.UI.Colors.Green,
            CapabilityState.Attention => Microsoft.UI.Colors.DarkOrange,
            _ => Microsoft.UI.Colors.Gray
        });
    }

    private void RenderDashboard(RuntimeDashboardSnapshot dashboard)
    {
        SkillCount.Text = dashboard.CountsAvailable ? dashboard.SkillCount.ToString() : "—";
        McpCount.Text = dashboard.CountsAvailable ? dashboard.McpCount.ToString() : "—";
        PluginCount.Text = dashboard.CountsAvailable ? dashboard.PluginCount.ToString() : "—";

        RecentActivityPanel.Children.Clear();
        if (!dashboard.DiagnosticsAvailable)
        {
            RecentActivityPanel.Children.Add(CreateEmptyActivityText(UiText.Get("RecentActivityUnavailable")));
            return;
        }

        var recentCalls = dashboard.RecentCalls.Take(3).ToList();
        if (recentCalls.Count == 0)
        {
            RecentActivityPanel.Children.Add(CreateEmptyActivityText(UiText.Get("NoRecentActivity")));
            return;
        }

        for (var index = 0; index < recentCalls.Count; index++)
        {
            if (index > 0)
            {
                RecentActivityPanel.Children.Add(new Border
                {
                    Height = 1,
                    Margin = new Thickness(13, 0, 0, 0),
                    Background = (Brush)Application.Current.Resources["DividerStrokeColorDefaultBrush"]
                });
            }
            RecentActivityPanel.Children.Add(CreateActivityRow(recentCalls[index]));
        }
    }

    private static TextBlock CreateEmptyActivityText(string text) =>
        new()
        {
            Text = text,
            FontSize = 12.5,
            Foreground = (Brush)Application.Current.Resources["TextFillColorSecondaryBrush"],
            Margin = new Thickness(13, 14, 13, 14)
        };

    private static Grid CreateActivityRow(RuntimeRecentCall call)
    {
        var row = new Grid
        {
            MinHeight = 54,
            Padding = new Thickness(13, 7, 13, 7),
            ColumnSpacing = 16
        };
        row.ColumnDefinitions.Add(new ColumnDefinition());
        row.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });

        var details = new StackPanel { Spacing = 2 };
        details.Children.Add(new TextBlock
        {
            Text = call.Tool,
            FontSize = 13,
            FontWeight = Microsoft.UI.Text.FontWeights.Medium
        });
        details.Children.Add(new TextBlock
        {
            Text = $"{ActivitySource(call.Source)} · {RelativeTime(call.StartedAt)}",
            FontSize = 11.5,
            Foreground = (Brush)Application.Current.Resources["TextFillColorSecondaryBrush"]
        });
        row.Children.Add(details);

        var state = new StackPanel
        {
            Orientation = Orientation.Horizontal,
            Spacing = 6,
            VerticalAlignment = VerticalAlignment.Center
        };
        state.Children.Add(new Microsoft.UI.Xaml.Shapes.Ellipse
        {
            Width = 7,
            Height = 7,
            VerticalAlignment = VerticalAlignment.Center,
            Fill = new SolidColorBrush(call.Success ? Microsoft.UI.Colors.Green : Microsoft.UI.Colors.DarkOrange)
        });
        state.Children.Add(new TextBlock
        {
            Text = call.Success ? UiText.Get("Succeeded") : UiText.Get("Failed"),
            FontSize = 12,
            VerticalAlignment = VerticalAlignment.Center
        });
        Grid.SetColumn(state, 1);
        row.Children.Add(state);
        return row;
    }

    private static string ActivitySource(string source) =>
        source.Trim().ToLowerInvariant() switch
        {
            "nexus" => UiText.Get("ActivitySourceRemote"),
            "internal" => UiText.Get("ActivitySourceLocal"),
            "mcp" => "MCP",
            _ => source
        };

    private static string RelativeTime(DateTimeOffset startedAt)
    {
        var elapsed = DateTimeOffset.Now - startedAt.ToLocalTime();
        if (elapsed < TimeSpan.Zero || elapsed < TimeSpan.FromMinutes(1))
        {
            return UiText.Get("JustNow");
        }
        if (elapsed < TimeSpan.FromHours(1))
        {
            return UiText.Format("MinutesAgo", Math.Max(1, (int)elapsed.TotalMinutes));
        }
        if (elapsed < TimeSpan.FromDays(1))
        {
            return UiText.Format("HoursAgo", Math.Max(1, (int)elapsed.TotalHours));
        }
        return startedAt.ToLocalTime().ToString("g");
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

using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Navigation;

namespace AgentDock.ControlPanel;

public sealed partial class HomePage : Page
{
    private RuntimeService? _runtime;
    private RuntimeSnapshot? _snapshot;

    internal event EventHandler<string>? ShortcutRequested;

    public HomePage()
    {
        InitializeComponent();
        PageTitle.Text = UiText.Get("Home");
        AgentDockSection.Title = "AgentDock";
        QuickAccessSection.Title = UiText.Get("QuickAccess");
        ConnectionsShortcutLabel.Text = UiText.Get("ConnectAIClients");
        CapabilitiesShortcutLabel.Text = UiText.Get("ManageCapabilities");
        ActivityShortcutLabel.Text = UiText.Get("ViewActivity");
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
            RuntimeHeadline.Text = UiText.Get("AgentDockStopped");
            RuntimeDescription.Text = UiText.Get("StartAgentDockForClients");
            AgentDockState.Text = UiText.Get("Stopped");
        }
        else if (serviceHealthy)
        {
            RuntimeHeadline.Text = UiText.Get("AgentDockRunning");
            RuntimeDescription.Text = UiText.Get("LocalServiceReady");
            AgentDockState.Text = UiText.Get("Running") + " ●";
        }
        else
        {
            RuntimeHeadline.Text = UiText.Get("AgentDockNeedsAttention");
            RuntimeDescription.Text = UiText.Get("AgentDockNeedsAttentionDetail");
            AgentDockState.Text = UiText.Get("NeedsAttention");
        }

        NexusState.Text = _snapshot.NexusConnected ? UiText.Get("Connected") + " ●" : UiText.Get("NotConnected");
        RuntimeAction.Content = serviceLoaded ? UiText.Get("Stop") : UiText.Get("Start");
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

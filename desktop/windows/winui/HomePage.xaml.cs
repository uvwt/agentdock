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
        RuntimeSection.Title = UiText.Get("RuntimeStatus");
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
        var running = _snapshot.CoreRunning;
        RuntimeHeadline.Text = running ? UiText.Get("AgentDockRunning") : UiText.Get("AgentDockStopped");
        RuntimeDescription.Text = running ? UiText.Get("LocalServiceReady") : UiText.Get("StartAgentDockForClients");
        RuntimeState.Text = running ? UiText.Get("Running") + " ●" : UiText.Get("Stopped");
        McpState.Text = _snapshot.Healthy ? UiText.Get("Ready") + " ●" : UiText.Get("Unavailable");
        NexusState.Text = _snapshot.NexusConnected ? UiText.Get("Connected") + " ●" : UiText.Get("NotConnected");
        RuntimeAction.Content = running ? UiText.Get("Stop") : UiText.Get("Start");
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
        finally { RuntimeAction.IsEnabled = true; }
    }

    private void ConnectionsShortcut_Click(object sender, RoutedEventArgs e) =>
        ShortcutRequested?.Invoke(this, "connections");

    private void CapabilitiesShortcut_Click(object sender, RoutedEventArgs e) =>
        ShortcutRequested?.Invoke(this, "capabilities");

    private void ActivityShortcut_Click(object sender, RoutedEventArgs e) =>
        ShortcutRequested?.Invoke(this, "activity");
}

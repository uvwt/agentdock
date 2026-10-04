using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Navigation;

namespace AgentDock.ControlPanel;

public sealed partial class ActivityPage : Page
{
    private RuntimeService? _runtime;

    public ActivityPage()
    {
        InitializeComponent();
        PageTitle.Text = UiText.Get("Activity");
        PageDetail.Text = UiText.Get("ActivityDetail");
        RuntimeLabel.Text = "Runtime";
        VersionLabel.Text = UiText.Get("Version");
        RefreshLabel.Text = UiText.Get("LastStatusRefresh");
        DiagnosticsSection.Title = UiText.Get("Diagnostics");
        LogsLabel.Text = UiText.Get("LogsDirectory");
        ConfigLabel.Text = UiText.Get("ConfigurationDirectory");
        OpenLogsButton.Content = UiText.Get("Open");
        OpenConfigButton.Content = UiText.Get("Open");
    }

    protected override async void OnNavigatedTo(NavigationEventArgs e)
    {
        _runtime = e.Parameter as RuntimeService;
        if (_runtime is null) return;

        var snapshot = await _runtime.GetSnapshotAsync(includeNexusConnection: false);
        RuntimeState.Text = snapshot.CoreRunning
            ? UiText.Get("Running") + " ●"
            : UiText.Get("Stopped");
        VersionValue.Text = string.IsNullOrWhiteSpace(snapshot.Version) ? "—" : snapshot.Version;
        RefreshValue.Text = snapshot.CheckedAt.ToLocalTime().ToString("HH:mm:ss");
    }

    private void OpenLogsButton_Click(object sender, RoutedEventArgs e)
    {
        _runtime?.OpenLogsDirectory();
    }

    private void OpenConfigButton_Click(object sender, RoutedEventArgs e)
    {
        _runtime?.OpenConfigDirectory();
    }
}

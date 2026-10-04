using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Navigation;
using Windows.ApplicationModel.DataTransfer;

namespace AgentDock.ControlPanel;

public sealed partial class ConnectionsPage : Page
{
    private RuntimeSnapshot? _snapshot;

    public ConnectionsPage()
    {
        InitializeComponent();
        PageTitle.Text = UiText.Get("Connections");
        PageDetail.Text = UiText.Get("ConnectionsDetail");
        LocalSection.Title = UiText.Get("LocalConnection");
        RemoteSection.Title = UiText.Get("RemoteConnection");
        CopyLocalButton.Content = UiText.Get("Copy");
        CopyPublicButton.Content = UiText.Get("Copy");
    }

    protected override async void OnNavigatedTo(NavigationEventArgs e)
    {
        if (e.Parameter is not RuntimeService runtime) return;

        _snapshot = await runtime.GetSnapshotAsync(includeNexusConnection: true);
        LocalAddress.Text = _snapshot.LocalMcpUrl;

        var hasPublicAddress = !string.IsNullOrWhiteSpace(_snapshot.PublicMcpUrl);
        PublicAddress.Text = hasPublicAddress ? _snapshot.PublicMcpUrl : UiText.Get("Disabled");
        CopyPublicButton.IsEnabled = hasPublicAddress;

        NexusEndpoint.Text = _snapshot.Nexus.Paired
            ? _snapshot.Nexus.Endpoint
            : UiText.Get("NotConfigured");
        NexusState.Text = _snapshot.NexusConnected
            ? UiText.Get("Connected") + " ●"
            : _snapshot.Nexus.Paired
                ? UiText.Get("NotConnected")
                : UiText.Get("NotConfigured");
    }

    private void CopyLocalButton_Click(object sender, RoutedEventArgs e)
    {
        if (_snapshot is not null) CopyText(_snapshot.LocalMcpUrl);
    }

    private void CopyPublicButton_Click(object sender, RoutedEventArgs e)
    {
        if (_snapshot is not null && !string.IsNullOrWhiteSpace(_snapshot.PublicMcpUrl))
        {
            CopyText(_snapshot.PublicMcpUrl);
        }
    }

    private static void CopyText(string text)
    {
        var package = new DataPackage();
        package.SetText(text);
        Clipboard.SetContent(package);
    }
}

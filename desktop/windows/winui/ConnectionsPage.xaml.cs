using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Navigation;
using Windows.ApplicationModel.DataTransfer;

namespace AgentDock.ControlPanel;

public sealed partial class ConnectionsPage : Page
{
    private RuntimeService? _runtime;
    private RuntimeSnapshot? _snapshot;
    private bool _updatingUi;

    public ConnectionsPage()
    {
        InitializeComponent();
        PageTitle.Text = UiText.Get("Connections");
        PageDetail.Text = UiText.Get("ConnectionsDetail");
        LocalSection.Title = UiText.Get("LocalConnection");
        RemoteSection.Title = UiText.Get("RemoteConnection");
        CopyLocalButton.Content = UiText.Get("Copy");
        CopyPublicButton.Content = UiText.Get("Copy");
        PublicModeLabel.Text = UiText.Get("PublicMcpMode");
        LocalOnlyModeItem.Content = UiText.Get("LocalOnlyNative");
        QuickModeItem.Content = UiText.Get("TemporaryPublicAddress");
        NamedModeItem.Content = UiText.Get("CustomDomain");
        NamedServerUrlTextBox.Header = UiText.Get("HttpsAddress");
        TunnelTokenPasswordBox.Header = "Cloudflare Tunnel Token";
        ApplyTunnelButton.Content = UiText.Get("Apply");
        RegenerateQuickButton.Content = UiText.Get("RegenerateTemporaryAddress");
        NexusEndpointTextBox.Header = UiText.Get("NexusAddress");
        NexusPairingCodeBox.Header = UiText.Get("OneTimePairingCode");
        PairNexusButton.Content = UiText.Get("Pair");
    }

    protected override async void OnNavigatedTo(NavigationEventArgs e)
    {
        _runtime = e.Parameter as RuntimeService;
        if (_runtime is not null) await RefreshAsync();
    }

    private async Task RefreshAsync()
    {
        if (_runtime is null) return;
        _snapshot = await _runtime.GetSnapshotAsync(includeNexusConnection: true);
        _updatingUi = true;
        LocalAddress.Text = _snapshot.LocalMcpUrl;
        var hasPublicAddress = !string.IsNullOrWhiteSpace(_snapshot.PublicMcpUrl);
        PublicAddress.Text = hasPublicAddress ? _snapshot.PublicMcpUrl : UiText.Get("Disabled");
        CopyPublicButton.IsEnabled = hasPublicAddress;
        NexusEndpoint.Text = _snapshot.Nexus.Paired ? _snapshot.Nexus.Endpoint : UiText.Get("NotConfigured");
        NexusState.Text = _snapshot.NexusConnected
            ? UiText.Get("Connected") + " ●"
            : _snapshot.Nexus.Paired ? UiText.Get("NotConnected") : UiText.Get("NotConfigured");
        NexusEndpointTextBox.Text = _snapshot.Nexus.Paired ? _snapshot.Nexus.Endpoint : "https://mcp.nexusdock.co";
        SelectTunnelMode(_snapshot.TunnelMode);
        NamedServerUrlTextBox.Text = _snapshot.SavedNamedOrigin;
        TunnelTokenPasswordBox.PlaceholderText = _snapshot.TunnelTokenStored ? UiText.Get("TunnelTokenSavedPlaceholder") : "Cloudflare Tunnel Token";
        _updatingUi = false;
        UpdateTunnelControls();
    }

    private void CopyLocalButton_Click(object sender, RoutedEventArgs e)
    {
        if (_snapshot is not null) CopyText(_snapshot.LocalMcpUrl);
    }

    private void CopyPublicButton_Click(object sender, RoutedEventArgs e)
    {
        if (_snapshot is not null && !string.IsNullOrWhiteSpace(_snapshot.PublicMcpUrl)) CopyText(_snapshot.PublicMcpUrl);
    }

    private static void CopyText(string text)
    {
        var package = new DataPackage();
        package.SetText(text);
        Clipboard.SetContent(package);
    }

    private void SelectTunnelMode(string mode)
    {
        foreach (var item in TunnelModeComboBox.Items.OfType<ComboBoxItem>())
        {
            if (string.Equals(item.Tag?.ToString(), mode, StringComparison.OrdinalIgnoreCase))
            {
                TunnelModeComboBox.SelectedItem = item;
                return;
            }
        }
        TunnelModeComboBox.SelectedIndex = 0;
    }

    private string SelectedTunnelMode() =>
        (TunnelModeComboBox.SelectedItem as ComboBoxItem)?.Tag?.ToString() ?? "none";

    private void TunnelModeComboBox_SelectionChanged(object sender, SelectionChangedEventArgs e)
    {
        if (!_updatingUi) UpdateTunnelControls();
    }

    private void UpdateTunnelControls()
    {
        var mode = SelectedTunnelMode();
        NamedServerUrlTextBox.Visibility = mode == "named" ? Visibility.Visible : Visibility.Collapsed;
        TunnelTokenPasswordBox.Visibility = mode == "named" ? Visibility.Visible : Visibility.Collapsed;
        RegenerateQuickButton.Visibility = mode == "quick" ? Visibility.Visible : Visibility.Collapsed;
    }

    private async void ApplyTunnelButton_Click(object sender, RoutedEventArgs e)
    {
        if (_runtime is null) return;
        TunnelStatus.Text = UiText.Get("Applying");
        try
        {
            await _runtime.SetTunnelModeAsync(SelectedTunnelMode(), NamedServerUrlTextBox.Text.Trim(), TunnelTokenPasswordBox.Password);
            TunnelTokenPasswordBox.Password = "";
            await RefreshAsync();
            TunnelStatus.Text = UiText.Get("Applied");
        }
        catch (Exception ex) { TunnelStatus.Text = ex.Message; }
    }

    private async void RegenerateQuickButton_Click(object sender, RoutedEventArgs e)
    {
        if (_runtime is null) return;
        TunnelStatus.Text = UiText.Get("Generating");
        try
        {
            await _runtime.RegenerateQuickTunnelAsync();
            await RefreshAsync();
            TunnelStatus.Text = UiText.Get("Generated");
        }
        catch (Exception ex) { TunnelStatus.Text = ex.Message; }
    }

    private async void PairNexusButton_Click(object sender, RoutedEventArgs e)
    {
        if (_runtime is null) return;
        NexusActionStatus.Text = UiText.Get("Pairing");
        try
        {
            await _runtime.PairNexusAsync(NexusEndpointTextBox.Text, NexusPairingCodeBox.Password);
            NexusPairingCodeBox.Password = "";
            await RefreshAsync();
            NexusActionStatus.Text = UiText.Get("PairingCompleted");
        }
        catch (Exception ex) { NexusActionStatus.Text = ex.Message; }
    }
}

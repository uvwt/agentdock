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
        TunnelTokenPasswordBox.PlaceholderText = _snapshot.TunnelTokenStored ? "已保存；留空保持不变" : "Cloudflare Tunnel Token";
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
        TunnelStatus.Text = "正在应用…";
        try
        {
            await _runtime.SetTunnelModeAsync(SelectedTunnelMode(), NamedServerUrlTextBox.Text.Trim(), TunnelTokenPasswordBox.Password);
            TunnelTokenPasswordBox.Password = "";
            await RefreshAsync();
            TunnelStatus.Text = "已应用";
        }
        catch (Exception ex) { TunnelStatus.Text = ex.Message; }
    }

    private async void RegenerateQuickButton_Click(object sender, RoutedEventArgs e)
    {
        if (_runtime is null) return;
        TunnelStatus.Text = "正在生成…";
        try
        {
            await _runtime.RegenerateQuickTunnelAsync();
            await RefreshAsync();
            TunnelStatus.Text = "已生成";
        }
        catch (Exception ex) { TunnelStatus.Text = ex.Message; }
    }

    private async void PairNexusButton_Click(object sender, RoutedEventArgs e)
    {
        if (_runtime is null) return;
        NexusActionStatus.Text = "正在配对…";
        try
        {
            await _runtime.PairNexusAsync(NexusEndpointTextBox.Text, NexusPairingCodeBox.Password);
            NexusPairingCodeBox.Password = "";
            await RefreshAsync();
            NexusActionStatus.Text = "配对完成";
        }
        catch (Exception ex) { NexusActionStatus.Text = ex.Message; }
    }
}

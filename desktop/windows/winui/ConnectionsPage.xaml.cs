using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Navigation;
using Windows.ApplicationModel.DataTransfer;

namespace AgentDock.ControlPanel;

public sealed partial class ConnectionsPage : Page
{
    private RuntimeService? _runtime;
    private RuntimeSnapshot? _snapshot;

    public ConnectionsPage()
    {
        InitializeComponent();

        PageTitle.Text = UiText.Get("Connections");
        PageDetail.Text = UiText.Get("ConnectionsRecommendedDetail");
        NexusSection.Title = "NexusDock";
        RecommendedConnectionLabel.Text = UiText.Get("RecommendedConnection");
        NexusIntro.Text = UiText.Get("NexusRecommendedIntro");
        NexusEndpointTextBox.Header = UiText.Get("NexusAddress");
        NexusPairingCodeBox.Header = UiText.Get("OneTimePairingCode");
        PairNexusButton.Content = UiText.Get("Pair");

        AdvancedConnectionTitle.Text = UiText.Get("AdvancedConnectionSettings");
        AdvancedConnectionDetail.Text = UiText.Get("AdvancedConnectionDetail");
        LocalMcpSection.Title = "Local MCP";
        AuthTokenLabel.Text = UiText.Get("AuthenticationToken");
        OAuthPasswordLabel.Text = UiText.Get("OAuthPassword");
        CopyLocalButton.Content = UiText.Get("Copy");
        CopyAuthTokenButton.Content = UiText.Get("Copy");
        CopyOAuthPasswordButton.Content = UiText.Get("Copy");

        PublicAccessSection.Title = UiText.Get("PublicAccess");
        PublicAddressLabel.Text = UiText.Get("PublicAddress");
        CopyPublicButton.Content = UiText.Get("Copy");
        TemporaryTunnelDetail.Text = UiText.Get("TemporaryTunnelDetail");
        FixedDomainTitle.Text = UiText.Get("FixedDomain");
        FixedDomainDetail.Text = UiText.Get("FixedDomainDetail");
        NamedServerUrlTextBox.Header = UiText.Get("HttpsAddress");
        TunnelTokenPasswordBox.Header = "Cloudflare Tunnel Token";
        ApplyFixedDomainButton.Content = UiText.Get("ApplyFixedDomain");
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
        LocalAddress.Text = _snapshot.LocalMcpUrl;

        var hasPublicAddress = !string.IsNullOrWhiteSpace(_snapshot.PublicMcpUrl);
        PublicAddress.Text = hasPublicAddress ? _snapshot.PublicMcpUrl : UiText.Get("Disabled");
        CopyPublicButton.IsEnabled = hasPublicAddress;
        TemporaryTunnelButton.Content = string.Equals(_snapshot.TunnelMode, "quick", StringComparison.OrdinalIgnoreCase)
            ? UiText.Get("RegenerateTemporaryAddress")
            : UiText.Get("GenerateTemporaryAddress");

        NexusEndpoint.Text = _snapshot.Nexus.Paired
            ? _snapshot.Nexus.Endpoint
            : UiText.Get("ConnectThisDeviceToNexus");
        NexusState.Text = _snapshot.NexusConnected
            ? UiText.Get("Connected") + " ●"
            : _snapshot.Nexus.Paired ? UiText.Get("NotConnected") : UiText.Get("NotConfigured");
        NexusEndpointTextBox.Text = _snapshot.Nexus.Paired ? _snapshot.Nexus.Endpoint : "https://mcp.nexusdock.co";

        NamedServerUrlTextBox.Text = string.Equals(_snapshot.TunnelMode, "named", StringComparison.OrdinalIgnoreCase)
            ? _snapshot.SavedNamedOrigin
            : "";
        TunnelTokenPasswordBox.PlaceholderText = _snapshot.TunnelTokenStored
            ? UiText.Get("TunnelTokenSavedPlaceholder")
            : "Cloudflare Tunnel Token";

        AuthTokenPasswordBox.Password = ReadCredential("bearer");
        OAuthPasswordBox.Password = ReadCredential("oauth");
    }

    private string ReadCredential(string kind)
    {
        if (_runtime is null) return "";
        try
        {
            return kind == "bearer" ? _runtime.ReadBearerToken() : _runtime.ReadOAuthPassword();
        }
        catch
        {
            return "";
        }
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

    private void CopyAuthTokenButton_Click(object sender, RoutedEventArgs e) =>
        CopyText(AuthTokenPasswordBox.Password);

    private void CopyOAuthPasswordButton_Click(object sender, RoutedEventArgs e) =>
        CopyText(OAuthPasswordBox.Password);

    private static void CopyText(string text)
    {
        if (string.IsNullOrWhiteSpace(text)) return;
        var package = new DataPackage();
        package.SetText(text);
        Clipboard.SetContent(package);
    }

    private async void TemporaryTunnelButton_Click(object sender, RoutedEventArgs e)
    {
        if (_runtime is null || sender is not Button button) return;

        button.IsEnabled = false;
        TunnelStatus.Text = UiText.Get("Generating");
        try
        {
            if (string.Equals(_snapshot?.TunnelMode, "quick", StringComparison.OrdinalIgnoreCase))
            {
                await _runtime.RegenerateQuickTunnelAsync();
            }
            else
            {
                await _runtime.SetTunnelModeAsync("quick", "", "");
            }

            await RefreshAsync();
            TunnelStatus.Text = UiText.Get("Generated");
        }
        catch (Exception ex)
        {
            TunnelStatus.Text = ex.Message;
        }
        finally
        {
            button.IsEnabled = true;
        }
    }

    private async void ApplyFixedDomainButton_Click(object sender, RoutedEventArgs e)
    {
        if (_runtime is null || sender is not Button button) return;

        var serverUrl = NamedServerUrlTextBox.Text.Trim();
        if (string.IsNullOrWhiteSpace(serverUrl)) return;

        button.IsEnabled = false;
        TunnelStatus.Text = UiText.Get("Applying");
        try
        {
            await _runtime.SetTunnelModeAsync("named", serverUrl, TunnelTokenPasswordBox.Password);
            TunnelTokenPasswordBox.Password = "";
            await RefreshAsync();
            TunnelStatus.Text = UiText.Get("Applied");
        }
        catch (Exception ex)
        {
            TunnelStatus.Text = ex.Message;
        }
        finally
        {
            button.IsEnabled = true;
        }
    }

    private async void PairNexusButton_Click(object sender, RoutedEventArgs e)
    {
        if (_runtime is null || sender is not Button button) return;

        button.IsEnabled = false;
        NexusActionStatus.Text = UiText.Get("Pairing");
        try
        {
            await _runtime.PairNexusAsync(NexusEndpointTextBox.Text, NexusPairingCodeBox.Password);
            NexusPairingCodeBox.Password = "";
            await RefreshAsync();
            NexusActionStatus.Text = UiText.Get("PairingCompleted");
        }
        catch (Exception ex)
        {
            NexusActionStatus.Text = ex.Message;
        }
        finally
        {
            button.IsEnabled = true;
        }
    }
}

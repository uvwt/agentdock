using System.Diagnostics;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Navigation;

namespace AgentDock.ControlPanel;

public sealed partial class ConnectionsPage : Page
{
    private const string OfficialEndpoint = "https://mcp.nexusdock.co";
    private const string OfficialDevicesUrl = "https://mcp.nexusdock.co/workspace/devices";

    private RuntimeService? _runtime;
    private NexusConnectionSnapshot? _connection;
    private bool _updatingRemoteService;
    private bool _rePairing;

    internal event EventHandler? AdvancedSettingsRequested;

    public ConnectionsPage()
    {
        InitializeComponent();

        PageTitle.Text = UiText.Get("Connections");
        PageDetail.Text = UiText.Get("RemoteConnectionPageDetail");
        RemoteSection.Title = UiText.Get("RemoteConnection");
        StatusLabel.Text = UiText.Get("Status");
        RemoteServiceLabel.Text = UiText.Get("RemoteService");
        ChangeRemoteServiceButton.Content = UiText.Get("Change");
        OfficialServiceItem.Content = UiText.Get("OfficialService");
        SelfHostedServiceItem.Content = UiText.Get("SelfHostedService");
        OfficialServiceHint.Text = UiText.Get("OfficialServiceHint");
        SelfHostedEndpointTextBox.Header = UiText.Get("ServiceAddress");
        PairingIntro.Text = UiText.Get("RemotePairingIntro");
        PairingCodeBox.Header = UiText.Get("OneTimePairingCode");
        NexusDevicesLink.Content = UiText.Get("GetPairingCodeFromNexusDock");
        ConnectButton.Content = UiText.Get("Connect");

        AdvancedEntrySection.Title = UiText.Get("AdvancedConnectionSettings");
        AdvancedEntryTitle.Text = UiText.Get("AdvancedConnectionSettings");
        AdvancedEntryDetail.Text = UiText.Get("AdvancedConnectionDetail");
    }

    protected override async void OnNavigatedTo(NavigationEventArgs e)
    {
        if (e.Parameter is ConnectionsNavigationRequest request)
        {
            _runtime = request.Runtime;
            ApplyConnectionState(request.State);
            return;
        }

        _runtime = e.Parameter as RuntimeService;
        if (_runtime is not null) await RefreshAsync();
    }

    private async Task RefreshAsync()
    {
        if (_runtime is null) return;
        ApplyConnectionState(await _runtime.GetNexusConnectionSnapshotAsync());
    }

    private void ApplyConnectionState(NexusConnectionSnapshot state)
    {
        _connection = state;
        RemoteState.Text = state.NexusConnected
            ? UiText.Get("Connected")
            : state.Nexus.Paired ? UiText.Get("NotConnected") : UiText.Get("NotConfigured");
        RemoteStateDot.Fill = new Microsoft.UI.Xaml.Media.SolidColorBrush(
            state.NexusConnected
                ? Microsoft.UI.Colors.Green
                : state.Nexus.Paired ? Microsoft.UI.Colors.DarkOrange : Microsoft.UI.Colors.Gray);

        var endpoint = state.Nexus.Paired ? state.Nexus.Endpoint : OfficialEndpoint;
        var official = IsOfficialEndpoint(endpoint);
        RemoteServiceValue.Text = official
            ? UiText.Get("OfficialService") + " · nexusdock.co"
            : UiText.Get("SelfHostedService") + " · " + endpoint;

        _updatingRemoteService = true;
        SelectRemoteService(official ? "official" : "self-hosted");
        SelfHostedEndpointTextBox.Text = official ? "" : endpoint;
        _updatingRemoteService = false;
        UpdateRemoteServiceEditor();
    }

    private void ChangeRemoteServiceButton_Click(object sender, RoutedEventArgs e)
    {
        var editing = RemoteServiceEditor.Visibility != Visibility.Visible;
        RemoteServiceEditor.Visibility = editing ? Visibility.Visible : Visibility.Collapsed;
        ChangeRemoteServiceButton.Content = UiText.Get(editing ? "Done" : "Change");
    }

    private void RemoteServiceComboBox_SelectionChanged(object sender, SelectionChangedEventArgs e)
    {
        if (_updatingRemoteService) return;
        UpdateRemoteServiceEditor();
    }

    private void SelfHostedEndpointTextBox_TextChanged(object sender, TextChangedEventArgs e)
    {
        if (_updatingRemoteService) return;
        UpdateRemoteServiceValue();
    }

    private void UpdateRemoteServiceEditor()
    {
        var selfHosted = SelectedRemoteService() == "self-hosted";
        SelfHostedEndpointTextBox.Visibility = selfHosted ? Visibility.Visible : Visibility.Collapsed;
        OfficialServiceHint.Visibility = selfHosted ? Visibility.Collapsed : Visibility.Visible;
        UpdatePairingControls();
        UpdateRemoteServiceValue();
    }

    private void UpdatePairingControls()
    {
        var paired = _connection?.Nexus.Paired == true;
        var showPairingInput = !paired || _rePairing;

        PairingIntro.Visibility = showPairingInput ? Visibility.Visible : Visibility.Collapsed;
        PairingCodeBox.Visibility = showPairingInput ? Visibility.Visible : Visibility.Collapsed;
        ConnectButton.Content = paired && !_rePairing ? UiText.Get("RePair") : UiText.Get("Connect");

        NexusDevicesLink.Visibility = SelectedRemoteService() == "official"
            ? Visibility.Visible
            : Visibility.Collapsed;
        NexusDevicesLink.Content = paired && !_rePairing && IsOfficialEndpoint(_connection!.Nexus.Endpoint)
            ? UiText.Get("ManageConnectedDevices")
            : UiText.Get("GetPairingCodeFromNexusDock");
    }

    private void UpdateRemoteServiceValue()
    {
        if (SelectedRemoteService() != "self-hosted")
        {
            RemoteServiceValue.Text = UiText.Get("OfficialService") + " · nexusdock.co";
            return;
        }
        var endpoint = SelfHostedEndpointTextBox.Text.Trim();
        RemoteServiceValue.Text = string.IsNullOrWhiteSpace(endpoint)
            ? UiText.Get("SelfHostedService")
            : UiText.Get("SelfHostedService") + " · " + endpoint;
    }

    private void SelectRemoteService(string service)
    {
        foreach (var item in RemoteServiceComboBox.Items.OfType<ComboBoxItem>())
        {
            if (!string.Equals(item.Tag?.ToString(), service, StringComparison.OrdinalIgnoreCase)) continue;
            RemoteServiceComboBox.SelectedItem = item;
            return;
        }
        RemoteServiceComboBox.SelectedItem = OfficialServiceItem;
    }

    private string SelectedRemoteService() =>
        (RemoteServiceComboBox.SelectedItem as ComboBoxItem)?.Tag?.ToString() ?? "official";

    private string SelectedEndpoint() =>
        SelectedRemoteService() == "self-hosted"
            ? SelfHostedEndpointTextBox.Text.Trim()
            : OfficialEndpoint;

    private async void ConnectButton_Click(object sender, RoutedEventArgs e)
    {
        if (_runtime is null || sender is not Button button) return;

        if (_connection?.Nexus.Paired == true && !_rePairing)
        {
            _rePairing = true;
            PairingCodeBox.Password = "";
            UpdatePairingControls();
            PairingCodeBox.Focus(FocusState.Programmatic);
            return;
        }

        var endpoint = SelectedEndpoint();
        if (string.IsNullOrWhiteSpace(endpoint))
        {
            ConnectionActionStatus.Text = UiText.Get("ServiceAddressRequired");
            return;
        }

        if (string.IsNullOrWhiteSpace(PairingCodeBox.Password)) return;

        button.IsEnabled = false;
        ConnectionActionStatus.Text = UiText.Get("Pairing");
        try
        {
            await _runtime.PairNexusAsync(endpoint, PairingCodeBox.Password);
            PairingCodeBox.Password = "";
            _rePairing = false;
            RemoteServiceEditor.Visibility = Visibility.Collapsed;
            ChangeRemoteServiceButton.Content = UiText.Get("Change");
            await RefreshAsync();
            ConnectionActionStatus.Text = UiText.Get("PairingCompleted");
        }
        catch (Exception ex)
        {
            ConnectionActionStatus.Text = ex.Message;
        }
        finally
        {
            button.IsEnabled = true;
        }
    }

    private void AdvancedSettingsButton_Click(object sender, RoutedEventArgs e) =>
        AdvancedSettingsRequested?.Invoke(this, EventArgs.Empty);

    private void NexusDevicesLink_Click(object sender, RoutedEventArgs e) =>
        Process.Start(new ProcessStartInfo(OfficialDevicesUrl) { UseShellExecute = true });

    private static bool IsOfficialEndpoint(string endpoint) =>
        string.Equals(endpoint.Trim().TrimEnd('/'), OfficialEndpoint, StringComparison.OrdinalIgnoreCase);
}

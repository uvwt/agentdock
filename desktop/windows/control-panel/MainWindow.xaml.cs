using System.ComponentModel;
using System.IO;
using System.Text.Json;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Media;
using Application = System.Windows.Application;
using Clipboard = System.Windows.Clipboard;
using Color = System.Windows.Media.Color;
using MessageBox = System.Windows.MessageBox;
using Forms = System.Windows.Forms;

namespace AgentDock.ControlPanel;

public partial class MainWindow : Window
{
    private const string BrowserConnectionManaged = "managed";
    private const string BrowserConnectionReuse = "reuse";
    private const string BrowserConnectionSpecified = "specified";

    private readonly RuntimeService _runtime;
    private readonly SemaphoreSlim _refreshGate = new(1, 1);
    private readonly Dictionary<PasswordBox, Stack<string>> _passwordHistory = new();
    private RuntimeSnapshot? _snapshot;
    private string _bearerToken = "";
    private string _oauthPassword = "";
    private bool _showBearer;
    private bool _showOAuth;
    private bool _updatingUi;
    private bool _settingsLoaded;
    private string _lastAutoTestOrigin = "";
    private DateTimeOffset _lastAutoTestAt = DateTimeOffset.MinValue;

    public MainWindow(RuntimeService runtime)
    {
        _runtime = runtime;
        InitializeComponent();
        _updatingUi = true;
        SelectUiLanguage(UiText.ReadPreference());
        _updatingUi = false;
        Closing += MainWindow_Closing;
    }

    internal void CloseForReplacement()
    {
        Closing -= MainWindow_Closing;
        Close();
    }

    public async Task RefreshAsync()
    {
        if (!await _refreshGate.WaitAsync(0))
        {
            return;
        }

        try
        {
            FooterStatusText.Text = UiText.Get("Refreshing");
            var snapshot = await _runtime.GetSnapshotAsync(includeNexusConnection: true);
            _snapshot = snapshot;
            _bearerToken = _runtime.ReadBearerToken();
            _oauthPassword = _runtime.ReadOAuthPassword();
            ApplySnapshot(snapshot);
            FooterStatusText.Text = UiText.Format("LastRefresh", snapshot.CheckedAt);
            await AutoTestPublicAsync(snapshot);
        }
        catch (Exception ex)
        {
            FooterStatusText.Text = ex.Message;
            HeaderStatusText.Text = UiText.Get("StatusReadFailed");
            StatusDot.Fill = new SolidColorBrush(Color.FromRgb(217, 45, 32));
            NexusStatusText.Text = UiText.Get("StatusReadFailed");
        }
        finally
        {
            _refreshGate.Release();
        }
    }

    private void ApplySnapshot(RuntimeSnapshot snapshot)
    {
        _updatingUi = true;
        try
        {
            HeaderStatusText.Text = snapshot.Healthy ? UiText.Get("RunningNormally") : snapshot.CoreRunning ? UiText.Get("RunningHealthFailed") : UiText.Get("Stopped");
            StatusDot.Fill = new SolidColorBrush(snapshot.Healthy
                ? Color.FromRgb(18, 183, 106)
                : snapshot.CoreRunning ? Color.FromRgb(247, 144, 9) : Color.FromRgb(152, 162, 179));

            NexusStatusText.Text = !string.IsNullOrWhiteSpace(snapshot.Nexus.Error)
                ? UiText.Get("ConfigurationError")
                : !snapshot.Nexus.Paired
                    ? UiText.Get("NotConfigured")
                    : snapshot.NexusConnected ? UiText.Get("Connected") : UiText.Get("NotConnected");

            ServiceStatusText.Text = snapshot.CoreRunning ? UiText.Get("Running") : UiText.Get("Stopped");
            HealthStatusText.Text = snapshot.Healthy ? UiText.Get("Healthy") : UiText.Get("Unavailable");
            VersionText.Text = string.IsNullOrWhiteSpace(snapshot.Version) ? UiText.Get("Unknown") : snapshot.Version;
            LocalMcpTextBox.Text = snapshot.LocalMcpUrl;
            PublicMcpTextBox.Text = snapshot.PublicMcpUrl;
            UpdateCredentialText();

            LocalModeRadio.IsChecked = string.Equals(snapshot.TunnelMode, "none", StringComparison.OrdinalIgnoreCase);
            QuickModeRadio.IsChecked = string.Equals(snapshot.TunnelMode, "quick", StringComparison.OrdinalIgnoreCase);
            NamedModeRadio.IsChecked = string.Equals(snapshot.TunnelMode, "named", StringComparison.OrdinalIgnoreCase);
            if (!ServerUrlTextBox.IsKeyboardFocusWithin &&
                (string.Equals(snapshot.TunnelMode, "named", StringComparison.OrdinalIgnoreCase) || string.IsNullOrWhiteSpace(ServerUrlTextBox.Text)))
            {
                ServerUrlTextBox.Text = string.Equals(snapshot.TunnelMode, "named", StringComparison.OrdinalIgnoreCase)
                    ? snapshot.PublicOrigin
                    : snapshot.SavedNamedOrigin;
            }
            TunnelTokenStoredText.Text = snapshot.TunnelTokenStored ? UiText.Get("TunnelTokenSaved") : UiText.Get("NotSaved");
            ElevatedCoreCheckBox.IsChecked = string.Equals(snapshot.Manifest.PrivilegeMode, "elevated", StringComparison.OrdinalIgnoreCase);
            CoreStartupCheckBox.IsChecked = snapshot.CoreStartupEnabled;
            TrayStartupCheckBox.IsChecked = snapshot.TrayStartupEnabled;

            if (!NexusEndpointTextBox.IsKeyboardFocusWithin && snapshot.Nexus.Paired)
            {
                NexusEndpointTextBox.Text = snapshot.Nexus.Endpoint;
            }
            NexusDeviceTokenStatusText.Text = snapshot.Nexus.Error.Length > 0
                ? snapshot.Nexus.Error
                : snapshot.Nexus.Paired
                    ? UiText.Format("DeviceTokenSaved", snapshot.Nexus.NodeId)
                    : UiText.Get("DeviceNotPairedHelp");
            NexusDeviceTokenStatusText.Foreground = snapshot.Nexus.Error.Length > 0
                ? new SolidColorBrush(Color.FromRgb(217, 45, 32))
                : new SolidColorBrush(Color.FromRgb(102, 112, 133));

            if (!_settingsLoaded)
            {
                PortTextBox.Text = snapshot.Settings.Port.ToString();
                SelectLogLevel(snapshot.Settings.LogLevel);
                McpAppsEnabledCheckBox.IsChecked = snapshot.Settings.McpAppsEnabled;
                BrowserEnabledCheckBox.IsChecked = snapshot.Settings.BrowserEnabled;
                BrowserCdpUrlTextBox.Text = snapshot.Settings.BrowserCdpUrl;
                SelectBrowserConnectionMode(snapshot.Settings);
                AcpEnabledCheckBox.IsChecked = snapshot.Settings.AcpEnabled;
                SelectAcpAgent(snapshot.Settings.AcpAgent);
                AcpCommandTextBox.Text = snapshot.Settings.AcpAgent == "custom" ? snapshot.Settings.AcpCommand : "";
                AcpArgsTextBox.Text = snapshot.Settings.AcpAgent == "custom"
                    ? JsonSerializer.Serialize(snapshot.Settings.AcpArgs ?? [])
                    : "[]";
                _settingsLoaded = true;
            }

            UpdateTunnelModeUi();
            RefreshBrowserConnectionUi();
            RefreshAcpUi();
        }
        finally
        {
            _updatingUi = false;
        }
    }

    private async Task AutoTestPublicAsync(RuntimeSnapshot snapshot)
    {
        if (string.IsNullOrWhiteSpace(snapshot.PublicOrigin))
        {
            PublicTestStatusText.Text = snapshot.TunnelMode == "quick" ? UiText.Get("WaitingTemporaryAddress") : UiText.Get("PublicAddressNotConfigured");
            return;
        }

        var now = DateTimeOffset.UtcNow;
        if (string.Equals(snapshot.PublicOrigin, _lastAutoTestOrigin, StringComparison.OrdinalIgnoreCase) &&
            now - _lastAutoTestAt < TimeSpan.FromSeconds(15))
        {
            return;
        }

        _lastAutoTestOrigin = snapshot.PublicOrigin;
        _lastAutoTestAt = now;
        PublicTestStatusText.Text = UiText.Get("AutoDetectingPublicAddress");
        var result = await _runtime.TestUrlAsync(snapshot.PublicOrigin);
        PublicTestStatusText.Text = result.Message;
    }

    private async Task<bool> ExecuteActionAsync(string pendingText, Func<Task> action, TextBlock? statusTarget = null)
    {
        statusTarget ??= FooterStatusText;
        statusTarget.Text = pendingText;
        try
        {
            await action();
            statusTarget.Text = UiText.Get("OperationCompleted");
            await RefreshAsync();
            return true;
        }
        catch (Exception ex)
        {
            statusTarget.Text = ex.Message;
            MessageBox.Show(this, ex.Message, "AgentDock", MessageBoxButton.OK, MessageBoxImage.Error);
            return false;
        }
    }

    private Task RunCoreActionAsync(string action, string pendingText) =>
        ExecuteActionAsync(pendingText, () => _runtime.RunActionAsync(action));

    private async void StartButton_Click(object sender, RoutedEventArgs e) => await RunCoreActionAsync("start", UiText.Get("Starting"));
    private async void StopButton_Click(object sender, RoutedEventArgs e) => await RunCoreActionAsync("stop", UiText.Get("Stopping"));
    private async void RestartButton_Click(object sender, RoutedEventArgs e) => await RunCoreActionAsync("restart", UiText.Get("Restarting"));

    private async void UpdateButton_Click(object sender, RoutedEventArgs e) =>
        await ((App)Application.Current).CheckForUpdatesAsync(this);

    public void SetUpdateState(bool inProgress, string? status = null)
    {
        UpdateButton.IsEnabled = !inProgress;
        if (!string.IsNullOrWhiteSpace(status))
        {
            FooterStatusText.Text = status;
        }
    }

    public void SetUpdateStatus(string status) => FooterStatusText.Text = status;

    private async void RefreshButton_Click(object sender, RoutedEventArgs e) => await RefreshAsync();

    private void ToggleBearerButton_Click(object sender, RoutedEventArgs e)
    {
        _showBearer = !_showBearer;
        UpdateCredentialText();
    }

    private void ToggleOAuthButton_Click(object sender, RoutedEventArgs e)
    {
        _showOAuth = !_showOAuth;
        UpdateCredentialText();
    }

    private void UpdateCredentialText()
    {
        BearerTokenTextBox.Text = _showBearer ? _bearerToken : MaskSecret(_bearerToken);
        OAuthPasswordTextBox.Text = _showOAuth ? _oauthPassword : MaskSecret(_oauthPassword);
        ToggleBearerButton.Content = _showBearer ? UiText.Get("Hide") : UiText.Get("Show");
        ToggleOAuthButton.Content = _showOAuth ? UiText.Get("Hide") : UiText.Get("Show");
    }

    private static string MaskSecret(string value) => string.IsNullOrEmpty(value) ? UiText.Get("NotConfigured") : new string('●', Math.Clamp(value.Length, 8, 32));

    private async void TestPublicButton_Click(object sender, RoutedEventArgs e)
    {
        var origin = _snapshot?.PublicOrigin ?? "";
        if (string.IsNullOrWhiteSpace(origin))
        {
            PublicTestStatusText.Text = UiText.Get("CurrentNoPublicAddress");
            return;
        }
        PublicTestStatusText.Text = UiText.Get("Testing");
        var result = await _runtime.TestUrlAsync(origin);
        PublicTestStatusText.Text = result.Message;
    }

    private void TunnelModeRadio_Checked(object sender, RoutedEventArgs e)
    {
        if (!_updatingUi)
        {
            UpdateTunnelModeUi();
        }
    }

    private void UpdateTunnelModeUi()
    {
        var named = NamedModeRadio.IsChecked == true;
        var quick = QuickModeRadio.IsChecked == true;
        NamedTunnelGroup.IsEnabled = named;
        RegenerateQuickButton.IsEnabled = quick;
    }

    private string SelectedTunnelMode()
    {
        if (QuickModeRadio.IsChecked == true)
        {
            return "quick";
        }
        if (NamedModeRadio.IsChecked == true)
        {
            return "named";
        }
        return "none";
    }

    private async void ApplyTunnelModeButton_Click(object sender, RoutedEventArgs e)
    {
        var mode = SelectedTunnelMode();
        if (mode == "quick")
        {
            PublicMcpTextBox.Text = "";
            PublicTestStatusText.Text = UiText.Get("GeneratingTemporaryAddress");
            _lastAutoTestOrigin = "";
        }
        await ExecuteActionAsync(
            UiText.Get("SwitchingPublicAccess"),
            () => _runtime.SetTunnelModeAsync(mode, ServerUrlTextBox.Text.Trim(), TunnelTokenPasswordBox.Password),
            TunnelActionStatusText);
        TunnelTokenPasswordBox.Clear();
    }

    private async void RegenerateQuickButton_Click(object sender, RoutedEventArgs e)
    {
        PublicMcpTextBox.Text = "";
        PublicTestStatusText.Text = UiText.Get("GeneratingTemporaryAddress");
        TunnelActionStatusText.Text = UiText.Get("OldAddressHidden");
        _lastAutoTestOrigin = "";
        await ExecuteActionAsync(
            UiText.Get("OldAddressHidden"),
            () => _runtime.RegenerateQuickTunnelAsync(),
            TunnelActionStatusText);
    }

    private void AcpSetting_Changed(object sender, RoutedEventArgs e)
    {
        if (!IsInitialized || _updatingUi)
        {
            return;
        }
        RefreshAcpUi();
    }

    private void BrowserConnection_Changed(object sender, RoutedEventArgs e)
    {
        if (!IsInitialized || _updatingUi)
        {
            return;
        }
        RefreshBrowserConnectionUi();
    }

    private void RefreshBrowserConnectionUi()
    {
        var mode = SelectedBrowserConnectionMode();
        BrowserCdpUrlTextBox.Visibility = mode == BrowserConnectionSpecified
            ? Visibility.Visible
            : Visibility.Collapsed;

        BrowserConnectionHelpText.Text = mode switch
        {
            BrowserConnectionReuse => UiText.Get("BrowserReuseHelp"),
            BrowserConnectionSpecified => UiText.Get("BrowserSpecifiedHelp"),
            _ => UiText.Get("BrowserManagedHelp")
        };
    }

    private string SelectedBrowserConnectionMode()
    {
        return (BrowserConnectionModeComboBox.SelectedItem as ComboBoxItem)?.Tag as string
            ?? BrowserConnectionManaged;
    }

    private void SelectBrowserConnectionMode(ControlPanelSettings settings)
    {
        // 兼容旧配置和运行时优先级：显式 CDP URL 始终优先于复用开关。
        var mode = !string.IsNullOrWhiteSpace(settings.BrowserCdpUrl)
            ? BrowserConnectionSpecified
            : settings.BrowserReuseExistingCdp ? BrowserConnectionReuse : BrowserConnectionManaged;
        foreach (var item in BrowserConnectionModeComboBox.Items.OfType<ComboBoxItem>())
        {
            if (string.Equals(item.Tag as string, mode, StringComparison.Ordinal))
            {
                BrowserConnectionModeComboBox.SelectedItem = item;
                return;
            }
        }
        BrowserConnectionModeComboBox.SelectedIndex = 0;
    }

    private void RefreshAcpUi()
    {
        var enabled = AcpEnabledCheckBox.IsChecked == true;
        AcpAgentComboBox.IsEnabled = enabled;
        var agent = SelectedAcpAgent();
        var isCustom = agent == "custom";
        var customVisibility = isCustom ? Visibility.Visible : Visibility.Collapsed;
        AcpCommandLabel.Visibility = customVisibility;
        AcpCommandTextBox.Visibility = customVisibility;
        AcpArgsLabel.Visibility = customVisibility;
        AcpArgsTextBox.Visibility = customVisibility;
        AcpCommandTextBox.IsEnabled = enabled && isCustom;
        AcpArgsTextBox.IsEnabled = enabled && isCustom;

        IReadOnlyList<string>? configuredArguments;
        string configuredCommand;
        if (isCustom)
        {
            configuredCommand = AcpCommandTextBox.Text.Trim();
            if (!TryReadAcpArguments(AcpArgsTextBox.Text, out var customArguments))
            {
                AcpStatusText.Text = UiText.Get("ArgsJsonInvalid");
                AcpStatusText.Foreground = enabled
                    ? new SolidColorBrush(Color.FromRgb(217, 45, 32))
                    : new SolidColorBrush(Color.FromRgb(102, 112, 133));
                return;
            }
            configuredArguments = customArguments;
        }
        else
        {
            var sameAgent = string.Equals(_snapshot?.Settings.AcpAgent, agent, StringComparison.OrdinalIgnoreCase);
            configuredCommand = sameAgent ? _snapshot?.Settings.AcpCommand ?? "" : "";
            configuredArguments = sameAgent ? _snapshot?.Settings.AcpArgs : null;
        }

        var resolution = _runtime.ResolveAcpAdapter(agent, configuredCommand, configuredArguments);
        AcpStatusText.Text = enabled
            ? resolution.Message
            : resolution.Available
                ? isCustom ? UiText.Get("CustomConfigured") : UiText.Format("AgentDetected", AgentDisplayName(agent))
                : resolution.Message;
        AcpStatusText.Foreground = enabled && !resolution.Available
            ? new SolidColorBrush(Color.FromRgb(217, 45, 32))
            : new SolidColorBrush(Color.FromRgb(102, 112, 133));
    }

    private async void LanguageComboBox_SelectionChanged(object sender, SelectionChangedEventArgs e)
    {
        if (_updatingUi || Application.Current is not App app)
        {
            return;
        }

        var preference = SelectedUiLanguage();
        if (preference == UiText.ReadPreference())
        {
            return;
        }

        var confirm = MessageBox.Show(
            this,
            UiText.Get("LanguageChangeDiscardWarning"),
            "AgentDock",
            MessageBoxButton.YesNo,
            MessageBoxImage.Warning);
        if (confirm != MessageBoxResult.Yes)
        {
            _updatingUi = true;
            SelectUiLanguage(UiText.ReadPreference());
            _updatingUi = false;
            return;
        }

        try
        {
            await app.ApplyLanguagePreferenceAsync(preference);
        }
        catch (Exception ex)
        {
            _updatingUi = true;
            SelectUiLanguage(UiText.ReadPreference());
            _updatingUi = false;
            SettingsStatusText.Text = UiText.Format("LanguageChangeFailed", ex.Message);
            MessageBox.Show(this, SettingsStatusText.Text, "AgentDock", MessageBoxButton.OK, MessageBoxImage.Error);
        }
    }

    private void SelectUiLanguage(string preference)
    {
        foreach (var item in LanguageComboBox.Items.OfType<ComboBoxItem>())
        {
            if (string.Equals(item.Tag?.ToString(), preference, StringComparison.Ordinal))
            {
                LanguageComboBox.SelectedItem = item;
                return;
            }
        }
        LanguageComboBox.SelectedIndex = 0;
    }

    private string SelectedUiLanguage() =>
        (LanguageComboBox.SelectedItem as ComboBoxItem)?.Tag?.ToString() ?? UiText.SystemPreference;

    private async void SaveSettingsButton_Click(object sender, RoutedEventArgs e)
    {
        if (!int.TryParse(PortTextBox.Text.Trim(), out var port) || port is < 1 or > 65535)
        {
            MessageBox.Show(this, UiText.Get("PortInvalid"), "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
            return;
        }

        var acpEnabled = AcpEnabledCheckBox.IsChecked == true;
        var acpAgent = SelectedAcpAgent();
        var isCustomAcp = acpAgent == "custom";
        var sameAcpAgent = string.Equals(_snapshot?.Settings.AcpAgent, acpAgent, StringComparison.OrdinalIgnoreCase);
        var configuredAcpCommand = isCustomAcp
            ? AcpCommandTextBox.Text.Trim()
            : sameAcpAgent ? _snapshot?.Settings.AcpCommand ?? "" : "";
        IReadOnlyList<string>? configuredAcpArguments;
        if (isCustomAcp)
        {
            if (!TryReadAcpArguments(AcpArgsTextBox.Text, out var customArguments))
            {
                MessageBox.Show(this, UiText.Get("ArgsJsonInvalid"), "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
                return;
            }
            configuredAcpArguments = customArguments;
        }
        else
        {
            configuredAcpArguments = sameAcpAgent ? _snapshot?.Settings.AcpArgs : null;
        }
        if (acpEnabled && !_runtime.ResolveAcpAdapter(acpAgent, configuredAcpCommand, configuredAcpArguments).Available)
        {
            var message = isCustomAcp
                ? UiText.Get("CustomAcpUnavailable")
                : UiText.Format("AgentUnavailable", AgentDisplayName(acpAgent));
            MessageBox.Show(this, message, "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
            return;
        }

        var browserConnectionMode = SelectedBrowserConnectionMode();
        var browserCdpUrl = BrowserCdpUrlTextBox.Text.Trim();
        if (browserConnectionMode == BrowserConnectionSpecified && string.IsNullOrWhiteSpace(browserCdpUrl))
        {
            MessageBox.Show(this, UiText.Get("SpecifiedCdpRequired"), "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
            return;
        }

        var settings = new ControlPanelSettings
        {
            Port = port,
            LogLevel = SelectedLogLevel(),
            OAuthAccessTokenTtl = _snapshot?.Settings.OAuthAccessTokenTtl ?? "",
            McpAppsEnabled = McpAppsEnabledCheckBox.IsChecked == true,
            BrowserEnabled = BrowserEnabledCheckBox.IsChecked == true,
            BrowserCdpUrl = browserConnectionMode == BrowserConnectionSpecified ? browserCdpUrl : "",
            BrowserReuseExistingCdp = browserConnectionMode == BrowserConnectionReuse,
            AcpEnabled = acpEnabled,
            AcpAgent = acpAgent,
            AcpCommand = isCustomAcp ? configuredAcpCommand : "",
            AcpArgs = isCustomAcp ? configuredAcpArguments?.ToList() ?? [] : []
        };
        var saved = await ExecuteActionAsync(
            UiText.Get("SavingAndRestarting"),
            () => _runtime.SaveSettingsAsync(settings),
            SettingsStatusText);
        if (saved)
        {
            BrowserCdpUrlTextBox.Text = settings.BrowserCdpUrl;
            SelectBrowserConnectionMode(settings);
            RefreshBrowserConnectionUi();
        }
    }

    private async void PairNexusButton_Click(object sender, RoutedEventArgs e)
    {
        var endpoint = NexusEndpointTextBox.Text.Trim();
        var pairingCode = NexusPairingCodePasswordBox.Password.Trim();
        if (endpoint.Length == 0 || pairingCode.Length == 0)
        {
            MessageBox.Show(this, UiText.Get("PairingFieldsRequired"), "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
            return;
        }
        var paired = await ExecuteActionAsync(
            UiText.Get("PairingAndRestarting"),
            () => _runtime.PairNexusAsync(endpoint, pairingCode),
            NexusDeviceTokenStatusText);
        if (paired)
        {
            NexusPairingCodePasswordBox.Clear();
        }
    }

    private async void ElevatedCoreCheckBox_Click(object sender, RoutedEventArgs e)
    {
        if (_updatingUi)
        {
            return;
        }

        var elevated = ElevatedCoreCheckBox.IsChecked == true;
        ElevatedCoreCheckBox.IsEnabled = false;
        SettingsStatusText.Text = elevated ? UiText.Get("SwitchingAdmin") : UiText.Get("SwitchingStandard");
        try
        {
            await _runtime.SetPrivilegeModeAsync(elevated);
            SettingsStatusText.Text = elevated ? UiText.Get("SwitchedAdmin") : UiText.Get("SwitchedStandard");
        }
        catch (Exception ex)
        {
            SettingsStatusText.Text = ex.Message;
            MessageBox.Show(this, ex.Message, "AgentDock", MessageBoxButton.OK, MessageBoxImage.Error);
        }
        finally
        {
            ElevatedCoreCheckBox.IsEnabled = true;
            await RefreshAsync();
        }
    }

    private async void CoreStartupCheckBox_Click(object sender, RoutedEventArgs e)
    {
        if (_updatingUi)
        {
            return;
        }
        await ExecuteActionAsync(
            UiText.Get("UpdatingCoreStartup"),
            () => _runtime.SetStartupAsync("core", CoreStartupCheckBox.IsChecked == true),
            SettingsStatusText);
    }

    private async void TrayStartupCheckBox_Click(object sender, RoutedEventArgs e)
    {
        if (_updatingUi)
        {
            return;
        }
        await ExecuteActionAsync(
            UiText.Get("UpdatingTrayStartup"),
            () => _runtime.SetStartupAsync("tray", TrayStartupCheckBox.IsChecked == true),
            SettingsStatusText);
    }

    private void SelectAcpAgent(string value)
    {
        foreach (var item in AcpAgentComboBox.Items.OfType<ComboBoxItem>())
        {
            if (string.Equals(item.Tag?.ToString(), value, StringComparison.OrdinalIgnoreCase))
            {
                AcpAgentComboBox.SelectedItem = item;
                return;
            }
        }
        AcpAgentComboBox.SelectedIndex = 0;
    }

    private string SelectedAcpAgent() =>
        (AcpAgentComboBox.SelectedItem as ComboBoxItem)?.Tag?.ToString() ?? "codex";

    private static string AgentDisplayName(string agent) => agent switch
    {
        "codex" => "Codex",
        "claude" => "Claude",
        "grok" => "Grok Build",
        "custom" => UiText.Get("Custom"),
        _ => throw new ArgumentOutOfRangeException(nameof(agent), agent, UiText.Get("UnsupportedCodingAgent"))
    };

    private static bool TryReadAcpArguments(string raw, out List<string> arguments)
    {
        arguments = [];
        var value = raw.Trim();
        if (value.Length == 0)
        {
            return true;
        }
        try
        {
            var decoded = JsonSerializer.Deserialize<List<string>>(value);
            if (decoded is null)
            {
                return false;
            }
            arguments = decoded;
            return true;
        }
        catch (JsonException)
        {
            return false;
        }
    }

    private void SelectLogLevel(string value)
    {
        foreach (var item in LogLevelComboBox.Items.OfType<ComboBoxItem>())
        {
            if (string.Equals(item.Content?.ToString(), value, StringComparison.OrdinalIgnoreCase))
            {
                LogLevelComboBox.SelectedItem = item;
                return;
            }
        }
        LogLevelComboBox.SelectedIndex = 1;
    }

    private string SelectedLogLevel() =>
        (LogLevelComboBox.SelectedItem as ComboBoxItem)?.Content?.ToString() ?? "info";

    private void OpenLogsButton_Click(object sender, RoutedEventArgs e)
    {
        try
        {
            _runtime.OpenLogsDirectory();
        }
        catch (Exception ex)
        {
            MessageBox.Show(this, ex.Message, "AgentDock", MessageBoxButton.OK, MessageBoxImage.Error);
        }
    }

    private void OpenConfigButton_Click(object sender, RoutedEventArgs e)
    {
        try
        {
            _runtime.OpenConfigDirectory();
        }
        catch (Exception ex)
        {
            MessageBox.Show(this, ex.Message, "AgentDock", MessageBoxButton.OK, MessageBoxImage.Error);
        }
    }

    private void PasswordBox_PreviewKeyDown(object sender, System.Windows.Input.KeyEventArgs e)
    {
        if (sender is not PasswordBox passwordBox || (Keyboard.Modifiers & ModifierKeys.Control) == 0)
        {
            return;
        }

        if (!_passwordHistory.TryGetValue(passwordBox, out var history))
        {
            history = new Stack<string>();
            _passwordHistory[passwordBox] = history;
        }

        switch (e.Key)
        {
            case Key.A:
                passwordBox.SelectAll();
                e.Handled = true;
                break;
            case Key.C:
                if (!string.IsNullOrEmpty(passwordBox.Password))
                {
                    Clipboard.SetText(passwordBox.Password);
                }
                e.Handled = true;
                break;
            case Key.X:
                history.Push(passwordBox.Password);
                if (!string.IsNullOrEmpty(passwordBox.Password))
                {
                    Clipboard.SetText(passwordBox.Password);
                }
                passwordBox.Clear();
                e.Handled = true;
                break;
            case Key.V:
                history.Push(passwordBox.Password);
                if (Clipboard.ContainsText())
                {
                    passwordBox.Password = Clipboard.GetText();
                    passwordBox.SelectAll();
                }
                e.Handled = true;
                break;
            case Key.Z:
                if (history.Count > 0)
                {
                    passwordBox.Password = history.Pop();
                    passwordBox.SelectAll();
                }
                e.Handled = true;
                break;
        }
    }

    private void MainWindow_Closing(object? sender, CancelEventArgs e)
    {
        if (Application.Current is App app && !app.ExitRequested)
        {
            e.Cancel = true;
            Hide();
            FooterStatusText.Text = UiText.Get("MinimizedToTray");
        }
    }
}

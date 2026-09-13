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
    private List<AcpProfileSettings> _acpProfiles = [];
    private string _acpDefaultProfile = "";
    private string _activeAcpProfileId = "";
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
                _acpProfiles = snapshot.Settings.AcpProfiles.Select(CloneAcpProfile).ToList();
                _acpDefaultProfile = snapshot.Settings.AcpDefaultProfile;
                _activeAcpProfileId = _acpProfiles.Any(profile => profile.Id == _acpDefaultProfile)
                    ? _acpDefaultProfile
                    : _acpProfiles.FirstOrDefault()?.Id ?? "";
                RefreshAcpProfileSelector(_activeAcpProfileId);
                LoadAcpProfileControls(_activeAcpProfileId);
                AcpAddProfileComboBox.SelectedIndex = 0;
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

    private void AcpProfile_Changed(object sender, SelectionChangedEventArgs e)
    {
        if (!IsInitialized || _updatingUi)
        {
            return;
        }
        var selectedId = (AcpProfileComboBox.SelectedItem as ComboBoxItem)?.Tag?.ToString() ?? "";
        if (selectedId.Length == 0 || selectedId == _activeAcpProfileId)
        {
            return;
        }
        if (!SaveActiveAcpProfile(showErrors: true))
        {
            RefreshAcpProfileSelector(_activeAcpProfileId);
            return;
        }
        RefreshAcpProfileSelector(selectedId);
        _activeAcpProfileId = selectedId;
        LoadAcpProfileControls(selectedId);
        RefreshAcpUi();
    }

    private void AcpProfileFlag_Changed(object sender, RoutedEventArgs e)
    {
        if (!IsInitialized || _updatingUi || _activeAcpProfileId.Length == 0)
        {
            return;
        }
        if (AcpProfileDefaultCheckBox.IsChecked != true && _acpDefaultProfile == _activeAcpProfileId)
        {
            _updatingUi = true;
            AcpProfileDefaultCheckBox.IsChecked = true;
            _updatingUi = false;
        }
        else if (AcpProfileDefaultCheckBox.IsChecked == true)
        {
            _acpDefaultProfile = _activeAcpProfileId;
        }
        if (AcpProfileDefaultCheckBox.IsChecked == true && AcpProfileEnabledCheckBox.IsChecked != true)
        {
            _updatingUi = true;
            AcpProfileEnabledCheckBox.IsChecked = true;
            _updatingUi = false;
        }
        _ = SaveActiveAcpProfile(showErrors: false);
        RefreshAcpProfileSelector(_activeAcpProfileId);
        RefreshAcpUi();
    }

    private void AcpAddProfile_Changed(object sender, SelectionChangedEventArgs e)
    {
        if (!IsInitialized || _updatingUi)
        {
            return;
        }
        var kind = (AcpAddProfileComboBox.SelectedItem as ComboBoxItem)?.Tag?.ToString() ?? "";
        if (kind.Length == 0)
        {
            return;
        }
        if (!SaveActiveAcpProfile(showErrors: true))
        {
            ResetAcpAddProfileSelector();
            return;
        }

        string id;
        if (kind == "custom")
        {
            id = "custom";
            var suffix = 2;
            while (_acpProfiles.Any(profile => string.Equals(profile.Id, id, StringComparison.Ordinal)))
            {
                id = $"custom-{suffix++}";
            }
        }
        else
        {
            id = kind;
            if (_acpProfiles.Any(profile => string.Equals(profile.Id, id, StringComparison.Ordinal)))
            {
                _activeAcpProfileId = id;
                RefreshAcpProfileSelector(id);
                LoadAcpProfileControls(id);
                ResetAcpAddProfileSelector();
                return;
            }
        }

        var resolution = kind == "custom" ? null : _runtime.ResolveAcpAdapter(kind);
        _acpProfiles.Add(new AcpProfileSettings
        {
            Id = id,
            Kind = kind,
            Command = resolution?.Command ?? "",
            Args = resolution?.Arguments?.ToList() ?? [],
            Enabled = true
        });
        if (_acpDefaultProfile.Length == 0)
        {
            _acpDefaultProfile = id;
        }
        _activeAcpProfileId = id;
        RefreshAcpProfileSelector(id);
        LoadAcpProfileControls(id);
        ResetAcpAddProfileSelector();
        RefreshAcpUi();
    }

    private void AcpRemoveProfile_Click(object sender, RoutedEventArgs e)
    {
        if (_updatingUi || _acpProfiles.Count <= 1)
        {
            return;
        }
        var index = _acpProfiles.FindIndex(profile => profile.Id == _activeAcpProfileId);
        if (index < 0)
        {
            return;
        }
        var removedId = _acpProfiles[index].Id;
        _acpProfiles.RemoveAt(index);
        if (_acpDefaultProfile == removedId)
        {
            _acpDefaultProfile = _acpProfiles.FirstOrDefault(profile => profile.Enabled)?.Id ?? _acpProfiles[0].Id;
        }
        _activeAcpProfileId = _acpProfiles[Math.Min(index, _acpProfiles.Count - 1)].Id;
        RefreshAcpProfileSelector(_activeAcpProfileId);
        LoadAcpProfileControls(_activeAcpProfileId);
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

    private static AcpProfileSettings CloneAcpProfile(AcpProfileSettings profile) => new()
    {
        Id = profile.Id,
        Kind = profile.Kind,
        Command = profile.Command,
        Args = [.. profile.Args],
        EnvFromEnv = profile.EnvFromEnv is null ? null : new Dictionary<string, string>(profile.EnvFromEnv),
        Enabled = profile.Enabled
    };

    private void RefreshAcpProfileSelector(string selectedId)
    {
        var previous = _updatingUi;
        _updatingUi = true;
        try
        {
            AcpProfileComboBox.Items.Clear();
            foreach (var profile in _acpProfiles)
            {
                var suffix = profile.Id == _acpDefaultProfile ? " · Default" : "";
                AcpProfileComboBox.Items.Add(new ComboBoxItem
                {
                    Content = $"{profile.Id} · {AgentDisplayName(profile.Kind)}{suffix}",
                    Tag = profile.Id
                });
            }
            var selected = AcpProfileComboBox.Items.OfType<ComboBoxItem>()
                .FirstOrDefault(item => string.Equals(item.Tag?.ToString(), selectedId, StringComparison.Ordinal));
            if (selected is not null)
            {
                AcpProfileComboBox.SelectedItem = selected;
            }
            else if (AcpProfileComboBox.Items.Count > 0)
            {
                AcpProfileComboBox.SelectedIndex = 0;
            }
        }
        finally
        {
            _updatingUi = previous;
        }
    }

    private void LoadAcpProfileControls(string profileId)
    {
        var profile = _acpProfiles.FirstOrDefault(item => item.Id == profileId);
        if (profile is null)
        {
            return;
        }
        var previous = _updatingUi;
        _updatingUi = true;
        try
        {
            _activeAcpProfileId = profile.Id;
            AcpProfileIdTextBox.Text = profile.Id;
            AcpProfileEnabledCheckBox.IsChecked = profile.Enabled;
            AcpProfileDefaultCheckBox.IsChecked = profile.Id == _acpDefaultProfile;
            SelectAcpAgent(profile.Kind);
            AcpCommandTextBox.Text = profile.Kind == "custom" ? profile.Command : "";
            AcpArgsTextBox.Text = profile.Kind == "custom" ? JsonSerializer.Serialize(profile.Args) : "[]";
        }
        finally
        {
            _updatingUi = previous;
        }
    }

    private void ResetAcpAddProfileSelector()
    {
        var previous = _updatingUi;
        _updatingUi = true;
        AcpAddProfileComboBox.SelectedIndex = 0;
        _updatingUi = previous;
    }

    private bool SaveActiveAcpProfile(bool showErrors)
    {
        var index = _acpProfiles.FindIndex(profile => profile.Id == _activeAcpProfileId);
        if (index < 0)
        {
            return _acpProfiles.Count == 0;
        }
        var profile = _acpProfiles[index];
        var oldId = profile.Id;
        var newId = profile.Kind == "custom" ? AcpProfileIdTextBox.Text.Trim() : profile.Kind;
        if (!IsValidAcpProfileId(newId) || _acpProfiles.Where((_, candidateIndex) => candidateIndex != index).Any(item => item.Id == newId))
        {
            if (showErrors)
            {
                MessageBox.Show(this, "Coding Agent profile IDs must be unique and use only letters, numbers, '.', '_' or '-'.", "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
            }
            return false;
        }
        if (profile.Kind == "custom")
        {
            if (!TryReadAcpArguments(AcpArgsTextBox.Text, out var arguments))
            {
                if (showErrors)
                {
                    MessageBox.Show(this, UiText.Get("ArgsJsonInvalid"), "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
                }
                return false;
            }
            profile.Command = AcpCommandTextBox.Text.Trim();
            profile.Args = arguments;
        }
        profile.Id = newId;
        profile.Enabled = AcpProfileEnabledCheckBox.IsChecked == true;
        _acpProfiles[index] = profile;
        if (_acpDefaultProfile == oldId)
        {
            _acpDefaultProfile = newId;
        }
        _activeAcpProfileId = newId;
        return true;
    }

    private static bool IsValidAcpProfileId(string value) =>
        value.Length is >= 1 and <= 64 && value.All(character =>
            character is >= 'a' and <= 'z'
            or >= 'A' and <= 'Z'
            or >= '0' and <= '9'
            or '.' or '_' or '-');

    private void RefreshAcpUi()
    {
        var profile = _acpProfiles.FirstOrDefault(item => item.Id == _activeAcpProfileId);
        if (profile is null)
        {
            AcpStatusText.Text = "Add a Coding Agent profile to continue.";
            return;
        }

        var globallyEnabled = AcpEnabledCheckBox.IsChecked == true;
        var profileEnabled = AcpProfileEnabledCheckBox.IsChecked == true;
        var effectiveEnabled = globallyEnabled && profileEnabled;
        var isCustom = profile.Kind == "custom";
        var customVisibility = isCustom ? Visibility.Visible : Visibility.Collapsed;
        AcpCommandLabel.Visibility = customVisibility;
        AcpCommandTextBox.Visibility = customVisibility;
        AcpArgsLabel.Visibility = customVisibility;
        AcpArgsTextBox.Visibility = customVisibility;
        AcpAgentComboBox.IsEnabled = false;
        AcpProfileComboBox.IsEnabled = true;
        AcpProfileIdTextBox.IsEnabled = isCustom;
        AcpProfileEnabledCheckBox.IsEnabled = true;
        AcpProfileDefaultCheckBox.IsEnabled = true;
        AcpRemoveProfileButton.IsEnabled = _acpProfiles.Count > 1;
        AcpCommandTextBox.IsEnabled = isCustom;
        AcpArgsTextBox.IsEnabled = isCustom;

        IReadOnlyList<string>? configuredArguments;
        string configuredCommand;
        if (isCustom)
        {
            configuredCommand = AcpCommandTextBox.Text.Trim();
            if (!TryReadAcpArguments(AcpArgsTextBox.Text, out var customArguments))
            {
                AcpStatusText.Text = UiText.Get("ArgsJsonInvalid");
                AcpStatusText.Foreground = effectiveEnabled
                    ? new SolidColorBrush(Color.FromRgb(217, 45, 32))
                    : new SolidColorBrush(Color.FromRgb(102, 112, 133));
                return;
            }
            configuredArguments = customArguments;
        }
        else
        {
            configuredCommand = profile.Command;
            configuredArguments = profile.Args;
        }

        var resolution = _runtime.ResolveAcpAdapter(profile.Kind, configuredCommand, configuredArguments);
        AcpStatusText.Text = effectiveEnabled
            ? resolution.Message
            : resolution.Available ? $"Configured {profile.Id} · takes effect when enabled" : resolution.Message;
        AcpStatusText.Foreground = effectiveEnabled && !resolution.Available
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
        if (!SaveActiveAcpProfile(showErrors: true))
        {
            return;
        }
        if (acpEnabled && !_acpProfiles.Any(profile => profile.Enabled))
        {
            MessageBox.Show(this, "Enable at least one Coding Agent profile.", "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
            return;
        }
        foreach (var profile in _acpProfiles)
        {
            if (!IsValidAcpProfileId(profile.Id))
            {
                MessageBox.Show(this, $"Invalid Coding Agent profile ID: {profile.Id}", "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
                return;
            }
            if ((profile.Kind is "codex" or "claude" or "grok") && profile.Id != profile.Kind)
            {
                MessageBox.Show(this, $"Built-in Coding Agent {profile.Kind} must use profile ID {profile.Kind}.", "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
                return;
            }
            if (profile.Kind == "custom" && (profile.Id is "codex" or "claude" or "grok"))
            {
                MessageBox.Show(this, $"Custom Coding Agent profile ID {profile.Id} is reserved.", "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
                return;
            }
            if (!string.IsNullOrWhiteSpace(profile.Command) && !Path.IsPathFullyQualified(profile.Command))
            {
                MessageBox.Show(this, $"Coding Agent profile {profile.Id} command must be an absolute path.", "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
                return;
            }
            if (!acpEnabled || !profile.Enabled)
            {
                continue;
            }
            var resolution = _runtime.ResolveAcpAdapter(profile.Kind, profile.Command, profile.Args);
            if (!resolution.Available)
            {
                MessageBox.Show(this, resolution.Message, "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
                return;
            }
            profile.Command = resolution.Command;
            profile.Args = resolution.Arguments.ToList();
        }
        var defaultProfile = _acpProfiles.FirstOrDefault(profile => profile.Id == _acpDefaultProfile);
        if (defaultProfile is null || acpEnabled && !defaultProfile.Enabled)
        {
            MessageBox.Show(this, "The default Coding Agent profile must reference an enabled profile.", "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
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
            AcpProfiles = _acpProfiles.Select(CloneAcpProfile).ToList(),
            AcpDefaultProfile = _acpDefaultProfile
        };
        var saved = await ExecuteActionAsync(
            UiText.Get("SavingAndRestarting"),
            () => _runtime.SaveSettingsAsync(settings),
            SettingsStatusText);
        if (saved)
        {
            _acpProfiles = settings.AcpProfiles.Select(CloneAcpProfile).ToList();
            _acpDefaultProfile = settings.AcpDefaultProfile;
            if (!_acpProfiles.Any(profile => profile.Id == _activeAcpProfileId))
            {
                _activeAcpProfileId = _acpDefaultProfile;
            }
            RefreshAcpProfileSelector(_activeAcpProfileId);
            LoadAcpProfileControls(_activeAcpProfileId);
            BrowserCdpUrlTextBox.Text = settings.BrowserCdpUrl;
            SelectBrowserConnectionMode(settings);
            RefreshBrowserConnectionUi();
            RefreshAcpUi();
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

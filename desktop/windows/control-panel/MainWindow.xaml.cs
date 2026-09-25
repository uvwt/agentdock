using System.ComponentModel;
using System.IO;
using System.Text.Json;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Media;
using Application = System.Windows.Application;
using Brushes = System.Windows.Media.Brushes;
using Button = System.Windows.Controls.Button;
using CheckBox = System.Windows.Controls.CheckBox;
using Clipboard = System.Windows.Clipboard;
using Color = System.Windows.Media.Color;
using HorizontalAlignment = System.Windows.HorizontalAlignment;
using MessageBox = System.Windows.MessageBox;
using Orientation = System.Windows.Controls.Orientation;
using TextBox = System.Windows.Controls.TextBox;
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
                RefreshAcpProfileOverview();
                _settingsLoaded = true;
            }

            UpdateTunnelModeUi();
            RefreshBrowserConnectionUi();
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

    private async Task<bool> ExecuteActionAsync(
        string pendingText,
        Func<Task> action,
        TextBlock? statusTarget = null,
        string diagnosticAction = "")
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
            _runtime.RecordControlPanelFailure(
                "window",
                string.IsNullOrWhiteSpace(diagnosticAction) ? "manual-action" : diagnosticAction,
                ex);
            var displayMessage = ControlPanelDiagnostics.LastNonEmptyLine(ex.Message);
            statusTarget.Text = displayMessage;
            MessageBox.Show(this, displayMessage, "AgentDock", MessageBoxButton.OK, MessageBoxImage.Error);
            return false;
        }
    }

    private Task RunCoreActionAsync(string action, string pendingText) =>
        ExecuteActionAsync(pendingText, () => _runtime.RunActionAsync(action), diagnosticAction: action);

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
            TunnelActionStatusText,
            "tunnel-configure");
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
            TunnelActionStatusText,
            "tunnel-regenerate");
    }

    private void AcpOverviewToggle_Changed(object sender, RoutedEventArgs e)
    {
        if (_updatingUi || sender is not CheckBox toggle || toggle.Tag is not string key)
        {
            return;
        }

        if (key.StartsWith("builtin:", StringComparison.Ordinal))
        {
            var kind = key["builtin:".Length..];
            var index = _acpProfiles.FindIndex(profile => profile.Kind == kind);
            if (index >= 0)
            {
                UpdateAcpProfileEnabled(index, toggle.IsChecked == true);
            }
            else if (toggle.IsChecked == true)
            {
                var resolution = _runtime.ResolveAcpAdapter(kind);
                _acpProfiles.Add(new AcpProfileSettings
                {
                    Id = kind,
                    Kind = kind,
                    Command = resolution.Command,
                    Args = resolution.Arguments.ToList(),
                    Enabled = true
                });
                if (_acpDefaultProfile.Length == 0)
                {
                    _acpDefaultProfile = kind;
                }
            }
        }
        else if (key.StartsWith("profile:", StringComparison.Ordinal))
        {
            var profileId = key["profile:".Length..];
            var index = _acpProfiles.FindIndex(profile => profile.Id == profileId);
            if (index >= 0)
            {
                UpdateAcpProfileEnabled(index, toggle.IsChecked == true);
            }
        }
        RefreshAcpProfileOverview();
    }

    private void UpdateAcpProfileEnabled(int index, bool enabled)
    {
        var profileId = _acpProfiles[index].Id;
        _acpProfiles[index].Enabled = enabled;
        if (!enabled && _acpDefaultProfile == profileId)
        {
            var replacement = _acpProfiles.FirstOrDefault(profile => profile.Id != profileId && profile.Enabled);
            _acpDefaultProfile = replacement?.Id ?? "";
        }
        if (enabled && _acpDefaultProfile.Length == 0)
        {
            _acpDefaultProfile = profileId;
        }
    }

    private void AcpDefaultProfile_SelectionChanged(object sender, SelectionChangedEventArgs e)
    {
        if (!IsInitialized || _updatingUi || AcpDefaultProfileComboBox.SelectedItem is not ComboBoxItem item || item.Tag is not string profileId)
        {
            return;
        }
        if (_acpProfiles.Any(profile => profile.Id == profileId && profile.Enabled))
        {
            _acpDefaultProfile = profileId;
        }
    }

    private void AcpAddCustomProfile_Click(object sender, RoutedEventArgs e)
    {
        if (_updatingUi)
        {
            return;
        }
        var result = ShowCustomAcpProfileDialog(null);
        if (result is null)
        {
            return;
        }
        var id = UniqueCustomAcpProfileId(result.Name);
        _acpProfiles.Add(new AcpProfileSettings
        {
            Id = id,
            DisplayName = result.Name,
            Kind = "custom",
            Command = result.Command,
            Args = result.Arguments,
            Enabled = false
        });
        RefreshAcpProfileOverview();
    }

    private void AcpCustomProfileEdit_Click(object sender, MouseButtonEventArgs e)
    {
        if (_updatingUi || sender is not TextBlock label || label.Tag is not string profileId)
        {
            return;
        }
        var index = _acpProfiles.FindIndex(profile => profile.Id == profileId);
        if (index < 0 || _acpProfiles[index].Kind != "custom")
        {
            return;
        }
        var result = ShowCustomAcpProfileDialog(_acpProfiles[index]);
        if (result is null)
        {
            return;
        }
        if (result.DeleteRequested)
        {
            var removedId = _acpProfiles[index].Id;
            _acpProfiles.RemoveAt(index);
            if (_acpDefaultProfile == removedId)
            {
                _acpDefaultProfile = _acpProfiles.FirstOrDefault(profile => profile.Enabled)?.Id ?? "";
            }
        }
        else
        {
            // 自定义 Agent 的内部 ID 是已有 Session 身份的一部分；编辑只更新用户可见配置，绝不重生成 ID。
            var profile = _acpProfiles[index];
            profile.DisplayName = result.Name;
            profile.Command = result.Command;
            profile.Args = result.Arguments;
            _acpProfiles[index] = profile;
        }
        RefreshAcpProfileOverview();
    }

    private CustomAcpProfileDialogResult? ShowCustomAcpProfileDialog(AcpProfileSettings? existing)
    {
        var editing = existing is not null;
        var dialog = new Window
        {
            Title = UiText.Get(editing ? "EditCustomAcpTitle" : "AddCustomAcpTitle"),
            Owner = this,
            Width = 520,
            SizeToContent = SizeToContent.Height,
            WindowStartupLocation = WindowStartupLocation.CenterOwner,
            ResizeMode = ResizeMode.NoResize,
            ShowInTaskbar = false
        };
        var nameInput = new TextBox { Text = existing is null ? "" : AcpDisplayName(existing), Margin = new Thickness(0, 5, 0, 10) };
        var commandInput = new TextBox { Text = existing?.Command ?? "", Margin = new Thickness(0, 5, 0, 10) };
        var argsInput = new TextBox
        {
            Text = JsonSerializer.Serialize(existing?.Args ?? []),
            Margin = new Thickness(0, 5, 0, 14)
        };
        var save = new Button { Content = UiText.Get(editing ? "Save" : "Add"), IsDefault = true, MinWidth = 88, Height = 32, Margin = new Thickness(8, 0, 0, 0) };
        var cancel = new Button { Content = UiText.Get("Cancel"), IsCancel = true, MinWidth = 88, Height = 32, Margin = new Thickness(8, 0, 0, 0) };
        var delete = new Button { Content = UiText.Get("Delete"), MinWidth = 88, Height = 32 };
        var deleteRequested = false;
        save.Click += (_, _) =>
        {
            if (nameInput.Text.Trim().Length == 0)
            {
                nameInput.Focus();
                return;
            }
            if (!TryReadAcpArguments(argsInput.Text, out _))
            {
                MessageBox.Show(dialog, UiText.Get("ArgsJsonInvalid"), "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
                argsInput.Focus();
                return;
            }
            dialog.DialogResult = true;
        };
        delete.Click += (_, _) =>
        {
            deleteRequested = true;
            dialog.DialogResult = true;
        };
        var buttons = new Grid { Margin = new Thickness(0, 4, 0, 0) };
        buttons.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        buttons.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        buttons.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var rightButtons = new StackPanel { Orientation = Orientation.Horizontal, HorizontalAlignment = HorizontalAlignment.Right };
        if (editing)
        {
            Grid.SetColumn(delete, 0);
            buttons.Children.Add(delete);
        }
        rightButtons.Children.Add(save);
        rightButtons.Children.Add(cancel);
        Grid.SetColumn(rightButtons, 2);
        buttons.Children.Add(rightButtons);
        var content = new StackPanel { Margin = new Thickness(18) };
        content.Children.Add(new TextBlock { Text = UiText.Get("Name"), FontWeight = FontWeights.SemiBold });
        content.Children.Add(nameInput);
        content.Children.Add(new TextBlock { Text = UiText.Get("Command"), FontWeight = FontWeights.SemiBold });
        content.Children.Add(commandInput);
        content.Children.Add(new TextBlock { Text = UiText.Get("ArgsJson"), FontWeight = FontWeights.SemiBold });
        content.Children.Add(argsInput);
        content.Children.Add(buttons);
        dialog.Content = content;
        dialog.Loaded += (_, _) => nameInput.Focus();
        if (dialog.ShowDialog() != true)
        {
            return null;
        }
        if (deleteRequested)
        {
            return new CustomAcpProfileDialogResult("", "", [], true);
        }
        _ = TryReadAcpArguments(argsInput.Text, out var arguments);
        return new CustomAcpProfileDialogResult(
            nameInput.Text.Trim(),
            commandInput.Text.Trim(),
            arguments,
            false);
    }

    private string UniqueCustomAcpProfileId(string name)
    {
        var chars = name.ToLowerInvariant().Select(character =>
            character is >= 'a' and <= 'z' or >= '0' and <= '9' ? character : '-').ToArray();
        var baseId = new string(chars);
        while (baseId.Contains("--", StringComparison.Ordinal))
        {
            baseId = baseId.Replace("--", "-", StringComparison.Ordinal);
        }
        baseId = baseId.Trim('-');
        if (baseId.Length == 0)
        {
            baseId = "custom";
        }
        if (baseId is "codex" or "claude" or "grok")
        {
            baseId += "-custom";
        }
        if (baseId.Length > 48)
        {
            baseId = baseId[..48].TrimEnd('-');
        }
        var existing = _acpProfiles.Select(profile => profile.Id).ToHashSet(StringComparer.Ordinal);
        if (!existing.Contains(baseId))
        {
            return baseId;
        }
        var suffix = 2;
        while (existing.Contains($"{baseId}-{suffix}"))
        {
            suffix++;
        }
        return $"{baseId}-{suffix}";
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
        DisplayName = profile.DisplayName,
        Kind = profile.Kind,
        Command = profile.Command,
        Args = [.. profile.Args],
        EnvFromEnv = profile.EnvFromEnv is null ? null : new Dictionary<string, string>(profile.EnvFromEnv),
        Enabled = profile.Enabled
    };

    private void RefreshAcpProfileOverview()
    {
        var previous = _updatingUi;
        _updatingUi = true;
        try
        {
            AcpProfileListPanel.Children.Clear();
            foreach (var kind in new[] { "codex", "claude", "grok" })
            {
                var profile = _acpProfiles.FirstOrDefault(item => item.Kind == kind);
                AcpProfileListPanel.Children.Add(BuildAcpOverviewRow(kind, profile));
            }
            foreach (var profile in _acpProfiles.Where(profile => profile.Kind == "custom"))
            {
                AcpProfileListPanel.Children.Add(BuildAcpOverviewRow("custom", profile));
            }
            RefreshAcpDefaultProfileOptions();
        }
        finally
        {
            _updatingUi = previous;
        }
    }

    private void RefreshAcpDefaultProfileOptions()
    {
        var enabledProfiles = _acpProfiles.Where(profile => profile.Enabled).ToList();
        if (!enabledProfiles.Any(profile => profile.Id == _acpDefaultProfile))
        {
            _acpDefaultProfile = enabledProfiles.FirstOrDefault()?.Id ?? "";
        }

        AcpDefaultProfileComboBox.Items.Clear();
        foreach (var profile in enabledProfiles)
        {
            var item = new ComboBoxItem { Content = AcpDisplayName(profile), Tag = profile.Id };
            AcpDefaultProfileComboBox.Items.Add(item);
            if (profile.Id == _acpDefaultProfile)
            {
                AcpDefaultProfileComboBox.SelectedItem = item;
            }
        }
        AcpDefaultProfileComboBox.IsEnabled = enabledProfiles.Count > 0;
    }

    private Border BuildAcpOverviewRow(string kind, AcpProfileSettings? profile)
    {
        var name = profile is not null ? AcpDisplayName(profile) : AgentDisplayName(kind);
        var nameView = new TextBlock
        {
            Text = name,
            FontWeight = FontWeights.Medium,
            VerticalAlignment = VerticalAlignment.Center
        };
        if (profile?.Kind == "custom")
        {
            nameView.Tag = profile.Id;
            nameView.Cursor = System.Windows.Input.Cursors.Hand;
            nameView.MouseLeftButtonUp += AcpCustomProfileEdit_Click;
        }
        var toggle = new CheckBox
        {
            IsChecked = profile?.Enabled == true,
            Tag = profile is null ? $"builtin:{kind}" : $"profile:{profile.Id}",
            VerticalAlignment = VerticalAlignment.Center
        };
        toggle.Checked += AcpOverviewToggle_Changed;
        toggle.Unchecked += AcpOverviewToggle_Changed;

        var row = new Grid();
        row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        row.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        Grid.SetColumn(nameView, 0);
        Grid.SetColumn(toggle, 1);
        row.Children.Add(nameView);
        row.Children.Add(toggle);

        return new Border
        {
            Child = row,
            HorizontalAlignment = HorizontalAlignment.Stretch,
            BorderBrush = new SolidColorBrush(Color.FromRgb(234, 236, 240)),
            BorderThickness = new Thickness(0, 0, 0, 1),
            Padding = new Thickness(8, 8, 8, 8)
        };
    }

    private static string AcpDisplayName(AcpProfileSettings profile)
    {
        if (profile.Kind != "custom")
        {
            return AgentDisplayName(profile.Kind);
        }
        var name = (profile.DisplayName ?? "").Trim();
        return name.Length == 0 ? profile.Id : name;
    }

    private static bool IsValidAcpProfileId(string value) =>
        value.Length is >= 1 and <= 64 && value.All(character =>
            character is >= 'a' and <= 'z'
            or >= 'A' and <= 'Z'
            or >= '0' and <= '9'
            or '.' or '_' or '-');

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
        if (acpEnabled && (defaultProfile is null || !defaultProfile.Enabled))
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
            SettingsStatusText,
            "settings-save");
        if (saved)
        {
            _acpProfiles = settings.AcpProfiles.Select(CloneAcpProfile).ToList();
            _acpDefaultProfile = settings.AcpDefaultProfile;
            RefreshAcpProfileOverview();
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
            NexusDeviceTokenStatusText,
            "nexus-pair");
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
            _runtime.RecordControlPanelFailure(
                "window",
                elevated ? "privilege-elevated" : "privilege-standard",
                ex);
            var displayMessage = ControlPanelDiagnostics.LastNonEmptyLine(ex.Message);
            SettingsStatusText.Text = displayMessage;
            MessageBox.Show(this, displayMessage, "AgentDock", MessageBoxButton.OK, MessageBoxImage.Error);
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
            SettingsStatusText,
            "core-autostart");
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
            SettingsStatusText,
            "tray-autostart");
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

    private sealed record CustomAcpProfileDialogResult(
        string Name,
        string Command,
        List<string> Arguments,
        bool DeleteRequested);

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

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
        Closing += MainWindow_Closing;
    }

    public async Task RefreshAsync()
    {
        if (!await _refreshGate.WaitAsync(0))
        {
            return;
        }

        try
        {
            FooterStatusText.Text = "正在刷新…";
            var snapshot = await _runtime.GetSnapshotAsync(includeNexusConnection: true);
            _snapshot = snapshot;
            _bearerToken = _runtime.ReadBearerToken();
            _oauthPassword = _runtime.ReadOAuthPassword();
            ApplySnapshot(snapshot);
            FooterStatusText.Text = $"上次刷新：{snapshot.CheckedAt:HH:mm:ss}";
            await AutoTestPublicAsync(snapshot);
        }
        catch (Exception ex)
        {
            FooterStatusText.Text = ex.Message;
            HeaderStatusText.Text = "状态读取失败";
            StatusDot.Fill = new SolidColorBrush(Color.FromRgb(217, 45, 32));
            NexusStatusText.Text = "状态读取失败";
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
            HeaderStatusText.Text = snapshot.Healthy ? "运行正常" : snapshot.CoreRunning ? "运行但健康检查失败" : "已停止";
            StatusDot.Fill = new SolidColorBrush(snapshot.Healthy
                ? Color.FromRgb(18, 183, 106)
                : snapshot.CoreRunning ? Color.FromRgb(247, 144, 9) : Color.FromRgb(152, 162, 179));

            NexusStatusText.Text = !string.IsNullOrWhiteSpace(snapshot.Nexus.Error)
                ? "配置异常"
                : !snapshot.Nexus.Paired
                    ? "未配置"
                    : snapshot.NexusConnected ? "已连接" : "未连接";

            ServiceStatusText.Text = snapshot.CoreRunning ? "正在运行" : "已停止";
            HealthStatusText.Text = snapshot.Healthy ? "正常" : "不可用";
            VersionText.Text = string.IsNullOrWhiteSpace(snapshot.Version) ? "未知" : snapshot.Version;
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
            TunnelTokenStoredText.Text = snapshot.TunnelTokenStored ? "Tunnel Token已加密保存" : "未保存";
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
                    ? $"已安全保存 · node_id={snapshot.Nexus.NodeId}"
                    : "尚未配对；Device Token 将由一次性配对自动生成。";
            NexusDeviceTokenStatusText.Foreground = snapshot.Nexus.Error.Length > 0
                ? new SolidColorBrush(Color.FromRgb(217, 45, 32))
                : new SolidColorBrush(Color.FromRgb(102, 112, 133));

            if (!_settingsLoaded)
            {
                PortTextBox.Text = snapshot.Settings.Port.ToString();
                SelectLogLevel(snapshot.Settings.LogLevel);
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
            PublicTestStatusText.Text = snapshot.TunnelMode == "quick" ? "正在等待新的临时地址…" : "未配置公网地址";
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
        PublicTestStatusText.Text = "正在自动检测公网地址…";
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
            statusTarget.Text = "操作完成";
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

    private async void StartButton_Click(object sender, RoutedEventArgs e) => await RunCoreActionAsync("start", "正在启动…");
    private async void StopButton_Click(object sender, RoutedEventArgs e) => await RunCoreActionAsync("stop", "正在停止…");
    private async void RestartButton_Click(object sender, RoutedEventArgs e) => await RunCoreActionAsync("restart", "正在重启…");

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
        ToggleBearerButton.Content = _showBearer ? "隐藏" : "显示";
        ToggleOAuthButton.Content = _showOAuth ? "隐藏" : "显示";
    }

    private static string MaskSecret(string value) => string.IsNullOrEmpty(value) ? "未配置" : new string('●', Math.Clamp(value.Length, 8, 32));

    private async void TestPublicButton_Click(object sender, RoutedEventArgs e)
    {
        var origin = _snapshot?.PublicOrigin ?? "";
        if (string.IsNullOrWhiteSpace(origin))
        {
            PublicTestStatusText.Text = "当前没有公网地址";
            return;
        }
        PublicTestStatusText.Text = "正在测试…";
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
            PublicTestStatusText.Text = "正在生成新的临时地址…";
            _lastAutoTestOrigin = "";
        }
        await ExecuteActionAsync(
            "正在切换公网访问模式…",
            () => _runtime.SetTunnelModeAsync(mode, ServerUrlTextBox.Text.Trim(), TunnelTokenPasswordBox.Password),
            TunnelActionStatusText);
        TunnelTokenPasswordBox.Clear();
    }

    private async void RegenerateQuickButton_Click(object sender, RoutedEventArgs e)
    {
        PublicMcpTextBox.Text = "";
        PublicTestStatusText.Text = "正在生成新的临时地址…";
        TunnelActionStatusText.Text = "旧地址已隐藏，正在启动新的 Quick Tunnel…";
        _lastAutoTestOrigin = "";
        await ExecuteActionAsync(
            "旧地址已隐藏，正在启动新的 Quick Tunnel…",
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
            BrowserConnectionReuse => "优先使用本机已有 CDP 浏览器；未找到时自动使用隔离浏览器。可能沿用已有登录状态。",
            BrowserConnectionSpecified => "使用指定 CDP 浏览器，并可能沿用其中的登录状态。",
            _ => "使用隔离浏览器，不沿用日常浏览器登录状态。"
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
                AcpStatusText.Text = "Args JSON 必须是 JSON 字符串数组。";
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
                ? isCustom ? "已配置 自定义 · 启用后生效" : $"已检测到 {AgentDisplayName(agent)} · 启用后生效"
                : resolution.Message;
        AcpStatusText.Foreground = enabled && !resolution.Available
            ? new SolidColorBrush(Color.FromRgb(217, 45, 32))
            : new SolidColorBrush(Color.FromRgb(102, 112, 133));
    }

    private async void SaveSettingsButton_Click(object sender, RoutedEventArgs e)
    {
        if (!int.TryParse(PortTextBox.Text.Trim(), out var port) || port is < 1 or > 65535)
        {
            MessageBox.Show(this, "端口必须是 1 到 65535 之间的整数。", "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
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
                MessageBox.Show(this, "Args JSON 必须是 JSON 字符串数组。", "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
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
                ? "自定义 ACP Adapter 当前不可用，请检查 Command 与 Args JSON。"
                : $"{AgentDisplayName(acpAgent)} 当前不可用，请先安装对应命令。";
            MessageBox.Show(this, message, "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
            return;
        }

        var browserConnectionMode = SelectedBrowserConnectionMode();
        var browserCdpUrl = BrowserCdpUrlTextBox.Text.Trim();
        if (browserConnectionMode == BrowserConnectionSpecified && string.IsNullOrWhiteSpace(browserCdpUrl))
        {
            MessageBox.Show(this, "选择“连接指定 CDP 浏览器”时必须填写 CDP 地址。", "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
            return;
        }

        var settings = new ControlPanelSettings
        {
            Port = port,
            LogLevel = SelectedLogLevel(),
            OAuthAccessTokenTtl = _snapshot?.Settings.OAuthAccessTokenTtl ?? "",
            BrowserEnabled = BrowserEnabledCheckBox.IsChecked == true,
            BrowserCdpUrl = browserConnectionMode == BrowserConnectionSpecified ? browserCdpUrl : "",
            BrowserReuseExistingCdp = browserConnectionMode == BrowserConnectionReuse,
            AcpEnabled = acpEnabled,
            AcpAgent = acpAgent,
            AcpCommand = isCustomAcp ? configuredAcpCommand : "",
            AcpArgs = isCustomAcp ? configuredAcpArguments?.ToList() ?? [] : []
        };
        var saved = await ExecuteActionAsync(
            "正在保存设置并重启…",
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
            MessageBox.Show(this, "请填写 NexusDock 地址和一次性配对码。", "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
            return;
        }
        var paired = await ExecuteActionAsync(
            "正在配对并重启 AgentDock…",
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
        SettingsStatusText.Text = elevated ? "正在切换到管理员模式…" : "正在切换到普通用户模式…";
        try
        {
            await _runtime.SetPrivilegeModeAsync(elevated);
            SettingsStatusText.Text = elevated ? "已切换到管理员模式" : "已切换到普通用户模式";
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
            "正在更新核心开机启动…",
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
            "正在更新托盘开机启动…",
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
        "custom" => "自定义",
        _ => throw new ArgumentOutOfRangeException(nameof(agent), agent, "不支持的 Coding Agent")
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
            FooterStatusText.Text = "控制面板已最小化到系统托盘";
        }
    }
}

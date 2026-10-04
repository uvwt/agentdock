using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Navigation;
using Microsoft.UI.Xaml.Shapes;

namespace AgentDock.ControlPanel;

public sealed partial class SettingsPage : Page
{
    private RuntimeService? _runtime;
    private RuntimeSnapshot? _snapshot;
    private string _activeTag = "runtime";

    public SettingsPage() { InitializeComponent(); }

    protected override async void OnNavigatedTo(NavigationEventArgs e)
    {
        _runtime = e.Parameter as RuntimeService;
        await RefreshAsync();
        if (SettingsNavigation.SelectedIndex < 0) SettingsNavigation.SelectedIndex = 0;
        else Render(_activeTag);
    }

    private async Task RefreshAsync()
    {
        if (_runtime is not null) _snapshot = await _runtime.GetSnapshotAsync(includeNexusConnection: false);
    }

    private void SettingsNavigation_SelectionChanged(object sender, SelectionChangedEventArgs e)
    {
        if (SettingsContent is null) return;
        if (SettingsNavigation.SelectedItem is ListViewItem item && item.Tag is string tag)
        {
            _activeTag = tag;
            Render(tag);
        }
    }

    private void Render(string tag)
    {
        SettingsContent.Children.Clear();
        var header = new StackPanel { Spacing = 5 };
        header.Children.Add(new TextBlock { Text = PageTitle(tag), FontSize = 20, FontWeight = Microsoft.UI.Text.FontWeights.SemiBold });
        header.Children.Add(new TextBlock { Text = PageDetail(tag), FontSize = 12.5, Opacity = 0.62 });
        SettingsContent.Children.Add(header);

        var section = new SectionCard { Title = SectionTitle(tag) };
        section.SectionContent = tag switch
        {
            "permissions" => BuildPermissions(),
            "startup" => BuildStartup(),
            "credentials" => BuildCredentials(),
            _ => BuildRuntime()
        };
        SettingsContent.Children.Add(section);
    }

    private UIElement BuildRuntime()
    {
        var rows = new StackPanel();
        rows.Children.Add(Row("状态", _snapshot?.CoreRunning == true ? "正在运行 ●" : "已停止"));
        rows.Children.Add(Divider());
        rows.Children.Add(Row("本地地址", _snapshot?.LocalMcpUrl ?? "—"));
        rows.Children.Add(Divider());

        var action = new Button { Content = _snapshot?.CoreRunning == true ? "停止" : "启动" };
        action.Click += RuntimeAction_Click;
        rows.Children.Add(ActionRow("服务", action));
        rows.Children.Add(Divider());

        var port = new NumberBox { Value = _snapshot?.Settings.Port ?? 8765, Minimum = 1, Maximum = 65535, Width = 130, Tag = "port" };
        rows.Children.Add(ActionRow("端口", port));
        rows.Children.Add(Divider());

        var log = new ComboBox { Width = 130, Tag = "log" };
        foreach (var value in new[] { "debug", "info", "warn", "error" }) log.Items.Add(new ComboBoxItem { Content = value, Tag = value });
        SelectComboTag(log, _snapshot?.Settings.LogLevel ?? "info");
        rows.Children.Add(ActionRow("日志级别", log));
        rows.Children.Add(Divider());

        var language = new ComboBox { Width = 150, Tag = "language" };
        language.Items.Add(new ComboBoxItem { Content = "跟随系统", Tag = UiText.SystemPreference });
        language.Items.Add(new ComboBoxItem { Content = "简体中文", Tag = UiText.SimplifiedChinesePreference });
        language.Items.Add(new ComboBoxItem { Content = "English", Tag = UiText.EnglishPreference });
        SelectComboTag(language, UiText.ReadPreference());
        rows.Children.Add(ActionRow("界面语言", language));
        rows.Children.Add(Divider());

        var save = new Button { Content = "保存并重启" };
        save.Click += SaveRuntimeSettings_Click;
        rows.Children.Add(ActionRow("Runtime 配置", save));
        rows.Children.Add(Divider());

        var update = new Button { Content = "检查更新" };
        update.Click += CheckUpdate_Click;
        rows.Children.Add(ActionRow("AgentDock 更新", update));
        return rows;
    }

    private UIElement BuildPermissions()
    {
        var rows = new StackPanel();
        rows.Children.Add(Row("AgentDock 权限", "由 Windows 系统管理"));
        rows.Children.Add(Divider());
        rows.Children.Add(Row("管理员模式", string.Equals(_snapshot?.Manifest.PrivilegeMode, "elevated", StringComparison.OrdinalIgnoreCase) ? "已启用" : "未启用"));
        var toggle = new ToggleSwitch
        {
            IsOn = string.Equals(_snapshot?.Manifest.PrivilegeMode, "elevated", StringComparison.OrdinalIgnoreCase),
            Tag = "elevated"
        };
        toggle.Toggled += PrivilegeToggle_Toggled;
        rows.Children.Add(ActionRow("以管理员权限运行 Core", toggle));
        return rows;
    }

    private UIElement BuildStartup()
    {
        var rows = new StackPanel();
        var core = new ToggleSwitch { IsOn = _snapshot?.CoreStartupEnabled == true, Tag = "core" };
        core.Toggled += StartupToggle_Toggled;
        rows.Children.Add(ActionRow("Core 后台服务", core));
        rows.Children.Add(Divider());
        var tray = new ToggleSwitch { IsOn = _snapshot?.TrayStartupEnabled == true, Tag = "tray" };
        tray.Toggled += StartupToggle_Toggled;
        rows.Children.Add(ActionRow("托盘应用", tray));
        return rows;
    }

    private UIElement BuildCredentials()
    {
        var rows = new StackPanel();
        rows.Children.Add(CredentialRow("认证令牌", "bearer"));
        rows.Children.Add(Divider());
        rows.Children.Add(CredentialRow("OAuth 密码", "oauth"));
        return rows;
    }

    private Grid CredentialRow(string title, string kind)
    {
        var value = new PasswordBox { Password = ReadCredential(kind), IsPasswordRevealButtonEnabled = true, Width = 330 };
        return ActionRow(title, value);
    }

    private string ReadCredential(string kind)
    {
        if (_runtime is null) return "";
        try { return kind == "bearer" ? _runtime.ReadBearerToken() : _runtime.ReadOAuthPassword(); }
        catch { return ""; }
    }

    private async void RuntimeAction_Click(object sender, RoutedEventArgs e)
    {
        if (_runtime is null || sender is not Button button) return;
        button.IsEnabled = false;
        try
        {
            await _runtime.RunCoreActionAsync(_snapshot?.CoreRunning == true ? "stop" : "start");
            await RefreshAsync();
            Render("runtime");
        }
        finally { button.IsEnabled = true; }
    }

    private async void SaveRuntimeSettings_Click(object sender, RoutedEventArgs e)
    {
        if (_runtime is null || _snapshot is null) return;
        var rows = (sender as Button)?.Parent is Grid actionRow && actionRow.Parent is StackPanel panel ? panel : null;
        if (rows is null) return;
        var port = FindTagged<NumberBox>(rows, "port");
        var log = FindTagged<ComboBox>(rows, "log");
        var language = FindTagged<ComboBox>(rows, "language");
        if (port is null || log?.SelectedItem is not ComboBoxItem logItem || language?.SelectedItem is not ComboBoxItem languageItem) return;
        _snapshot.Settings.Port = (int)port.Value;
        _snapshot.Settings.LogLevel = logItem.Tag?.ToString() ?? "info";
        try
        {
            await _runtime.SaveSettingsAsync(_snapshot.Settings);
            await _runtime.RunCoreActionAsync("restart");
            var languagePreference = languageItem.Tag?.ToString() ?? UiText.SystemPreference;
            if (!string.Equals(languagePreference, UiText.ReadPreference(), StringComparison.Ordinal))
            {
                UiText.SetPreference(languagePreference);
                System.Diagnostics.Process.Start(new System.Diagnostics.ProcessStartInfo(Environment.ProcessPath!) { UseShellExecute = true });
                Environment.Exit(0);
            }
            await RefreshAsync();
            Render("runtime");
        }
        catch (Exception ex) { await ShowMessageAsync("保存失败", ex.Message); }
    }

    private async void StartupToggle_Toggled(object sender, RoutedEventArgs e)
    {
        if (_runtime is null || sender is not ToggleSwitch toggle || toggle.Tag is not string component) return;
        try
        {
            await _runtime.SetStartupAsync(component, toggle.IsOn);
            await RefreshAsync();
        }
        catch (Exception ex)
        {
            await ShowMessageAsync("启动设置失败", ex.Message);
            await RefreshAsync();
            Render("startup");
        }
    }

    private async void PrivilegeToggle_Toggled(object sender, RoutedEventArgs e)
    {
        if (_runtime is null || sender is not ToggleSwitch toggle) return;
        try
        {
            await _runtime.SetPrivilegeModeAsync(toggle.IsOn);
            await RefreshAsync();
        }
        catch (Exception ex)
        {
            await ShowMessageAsync("权限设置失败", ex.Message);
            await RefreshAsync();
            Render("permissions");
        }
    }

    private async void CheckUpdate_Click(object sender, RoutedEventArgs e)
    {
        if (_runtime is null || sender is not Button button) return;
        button.IsEnabled = false;
        try
        {
            var check = await _runtime.CheckForUpdatesAsync();
            if (!check.UpdateAvailable)
            {
                await ShowMessageAsync("AgentDock 更新", check.Message);
                return;
            }
            var dialog = new ContentDialog
            {
                XamlRoot = XamlRoot,
                Title = "发现新版本",
                Content = $"{check.CurrentVersion} → {check.LatestVersion}",
                PrimaryButtonText = "更新",
                CloseButtonText = "取消"
            };
            if (await dialog.ShowAsync() != ContentDialogResult.Primary) return;
            var progress = new Progress<UpdateProgress>(value => button.Content = string.IsNullOrWhiteSpace(value.Message) ? "正在更新…" : value.Message);
            var output = await _runtime.RunUpdateAsync(progress);
            await ShowMessageAsync("AgentDock 更新", LastLine(output, "更新完成"));
        }
        catch (Exception ex) { await ShowMessageAsync("更新失败", ex.Message); }
        finally { button.Content = "检查更新"; button.IsEnabled = true; }
    }

    private async Task ShowMessageAsync(string title, string message)
    {
        var dialog = new ContentDialog { XamlRoot = XamlRoot, Title = title, Content = message, CloseButtonText = "确定" };
        await dialog.ShowAsync();
    }

    private static T? FindTagged<T>(Panel panel, string tag) where T : FrameworkElement
    {
        foreach (var child in panel.Children)
        {
            if (child is Grid grid)
            {
                foreach (var item in grid.Children)
                {
                    if (item is T element && string.Equals(element.Tag?.ToString(), tag, StringComparison.Ordinal)) return element;
                }
            }
        }
        return null;
    }

    private static void SelectComboTag(ComboBox combo, string tag)
    {
        foreach (var item in combo.Items.OfType<ComboBoxItem>())
        {
            if (string.Equals(item.Tag?.ToString(), tag, StringComparison.OrdinalIgnoreCase))
            {
                combo.SelectedItem = item;
                return;
            }
        }
        combo.SelectedIndex = 0;
    }

    private static string LastLine(string value, string fallback) =>
        value.Split(['\r', '\n'], StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries).LastOrDefault() ?? fallback;

    private static string PageTitle(string tag) => tag switch
    {
        "permissions" => "Permissions", "startup" => "Startup", "credentials" => "Access Credentials", _ => "Runtime"
    };

    private static string PageDetail(string tag) => tag switch
    {
        "permissions" => "查看和调整 Windows Runtime 权限。",
        "startup" => "后台服务和托盘应用的启动行为。",
        "credentials" => "查看由 Windows DPAPI 保护的本机访问凭据。",
        _ => "本机 AgentDock Runtime 状态、配置与更新。"
    };

    private static string SectionTitle(string tag) => tag switch
    {
        "permissions" => "系统权限", "startup" => "Startup", "credentials" => "Credentials", _ => "AgentDock Runtime"
    };

    private static Grid Row(string left, string right)
    {
        var grid = new Grid { MinHeight = 46, Padding = new Thickness(13, 0, 13, 0) };
        grid.Children.Add(new TextBlock { Text = left, VerticalAlignment = VerticalAlignment.Center });
        grid.Children.Add(new TextBlock { Text = right, HorizontalAlignment = HorizontalAlignment.Right, VerticalAlignment = VerticalAlignment.Center, Opacity = 0.62 });
        return grid;
    }

    private static Grid ActionRow(string left, UIElement trailing)
    {
        var grid = new Grid { MinHeight = 46, Padding = new Thickness(13, 5, 13, 5) };
        grid.Children.Add(new TextBlock { Text = left, VerticalAlignment = VerticalAlignment.Center });
        if (trailing is FrameworkElement element)
        {
            element.HorizontalAlignment = HorizontalAlignment.Right;
            element.VerticalAlignment = VerticalAlignment.Center;
        }
        grid.Children.Add(trailing);
        return grid;
    }

    private static Rectangle Divider() => new()
    {
        Height = 1, Margin = new Thickness(13, 0, 0, 0),
        Fill = new Microsoft.UI.Xaml.Media.SolidColorBrush(Microsoft.UI.Colors.Gray), Opacity = 0.18
    };
}

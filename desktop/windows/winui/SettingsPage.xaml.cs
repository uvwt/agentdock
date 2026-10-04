using System.Diagnostics;
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

    public SettingsPage()
    {
        InitializeComponent();
        SettingsPageTitle.Text = UiText.Get("Settings");
        RuntimeNavigationItem.Content = UiText.Get("Runtime");
        PermissionsNavigationItem.Content = UiText.Get("Permissions");
        StartupNavigationItem.Content = UiText.Get("Startup");
        AppearanceNavigationItem.Content = UiText.Get("Appearance");
        AboutNavigationItem.Content = UiText.Get("About");
    }

    protected override async void OnNavigatedTo(NavigationEventArgs e)
    {
        _runtime = e.Parameter as RuntimeService;
        await RefreshAsync();
        if (SettingsNavigation.SelectedIndex < 0) SettingsNavigation.SelectedIndex = 0;
        else Render(_activeTag);
    }

    internal void SelectPage(string tag)
    {
        var item = SettingsNavigation.Items
            .OfType<ListViewItem>()
            .FirstOrDefault(candidate => string.Equals(candidate.Tag?.ToString(), tag, StringComparison.Ordinal));
        if (item is null) return;
        _activeTag = tag;
        if (!ReferenceEquals(SettingsNavigation.SelectedItem, item))
        {
            SettingsNavigation.SelectedItem = item;
            return;
        }
        Render(tag);
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

        if (tag == "about")
        {
            SettingsContent.Children.Add(BuildAbout());
            return;
        }

        var section = new SectionCard { Title = SectionTitle(tag) };
        section.SectionContent = tag switch
        {
            "permissions" => BuildPermissions(),
            "startup" => BuildStartup(),
            "appearance" => BuildAppearance(),
            _ => BuildRuntime()
        };
        SettingsContent.Children.Add(section);
    }

    private UIElement BuildRuntime()
    {
        var rows = new StackPanel();
        rows.Children.Add(Row(UiText.Get("Status"), _snapshot?.CoreRunning == true ? UiText.Get("Running") + " ●" : UiText.Get("Stopped")));
        rows.Children.Add(Divider());

        var action = new Button { Content = _snapshot?.CoreRunning == true ? UiText.Get("Stop") : UiText.Get("Start") };
        action.Click += RuntimeAction_Click;
        rows.Children.Add(ActionRow(UiText.Get("Service"), action));
        rows.Children.Add(Divider());

        var port = new NumberBox { Value = _snapshot?.Settings.Port ?? 8765, Minimum = 1, Maximum = 65535, Width = 130, Tag = "port" };
        rows.Children.Add(ActionRow(UiText.Get("Port"), port));
        rows.Children.Add(Divider());

        var log = new ComboBox { Width = 130, Tag = "log" };
        foreach (var value in new[] { "debug", "info", "warn", "error" }) log.Items.Add(new ComboBoxItem { Content = value, Tag = value });
        SelectComboTag(log, _snapshot?.Settings.LogLevel ?? "info");
        rows.Children.Add(ActionRow(UiText.Get("LogLevel"), log));
        rows.Children.Add(Divider());

        var save = new Button { Content = UiText.Get("SaveAndRestart") };
        save.Click += SaveRuntimeSettings_Click;
        rows.Children.Add(ActionRow(UiText.Get("RuntimeConfiguration"), save));
        return rows;
    }

    private UIElement BuildPermissions()
    {
        var rows = new StackPanel();
        rows.Children.Add(Row(UiText.Get("AgentDockPermissions"), UiText.Get("ManagedByWindows")));
        rows.Children.Add(Divider());
        rows.Children.Add(Row(UiText.Get("AdministratorMode"), string.Equals(_snapshot?.Manifest.PrivilegeMode, "elevated", StringComparison.OrdinalIgnoreCase) ? UiText.Get("Enabled") : UiText.Get("Disabled")));
        var toggle = new ToggleSwitch
        {
            IsOn = string.Equals(_snapshot?.Manifest.PrivilegeMode, "elevated", StringComparison.OrdinalIgnoreCase),
            Tag = "elevated"
        };
        toggle.Toggled += PrivilegeToggle_Toggled;
        rows.Children.Add(ActionRow(UiText.Get("RunCoreElevated"), toggle));
        return rows;
    }

    private UIElement BuildStartup()
    {
        var rows = new StackPanel();
        var core = new ToggleSwitch { IsOn = _snapshot?.CoreStartupEnabled == true, Tag = "core" };
        core.Toggled += StartupToggle_Toggled;
        rows.Children.Add(ActionRow(UiText.Get("CoreBackgroundService"), core));
        rows.Children.Add(Divider());
        var tray = new ToggleSwitch { IsOn = _snapshot?.TrayStartupEnabled == true, Tag = "tray" };
        tray.Toggled += StartupToggle_Toggled;
        rows.Children.Add(ActionRow(UiText.Get("TrayApp"), tray));
        return rows;
    }

    private UIElement BuildAppearance()
    {
        var rows = new StackPanel();

        var theme = new ComboBox { Width = 150, Tag = "theme" };
        theme.Items.Add(new ComboBoxItem { Content = UiText.Get("FollowSystem"), Tag = UiThemePreference.SystemPreference });
        theme.Items.Add(new ComboBoxItem { Content = UiText.Get("LightTheme"), Tag = UiThemePreference.LightPreference });
        theme.Items.Add(new ComboBoxItem { Content = UiText.Get("DarkTheme"), Tag = UiThemePreference.DarkPreference });
        SelectComboTag(theme, UiThemePreference.ReadPreference());
        theme.SelectionChanged += ThemePreference_SelectionChanged;
        rows.Children.Add(ActionRow(UiText.Get("Theme"), theme));
        rows.Children.Add(Divider());

        var language = new ComboBox { Width = 150, Tag = "language" };
        language.Items.Add(new ComboBoxItem { Content = UiText.Get("FollowSystem"), Tag = UiText.SystemPreference });
        language.Items.Add(new ComboBoxItem { Content = UiText.Get("SimplifiedChinese"), Tag = UiText.SimplifiedChinesePreference });
        language.Items.Add(new ComboBoxItem { Content = UiText.Get("EnglishLanguage"), Tag = UiText.EnglishPreference });
        SelectComboTag(language, UiText.ReadPreference());
        language.SelectionChanged += LanguagePreference_SelectionChanged;
        rows.Children.Add(ActionRow(UiText.Get("InterfaceLanguage"), language));
        return rows;
    }

    private UIElement BuildAbout()
    {
        var content = new StackPanel { Spacing = 20 };

        var product = new StackPanel { Spacing = 8 };
        product.Children.Add(new FontIcon
        {
            Glyph = "\uE946",
            FontSize = 30,
            HorizontalAlignment = HorizontalAlignment.Left
        });
        product.Children.Add(new TextBlock
        {
            Text = "AgentDock",
            FontSize = 22,
            FontWeight = Microsoft.UI.Text.FontWeights.SemiBold
        });
        product.Children.Add(new TextBlock
        {
            Text = UiText.Get("AboutDescription"),
            FontSize = 13,
            Opacity = 0.62,
            TextWrapping = TextWrapping.Wrap,
            MaxWidth = 620,
            HorizontalAlignment = HorizontalAlignment.Left
        });
        content.Children.Add(product);

        var application = new SectionCard { Title = UiText.Get("Application") };
        var applicationRows = new StackPanel();
        applicationRows.Children.Add(Row(
            UiText.Get("Version"),
            string.IsNullOrWhiteSpace(_snapshot?.Version) ? "—" : _snapshot.Version
        ));
        applicationRows.Children.Add(Divider());
        var update = new Button { Content = UiText.Get("CheckForUpdates") };
        update.Click += CheckUpdate_Click;
        applicationRows.Children.Add(ActionRow(UiText.Get("AgentDockUpdate"), update));
        application.SectionContent = applicationRows;
        content.Children.Add(application);

        var resources = new SectionCard { Title = UiText.Get("Resources") };
        var resourceRows = new StackPanel();
        var documentation = new Button { Content = UiText.Get("Open") };
        documentation.Click += (_, _) => OpenExternalUrl("https://uvwt.github.io/agentdock-docs/");
        resourceRows.Children.Add(ActionRow(UiText.Get("Documentation"), documentation));
        resourceRows.Children.Add(Divider());
        var repository = new Button { Content = UiText.Get("Open") };
        repository.Click += (_, _) => OpenExternalUrl("https://github.com/uvwt/agentdock");
        resourceRows.Children.Add(ActionRow("GitHub", repository));
        resources.SectionContent = resourceRows;
        content.Children.Add(resources);

        return content;
    }

    private void ThemePreference_SelectionChanged(object sender, SelectionChangedEventArgs e)
    {
        if (sender is not ComboBox combo || combo.SelectedItem is not ComboBoxItem item) return;
        var preference = item.Tag?.ToString() ?? UiThemePreference.SystemPreference;
        if (string.Equals(preference, UiThemePreference.ReadPreference(), StringComparison.Ordinal)) return;
        try
        {
            (Application.Current as NativeApp)?.ApplyThemePreference(preference);
        }
        catch (Exception ex)
        {
            _ = ShowMessageAsync(UiText.Get("Settings"), ex.Message);
        }
    }

    private void LanguagePreference_SelectionChanged(object sender, SelectionChangedEventArgs e)
    {
        if (sender is not ComboBox combo || combo.SelectedItem is not ComboBoxItem item) return;
        var preference = item.Tag?.ToString() ?? UiText.SystemPreference;
        if (string.Equals(preference, UiText.ReadPreference(), StringComparison.Ordinal)) return;
        try
        {
            (Application.Current as NativeApp)?.ApplyLanguagePreference(preference);
        }
        catch (Exception ex)
        {
            _ = ShowMessageAsync(UiText.Get("Settings"), UiText.Format("LanguageChangeFailed", ex.Message));
        }
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
        if (port is null || log?.SelectedItem is not ComboBoxItem logItem) return;
        _snapshot.Settings.Port = (int)port.Value;
        _snapshot.Settings.LogLevel = logItem.Tag?.ToString() ?? "info";
        try
        {
            await _runtime.SaveSettingsAsync(_snapshot.Settings);
            await _runtime.RunCoreActionAsync("restart");
            await RefreshAsync();
            Render("runtime");
        }
        catch (Exception ex) { await ShowMessageAsync(UiText.Get("SaveFailed"), ex.Message); }
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
            await ShowMessageAsync(UiText.Get("StartupSettingsFailed"), ex.Message);
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
            await ShowMessageAsync(UiText.Get("PermissionSettingsFailed"), ex.Message);
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
                await ShowMessageAsync(UiText.Get("AgentDockUpdate"), check.Message);
                return;
            }
            var dialog = new ContentDialog
            {
                XamlRoot = XamlRoot,
                Title = UiText.Get("NewVersionAvailable"),
                Content = $"{check.CurrentVersion} → {check.LatestVersion}",
                PrimaryButtonText = UiText.Get("Update"),
                CloseButtonText = UiText.Get("Cancel")
            };
            if (await dialog.ShowAsync() != ContentDialogResult.Primary) return;
            var progress = new Progress<UpdateProgress>(value => button.Content = string.IsNullOrWhiteSpace(value.Message) ? UiText.Get("UpdatingAgentDock") : value.Message);
            var output = await _runtime.RunUpdateAsync(progress);
            await ShowMessageAsync(UiText.Get("AgentDockUpdate"), LastLine(output, UiText.Get("UpdateCompleted")));
        }
        catch (Exception ex) { await ShowMessageAsync(UiText.Get("UpdateFailed"), ex.Message); }
        finally { button.Content = UiText.Get("CheckForUpdates"); button.IsEnabled = true; }
    }

    private async Task ShowMessageAsync(string title, string message)
    {
        var dialog = new ContentDialog { XamlRoot = XamlRoot, Title = title, Content = message, CloseButtonText = UiText.Get("Confirm") };
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
        "permissions" => UiText.Get("Permissions"),
        "startup" => UiText.Get("Startup"),
        "appearance" => UiText.Get("Appearance"),
        "about" => UiText.Get("About"),
        _ => UiText.Get("Runtime")
    };

    private static string PageDetail(string tag) => tag switch
    {
        "permissions" => UiText.Get("PermissionsDetail"),
        "startup" => UiText.Get("StartupDetail"),
        "appearance" => UiText.Get("AppearanceDetail"),
        "about" => UiText.Get("AboutDetail"),
        _ => UiText.Get("RuntimeDetail")
    };

    private static string SectionTitle(string tag) => tag switch
    {
        "permissions" => UiText.Get("RuntimePermissions"),
        "startup" => UiText.Get("Startup"),
        "appearance" => UiText.Get("Appearance"),
        _ => "AgentDock Runtime"
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

    private static void OpenExternalUrl(string url) =>
        Process.Start(new ProcessStartInfo(url) { UseShellExecute = true });
}

using System.Diagnostics;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Navigation;
using Microsoft.UI.Xaml.Shapes;
using Windows.ApplicationModel.DataTransfer;

namespace AgentDock.ControlPanel;

public sealed partial class SettingsPage : Page
{
    private RuntimeService? _runtime;
    private RuntimeSnapshot? _snapshot;
    private string _activeTag = "runtime";
    private string _advancedStatus = "";

    public SettingsPage()
    {
        InitializeComponent();
        SettingsPageTitle.Text = UiText.Get("Settings");
        RuntimeNavigationItem.Content = UiText.Get("Runtime");
        PermissionsNavigationItem.Content = UiText.Get("Permissions");
        StartupNavigationItem.Content = UiText.Get("Startup");

        AdvancedConnectionNavigationItem.Content = UiText.Get("AdvancedConnection");

        AppearanceNavigationItem.Content = UiText.Get("Appearance");
        AboutNavigationItem.Content = UiText.Get("About");

    }

    protected override async void OnNavigatedTo(NavigationEventArgs e)
    {
        if (e.Parameter is SettingsNavigationRequest request)
        {
            _runtime = request.Runtime;
            _activeTag = request.Tag;
        }
        else
        {
            _runtime = e.Parameter as RuntimeService;
            _activeTag = "runtime";
        }
        await RefreshAsync();
        SelectSettingsTag(_activeTag);
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

    private void SelectSettingsTag(string tag)
    {
        var item = SettingsNavigation.Items
            .OfType<ListViewItem>()
            .FirstOrDefault(candidate => string.Equals(candidate.Tag?.ToString(), tag, StringComparison.Ordinal));
        SettingsNavigation.SelectedItem = item ?? RuntimeNavigationItem;
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


        if (tag == "advancedConnection")
        {
            SettingsContent.Children.Add(BuildAdvancedConnection());
            return;
        }

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


    private UIElement BuildAdvancedConnection()
    {
        var content = new StackPanel { Spacing = 20 };

        var localRows = new StackPanel();
        var copyLocal = new Button { Content = UiText.Get("Copy") };
        copyLocal.Click += (_, _) => CopyText(_snapshot?.LocalMcpUrl ?? "");
        localRows.Children.Add(DetailActionRow(
            UiText.Get("LocalAddress"),
            _snapshot?.LocalMcpUrl ?? "—",
            copyLocal
        ));
        content.Children.Add(new SectionCard
        {
            Title = "Local MCP",
            SectionContent = localRows
        });

        var credentialRows = new StackPanel();
        credentialRows.Children.Add(CredentialActionRow(UiText.Get("AuthenticationToken"), "bearer"));
        credentialRows.Children.Add(Divider());
        credentialRows.Children.Add(CredentialActionRow(UiText.Get("OAuthPassword"), "oauth"));
        content.Children.Add(new SectionCard
        {
            Title = UiText.Get("AccessCredentials"),
            SectionContent = credentialRows
        });

        var publicRows = new StackPanel();
        var publicAddress = _snapshot?.PublicMcpUrl ?? "";
        var copyPublic = new Button
        {
            Content = UiText.Get("Copy"),
            IsEnabled = !string.IsNullOrWhiteSpace(publicAddress)
        };
        copyPublic.Click += (_, _) => CopyText(publicAddress);
        publicRows.Children.Add(DetailActionRow(
            UiText.Get("PublicAddress"),
            string.IsNullOrWhiteSpace(publicAddress) ? UiText.Get("Disabled") : publicAddress,
            copyPublic
        ));
        publicRows.Children.Add(Divider());

        var temporary = new Button
        {
            Content = string.Equals(_snapshot?.TunnelMode, "quick", StringComparison.OrdinalIgnoreCase)
                ? UiText.Get("RegenerateTemporaryAddress")
                : UiText.Get("GenerateTemporaryAddress")
        };
        temporary.Click += TemporaryTunnelButton_Click;
        publicRows.Children.Add(DetailActionRow(
            "Cloudflare Tunnel",
            UiText.Get("TemporaryTunnelDetail"),
            temporary
        ));
        publicRows.Children.Add(Divider());

        var fixedPanel = new StackPanel { Padding = new Thickness(13, 12, 13, 12), Spacing = 9 };
        fixedPanel.Children.Add(new TextBlock
        {
            Text = UiText.Get("FixedDomain"),
            FontSize = 13,
            FontWeight = Microsoft.UI.Text.FontWeights.SemiBold
        });
        fixedPanel.Children.Add(new TextBlock
        {
            Text = UiText.Get("FixedDomainDetail"),
            FontSize = 11.5,
            Opacity = 0.62,
            TextWrapping = TextWrapping.Wrap
        });
        var serverUrl = new TextBox
        {
            Header = UiText.Get("HttpsAddress"),
            PlaceholderText = "https://mcp.example.com",
            Text = string.Equals(_snapshot?.TunnelMode, "named", StringComparison.OrdinalIgnoreCase)
                ? _snapshot?.SavedNamedOrigin ?? ""
                : "",
            Tag = "fixed-domain-url"
        };
        fixedPanel.Children.Add(serverUrl);
        var token = new PasswordBox
        {
            Header = "Cloudflare Tunnel Token",
            PlaceholderText = _snapshot?.TunnelTokenStored == true
                ? UiText.Get("TunnelTokenSavedPlaceholder")
                : "Cloudflare Tunnel Token",
            Tag = "fixed-domain-token"
        };
        fixedPanel.Children.Add(token);
        var apply = new Button
        {
            Content = UiText.Get("ApplyFixedDomain"),
            Tag = fixedPanel
        };
        apply.Click += ApplyFixedDomainButton_Click;
        fixedPanel.Children.Add(apply);
        publicRows.Children.Add(fixedPanel);
        content.Children.Add(new SectionCard
        {
            Title = UiText.Get("PublicAccess"),
            SectionContent = publicRows
        });

        if (!string.IsNullOrWhiteSpace(_advancedStatus))
        {
            content.Children.Add(new TextBlock
            {
                Text = _advancedStatus,
                FontSize = 11.5,
                Opacity = 0.62,
                TextWrapping = TextWrapping.Wrap
            });
        }
        return content;
    }

    private Grid CredentialActionRow(string title, string kind)
    {
        var value = new PasswordBox
        {
            Password = ReadCredential(kind),
            IsPasswordRevealButtonEnabled = true,
            Width = 300
        };
        var copy = new Button { Content = UiText.Get("Copy") };
        copy.Click += (_, _) => CopyText(value.Password);
        var actions = new StackPanel { Orientation = Orientation.Horizontal, Spacing = 8 };
        actions.Children.Add(value);
        actions.Children.Add(copy);
        return ActionRow(title, actions);
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

    private async void TemporaryTunnelButton_Click(object sender, RoutedEventArgs e)
    {
        if (_runtime is null || sender is not Button button) return;
        button.IsEnabled = false;
        button.Content = UiText.Get("Generating");
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
            _advancedStatus = UiText.Get("Generated");
        }
        catch (Exception ex)
        {
            _advancedStatus = ex.Message;
        }
        Render("advancedConnection");
    }

    private async void ApplyFixedDomainButton_Click(object sender, RoutedEventArgs e)
    {
        if (_runtime is null || sender is not Button button || button.Tag is not Panel panel) return;
        var serverUrl = FindTagged<TextBox>(panel, "fixed-domain-url")?.Text.Trim() ?? "";
        var token = FindTagged<PasswordBox>(panel, "fixed-domain-token")?.Password ?? "";
        if (string.IsNullOrWhiteSpace(serverUrl)) return;

        button.IsEnabled = false;
        button.Content = UiText.Get("Applying");
        try
        {
            await _runtime.SetTunnelModeAsync("named", serverUrl, token);
            await RefreshAsync();
            _advancedStatus = UiText.Get("Applied");
        }
        catch (Exception ex)
        {
            _advancedStatus = ex.Message;
        }
        Render("advancedConnection");
    }

    private static void CopyText(string text)
    {
        if (string.IsNullOrWhiteSpace(text)) return;
        var package = new DataPackage();
        package.SetText(text);
        Clipboard.SetContent(package);
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
            if (child is T element && string.Equals(element.Tag?.ToString(), tag, StringComparison.Ordinal))
            {
                return element;
            }
            if (child is Panel nested)
            {
                var found = FindTagged<T>(nested, tag);
                if (found is not null) return found;
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

        "advancedConnection" => UiText.Get("AdvancedConnection"),

        "appearance" => UiText.Get("Appearance"),
        "about" => UiText.Get("About"),

        _ => UiText.Get("Runtime")
    };

    private static string PageDetail(string tag) => tag switch
    {
        "permissions" => UiText.Get("PermissionsDetail"),
        "startup" => UiText.Get("StartupDetail"),

        "advancedConnection" => UiText.Get("AdvancedConnectionDetail"),

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

    private static Grid DetailActionRow(string title, string detail, UIElement trailing)
    {
        var grid = new Grid { MinHeight = 56, Padding = new Thickness(13, 6, 13, 6), ColumnSpacing = 16 };
        grid.ColumnDefinitions.Add(new ColumnDefinition());
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });

        var labels = new StackPanel { Spacing = 2, VerticalAlignment = VerticalAlignment.Center };
        labels.Children.Add(new TextBlock { Text = title, FontSize = 13 });
        labels.Children.Add(new TextBlock
        {
            Text = detail,
            FontSize = 11.5,
            Opacity = 0.62,
            TextTrimming = TextTrimming.CharacterEllipsis
        });
        grid.Children.Add(labels);

        if (trailing is FrameworkElement element)
        {
            element.HorizontalAlignment = HorizontalAlignment.Right;
            element.VerticalAlignment = VerticalAlignment.Center;
            Grid.SetColumn(element, 1);
        }
        grid.Children.Add(trailing);
        return grid;
    }

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

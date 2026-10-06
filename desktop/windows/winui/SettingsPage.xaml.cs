using System.Diagnostics;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media.Imaging;
using Microsoft.UI.Xaml.Navigation;
using Microsoft.UI.Xaml.Shapes;
using Windows.ApplicationModel.DataTransfer;

namespace AgentDock.ControlPanel;

public sealed partial class SettingsPage : Page
{
    private RuntimeService? _runtime;
    private RuntimeSnapshot? _snapshot;
    private RuntimeAnalyticsPayload? _analytics;
    private string _activeTag = "appearance";
    private string _advancedStatus = "";
    private PublicEndpointCheckResult? _publicEndpointCheckResult;
    private string _publicEndpointCheckUrl = "";
    private bool _publicEndpointCheckInProgress;
    private bool _componentOperationInProgress;
    private bool _updateCheckInProgress;
    private Button? _updateButton;

    public SettingsPage()
    {
        InitializeComponent();
        SettingsPageTitle.Text = UiText.Get("Settings");
        PermissionsNavigationItem.Content = UiText.Get("Permissions");
        StartupNavigationItem.Content = UiText.Get("Startup");
        LogsNavigationItem.Content = UiText.Get("Logs");

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
            _activeTag = "appearance";
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
        if (_runtime is null) return;
        _snapshot = await _runtime.GetSnapshotAsync(includeNexusConnection: false);
        _analytics = _snapshot is null ? null : await _runtime.GetRuntimeAnalyticsAsync(_snapshot);
    }

    private void SelectSettingsTag(string tag)
    {
        var item = SettingsNavigation.Items
            .OfType<ListViewItem>()
            .FirstOrDefault(candidate => string.Equals(candidate.Tag?.ToString(), tag, StringComparison.Ordinal));
        var target = item ?? AppearanceNavigationItem;
        _activeTag = target.Tag?.ToString() ?? "appearance";
        SettingsNavigation.SelectedItem = target;
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
        _updateButton = null;
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
            "logs" => BuildLogs(),
            "appearance" => BuildAppearance(),
            _ => BuildPermissions()
        };
        SettingsContent.Children.Add(section);
    }

    private UIElement BuildLogs()
    {
        var rows = new StackPanel
        {
            HorizontalAlignment = HorizontalAlignment.Stretch
        };

        var log = new ComboBox { Width = 130, Tag = "log" };
        foreach (var value in new[] { "debug", "info", "warn", "error" })
        {
            log.Items.Add(new ComboBoxItem { Content = LogLevelLabel(value), Tag = value });
        }
        SelectComboTag(log, _snapshot?.Settings.LogLevel ?? "info");
        log.SelectionChanged += LogLevel_SelectionChanged;
        rows.Children.Add(ActionRow(UiText.Get("LogLevel"), log));
        rows.Children.Add(Divider());

        var openLogs = new Button { Content = UiText.Get("Open") };
        openLogs.Click += (_, _) => _runtime?.OpenLogsDirectory();
        rows.Children.Add(ActionRow(UiText.Get("LogsDirectory"), openLogs));
        rows.Children.Add(Divider());

        var openConfig = new Button { Content = UiText.Get("Open") };
        openConfig.Click += (_, _) => _runtime?.OpenConfigDirectory();
        rows.Children.Add(ActionRow(UiText.Get("ConfigurationDirectory"), openConfig));
        rows.Children.Add(Divider());

        var diagnosticsRoot = new StackPanel
        {
            HorizontalAlignment = HorizontalAlignment.Stretch
        };
        var diagnosticsContent = BuildAdvancedDiagnostics();
        diagnosticsContent.Visibility = Visibility.Collapsed;

        var diagnosticsHeader = new Grid { ColumnSpacing = 10 };
        diagnosticsHeader.ColumnDefinitions.Add(new ColumnDefinition());
        diagnosticsHeader.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        diagnosticsHeader.Children.Add(new TextBlock
        {
            Text = UiText.Get("AdvancedDiagnostics"),
            FontSize = 13,
            FontWeight = Microsoft.UI.Text.FontWeights.Medium,
            VerticalAlignment = VerticalAlignment.Center
        });
        var diagnosticsChevron = new TextBlock
        {
            Text = "›",
            FontSize = 15,
            Foreground = (Microsoft.UI.Xaml.Media.Brush)Application.Current.Resources["TextFillColorSecondaryBrush"],
            VerticalAlignment = VerticalAlignment.Center
        };
        Grid.SetColumn(diagnosticsChevron, 1);
        diagnosticsHeader.Children.Add(diagnosticsChevron);

        var diagnosticsButton = new Button
        {
            Content = diagnosticsHeader,
            HorizontalAlignment = HorizontalAlignment.Stretch,
            HorizontalContentAlignment = HorizontalAlignment.Stretch,
            Padding = new Thickness(13, 10, 13, 10),
            BorderThickness = new Thickness(0),
            Background = new Microsoft.UI.Xaml.Media.SolidColorBrush(Microsoft.UI.Colors.Transparent)
        };
        diagnosticsButton.Click += (_, _) =>
        {
            var expanding = diagnosticsContent.Visibility != Visibility.Visible;
            diagnosticsContent.Visibility = expanding ? Visibility.Visible : Visibility.Collapsed;
            diagnosticsChevron.Text = expanding ? "⌄" : "›";
        };

        diagnosticsRoot.Children.Add(diagnosticsButton);
        diagnosticsRoot.Children.Add(diagnosticsContent);
        rows.Children.Add(diagnosticsRoot);

        return rows;
    }

    private FrameworkElement BuildAdvancedDiagnostics()
    {
        var rows = new StackPanel
        {
            HorizontalAlignment = HorizontalAlignment.Stretch
        };
        rows.Children.Add(Row(
            UiText.Get("Runtime"),
            _snapshot?.CoreRunning == true ? UiText.Get("Running") : UiText.Get("Stopped")));
        rows.Children.Add(Divider());
        rows.Children.Add(Row(
            UiText.Get("Version"),
            string.IsNullOrWhiteSpace(_snapshot?.Version) ? "—" : _snapshot.Version));

        if (_analytics is null)
        {
            rows.Children.Add(Divider());
            rows.Children.Add(new TextBlock
            {
                Text = UiText.Get("AnalyticsUnavailable"),
                FontSize = 12.5,
                Opacity = 0.62,
                Margin = new Thickness(13, 12, 13, 12)
            });
            return rows;
        }

        rows.Children.Add(Divider());
        rows.Children.Add(Row(UiText.Get("TotalCalls"), _analytics.TotalCalls.ToString()));
        rows.Children.Add(Divider());
        rows.Children.Add(Row(UiText.Get("FailedCalls"), _analytics.TotalErrors.ToString()));
        rows.Children.Add(Divider());
        rows.Children.Add(Row(UiText.Get("RecentP95Latency"), FormatDuration(RecentP95(_analytics))));
        rows.Children.Add(Divider());
        rows.Children.Add(Row(UiText.Get("GoHeap"), FormatBytes(_analytics.Process.HeapAllocBytes)));
        rows.Children.Add(Divider());
        rows.Children.Add(Row(UiText.Get("Goroutines"), _analytics.Process.Goroutines.ToString()));
        rows.Children.Add(Divider());
        rows.Children.Add(Row(UiText.Get("GcCycles"), _analytics.Process.GcCycles.ToString()));
        rows.Children.Add(Divider());
        rows.Children.Add(Row(UiText.Get("Uptime"), FormatUptime(_analytics.Process.UptimeMs)));
        rows.Children.Add(Divider());

        var copy = new Button { Content = UiText.Get("CopyDiagnostics") };
        copy.Click += (_, _) => CopyDiagnostics(_analytics);
        rows.Children.Add(ActionRow(UiText.Get("DiagnosticInformation"), copy));

        return rows;
    }

    private UIElement BuildPermissions()
    {
        var rows = new StackPanel();
        var toggle = new ToggleSwitch
        {
            IsOn = string.Equals(_snapshot?.Manifest.PrivilegeMode, "elevated", StringComparison.OrdinalIgnoreCase),
            Tag = "elevated",
            MinWidth = 0,
            OnContent = "",
            OffContent = ""
        };
        toggle.Toggled += PrivilegeToggle_Toggled;
        rows.Children.Add(ActionRow(UiText.Get("RunCoreElevated"), toggle));
        return rows;
    }

    private UIElement BuildStartup()
    {
        var rows = new StackPanel();
        var core = new ToggleSwitch
        {
            IsOn = _snapshot?.CoreStartupEnabled == true,
            Tag = "core",
            MinWidth = 0,
            OnContent = "",
            OffContent = ""
        };
        core.Toggled += StartupToggle_Toggled;
        rows.Children.Add(ActionRow(UiText.Get("CoreBackgroundService"), core));
        rows.Children.Add(Divider());
        var tray = new ToggleSwitch
        {
            IsOn = _snapshot?.TrayStartupEnabled == true,
            Tag = "tray",
            MinWidth = 0,
            OnContent = "",
            OffContent = ""
        };
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
        product.Children.Add(new Image
        {
            Source = LoadProductLogo(),
            Width = 40,
            Height = 40,
            Stretch = Microsoft.UI.Xaml.Media.Stretch.Uniform,
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
        var update = new Button
        {
            Content = _updateCheckInProgress ? UiText.Get("CheckingForUpdates") : UiText.Get("CheckForUpdates"),
            IsEnabled = !_updateCheckInProgress
        };
        _updateButton = update;
        update.Click += CheckUpdate_Click;
        applicationRows.Children.Add(ActionRow(UiText.Get("AgentDockUpdate"), update));
        application.SectionContent = applicationRows;
        content.Children.Add(application);

        var resources = new SectionCard { Title = UiText.Get("Resources") };
        var resourceRows = new StackPanel();
        var website = new Button { Content = UiText.Get("Open") };
        website.Click += (_, _) => OpenExternalUrl("https://nexusdock.co/");
        resourceRows.Children.Add(ActionRow(UiText.Get("Website"), website));
        resourceRows.Children.Add(Divider());
        var documentation = new Button { Content = UiText.Get("Open") };
        documentation.Click += (_, _) => OpenExternalUrl("https://docs.nexusdock.co/agentdock/");
        resourceRows.Children.Add(ActionRow(UiText.Get("Documentation"), documentation));
        resourceRows.Children.Add(Divider());
        var repository = new Button { Content = UiText.Get("Open") };
        repository.Click += (_, _) => OpenExternalUrl("https://github.com/uvwt/agentdock");
        resourceRows.Children.Add(ActionRow("GitHub", repository));
        resources.SectionContent = resourceRows;
        content.Children.Add(resources);

        return content;
    }

    private static BitmapImage? LoadProductLogo()
    {
        var path = System.IO.Path.Combine(AppContext.BaseDirectory, "Assets", "agentdock.png");
        return File.Exists(path) ? new BitmapImage(new System.Uri(path, System.UriKind.Absolute)) : null;
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
        localRows.Children.Add(Divider());

        var customPortContent = new StackPanel
        {
            Visibility = Visibility.Collapsed,
            HorizontalAlignment = HorizontalAlignment.Stretch
        };
        var port = new NumberBox
        {
            Value = _snapshot?.Settings.Port ?? 8765,
            Minimum = 1,
            Maximum = 65535,
            Width = 130,
            Tag = "port"
        };
        customPortContent.Children.Add(ActionRow(UiText.Get("ServicePort"), port));
        customPortContent.Children.Add(Divider());
        var applyPort = new Button
        {
            Content = UiText.Get("ApplyChanges"),
            IsEnabled = false,
            Tag = customPortContent
        };
        port.ValueChanged += (_, _) =>
        {
            applyPort.IsEnabled = !double.IsNaN(port.Value) &&
                                  (int)port.Value != (_snapshot?.Settings.Port ?? 8765);
        };
        applyPort.Click += SavePortSettings_Click;
        customPortContent.Children.Add(DetailActionRow(
            UiText.Get("PortRestartHint"),
            UiText.Get("PortRestartDetail"),
            applyPort
        ));

        var customPortHeader = new Grid { ColumnSpacing = 10 };
        customPortHeader.ColumnDefinitions.Add(new ColumnDefinition());
        customPortHeader.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        customPortHeader.Children.Add(new TextBlock
        {
            Text = UiText.Get("CustomPort"),
            FontSize = 13,
            FontWeight = Microsoft.UI.Text.FontWeights.Medium,
            VerticalAlignment = VerticalAlignment.Center
        });
        var customPortChevron = new TextBlock
        {
            Text = "›",
            FontSize = 15,
            Foreground = (Microsoft.UI.Xaml.Media.Brush)Application.Current.Resources["TextFillColorSecondaryBrush"],
            VerticalAlignment = VerticalAlignment.Center
        };
        Grid.SetColumn(customPortChevron, 1);
        customPortHeader.Children.Add(customPortChevron);
        var customPortButton = new Button
        {
            Content = customPortHeader,
            HorizontalAlignment = HorizontalAlignment.Stretch,
            HorizontalContentAlignment = HorizontalAlignment.Stretch,
            Padding = new Thickness(13, 10, 13, 10),
            BorderThickness = new Thickness(0),
            Background = new Microsoft.UI.Xaml.Media.SolidColorBrush(Microsoft.UI.Colors.Transparent)
        };
        customPortButton.Click += (_, _) =>
        {
            var expanding = customPortContent.Visibility != Visibility.Visible;
            customPortContent.Visibility = expanding ? Visibility.Visible : Visibility.Collapsed;
            customPortChevron.Text = expanding ? "⌄" : "›";
        };
        localRows.Children.Add(customPortButton);
        localRows.Children.Add(customPortContent);
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

        var componentState = (_snapshot?.CloudflaredComponentState ?? "not_installed")
            .Trim()
            .ToLowerInvariant();
        content.Children.Add(BuildCloudflaredComponentSection(componentState));
        if (!string.Equals(componentState, "ready", StringComparison.Ordinal))
        {
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

        var publicRows = new StackPanel();
        var publicAddress = _snapshot?.PublicMcpUrl ?? "";
        var publicActions = new StackPanel
        {
            Orientation = Orientation.Horizontal,
            Spacing = 8,
            VerticalAlignment = VerticalAlignment.Center
        };

        if (!_publicEndpointCheckInProgress &&
            string.Equals(_publicEndpointCheckUrl, publicAddress, StringComparison.Ordinal) &&
            _publicEndpointCheckResult is { } endpointResult)
        {
            var endpointStatusColor = endpointResult.IsReachable
                ? Microsoft.UI.Colors.Green
                : endpointResult.HttpStatusCode is >= 400 and < 500
                    ? Microsoft.UI.Colors.DarkOrange
                    : Microsoft.UI.Colors.Red;

            publicActions.Children.Add(new TextBlock
            {
                Text = endpointResult.IsReachable && endpointResult.LatencyMilliseconds is { } latency
                    ? $"{latency} ms"
                    : endpointResult.HttpStatusCode is { } statusCode
                        ? endpointResult.LatencyMilliseconds is { } failedLatency
                            ? $"HTTP {statusCode} · {UiText.Get("Failed")} · {failedLatency} ms"
                            : $"HTTP {statusCode} · {UiText.Get("Failed")}"
                        : UiText.Get("Failed"),
                FontSize = 11.5,
                FontWeight = Microsoft.UI.Text.FontWeights.SemiBold,
                Foreground = new Microsoft.UI.Xaml.Media.SolidColorBrush(endpointStatusColor),
                VerticalAlignment = VerticalAlignment.Center
            });
        }

        var testPublic = new Button
        {
            Content = _publicEndpointCheckInProgress ? UiText.Get("Testing") : UiText.Get("Test"),
            IsEnabled = !string.IsNullOrWhiteSpace(publicAddress) && !_publicEndpointCheckInProgress,
            Tag = publicAddress
        };
        testPublic.Click += TestPublicAddress_Click;
        publicActions.Children.Add(testPublic);

        var copyPublic = new Button
        {
            Content = UiText.Get("Copy"),
            IsEnabled = !string.IsNullOrWhiteSpace(publicAddress)
        };
        copyPublic.Click += (_, _) => CopyText(publicAddress);
        publicActions.Children.Add(copyPublic);

        publicRows.Children.Add(DetailActionRow(
            UiText.Get("PublicAddress"),
            string.IsNullOrWhiteSpace(publicAddress) ? UiText.Get("Disabled") : publicAddress,
            publicActions
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
            UiText.Get("TemporaryDomain"),
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

    private SectionCard BuildCloudflaredComponentSection(string state)
    {
        var rows = new StackPanel();
        var actions = new StackPanel
        {
            Orientation = Orientation.Horizontal,
            Spacing = 8,
            VerticalAlignment = VerticalAlignment.Center
        };

        string stateText;
        string detail;
        switch (state)
        {
            case "ready":
                stateText = string.IsNullOrWhiteSpace(_snapshot?.CloudflaredComponentVersion)
                    ? UiText.Get("ComponentReady")
                    : UiText.Format("ComponentReadyVersion", _snapshot!.CloudflaredComponentVersion);
                detail = UiText.Get("CloudflaredComponentPurpose");
                actions.Children.Add(ComponentActionButton(UiText.Get("Update"), "update"));
                actions.Children.Add(ComponentActionButton(UiText.Get("Uninstall"), "uninstall"));
                break;
            case "broken":
                stateText = UiText.Get("ComponentNeedsRepair");
                detail = UiText.Get("CloudflaredComponentPurpose");
                actions.Children.Add(ComponentActionButton(UiText.Get("ComponentRepairAction"), "repair"));
                break;
            default:
                stateText = UiText.Get("ComponentNotInstalled");
                detail = UiText.Get("CloudflaredComponentPurpose");
                actions.Children.Add(ComponentActionButton(UiText.Get("Install"), "install"));
                break;
        }

        rows.Children.Add(DetailActionRow(
            UiText.Get("CloudflareTunnel"),
            stateText,
            actions
        ));
        rows.Children.Add(Divider());
        rows.Children.Add(new TextBlock
        {
            Text = detail,
            FontSize = 11.5,
            Opacity = 0.62,
            TextWrapping = TextWrapping.Wrap,
            Padding = new Thickness(13, 10, 13, 12)
        });

        return new SectionCard
        {
            Title = UiText.Get("CloudflareTunnelComponent"),
            SectionContent = rows
        };
    }

    private Button ComponentActionButton(string title, string action)
    {
        var button = new Button
        {
            Content = title,
            Tag = action,
            IsEnabled = !_componentOperationInProgress
        };
        button.Click += CloudflaredComponentAction_Click;
        return button;
    }

    private async void CloudflaredComponentAction_Click(object sender, RoutedEventArgs e)
    {
        if (_runtime is null ||
            sender is not Button button ||
            button.Tag is not string action ||
            _componentOperationInProgress)
        {
            return;
        }

        _componentOperationInProgress = true;
        _advancedStatus = UiText.Get(action switch
        {
            "update" => "UpdatingComponent",
            "uninstall" => "UninstallingComponent",
            "repair" => "RepairingComponent",
            _ => "InstallingComponent"
        });
        Render("advancedConnection");

        try
        {
            var progress = new Progress<ComponentProgress>(componentProgress =>
            {
                _advancedStatus = ComponentProgressText(componentProgress);
                Render("advancedConnection");
            });
            switch (action)
            {
                case "update":
                    await _runtime.UpdateCloudflaredComponentAsync(progress);
                    break;
                case "uninstall":
                    await _runtime.UninstallCloudflaredComponentAsync();
                    break;
                case "repair":
                case "install":
                    await _runtime.InstallCloudflaredComponentAsync(progress);
                    break;
                default:
                    return;
            }
            await RefreshAsync();
            _advancedStatus = UiText.Get(action == "uninstall"
                ? "ComponentUninstalled"
                : action == "update"
                    ? "ComponentUpdated"
                    : action == "repair"
                        ? "ComponentRepaired"
                        : "ComponentInstalled");
        }
        catch (Exception ex)
        {
            Debug.WriteLine($"cloudflared component {action} failed: {ex}");
            _advancedStatus = UiText.Get("ComponentOperationFailed");
        }
        finally
        {
            _componentOperationInProgress = false;
            Render("advancedConnection");
        }
    }

    private static string ComponentProgressText(ComponentProgress progress) =>
        progress.Stage switch
        {
            "catalog" => UiText.Get("ComponentChecking"),
            "download" or "downloading" => progress.Total > 0
                ? UiText.Format("ComponentDownloadingProgress", progress.Bytes * 100 / Math.Max(1, progress.Total))
                : UiText.Get("ComponentDownloading"),
            "verify" or "verifying" => UiText.Get("ComponentVerifying"),
            "install" => UiText.Get("InstallingComponent"),
            "ready" => UiText.Get("ComponentReady"),
            _ => UiText.Get("ComponentWorking")
        };

    private Grid CredentialActionRow(string title, string kind)
    {
        var value = ReadCredential(kind);
        var copy = new Button { Content = UiText.Get("Copy"), IsEnabled = !string.IsNullOrWhiteSpace(value) };
        copy.Click += (_, _) => CopyText(value);
        return DetailActionRow(
            title,
            string.IsNullOrWhiteSpace(value) ? "—" : "••••••••••••",
            copy
        );
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

    private async void TestPublicAddress_Click(object sender, RoutedEventArgs e)
    {
        if (_runtime is null ||
            sender is not Button button ||
            button.Tag is not string publicAddress ||
            string.IsNullOrWhiteSpace(publicAddress) ||
            _publicEndpointCheckInProgress)
        {
            return;
        }

        _publicEndpointCheckInProgress = true;
        _publicEndpointCheckUrl = publicAddress;
        _publicEndpointCheckResult = null;
        Render("advancedConnection");

        try
        {
            var result = await _runtime.CheckPublicEndpointAsync(publicAddress);
            if (string.Equals(_snapshot?.PublicMcpUrl, publicAddress, StringComparison.Ordinal))
            {
                _publicEndpointCheckResult = result;
            }
        }
        catch (Exception ex)
        {
            if (string.Equals(_snapshot?.PublicMcpUrl, publicAddress, StringComparison.Ordinal))
            {
                _publicEndpointCheckResult = new PublicEndpointCheckResult(
                    false,
                    UiText.Format("AccessFailed", ex.Message),
                    null);
            }
        }
        finally
        {
            _publicEndpointCheckInProgress = false;
            Render("advancedConnection");
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

    private async void SavePortSettings_Click(object sender, RoutedEventArgs e)
    {
        if (_runtime is null || _snapshot is null ||
            sender is not Button button ||
            button.Tag is not Panel panel)
        {
            return;
        }

        var port = FindTagged<NumberBox>(panel, "port");
        if (port is null || double.IsNaN(port.Value)) return;

        _snapshot.Settings.Port = (int)port.Value;
        try
        {
            await _runtime.SaveSettingsAsync(_snapshot.Settings);
            await _runtime.RunCoreActionAsync("restart");
            await RefreshAsync();
            Render("advancedConnection");
        }
        catch (Exception ex)
        {
            await ShowMessageAsync(UiText.Get("SaveFailed"), ex.Message);
            await RefreshAsync();
            Render("advancedConnection");
        }
    }

    private async void LogLevel_SelectionChanged(object sender, SelectionChangedEventArgs e)
    {
        if (_runtime is null || _snapshot is null || sender is not ComboBox log ||
            log.SelectedItem is not ComboBoxItem logItem)
        {
            return;
        }

        var value = logItem.Tag?.ToString() ?? "info";
        if (string.Equals(value, _snapshot.Settings.LogLevel, StringComparison.OrdinalIgnoreCase))
        {
            return;
        }

        _snapshot.Settings.LogLevel = value;
        try
        {
            await _runtime.SaveSettingsAsync(_snapshot.Settings);
            await _runtime.RunCoreActionAsync("restart");
            await RefreshAsync();
            Render("logs");
        }
        catch (Exception ex)
        {
            await ShowMessageAsync(UiText.Get("SaveFailed"), ex.Message);
            await RefreshAsync();
            Render("logs");
        }
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
        await CheckForUpdatesAsync();
    }

    internal async Task CheckForUpdatesAsync()
    {
        if (_runtime is null || _updateCheckInProgress) return;
        _updateCheckInProgress = true;
        SetUpdateCheckButtonState();
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
            var progress = new Progress<UpdateProgress>(value =>
            {
                if (_updateButton is not null)
                {
                    _updateButton.Content = string.IsNullOrWhiteSpace(value.Message)
                        ? UiText.Get("UpdatingAgentDock")
                        : value.Message;
                }
            });
            var output = await _runtime.RunUpdateAsync(progress);
            await ShowMessageAsync(UiText.Get("AgentDockUpdate"), LastLine(output, UiText.Get("UpdateCompleted")));
        }
        catch (Exception ex) { await ShowMessageAsync(UiText.Get("UpdateFailed"), ex.Message); }
        finally
        {
            _updateCheckInProgress = false;
            SetUpdateCheckButtonState();
        }
    }

    private void SetUpdateCheckButtonState()
    {
        if (_updateButton is null) return;
        _updateButton.Content = _updateCheckInProgress
            ? UiText.Get("CheckingForUpdates")
            : UiText.Get("CheckForUpdates");
        _updateButton.IsEnabled = !_updateCheckInProgress;
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

    private static string LogLevelLabel(string value) =>
        value switch
        {
            "debug" => UiText.Get("LogDebug"),
            "warn" => UiText.Get("LogWarning"),
            "error" => UiText.Get("LogError"),
            _ => UiText.Get("LogInfo")
        };

    private static double RecentP95(RuntimeAnalyticsPayload analytics)
    {
        var values = analytics.RecentCalls.Select(call => call.DurationMs).OrderBy(value => value).ToArray();
        if (values.Length == 0) return 0;
        var index = Math.Max(0, Math.Min(values.Length - 1, (int)Math.Ceiling(values.Length * 0.95) - 1));
        return values[index];
    }

    private static string FormatDuration(double milliseconds)
    {
        if (milliseconds < 1000) return $"{milliseconds:0} ms";
        if (milliseconds < 60_000) return $"{milliseconds / 1000:0.00} s";
        return $"{milliseconds / 60_000:0.0} min";
    }

    private static string FormatBytes(ulong bytes)
    {
        if (bytes >= 1UL << 30) return $"{bytes / (double)(1UL << 30):0.0} GB";
        if (bytes >= 1UL << 20) return $"{bytes / (double)(1UL << 20):0.0} MB";
        if (bytes >= 1UL << 10) return $"{bytes / (double)(1UL << 10):0.0} KB";
        return $"{bytes} B";
    }

    private static string FormatUptime(long milliseconds)
    {
        var seconds = Math.Max(0, milliseconds / 1000);
        if (seconds < 60) return $"{seconds} s";
        if (seconds < 3600) return $"{seconds / 60} min";
        if (seconds < 86_400) return $"{seconds / 3600} h";
        return $"{seconds / 86_400} d";
    }

    private void CopyDiagnostics(RuntimeAnalyticsPayload analytics)
    {
        var lines = new[]
        {
            $"AgentDock {_snapshot?.Version ?? "unknown"}",
            $"runtime={(_snapshot?.CoreRunning == true ? "running" : "stopped")}",
            $"total_calls={analytics.TotalCalls}",
            $"total_errors={analytics.TotalErrors}",
            $"recent_p95={FormatDuration(RecentP95(analytics))}",
            $"goroutines={analytics.Process.Goroutines}",
            $"heap_alloc={FormatBytes(analytics.Process.HeapAllocBytes)}",
            $"gc_cycles={analytics.Process.GcCycles}",
            $"uptime={FormatUptime(analytics.Process.UptimeMs)}"
        };
        CopyText(string.Join(Environment.NewLine, lines));
    }

    private static string LastLine(string value, string fallback) =>
        value.Split(['\r', '\n'], StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries).LastOrDefault() ?? fallback;

    private static string PageTitle(string tag) => tag switch
    {
        "permissions" => UiText.Get("Permissions"),
        "startup" => UiText.Get("Startup"),
        "logs" => UiText.Get("Logs"),

        "advancedConnection" => UiText.Get("AdvancedConnection"),

        "appearance" => UiText.Get("Appearance"),
        "about" => UiText.Get("About"),

        _ => UiText.Get("Permissions")
    };

    private static string PageDetail(string tag) => tag switch
    {
        "permissions" => UiText.Get("PermissionsDetail"),
        "startup" => UiText.Get("StartupDetail"),
        "logs" => UiText.Get("LogsDetail"),

        "advancedConnection" => UiText.Get("AdvancedConnectionDetail"),

        "appearance" => UiText.Get("AppearanceDetail"),
        "about" => UiText.Get("AboutDetail"),

        _ => UiText.Get("PermissionsDetail")
    };

    private static string SectionTitle(string tag) => tag switch
    {
        "permissions" => UiText.Get("RuntimePermissions"),
        "startup" => UiText.Get("Startup"),
        "logs" => UiText.Get("Logs"),
        "appearance" => UiText.Get("Appearance"),
        _ => UiText.Get("RuntimePermissions")
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
        var grid = new Grid { MinHeight = 46, Padding = new Thickness(13, 0, 13, 0), HorizontalAlignment = HorizontalAlignment.Stretch };
        grid.Children.Add(new TextBlock { Text = left, VerticalAlignment = VerticalAlignment.Center });
        grid.Children.Add(new TextBlock { Text = right, HorizontalAlignment = HorizontalAlignment.Right, VerticalAlignment = VerticalAlignment.Center, Opacity = 0.62 });
        return grid;
    }

    private static Grid ActionRow(string left, UIElement trailing)
    {
        var grid = new Grid
        {
            MinHeight = 46,
            Padding = new Thickness(13, 5, 13, 5),
            ColumnSpacing = 16,
            HorizontalAlignment = HorizontalAlignment.Stretch
        };
        grid.ColumnDefinitions.Add(new ColumnDefinition());
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });

        grid.Children.Add(new TextBlock { Text = left, VerticalAlignment = VerticalAlignment.Center });
        if (trailing is FrameworkElement element)
        {
            element.HorizontalAlignment = HorizontalAlignment.Right;
            element.VerticalAlignment = VerticalAlignment.Center;
            Grid.SetColumn(element, 1);
        }
        grid.Children.Add(trailing);
        return grid;
    }

    private static Grid TrailingActionRow(UIElement trailing)
    {
        var grid = new Grid { MinHeight = 46, Padding = new Thickness(13, 5, 13, 5) };
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

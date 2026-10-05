using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Navigation;

namespace AgentDock.ControlPanel;

public sealed partial class CapabilitiesPage : Page
{
    private RuntimeService? _runtime;
    private ControlPanelSettings? _settings;
    private bool _updatingUi;

    public CapabilitiesPage()
    {
        InitializeComponent();
        PageTitle.Text = UiText.Get("Capabilities");
        PageDetail.Text = UiText.Get("CapabilitiesDetail");
        CapabilitiesSectionTitle.Text = UiText.Get("BuiltInCapabilities");
        BrowserTitle.Text = UiText.Get("Browser");
        CodingAgentTitle.Text = UiText.Get("CodingAgent");
        McpAppsTitle.Text = UiText.Get("McpApps");
        ManagedBrowserModeItem.Content = UiText.Get("IsolatedBrowser");
        ReuseBrowserModeItem.Content = UiText.Get("ReuseLocalBrowser");
        SpecifiedBrowserModeItem.Content = UiText.Get("SpecifiedCdp");
        AddCustomProfileButton.Content = UiText.Get("AddCustomCodingAgent");
        McpAppsFullItem.Content = UiText.Get("Full");
        McpAppsCompactItem.Content = UiText.Get("Compact");
        McpAppsOffItem.Content = UiText.Get("Off");
        SaveButton.Content = UiText.Get("SaveAndRestart");
    }

    protected override async void OnNavigatedTo(NavigationEventArgs e)
    {
        if (e.Parameter is CapabilitiesNavigationRequest request)
        {
            _runtime = request.Runtime;
            ApplySettings(request.Settings);
            return;
        }

        _runtime = e.Parameter as RuntimeService;
        if (_runtime is null) return;
        ApplySettings(await _runtime.GetControlPanelSettingsAsync());
    }

    private void ApplySettings(ControlPanelSettings settings)
    {
        _settings = settings;
        foreach (var kind in new[] { "codex", "claude", "grok" })
        {
            if (settings.AcpProfiles.Any(profile => string.Equals(profile.Id, kind, StringComparison.OrdinalIgnoreCase))) continue;
            settings.AcpProfiles.Add(new AcpProfileSettings { Id = kind, Kind = kind, Enabled = false });
        }
        _updatingUi = true;
        BrowserEnabledToggle.IsOn = settings.BrowserEnabled;
        BrowserDetail.Text = BrowserModeDescription(settings);
        SelectBrowserMode(settings);
        BrowserCdpUrlTextBox.Text = settings.BrowserCdpUrl;
        CodingAgentEnabledToggle.IsOn = settings.AcpEnabled;
        UpdateCodingAgentDetail(settings);
        McpAppsDetail.Text = UiText.Get("McpAppsDetail");
        SelectComboTag(McpAppsModeComboBox, string.IsNullOrWhiteSpace(settings.McpAppsMode) ? "full" : settings.McpAppsMode);
        RenderProfiles(settings);
        _updatingUi = false;
        UpdateBrowserControls();
    }

    private static string BrowserModeDescription(ControlPanelSettings settings)
    {
        if (!settings.BrowserEnabled) return UiText.Get("BrowserCapabilityDetail");
        if (settings.BrowserReuseExistingCdp) return UiText.Get("BrowserReuseMode");
        if (!string.IsNullOrWhiteSpace(settings.BrowserCdpUrl)) return UiText.Get("BrowserSpecifiedMode");
        return UiText.Get("BrowserIsolatedMode");
    }

    private void SelectBrowserMode(ControlPanelSettings settings)
    {
        var mode = !string.IsNullOrWhiteSpace(settings.BrowserCdpUrl)
            ? "specified"
            : settings.BrowserReuseExistingCdp ? "reuse" : "managed";
        SelectComboTag(BrowserModeComboBox, mode);
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

    private void RenderProfiles(ControlPanelSettings settings)
    {
        AcpProfilesPanel.Children.Clear();

        var enabled = settings.AcpProfiles.Where(profile => profile.Enabled).ToList();
        if (enabled.Count > 0)
        {
            if (!enabled.Any(profile => string.Equals(profile.Id, settings.AcpDefaultProfile, StringComparison.OrdinalIgnoreCase)))
            {
                settings.AcpDefaultProfile = enabled[0].Id;
            }

            var defaults = new ComboBox
            {
                Header = UiText.Get("DefaultCodingAgent"),
                Width = 260,
                HorizontalAlignment = HorizontalAlignment.Left
            };
            foreach (var profile in enabled)
            {
                defaults.Items.Add(new ComboBoxItem
                {
                    Content = ProfileTitle(profile),
                    Tag = profile.Id
                });
            }
            SelectComboTag(defaults, settings.AcpDefaultProfile);
            defaults.SelectionChanged += DefaultProfileChanged;
            AcpProfilesPanel.Children.Add(defaults);
        }

        var checkBoxPanel = new StackPanel
        {
            Width = 260,
            HorizontalAlignment = HorizontalAlignment.Left,
            Spacing = 8
        };
        foreach (var profile in settings.AcpProfiles)
        {
            var row = new StackPanel
            {
                Orientation = Orientation.Horizontal,
                Spacing = 8,
                HorizontalAlignment = HorizontalAlignment.Left
            };
            var checkBox = new CheckBox
            {
                IsChecked = profile.Enabled,
                Tag = profile.Id,
                MinWidth = 20,
                HorizontalAlignment = HorizontalAlignment.Left,
                VerticalAlignment = VerticalAlignment.Center
            };
            checkBox.Checked += ProfileCheckBox_Changed;
            checkBox.Unchecked += ProfileCheckBox_Changed;
            row.Children.Add(checkBox);
            row.Children.Add(new TextBlock
            {
                Text = ProfileTitle(profile),
                VerticalAlignment = VerticalAlignment.Center
            });
            checkBoxPanel.Children.Add(row);
        }
        AcpProfilesPanel.Children.Add(checkBoxPanel);
    }

    private void ProfileCheckBox_Changed(object sender, RoutedEventArgs e)
    {
        if (_updatingUi || _settings is null || sender is not CheckBox checkBox || checkBox.Tag is not string id) return;
        var profile = _settings.AcpProfiles.FirstOrDefault(value => value.Id == id);
        if (profile is null) return;
        profile.Enabled = checkBox.IsChecked == true;
        RenderProfiles(_settings);
        UpdateCodingAgentDetail(_settings);
    }

    private void UpdateCodingAgentDetail(ControlPanelSettings settings)
    {
        var enabledProfiles = settings.AcpProfiles.Count(profile => profile.Enabled);
        if (enabledProfiles == 0)
        {
            CodingAgentDetail.Text = UiText.Get("NoProfilesConfigured");
            return;
        }

        var defaultProfile = settings.AcpProfiles.FirstOrDefault(profile =>
            string.Equals(profile.Id, settings.AcpDefaultProfile, StringComparison.OrdinalIgnoreCase));
        var defaultTitle = defaultProfile is null ? settings.AcpDefaultProfile : ProfileTitle(defaultProfile);
        CodingAgentDetail.Text = UiText.Format("ProfilesConfigured", enabledProfiles, defaultTitle);
    }

    private static string ProfileTitle(AcpProfileSettings profile)
    {
        if (!string.IsNullOrWhiteSpace(profile.DisplayName)) return profile.DisplayName;
        if (string.Equals(profile.Kind, "custom", StringComparison.OrdinalIgnoreCase)) return profile.Id;

        return profile.Kind.ToLowerInvariant() switch
        {
            "codex" => "Codex",
            "claude" => "Claude",
            "grok" => "Grok Build",
            _ when !string.IsNullOrEmpty(profile.Id) => char.ToUpperInvariant(profile.Id[0]) + profile.Id[1..],
            _ => profile.Id
        };
    }

    private void DefaultProfileChanged(object sender, SelectionChangedEventArgs e)
    {
        if (_updatingUi || _settings is null || sender is not ComboBox combo || combo.SelectedItem is not ComboBoxItem item) return;
        _settings.AcpDefaultProfile = item.Tag?.ToString() ?? "";
    }

    private async void AddCustomProfile_Click(object sender, RoutedEventArgs e)
    {
        if (_settings is null) return;
        var name = new TextBox { Header = UiText.Get("Name") };
        var command = new TextBox { Header = UiText.Get("Command") };
        var arguments = new TextBox { Header = UiText.Get("ArgumentsOnePerLine"), AcceptsReturn = true, MinHeight = 80 };
        var form = new StackPanel { Spacing = 10 };
        form.Children.Add(name); form.Children.Add(command); form.Children.Add(arguments);
        var dialog = new ContentDialog
        {
            XamlRoot = XamlRoot,
            Title = UiText.Get("AddCustomCodingAgent"),
            Content = form,
            PrimaryButtonText = UiText.Get("Add"),
            CloseButtonText = UiText.Get("Cancel")
        };
        if (await dialog.ShowAsync() != ContentDialogResult.Primary) return;
        var displayName = name.Text.Trim();
        if (displayName.Length == 0) return;
        var idBase = new string(displayName.ToLowerInvariant().Select(character =>
            char.IsLetterOrDigit(character) || character is '.' or '_' or '-' ? character : '-').ToArray()).Trim('-');
        if (idBase.Length == 0) idBase = "custom";
        var id = idBase;
        for (var suffix = 2; _settings.AcpProfiles.Any(profile => profile.Id == id); suffix++) id = $"{idBase}-{suffix}";
        _settings.AcpProfiles.Add(new AcpProfileSettings
        {
            Id = id,
            DisplayName = displayName,
            Kind = "custom",
            Command = command.Text.Trim(),
            Args = arguments.Text.Split(['\r', '\n'], StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries).ToList(),
            Enabled = true
        });
        if (string.IsNullOrWhiteSpace(_settings.AcpDefaultProfile)) _settings.AcpDefaultProfile = id;
        RenderProfiles(_settings);
    }

    private void BrowserModeChanged(object sender, SelectionChangedEventArgs e)
    {
        if (!_updatingUi) UpdateBrowserControls();
    }

    private void UpdateBrowserControls()
    {
        var mode = (BrowserModeComboBox.SelectedItem as ComboBoxItem)?.Tag?.ToString() ?? "managed";
        var specified = mode == "specified";
        BrowserCdpUrlTextBox.Visibility = Visibility.Visible;
        BrowserCdpUrlTextBox.Opacity = specified ? 1 : 0;
        BrowserCdpUrlTextBox.IsEnabled = specified;
        BrowserCdpUrlTextBox.IsTabStop = specified;
    }

    private async void SaveButton_Click(object sender, RoutedEventArgs e)
    {
        if (_runtime is null || _settings is null) return;
        var browserMode = (BrowserModeComboBox.SelectedItem as ComboBoxItem)?.Tag?.ToString() ?? "managed";
        _settings.BrowserEnabled = BrowserEnabledToggle.IsOn;
        _settings.BrowserCdpUrl = browserMode == "specified" ? BrowserCdpUrlTextBox.Text.Trim() : "";
        _settings.BrowserReuseExistingCdp = browserMode == "reuse";
        _settings.AcpEnabled = CodingAgentEnabledToggle.IsOn;
        _settings.McpAppsMode = (McpAppsModeComboBox.SelectedItem as ComboBoxItem)?.Tag?.ToString() ?? "full";
        SaveStatus.Text = UiText.Get("Saving");
        try
        {
            if (_settings.AcpEnabled)
            {
                var enabledProfiles = _settings.AcpProfiles.Where(profile => profile.Enabled).ToList();
                if (enabledProfiles.Count == 0) throw new InvalidOperationException(UiText.Get("AtLeastOneCodingAgent"));
                foreach (var profile in enabledProfiles)
                {
                    var resolution = _runtime.ResolveAcpAdapter(profile.Kind, profile.Command, profile.Args);
                    if (!resolution.Available) throw new InvalidOperationException($"{profile.Id}: {resolution.Message}");
                    profile.Command = resolution.Command;
                    profile.Args = [.. resolution.Arguments];
                }
                if (!enabledProfiles.Any(profile => profile.Id == _settings.AcpDefaultProfile))
                {
                    _settings.AcpDefaultProfile = enabledProfiles[0].Id;
                }
            }
            await _runtime.SaveSettingsAsync(_settings);
            await _runtime.RunCoreActionAsync("restart");
            SaveStatus.Text = UiText.Get("Saved");
        }
        catch (Exception ex) { SaveStatus.Text = ex.Message; }
    }
}

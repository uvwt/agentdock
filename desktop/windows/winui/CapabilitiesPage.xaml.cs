using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Navigation;

namespace AgentDock.ControlPanel;

public sealed partial class CapabilitiesPage : Page
{
    public CapabilitiesPage()
    {
        InitializeComponent();
        PageTitle.Text = UiText.Get("Capabilities");
        PageDetail.Text = UiText.Get("CapabilitiesDetail");
        CapabilitiesSection.Title = UiText.Get("AvailableCapabilities");
        BrowserTitle.Text = UiText.Get("Browser");
    }

    protected override async void OnNavigatedTo(NavigationEventArgs e)
    {
        if (e.Parameter is not RuntimeService runtime) return;

        var snapshot = await runtime.GetSnapshotAsync(includeNexusConnection: false);
        var settings = snapshot.Settings;

        BrowserState.Text = settings.BrowserEnabled
            ? UiText.Get("Enabled") + " ●"
            : UiText.Get("Disabled");
        BrowserDetail.Text = BrowserModeDescription(settings);

        var enabledProfiles = settings.AcpProfiles.Count(profile => profile.Enabled);
        CodingAgentState.Text = settings.AcpEnabled
            ? UiText.Get("Enabled") + " ●"
            : UiText.Get("Disabled");
        CodingAgentDetail.Text = enabledProfiles == 0
            ? UiText.Get("NoProfilesConfigured")
            : UiText.Format("ProfilesConfigured", enabledProfiles, settings.AcpDefaultProfile);

        McpAppsDetail.Text = UiText.Get("McpAppsDetail");
        McpAppsState.Text = settings.McpAppsMode switch
        {
            "off" => UiText.Get("Off"),
            "compact" => UiText.Get("Compact"),
            _ => UiText.Get("Full")
        };
    }

    private static string BrowserModeDescription(ControlPanelSettings settings)
    {
        if (!settings.BrowserEnabled) return UiText.Get("BrowserCapabilityDetail");
        if (settings.BrowserReuseExistingCdp) return UiText.Get("BrowserReuseMode");
        if (!string.IsNullOrWhiteSpace(settings.BrowserCdpUrl)) return UiText.Get("BrowserSpecifiedMode");
        return UiText.Get("BrowserIsolatedMode");
    }
}

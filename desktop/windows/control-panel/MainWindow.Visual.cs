using System.Windows;
using System.Windows.Controls;

namespace AgentDock.ControlPanel;

// Presentation-only navigation, layout and entry-point orchestration.
// The runtime service implementation, permissions and configuration format are unchanged.
public partial class MainWindow
{
    internal bool IsUiPreview { get; set; }
    private bool _tunnelSelectionBusy;

    private void Dashboard_Loaded(object sender, RoutedEventArgs e)
    {
        InitializeAppearance();
        // WPF sizes are device-independent. Fit the initial window to the working area;
        // all later resizing is handled by responsive panels, not whole-window scaling.
        var area = SystemParameters.WorkArea;
        Width = Math.Max(MinWidth, Math.Min(Width, area.Width));
        Height = Math.Max(MinHeight, Math.Min(Height, area.Height));
        if (Left + Width > area.Right) Left = Math.Max(area.Left, area.Right - Width);
        if (Top + Height > area.Bottom) Top = Math.Max(area.Top, area.Bottom - Height);
    }

    private void OpenAccessConfiguration_Click(object sender, RoutedEventArgs e) => MainTabs.SelectedIndex = 3;
    private void ReturnHome_Click(object sender, RoutedEventArgs e) => MainTabs.SelectedIndex = 0;

    private string SelectedModeLabel() => UiText.Get(SelectedTunnelMode() switch
    {
        "quick" => "TemporaryAddress",
        "named" => "UiFixedDomain",
        _ => "LocalOnly"
    });

    private bool NeedsFixedDomainConfiguration() =>
        !Uri.TryCreate(ServerUrlTextBox.Text.Trim(), UriKind.Absolute, out var uri)
        || !string.Equals(uri.Scheme, Uri.UriSchemeHttps, StringComparison.OrdinalIgnoreCase)
        || (string.IsNullOrWhiteSpace(TunnelTokenPasswordBox.Password) && _snapshot?.TunnelTokenStored != true);

    private async void HomeTunnelMode_Click(object sender, RoutedEventArgs e)
    {
        // Checked events caused by snapshots/keyboard focus never apply network changes.
        if (_updatingUi || _tunnelSelectionBusy) return;
        var mode = SelectedTunnelMode();
        if (mode == "named" && NeedsFixedDomainConfiguration())
        {
            HomeModeHintText.Text = UiText.Get("UiModePending");
            MainTabs.SelectedIndex = 3;
            ServerUrlTextBox.Focus();
            return;
        }
        if (IsUiPreview)
        {
            HomeModeHintText.Text = UiText.Format("UiModePreview", SelectedModeLabel());
            return;
        }
        if (_snapshot is null)
        {
            HomeModeHintText.Text = UiText.Get("LoadingStatus");
            return;
        }
        if (string.Equals(_snapshot.TunnelMode, mode, StringComparison.OrdinalIgnoreCase))
        {
            HomeModeHintText.Text = UiText.Format("UiModeCurrent", SelectedModeLabel());
            return;
        }
        await ApplySelectedTunnelModeFromUiAsync();
    }

    private async Task<bool> ApplySelectedTunnelModeFromUiAsync()
    {
        // A preview is always non-mutating, even when invoked by keyboard or automation.
        if (IsUiPreview)
        {
            HomeModeHintText.Text = UiText.Format("UiModePreview", SelectedModeLabel());
            return false;
        }
        if (_updatingUi || _tunnelSelectionBusy || _snapshot is null) return false;
        if (SelectedTunnelMode() == "named" && NeedsFixedDomainConfiguration())
        {
            HomeModeHintText.Text = UiText.Get("UiModePending");
            MainTabs.SelectedIndex = 3;
            return false;
        }
        var previousMode = _snapshot.TunnelMode;
        _tunnelSelectionBusy = true;
        HomeModesPanel.IsEnabled = false;
        ConfigurationContent.IsEnabled = false;
        HomeModeHintText.Text = UiText.Get("UiModeApplying");
        try
        {
            // This calls the existing SetTunnelModeAsync / ExecuteActionAsync sequence.
            var applied = await ApplySelectedTunnelModeCoreAsync();
            if (!applied)
            {
                RestoreModeSelection(previousMode);
                HomeModeHintText.Text = UiText.Get("UiModeFailed");
                return false;
            }
            HomeModeHintText.Text = UiText.Format("UiModeCurrent", SelectedModeLabel());
            return true;
        }
        finally
        {
            _tunnelSelectionBusy = false;
            HomeModesPanel.IsEnabled = true;
            ConfigurationContent.IsEnabled = true;
        }
    }

    private void RestoreModeSelection(string mode)
    {
        var previousUpdating = _updatingUi;
        _updatingUi = true;
        try
        {
            LocalModeRadio.IsChecked = mode == "none";
            QuickModeRadio.IsChecked = mode == "quick";
            NamedModeRadio.IsChecked = mode == "named";
            UpdateTunnelModeUi();
        }
        finally { _updatingUi = previousUpdating; }
    }


}

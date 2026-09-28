using System.Runtime.InteropServices;
using Forms = System.Windows.Forms;

namespace AgentDock.ControlPanel;

public partial class App
{
    private string _trayTheme = "light";

    internal void ApplyTrayTheme(string theme)
    {
        _trayTheme = UiThemePreference.Normalize(theme);
        if (_trayMenu is not null) TrayMenuPresentation.ApplyTheme(_trayMenu, _trayTheme);
    }

    private void ShowTrayPage(int index)
    {
        ShowControlPanel();
        ((System.Windows.Controls.TabControl)ControlPanelWindow.FindName("MainTabs")).SelectedIndex = index;
    }

    private async Task CheckPublicFromTrayAsync()
    {
        ShowTrayPage(0);
        await ControlPanelWindow.OpenPublicCheckFromTrayAsync();
    }

    private async Task CopyPublicFromTrayAsync()
    {
        var address = _traySnapshot?.PublicMcpUrl ?? "";
        if (string.IsNullOrWhiteSpace(address))
        {
            _notifyIcon?.ShowBalloonTip(2200, "AgentDock", UiText.Get("UiAddressEmpty"), Forms.ToolTipIcon.Info);
            return;
        }
        try
        {
            await UiClipboard.WriteAsync(address);
            // Never include the endpoint, hostname or credentials in notifications.
            _notifyIcon?.ShowBalloonTip(2200, "AgentDock", UiText.Get("UiCopyDone"), Forms.ToolTipIcon.Info);
        }
        catch (Exception ex) when (ex is ExternalException or InvalidOperationException or System.Security.SecurityException or ArgumentException)
        {
            _notifyIcon?.ShowBalloonTip(3500, "AgentDock", UiText.Get("UiCopyErrorRetry"), Forms.ToolTipIcon.Warning);
        }
    }
}

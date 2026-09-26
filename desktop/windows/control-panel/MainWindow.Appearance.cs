using System.IO;
using System.Runtime.InteropServices;
using System.Windows;
using System.Windows.Automation;
using System.Windows.Automation.Peers;
using System.Windows.Controls;
using RadioButton = System.Windows.Controls.RadioButton;
using Button = System.Windows.Controls.Button;

namespace AgentDock.ControlPanel;

// Appearance and interaction feedback only. No runtime, tunnel or permission writes.
public partial class MainWindow
{
    private bool _appearanceReady;
    private bool _changingTheme;
    private string _uiTheme = "light";
    private long _toastRevision;
    private readonly Dictionary<Button, long> _copyRevisions = new();
    private readonly CancellationTokenSource _feedbackLifetime = new();
    internal string? ThemePreferencePathOverride { get; set; }
    internal Func<string, Task> ClipboardWriter { get; set; } = UiClipboard.WriteAsync;
    internal Task LastCopyOperation { get; private set; } = Task.CompletedTask;
    private string ThemePreferencePath => ThemePreferencePathOverride ?? Path.Combine(
        Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "AgentDock",
        IsUiPreview ? "ui-theme-preview" : "ui-theme");

    private void InitializeAppearance()
    {
        if (_appearanceReady) return;
        _appearanceReady = true;
        ApplyUiTheme(UiThemePreference.Read(ThemePreferencePath), false);
        Closed += (_, _) => { _feedbackLifetime.Cancel(); CopyToastPopup.IsOpen = false; };
        IsVisibleChanged += (_, _) => { if (!IsVisible) CopyToastPopup.IsOpen = false; };
        Deactivated += (_, _) => CopyToastPopup.IsOpen = false;
        MainTabs.SelectionChanged += (_, e) =>
        {
            if (ReferenceEquals(e.Source, MainTabs)) CopyToastPopup.IsOpen = false;
        };
    }

    private void ThemeSelection_Changed(object sender, RoutedEventArgs e)
    {
        if (!_appearanceReady || _changingTheme || sender is not RadioButton radio || radio.IsChecked != true) return;
        ApplyUiTheme(radio.Tag?.ToString() ?? "light", true);
    }

    private void ApplyUiTheme(string theme, bool persist)
    {
        theme = UiThemePreference.Normalize(theme);
        _changingTheme = true;
        try
        {
            var palette = new ResourceDictionary
            {
                Source = new Uri($"/agentdock-tray;component/Themes/{(theme == "dark" ? "Dark" : "Light")}.xaml", UriKind.Relative)
            };
            Resources.MergedDictionaries[0] = palette;
            _uiTheme = theme;
            LightThemeRadio.IsChecked = theme == "light";
            DarkThemeRadio.IsChecked = theme == "dark";
            UiThemePreference.ApplyTitleBar(this, theme == "dark");
            if (System.Windows.Application.Current is App app) app.ApplyTrayTheme(theme);
            ThemeStatusText.SetResourceReference(TextBlock.ForegroundProperty, "UiMuted");
            ThemeStatusText.Text = UiText.Get("UiThemeImmediate");
            if (persist)
            {
                try
                {
                    UiThemePreference.Save(ThemePreferencePath, theme);
                    ThemeStatusText.Text = UiText.Get("UiThemeSaved");
                }
                catch (Exception ex) when (ex is IOException or UnauthorizedAccessException)
                {
                    ThemeStatusText.Text = UiText.Get("UiThemeSaveFailed");
                    ThemeStatusText.SetResourceReference(TextBlock.ForegroundProperty, "UiError");
                }
            }
        }
        finally { _changingTheme = false; }
    }

    private async void CopyMcpButton_Click(object sender, RoutedEventArgs e)
    {
        if (sender is not Button button) return;
        var address = Equals(button.Tag, "local") ? LocalMcpTextBox.Text : PublicMcpTextBox.Text;
        LastCopyOperation = CopyAddressWithFeedbackAsync(button, address);
        await LastCopyOperation;
    }

    private async Task CopyAddressWithFeedbackAsync(Button button, string address)
    {
        var revision = _copyRevisions.GetValueOrDefault(button) + 1;
        _copyRevisions[button] = revision;
        var success = false;
        string message;
        if (string.IsNullOrWhiteSpace(address)) message = UiText.Get("UiAddressEmpty");
        else
        {
            CopyFeedback.SetState(button, "working");
            button.ToolTip = UiText.Get("UiCopyBusy");
            try
            {
                await ClipboardWriter(address);
                success = true;
                message = UiText.Get(IsUiPreview ? "UiCopyExampleDone" : "UiCopyDone");
            }
            catch (Exception ex) when (ex is ExternalException or InvalidOperationException or System.Security.SecurityException or ArgumentException)
            {
                message = UiText.Get("UiCopyErrorRetry");
            }
        }
        if (_feedbackLifetime.IsCancellationRequested || _copyRevisions.GetValueOrDefault(button) != revision) return;
        CopyFeedback.SetState(button, success ? "success" : "error");
        button.ToolTip = message;
        AutomationProperties.SetHelpText(button, message);
        FooterStatusText.Text = message;
        var toast = ++_toastRevision;
        CopyToastText.Text = message;
        CopyToastText.SetResourceReference(TextBlock.ForegroundProperty, success ? "UiSuccess" : "UiError");
        CopyToastChrome.SetResourceReference(Border.BackgroundProperty, success ? "UiSuccessSoft" : "UiErrorSoft");
        CopyToastChrome.SetResourceReference(Border.BorderBrushProperty, success ? "UiSuccess" : "UiError");
        CopyToastPopup.PlacementTarget = button;
        CopyToastPopup.IsOpen = button.IsVisible && IsVisible;
        var peer = UIElementAutomationPeer.FromElement(CopyToastText) ?? UIElementAutomationPeer.CreatePeerForElement(CopyToastText);
        peer?.RaiseAutomationEvent(AutomationEvents.LiveRegionChanged);
        _ = ResetCopyFeedbackAsync(button, revision, toast, success ? 2200 : 3500);
    }

    private async Task ResetCopyFeedbackAsync(Button button, long revision, long toast, int delay)
    {
        try { await Task.Delay(delay, _feedbackLifetime.Token); }
        catch (OperationCanceledException) { return; }
        if (_copyRevisions.GetValueOrDefault(button) == revision)
        {
            CopyFeedback.SetState(button, "idle");
            button.ToolTip = UiText.Get(Equals(button.Tag, "local") ? "UiCopyLocal" : "UiCopyPublic");
            AutomationProperties.SetHelpText(button, "");
        }
        // A previous copy must not close a newer button's feedback popup.
        if (_toastRevision == toast) CopyToastPopup.IsOpen = false;
    }
}

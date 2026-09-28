using System.Globalization;
using System.Net.Http;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;

namespace AgentDock.ControlPanel;

// Presentation-only state for the existing RuntimeService.TestUrlAsync request.
public partial class MainWindow
{
    private CancellationTokenSource? _publicCheckCancellation;
    private string _publicCheckOrigin = "";
    private long _publicCheckRevision;
    internal string CurrentPublicCheckState { get; private set; } = "idle";
    internal Func<string, CancellationToken, Task<UrlTestResult>>? PublicCheckProbeOverride { get; set; }

    internal void SetThemeFromTray(string theme) => ApplyUiTheme(theme, persist: true);
    internal async Task OpenPublicCheckFromTrayAsync()
    {
        MainTabs.SelectedIndex = 0;
        UpdateLayout();
        PublicCheckCard.BringIntoView();
        await RunPublicCheckAsync(_snapshot?.PublicOrigin ?? "", _snapshot?.TunnelMode ?? "none", force: true);
    }

    internal async Task RunPublicCheckAsync(string origin, string mode, bool force)
    {
        origin = origin.Trim();
        if (_publicCheckCancellation is not null && _publicCheckOrigin == origin) return;
        if (!force && origin.Length > 0 && _lastAutoTestOrigin == origin &&
            DateTimeOffset.UtcNow - _lastAutoTestAt < TimeSpan.FromSeconds(15)) return;

        var revision = ++_publicCheckRevision;
        _publicCheckCancellation?.Cancel();
        _publicCheckOrigin = origin;
        PublicCheckHttpText.Text = "—";
        PublicCheckElapsedText.Text = "—";
        PublicCheckTimeText.Text = "—";
        if (origin.Length == 0)
        {
            RenderPublicCheck("empty", mode == "quick" ? "WaitingTemporaryAddress" : "PublicAddressNotConfigured", "UiPublicCheckScope");
            TestPublicButton.IsEnabled = true;
            return;
        }
        if (!Uri.TryCreate(origin, UriKind.Absolute, out var uri) ||
            uri.Scheme is not ("http" or "https"))
        {
            RenderPublicCheck("error", "InvalidPublicAddress", "UiPublicCheckScope");
            TestPublicButton.IsEnabled = true;
            return;
        }
        if (IsUiPreview && PublicCheckProbeOverride is null)
        {
            RenderPublicCheck("idle", "NotChecked", "UiPublicCheckPreview");
            return;
        }

        _lastAutoTestOrigin = origin;
        _lastAutoTestAt = DateTimeOffset.UtcNow;
        using var cancellation = CancellationTokenSource.CreateLinkedTokenSource(_feedbackLifetime.Token);
        _publicCheckCancellation = cancellation;
        TestPublicButton.IsEnabled = false;
        RenderPublicCheck("checking", "UiPublicChecking", "UiPublicCheckScope");
        try
        {
            var result = await (PublicCheckProbeOverride is not null
                ? PublicCheckProbeOverride(origin, cancellation.Token)
                : _runtime.TestUrlAsync(origin, cancellation.Token));
            if (revision != _publicCheckRevision || cancellation.IsCancellationRequested) return;
            RenderPublicCheck(result.Success ? "success" : "error",
                result.Success ? "UiPublicReachable" : "UiPublicUnreachable",
                result.Success ? "UiPublicCheckSuccess" : result.StatusCode.HasValue
                    ? "UiPublicCheckHttpFailure" : result.Message == UiText.Get("AccessTimeout")
                        ? "AccessTimeout" : "UiPublicCheckNetworkFailure");
            // Do not display exception messages or the private endpoint in the status card.
            PublicCheckHttpText.Text = result.StatusCode?.ToString(CultureInfo.InvariantCulture) ?? "—";
            PublicCheckElapsedText.Text = $"{Math.Max(0, Math.Round(result.Elapsed.TotalMilliseconds))} ms";
            PublicCheckTimeText.Text = DateTimeOffset.Now.ToString("HH:mm:ss", CultureInfo.CurrentCulture);
        }
        catch (OperationCanceledException) when (cancellation.IsCancellationRequested) { }
        catch (Exception ex) when (ex is HttpRequestException or TaskCanceledException or InvalidOperationException)
        {
            if (revision == _publicCheckRevision && !cancellation.IsCancellationRequested)
            {
                RenderPublicCheck("error", "UiPublicUnreachable", "UiPublicCheckNetworkFailure");
                PublicCheckTimeText.Text = DateTimeOffset.Now.ToString("HH:mm:ss", CultureInfo.CurrentCulture);
            }
        }
        finally
        {
            if (ReferenceEquals(_publicCheckCancellation, cancellation)) _publicCheckCancellation = null;
            if (revision == _publicCheckRevision) TestPublicButton.IsEnabled = true;
        }
    }

    private void RenderPublicCheck(string state, string summaryKey, string detailKey)
    {
        CurrentPublicCheckState = state;
        var ink = state switch { "success" => "UiSuccess", "error" => "UiError", "checking" => "UiAccentInk", _ => "UiMuted" };
        var surface = state switch { "success" => "UiSuccessSoft", "error" => "UiErrorSoft", "checking" => "UiTint", _ => "UiInset" };
        var glyph = state switch { "success" => "IconCheck", "error" => "IconClose", "checking" => "IconRefresh", _ => "IconInfo" };
        PublicCheckSummaryText.Text = UiText.Get(summaryKey);
        PublicTestStatusText.Text = UiText.Get(detailKey);
        PublicCheckSummaryText.SetResourceReference(TextBlock.ForegroundProperty, ink);
        PublicCheckStateBadge.SetResourceReference(Border.BackgroundProperty, surface);
        PublicCheckStateIcon.SetResourceReference(System.Windows.Shapes.Shape.StrokeProperty, ink);
        PublicCheckStateIcon.Data = (Geometry)FindResource(glyph);
    }
}

using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Navigation;

namespace AgentDock.ControlPanel;

public sealed partial class ActivityPage : Page
{
    private const int PageSize = 20;

    private readonly DispatcherTimer _analyticsTimer = new() { Interval = TimeSpan.FromSeconds(5) };
    private RuntimeService? _runtime;
    private RuntimeSnapshot? _snapshot;
    private bool _refreshingAnalytics;
    private bool _hasAnalyticsSnapshot;
    private int _visibleCallCount = PageSize;
    private ulong? _renderedLatestCallId;
    private int _renderedVisibleCallCount = -1;
    private IReadOnlyList<RuntimeAnalyticsCall> _latestCalls = Array.Empty<RuntimeAnalyticsCall>();

    public ActivityPage()
    {
        InitializeComponent();

        PageTitle.Text = UiText.Get("Activity");
        PageDetail.Text = UiText.Get("ActivityDetail");
        RecentCallsSection.Title = UiText.Get("RecentCalls");
        RecentCallsHint.Text = UiText.Get("RecentCallsPrivacyHint");
        _analyticsTimer.Tick += AnalyticsTimer_Tick;
    }

    protected override async void OnNavigatedTo(NavigationEventArgs e)
    {
        _visibleCallCount = PageSize;
        _hasAnalyticsSnapshot = false;
        _latestCalls = Array.Empty<RuntimeAnalyticsCall>();
        _renderedLatestCallId = null;
        _renderedVisibleCallCount = -1;

        _runtime = e.Parameter as RuntimeService;
        if (_runtime is null)
        {
            RenderAnalyticsUnavailable();
            return;
        }

        _snapshot = await _runtime.GetSnapshotAsync(includeNexusConnection: false);
        await RefreshAnalyticsAsync();
        _analyticsTimer.Start();
    }

    protected override void OnNavigatedFrom(NavigationEventArgs e)
    {
        _analyticsTimer.Stop();
        base.OnNavigatedFrom(e);
    }

    private async void AnalyticsTimer_Tick(object? sender, object e) =>
        await RefreshAnalyticsAsync();

    private async Task RefreshAnalyticsAsync()
    {
        if (_runtime is null || _snapshot is null || _refreshingAnalytics)
        {
            return;
        }

        _refreshingAnalytics = true;
        try
        {
            var analytics = await _runtime.GetRuntimeAnalyticsAsync(_snapshot);
            if (analytics is null)
            {
                RenderAnalyticsUnavailable();
                return;
            }

            var latestId = analytics.RecentCalls.Count == 0 ? (ulong?)null : analytics.RecentCalls[0].Id;
            if (_hasAnalyticsSnapshot && latestId == _renderedLatestCallId)
            {
                return;
            }

            _hasAnalyticsSnapshot = true;
            _latestCalls = analytics.RecentCalls;
            RenderRecentCalls(force: true);
        }
        finally
        {
            _refreshingAnalytics = false;
        }
    }

    private void RenderAnalyticsUnavailable()
    {
        RecentCallsPanel.Children.Clear();
        RecentCallsPanel.Children.Add(CreateEmptyText(UiText.Get("AnalyticsUnavailable")));
        _hasAnalyticsSnapshot = false;
        _latestCalls = Array.Empty<RuntimeAnalyticsCall>();
        _renderedLatestCallId = null;
        _renderedVisibleCallCount = -1;
    }

    private void RenderRecentCalls(bool force = false)
    {
        var latestId = _latestCalls.Count == 0 ? (ulong?)null : _latestCalls[0].Id;
        var visibleCount = Math.Min(_visibleCallCount, _latestCalls.Count);
        if (!force && _renderedLatestCallId == latestId && _renderedVisibleCallCount == visibleCount)
        {
            return;
        }

        _renderedLatestCallId = latestId;
        _renderedVisibleCallCount = visibleCount;
        RecentCallsPanel.Children.Clear();
        if (_latestCalls.Count == 0)
        {
            RecentCallsPanel.Children.Add(CreateEmptyText(UiText.Get("NoCallData")));
            return;
        }

        for (var index = 0; index < visibleCount; index++)
        {
            if (index > 0)
            {
                RecentCallsPanel.Children.Add(CreateDivider());
            }
            RecentCallsPanel.Children.Add(CreateCallExpander(_latestCalls[index]));
        }

        if (visibleCount < _latestCalls.Count)
        {
            RecentCallsPanel.Children.Add(CreateDivider());
            var showMore = new Button
            {
                Content = UiText.Get("ShowMore"),
                HorizontalAlignment = HorizontalAlignment.Stretch,
                HorizontalContentAlignment = HorizontalAlignment.Left,
                Padding = new Thickness(13, 10, 13, 10),
                BorderThickness = new Thickness(0),
                Background = new SolidColorBrush(Microsoft.UI.Colors.Transparent)
            };
            showMore.Click += (_, _) =>
            {
                _visibleCallCount += PageSize;
                RenderRecentCalls(force: true);
            };
            RecentCallsPanel.Children.Add(showMore);
        }
    }

    private static UIElement CreateCallExpander(RuntimeAnalyticsCall call)
    {
        var hasDetails = call.Stages.Count > 0 || !string.IsNullOrWhiteSpace(call.ErrorCode);
        var root = new StackPanel
        {
            HorizontalAlignment = HorizontalAlignment.Stretch
        };

        var header = new Grid { MinHeight = 42, ColumnSpacing = 16 };
        header.ColumnDefinitions.Add(new ColumnDefinition());
        header.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        header.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });

        var labels = new StackPanel { Spacing = 2 };
        var toolLine = new StackPanel
        {
            Orientation = Orientation.Horizontal,
            Spacing = 6
        };
        toolLine.Children.Add(new TextBlock
        {
            Text = call.Tool,
            FontSize = 13,
            FontWeight = Microsoft.UI.Text.FontWeights.Medium,
            VerticalAlignment = VerticalAlignment.Center
        });

        var chevron = new TextBlock
        {
            Text = "›",
            FontSize = 15,
            Foreground = SecondaryBrush(),
            VerticalAlignment = VerticalAlignment.Center,
            Visibility = hasDetails ? Visibility.Visible : Visibility.Collapsed
        };
        toolLine.Children.Add(chevron);
        labels.Children.Add(toolLine);
        labels.Children.Add(new TextBlock
        {
            Text = $"{ActivitySource(call.Source)} · {RelativeTime(call.StartedAt)}",
            FontSize = 11.5,
            Foreground = SecondaryBrush()
        });
        header.Children.Add(labels);

        var duration = new TextBlock
        {
            Text = FormatDuration(call.DurationMs),
            FontSize = 11.5,
            VerticalAlignment = VerticalAlignment.Center,
            Foreground = SecondaryBrush()
        };
        Grid.SetColumn(duration, 1);
        header.Children.Add(duration);

        var status = new TextBlock
        {
            Text = call.Success ? UiText.Get("Succeeded") : UiText.Get("Failed"),
            FontSize = 12,
            VerticalAlignment = VerticalAlignment.Center
        };
        Grid.SetColumn(status, 2);
        header.Children.Add(status);

        var details = new StackPanel
        {
            Spacing = 7,
            Margin = new Thickness(13, 4, 13, 8),
            Visibility = Visibility.Collapsed,
            HorizontalAlignment = HorizontalAlignment.Stretch
        };
        if (!string.IsNullOrWhiteSpace(call.ErrorCode))
        {
            details.Children.Add(new TextBlock
            {
                Text = $"{UiText.Get("ErrorCode")}: {call.ErrorCode}",
                FontSize = 11.5,
                Foreground = SecondaryBrush()
            });
        }

        foreach (var stage in call.Stages)
        {
            var stageRow = new Grid { ColumnSpacing = 12 };
            stageRow.ColumnDefinitions.Add(new ColumnDefinition());
            stageRow.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
            stageRow.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });

            stageRow.Children.Add(new TextBlock
            {
                Text = StageName(stage.Name),
                FontSize = 11.5
            });

            var stageDuration = new TextBlock
            {
                Text = FormatDuration(stage.DurationMs),
                FontSize = 11.5,
                Foreground = SecondaryBrush()
            };
            Grid.SetColumn(stageDuration, 1);
            stageRow.Children.Add(stageDuration);

            var stageStatus = new TextBlock
            {
                Text = stage.Success ? UiText.Get("Succeeded") : UiText.Get("Failed"),
                FontSize = 11.5
            };
            Grid.SetColumn(stageStatus, 2);
            stageRow.Children.Add(stageStatus);
            details.Children.Add(stageRow);
        }

        if (hasDetails)
        {
            var headerButton = new Button
            {
                Content = header,
                Background = new SolidColorBrush(Microsoft.UI.Colors.Transparent),
                BorderThickness = new Thickness(0),
                Padding = new Thickness(13, 4, 13, 4),
                HorizontalAlignment = HorizontalAlignment.Stretch,
                HorizontalContentAlignment = HorizontalAlignment.Stretch,
                MinWidth = 0,
                MinHeight = 0
            };
            headerButton.Click += (_, _) =>
            {
                var expanding = details.Visibility != Visibility.Visible;
                details.Visibility = expanding ? Visibility.Visible : Visibility.Collapsed;
                chevron.Text = expanding ? "⌄" : "›";
            };
            root.Children.Add(headerButton);
            root.Children.Add(details);
        }
        else
        {
            header.Margin = new Thickness(13, 4, 13, 4);
            root.Children.Add(header);
        }

        return root;
    }

    private static TextBlock CreateEmptyText(string text) =>
        new()
        {
            Text = text,
            FontSize = 12.5,
            Foreground = SecondaryBrush(),
            Margin = new Thickness(13, 14, 13, 14)
        };

    private static Border CreateDivider() =>
        new()
        {
            Height = 1,
            Margin = new Thickness(13, 0, 0, 0),
            Background = (Brush)Application.Current.Resources["DividerStrokeColorDefaultBrush"]
        };

    private static Brush SecondaryBrush() =>
        (Brush)Application.Current.Resources["TextFillColorSecondaryBrush"];

    private static string ActivitySource(string source) =>
        source.Trim().ToLowerInvariant() switch
        {
            "nexus" => UiText.Get("ActivitySourceRemote"),
            "internal" => UiText.Get("ActivitySourceLocal"),
            "mcp" => "MCP",
            _ => source
        };

    private static string StageName(string name) =>
        name switch
        {
            "mcp.refresh" => UiText.Get("StageMcpRefresh"),
            "mcp.remote_call" => UiText.Get("StageMcpRemoteCall"),
            "command.start" => UiText.Get("StageCommandStart"),
            "command.foreground_wait" => UiText.Get("StageCommandForegroundWait"),
            _ => name
        };

    private static string RelativeTime(DateTimeOffset startedAt)
    {
        var elapsed = DateTimeOffset.Now - startedAt.ToLocalTime();
        if (elapsed < TimeSpan.Zero)
        {
            elapsed = TimeSpan.Zero;
        }
        if (elapsed.TotalMinutes < 1)
        {
            return UiText.Get("JustNow");
        }
        if (elapsed.TotalHours < 1)
        {
            return UiText.Format("MinutesAgo", (int)elapsed.TotalMinutes);
        }
        return UiText.Format("HoursAgo", (int)elapsed.TotalHours);
    }

    private static string FormatDuration(double milliseconds)
    {
        if (milliseconds < 1000)
        {
            return $"{milliseconds:0} ms";
        }
        if (milliseconds < 60_000)
        {
            return $"{milliseconds / 1000:0.00} s";
        }
        return $"{milliseconds / 60_000:0.0} min";
    }
}

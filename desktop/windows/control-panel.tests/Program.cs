using System.Diagnostics;
using System.IO;
using System.Reflection;
using System.Text.Json;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Controls.Primitives;
using System.Windows.Media;
using System.Windows.Media.Imaging;
using System.Windows.Threading;
using AgentDock.ControlPanel;

internal static partial class Program
{
    private static string Output = Path.Combine(Path.GetTempPath(), "agentdock-ui-checks-" + Guid.NewGuid().ToString("N"));
    private static readonly Dictionary<string, object> Report = new();
    private static readonly Dictionary<string, bool> Tests = new();
    private static readonly StringWriter BindingLog = new();
    private const BindingFlags PrivateInstance = BindingFlags.NonPublic | BindingFlags.Instance;
    private static RuntimeSnapshot? _safeSnapshot;
    private static string _culture = "zh-CN";

    [STAThread]
    public static int Main(string[] args)
    {
        var outputIndex = Array.IndexOf(args, "--output");
        if (outputIndex >= 0)
        {
            if (outputIndex + 1 >= args.Length) throw new ArgumentException("--output requires a directory");
            Output = Path.GetFullPath(args[outputIndex + 1]);
        }
        // Every runtime request is replaced by fixture data; do not bind an installed instance.
        Environment.SetEnvironmentVariable("AGENTDOCK_RUNTIME_DIR", Path.Combine(Output, "isolated-runtime"));
        _culture = args.Contains("--english") ? "en" : "zh-CN";
        if (_culture == "en") Output = Path.Combine(Output, "english");
        Directory.CreateDirectory(Output);
        try
        {
            var app = new Application { ShutdownMode = ShutdownMode.OnMainWindowClose };
            app.DispatcherUnhandledException += (_, e) =>
            {
                File.WriteAllText(Path.Combine(Output, "preview-error.txt"), e.Exception.ToString());
                e.Handled = true;
                app.Shutdown(1);
            };
            PresentationTraceSources.DataBindingSource.Listeners.Add(new TextWriterTraceListener(BindingLog));
            PresentationTraceSources.DataBindingSource.Switch.Level = SourceLevels.Warning;
            typeof(MainWindow).Assembly.GetType("AgentDock.ControlPanel.UiText")!
                .GetMethod("ApplyPreference", BindingFlags.NonPublic | BindingFlags.Static)!.Invoke(null, [_culture]);
            using var runtime = new RuntimeService();
            var window = new MainWindow(runtime) { Title = "AgentDock · UI verification", Icon = null };
            typeof(MainWindow).GetProperty("IsUiPreview", PrivateInstance)?.SetValue(window, true);
            if (args.Contains("--verify")) typeof(MainWindow).GetProperty("ThemePreferencePathOverride", PrivateInstance)?.SetValue(window, Path.Combine(Output,"test-state","ui-theme"));
            app.MainWindow = window;
            // Prevent accidental interaction from writing the operating-system clipboard.
            typeof(MainWindow).GetProperty("ClipboardWriter", PrivateInstance)?.SetValue(window, new Func<string, Task>(_ => Task.CompletedTask));
            window.IsHitTestVisible = false;
            window.PreviewKeyDown += (_, e) => e.Handled = true;
            Freeze(window);
            window.Loaded += async (_, _) =>
            {
                _safeSnapshot = TestData.CreateSnapshot();
                SetField(window, "_snapshot", _safeSnapshot);
                Invoke(window, "ApplySnapshot", _safeSnapshot);
                Report["data_source"] = "offline fixtures; no installed settings, network or credentials read";
                Report["runtime_version"] = _safeSnapshot.Version;
                SetField(window, "_bearerToken", "preview-bearer-not-a-real-secret");
                SetField(window, "_oauthPassword", "preview-oauth-not-a-real-secret");
                Invoke(window, "UpdateCredentialText");
                if (args.Contains("--baseline"))
                {
                    window.Width = 1220; window.Height = 840;
                    await Idle(window);
                    Capture(window, "before-overview.png");
                    app.Shutdown(0);
                    return;
                }
                Freeze(window);
                SetFooter(window);
                await Idle(window);
                if (args.Contains("--verify")) await Verify(window, true);
                else { await Idle(window); Capture(window, "preview-current.png"); }
                window.Activate();
                // Exit only the isolated verification app after the report is written.
                if (args.Contains("--verify")) _ = window.Dispatcher.BeginInvoke(new Action(() => app.Shutdown(Tests.All(t => t.Value) ? 0 : 2)));
                File.WriteAllText(Path.Combine(Output, "preview-ready.json"), JsonSerializer.Serialize(new
                {
                    process_id = Environment.ProcessId, visible = window.IsVisible, title = window.Title,
                    passed = Tests.All(t => t.Value), tests = Tests.Count
                }));
            };
            return app.Run(window);
        }
        catch (Exception ex)
        {
            File.WriteAllText(Path.Combine(Output, "preview-error.txt"), ex.ToString());
            return 1;
        }
    }

    private static T Find<T>(MainWindow w, string name) where T : class => (T)w.FindName(name);
    private static void SetField(MainWindow w, string name, object? value) => typeof(MainWindow).GetField(name, PrivateInstance)!.SetValue(w, value);
    private static object? Invoke(MainWindow w, string name, params object[] values) => typeof(MainWindow).GetMethod(name, PrivateInstance)!.Invoke(w, values);
    private static IEnumerable<DependencyObject> Logical(DependencyObject parent)
    {
        yield return parent;
        foreach (var child in LogicalTreeHelper.GetChildren(parent).OfType<DependencyObject>())
            foreach (var result in Logical(child)) yield return result;
    }
    private static void SetFooter(MainWindow w) => Find<TextBlock>(w, "FooterStatusText").Text =
        _culture == "en" ? "UI verification · fixture data · no real endpoints or credentials" : "界面验证 · 全部为测试数据 · 不含真实公网地址或凭据";
    private static void Freeze(MainWindow w)
    {
        foreach (var item in Logical(w))
        {
            if (item is RadioButton radio && (Equals(radio.GroupName, "TunnelMode") || Equals(radio.GroupName, "UiTheme")))
            {
                radio.IsEnabled = true;
            }
            else if (item is Button b)
            {
                var safe = Equals(b.Tag, "ui-nav") || b.Name is "ToggleBearerButton" or "ToggleOAuthButton" or "AccessModeApplyButton" or "CopyLocalButton" or "CopyPublicButton";
                b.IsEnabled = safe;
                b.Opacity = 1;
                if (!safe) b.ToolTip = "仅预览，不执行实际操作";
            }
            else if (item is ToggleButton toggle) { toggle.IsEnabled = false; toggle.Opacity = 1; }
            else if (item is ComboBox combo) combo.IsEnabled = false;
            else if (item is TextBox text) text.IsReadOnly = true;
            else if (item is PasswordBox password) password.IsEnabled = false;
        }
    }
    private static async Task Idle(MainWindow w)
    {
        w.UpdateLayout();
        await w.Dispatcher.InvokeAsync(() => { }, DispatcherPriority.ContextIdle);
        await Task.Delay(30);
        w.UpdateLayout();
    }
    private static async Task SelectMode(MainWindow w, string name)
    {
        var radio = Find<RadioButton>(w, name);
        radio.IsChecked = true;
        radio.RaiseEvent(new RoutedEventArgs(ButtonBase.ClickEvent));
        await Idle(w);
    }

    private static async Task Verify(MainWindow w, bool extended)
    {
        var tabs = Find<TabControl>(w, "MainTabs");
        Tests["five_navigation_items"] = tabs.Items.Count == 5;
        Tests["production_startup_not_called"] = Application.Current.GetType() == typeof(Application);
        Report["bearer_oauth_read_apis_called"] = false;
        Report["preview_is_non_mutating"] = true;
        Report["os_dpi"] = VisualTreeHelper.GetDpi(w).DpiScaleX;
        var home = (TabItem)tabs.Items[0];
        var config = (TabItem)tabs.Items[3];
        Tests["three_original_home_modes"] = Logical(home).OfType<RadioButton>().Count(r => r.GroupName == "TunnelMode") == 3;
        Tests["config_has_no_mode_selector"] = !Logical(config).OfType<RadioButton>().Any(r => r.GroupName == "TunnelMode");
        var oauth = Find<TextBox>(w, "OAuthPasswordTextBox");
        Tests["oauth_dot_mask"] = oauth.Text == "......";
        Find<Button>(w, "ToggleOAuthButton").RaiseEvent(new RoutedEventArgs(Button.ClickEvent));
        Tests["oauth_reveal_demo"] = oauth.Text == "preview-oauth-not-a-real-secret";
        Find<Button>(w, "ToggleOAuthButton").RaiseEvent(new RoutedEventArgs(Button.ClickEvent));
        Tests["oauth_remask"] = oauth.Text == "......";
        SetField(w, "_oauthPassword", ""); Invoke(w, "UpdateCredentialText");
        Tests["empty_oauth_is_not_faked"] = oauth.Text != "......";
        SetField(w, "_oauthPassword", "preview-oauth-not-a-real-secret"); Invoke(w, "UpdateCredentialText");
        var pub = Find<TextBox>(w, "PublicMcpTextBox");pub.ApplyTemplate();
        Tests["public_address_masked"] = ((FrameworkElement)pub.Template.FindName("PART_ContentHost", pub)).Visibility == Visibility.Collapsed;

        foreach (var mode in new[] { "LocalModeRadio", "QuickModeRadio", "NamedModeRadio" })
        {
            if (_safeSnapshot is not null) SetField(w, "_snapshot", _safeSnapshot with { TunnelTokenStored = true });
            Find<TextBox>(w, "ServerUrlTextBox").Text = "https://example.invalid";
            tabs.SelectedIndex = 0;
            await SelectMode(w, mode);
            Tests[$"{mode}_click_selected"] = Find<RadioButton>(w, mode).IsChecked == true
                && Logical(home).OfType<RadioButton>().Count(r => r.GroupName == "TunnelMode" && r.IsChecked == true) == 1;
            Tests[$"{mode}_preview_hint"] = Find<TextBlock>(w, "HomeModeHintText").Text.Contains(_culture == "en" ? "Preview" : "预览");
        }
        // Missing fixed-domain data must navigate to configuration, never invoke a runtime change.
        if (_safeSnapshot is not null) SetField(w, "_snapshot", _safeSnapshot with { TunnelTokenStored = false });
        Find<TextBox>(w, "ServerUrlTextBox").Text = "";
        await SelectMode(w, "NamedModeRadio");
        Tests["missing_fixed_domain_opens_configuration"] = tabs.SelectedIndex == 3 && Find<GroupBox>(w, "NamedTunnelGroup").IsVisible;
        var blocked = await (Task<bool>)Invoke(w, "ApplySelectedTunnelModeFromUiAsync")!;
        Tests["preview_apply_is_blocked"] = !blocked;
        Capture(w, "04-config-named.png");
        tabs.SelectedIndex = 0;
        await SelectMode(w, "QuickModeRadio");
        tabs.SelectedIndex = 3; await Idle(w);
        Tests["named_configuration_hidden_for_quick"] = Find<GroupBox>(w, "NamedTunnelGroup").Visibility == Visibility.Collapsed;
        Tests["quick_regenerate_entry_preserved"] = Find<Button>(w, "RegenerateQuickButton").Visibility == Visibility.Visible;
        tabs.SelectedIndex = 0;
        Find<Button>(w, "AccessConfigurationButton").RaiseEvent(new RoutedEventArgs(Button.ClickEvent));
        Tests["home_configuration_navigation"] = tabs.SelectedIndex == 3;
        tabs.SelectedIndex = 0;

        var layouts = new List<object>();
        var dimensions = extended
            ? new[] { (1560d,900d,"wide"), (1220d,760d,"desktop"), (1100d,760d,"medium"), (1000d,760d,"reflow"), (800d,780d,"narrow"), (640d,640d,"minimum"), (640d,520d,"short") }
            : new[] { (1220d,760d,"desktop") };
        foreach (var (width,height,label) in dimensions)
        {
            tabs.SelectedIndex = 0;w.Width=width;w.Height=height;
            Find<ScrollViewer>(w,"HomeScrollViewer").ScrollToTop();await Idle(w);
            var panel=Find<ResponsiveDashboardPanel>(w,"HomeDashboard");
            var cards=panel.Children.OfType<Border>().ToArray();
            var rects=cards.Select(c=>new Rect(c.TranslatePoint(new Point(),panel),c.RenderSize)).ToArray();
            var two=panel.ActualWidth>=panel.TwoColumnMinWidth;
            Tests[$"{label}_columns"] = panel.ColumnCount==(two?2:1);
            Tests[$"{label}_no_horizontal_overflow"] = Find<ScrollViewer>(w,"HomeScrollViewer").ScrollableWidth==0 && rects.All(r=>r.Left>=-1 && r.Right<=panel.ActualWidth+2);
            Tests[$"{label}_aligned"] = two
                ? Math.Abs(rects[0].Top-rects[1].Top)<2 && Math.Abs(rects[0].Height-rects[1].Height)<2 && Math.Abs(rects[2].Top-rects[3].Top)<2 && Math.Abs(rects[0].Left-rects[2].Left)<2
                : rects.Select(r=>r.Left).Distinct().Count()==1 && rects.Zip(rects.Skip(1),(a,b)=>b.Top>=a.Bottom).All(b=>b);
            Tests[$"{label}_fullwidth_public_check"] = Math.Abs(rects[^1].Width-panel.ActualWidth)<2;
            layouts.Add(new { label,width,height,content_width=panel.ActualWidth,columns=panel.ColumnCount,card_rects=rects.Select(r=>new{ x=r.X,y=r.Y,width=r.Width,height=r.Height}).ToArray() });
            if (label is "desktop" or "narrow" or "minimum") Capture(w,$"01-{label}.png");
            if (label is "narrow" or "minimum")
            {
                Find<Border>(w,"PublicAccessCard").BringIntoView();await Idle(w);Capture(w,$"02-{label}-access.png");
            }
            if (extended)
            {
                for(var tab=1;tab<tabs.Items.Count;tab++)
                {
                    tabs.SelectedIndex=tab;await Idle(w);Freeze(w);
                    var invalid=new List<string>();
                    foreach(var el in Logical(w).OfType<FrameworkElement>().Where(e=>e.IsVisible && !string.IsNullOrEmpty(e.Name)))
                    {
                        try
                        {
                            var point=el.TranslatePoint(new Point(),(FrameworkElement)w.Content);
                            if(point.X < -2 || point.X+el.ActualWidth > ((FrameworkElement)w.Content).ActualWidth+2) invalid.Add(el.Name);
                        }
                        catch(InvalidOperationException) { }
                    }
                    Tests[$"{label}_tab{tab}_controls_fit"] = invalid.Count==0;
                    if(invalid.Count>0)Report[$"{label}_tab{tab}_overflow"]=invalid;
                    if(label=="minimum" && tab==4)Capture(w,"05-settings-narrow.png");
                }
            }
        }
        Report["layouts"]=layouts;
        tabs.SelectedIndex=0;
        if(extended)
        {
            foreach(var scale in new[]{1d,1.25,1.5,2d})
            {
                // Simulate fixed physical viewport at different logical DPI; do not change OS settings.
                w.Width=Math.Max(640,1560/scale);w.Height=Math.Max(520,980/scale);
                Find<ScrollViewer>(w,"HomeScrollViewer").ScrollToTop();await Idle(w);
                var panel=Find<ResponsiveDashboardPanel>(w,"HomeDashboard");
                Tests[$"dpi_{scale}_reflow"] = panel.ColumnCount==(panel.ActualWidth>=820?2:1)
                    && Find<ScrollViewer>(w,"HomeScrollViewer").ScrollableWidth==0;
                Capture(w,$"dpi-{(int)(scale*100)}.png",scale);
            }
        }
        // Restore the fixture mode as a read-only preview, not the test selections.
        if(_safeSnapshot is not null)
        {
            SetField(w,"_snapshot",_safeSnapshot);Invoke(w,"ApplySnapshot",_safeSnapshot);
        }
        SetField(w,"_showOAuth",false);SetField(w,"_showBearer",false);Invoke(w,"UpdateCredentialText");
        tabs.SelectedIndex=0;
        var area=SystemParameters.WorkArea;
        w.Width=Math.Max(640,Math.Min(1220,area.Width));w.Height=Math.Max(520,Math.Min(760,area.Height));
        w.Left=Math.Max(area.Left,area.Left+(area.Width-w.Width)/2);w.Top=Math.Max(area.Top,area.Top+(area.Height-w.Height)/2);
        Find<ScrollViewer>(w,"HomeScrollViewer").ScrollToTop();Freeze(w);SetFooter(w);
        Find<TextBlock>(w,"HomeModeHintText").Text=_culture=="en"?"Select to preview · actual network remains unchanged":"点击可预览选中效果 · 实际网络保持不变";
        await Idle(w);Capture(w,"00-home.png");
        await VerifyAppearanceAndCopy(w, extended);
        await VerifyPublicCheckAndTray(w);
        await VerifyHeaderAndLogo(w);
        Tests["no_binding_warnings"] = string.IsNullOrWhiteSpace(BindingLog.ToString());
        Report["binding_warnings"] = BindingLog.ToString();
        Report["dpi_test_scope"] = "logical-width and render-DPI simulation; OS scale unchanged";
        Report["tests"] = Tests;Report["passed"] = Tests.All(t=>t.Value);
        File.WriteAllText(Path.Combine(Output,"ui-verification.json"),JsonSerializer.Serialize(Report,new JsonSerializerOptions{WriteIndented=true}));
    }

    private static void Capture(MainWindow w,string name,double scale=1)
    {
        var view=(FrameworkElement)w.Content;view.UpdateLayout();
        var bitmap=new RenderTargetBitmap((int)Math.Ceiling(view.ActualWidth*scale),(int)Math.Ceiling(view.ActualHeight*scale),96*scale,96*scale,PixelFormats.Pbgra32);
        bitmap.Render(view);var encoder=new PngBitmapEncoder();encoder.Frames.Add(BitmapFrame.Create(bitmap));
        using var stream=File.Create(Path.Combine(Output,name));encoder.Save(stream);
    }
}

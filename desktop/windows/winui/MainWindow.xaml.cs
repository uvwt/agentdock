using System.Runtime.InteropServices;
using Microsoft.UI.Windowing;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Windows.Graphics;

namespace AgentDock.ControlPanel;

internal sealed record SettingsNavigationRequest(RuntimeService Runtime, string Tag);

public sealed partial class MainWindow : Window
{
    private const int DwmwaUseImmersiveDarkMode = 20;
    private const int DwmwaUseImmersiveDarkModeBefore20H1 = 19;
    private const int DefaultWidth = 1060;
    private const int DefaultHeight = 720;
    private const int MinimumWidth = 900;
    private const int MinimumHeight = 620;

    private readonly RuntimeService _runtime;
    private readonly IntPtr _windowHandle;
    private readonly string? _initialSettingsPage;
    private string _themePreference = UiThemePreference.SystemPreference;
    private string? _pendingSettingsTag;

    internal bool AllowClose { get; set; }

    public MainWindow(RuntimeService runtime, string initialPage = "home", string? initialSettingsPage = null)
    {
        _runtime = runtime;
        _initialSettingsPage = initialSettingsPage;
        InitializeComponent();
        _windowHandle = WinRT.Interop.WindowNative.GetWindowHandle(this);
        _themePreference = UiThemePreference.ReadPreference();
        Root.ActualThemeChanged += Root_ActualThemeChanged;
        ApplyThemePreference(_themePreference);
        ResizeToLogicalSize(DefaultWidth, DefaultHeight);

        HomeNavigationItem.Content = UiText.Get("Home");
        ConnectionsNavigationItem.Content = UiText.Get("Connections");
        CapabilitiesNavigationItem.Content = UiText.Get("Capabilities");
        ActivityNavigationItem.Content = UiText.Get("Activity");
        SettingsNavigationItem.Content = UiText.Get("Settings");

        var iconPath = System.IO.Path.Combine(AppContext.BaseDirectory, "agentdock.ico");
        if (File.Exists(iconPath))
        {
            AppWindow.SetIcon(iconPath);
        }
        ContentFrame.Navigated += ContentFrame_Navigated;
        AppWindow.Changed += (_, args) =>
        {
            if (!args.DidSizeChange) return;
            var size = AppWindow.Size;
            var minimumWidth = LogicalToPixels(MinimumWidth);
            var minimumHeight = LogicalToPixels(MinimumHeight);
            if (size.Width < minimumWidth || size.Height < minimumHeight)
            {
                AppWindow.Resize(new SizeInt32(Math.Max(size.Width, minimumWidth), Math.Max(size.Height, minimumHeight)));
            }
        };
        AppWindow.Closing += (_, args) =>
        {
            if (AllowClose) return;
            args.Cancel = true;
            AppWindow.Hide();
        };
        NavigateTo(initialPage);
    }

    internal void ShowAndActivate()
    {
        AppWindow.Show();
        Activate();
        _ = SetForegroundWindow(_windowHandle);
    }

    private void ResizeToLogicalSize(int width, int height) =>
        AppWindow.Resize(new SizeInt32(LogicalToPixels(width), LogicalToPixels(height)));

    private int LogicalToPixels(int logicalSize)
    {
        var dpi = GetDpiForWindow(_windowHandle);
        if (dpi == 0) dpi = 96;
        return (int)Math.Round(logicalSize * dpi / 96d);
    }

    [DllImport("user32.dll")]
    private static extern uint GetDpiForWindow(IntPtr hwnd);

    [DllImport("user32.dll")]
    private static extern bool SetForegroundWindow(IntPtr hwnd);

    [DllImport("dwmapi.dll")]
    private static extern int DwmSetWindowAttribute(
        IntPtr hwnd,
        int attribute,
        ref int attributeValue,
        int attributeSize);

    private void Navigation_SelectionChanged(NavigationView sender, NavigationViewSelectionChangedEventArgs args)
    {
        if (args.SelectedItemContainer?.Tag is not string tag) return;
        ContentFrame.Navigate(PageForTag(tag), NavigationParameterForTag(tag));
    }

    private void ContentFrame_Navigated(object sender, Microsoft.UI.Xaml.Navigation.NavigationEventArgs e)
    {
        if (e.Content is HomePage home)
        {
            home.ShortcutRequested += (_, tag) => NavigateTo(tag);
        }
        else if (e.Content is ConnectionsPage connections)
        {
            connections.AdvancedSettingsRequested += (_, _) => NavigateToSettings("advancedConnection");
        }

        if (e.Content is SettingsPage settings && !string.IsNullOrWhiteSpace(_initialSettingsPage))
        {
            settings.SelectPage(_initialSettingsPage);
        }
    }

    private void NavigateToSettings(string tag)
    {
        _pendingSettingsTag = tag;
        NavigateTo("settings");
    }

    internal void ApplyThemePreference(string preference)
    {
        _themePreference = UiThemePreference.NormalizePreference(preference);
        Root.RequestedTheme = UiThemePreference.ToElementTheme(_themePreference);
        ApplyNativeTitleBarTheme();
    }

    private void Root_ActualThemeChanged(FrameworkElement sender, object args) =>
        ApplyNativeTitleBarTheme();

    private void ApplyNativeTitleBarTheme()
    {
        // XAML 主题只覆盖客户区；原生标题栏属于非客户区，需要同步 DWM。
        // system 模式使用 ActualTheme，系统深浅色变化时也能实时跟随。
        var enabled = Root.ActualTheme == ElementTheme.Dark ? 1 : 0;
        var result = DwmSetWindowAttribute(
            _windowHandle,
            DwmwaUseImmersiveDarkMode,
            ref enabled,
            sizeof(int));
        if (result < 0)
        {
            _ = DwmSetWindowAttribute(
                _windowHandle,
                DwmwaUseImmersiveDarkModeBefore20H1,
                ref enabled,
                sizeof(int));
        }
    }

    private void NavigateTo(string tag)
    {
        var item = Navigation.MenuItems
            .OfType<NavigationViewItem>()
            .FirstOrDefault(candidate => string.Equals(candidate.Tag?.ToString(), tag, StringComparison.Ordinal));
        if (item is null) return;
        if (!ReferenceEquals(Navigation.SelectedItem, item))
        {
            Navigation.SelectedItem = item;
            return;
        }
        ContentFrame.Navigate(PageForTag(tag), NavigationParameterForTag(tag));
    }

    private object NavigationParameterForTag(string tag)
    {
        if (!string.Equals(tag, "settings", StringComparison.Ordinal)) return _runtime;
        if (_pendingSettingsTag is null) return _runtime;
        var target = _pendingSettingsTag;
        _pendingSettingsTag = null;
        return new SettingsNavigationRequest(_runtime, target);
    }

    private static Type PageForTag(string tag) => tag switch
    {
        "connections" => typeof(ConnectionsPage),
        "capabilities" => typeof(CapabilitiesPage),
        "activity" => typeof(ActivityPage),
        "settings" => typeof(SettingsPage),
        _ => typeof(HomePage)
    };
}

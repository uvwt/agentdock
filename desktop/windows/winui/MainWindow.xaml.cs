using System.Runtime.InteropServices;
using Microsoft.UI.Windowing;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Windows.Graphics;

namespace AgentDock.ControlPanel;

public sealed partial class MainWindow : Window
{
    private const int DefaultWidth = 1060;
    private const int DefaultHeight = 720;
    private const int MinimumWidth = 900;
    private const int MinimumHeight = 620;

    private readonly RuntimeService _runtime;
    private readonly IntPtr _windowHandle;
    private readonly string? _initialSettingsPage;
    private bool _initialSizeApplied;

    internal bool AllowClose { get; set; }

    public MainWindow(RuntimeService runtime, string initialPage = "home", string? initialSettingsPage = null)
    {
        _runtime = runtime;
        _initialSettingsPage = initialSettingsPage;
        InitializeComponent();
        ApplyThemePreference(UiThemePreference.ReadPreference());

        HomeNavigationItem.Content = UiText.Get("Home");
        ConnectionsNavigationItem.Content = UiText.Get("Connections");
        CapabilitiesNavigationItem.Content = UiText.Get("Capabilities");
        ActivityNavigationItem.Content = UiText.Get("Activity");
        SettingsNavigationItem.Content = UiText.Get("Settings");

        _windowHandle = WinRT.Interop.WindowNative.GetWindowHandle(this);
        Navigation.Loaded += Navigation_Loaded;
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

    private void Navigation_Loaded(object sender, RoutedEventArgs e)
    {
        if (_initialSizeApplied) return;
        _initialSizeApplied = true;
        Navigation.Loaded -= Navigation_Loaded;
        ResizeToLogicalSize(DefaultWidth, DefaultHeight);
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

    private void Navigation_SelectionChanged(NavigationView sender, NavigationViewSelectionChangedEventArgs args)
    {
        if (args.SelectedItemContainer?.Tag is not string tag) return;
        ContentFrame.Navigate(PageForTag(tag), _runtime);
    }

    private void ContentFrame_Navigated(object sender, Microsoft.UI.Xaml.Navigation.NavigationEventArgs e)
    {
        if (e.Content is HomePage home)
        {
            home.ShortcutRequested += (_, tag) => NavigateTo(tag);
        }
        if (e.Content is SettingsPage settings && !string.IsNullOrWhiteSpace(_initialSettingsPage))
        {
            settings.SelectPage(_initialSettingsPage);
        }
    }

    internal void ApplyThemePreference(string preference)
    {
        Navigation.RequestedTheme = UiThemePreference.ToElementTheme(preference);
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
        ContentFrame.Navigate(PageForTag(tag), _runtime);
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

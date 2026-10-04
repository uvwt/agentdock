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

    private readonly RuntimeService _runtime = new();
    private readonly IntPtr _windowHandle;
    private bool _initialSizeApplied;

    public MainWindow()
    {
        InitializeComponent();

        HomeNavigationItem.Content = UiText.Get("Home");
        ConnectionsNavigationItem.Content = UiText.Get("Connections");
        CapabilitiesNavigationItem.Content = UiText.Get("Capabilities");
        ActivityNavigationItem.Content = UiText.Get("Activity");
        SettingsNavigationItem.Content = UiText.Get("Settings");

        _windowHandle = WinRT.Interop.WindowNative.GetWindowHandle(this);
        Navigation.Loaded += Navigation_Loaded;

        AppWindow.Changed += (_, args) =>
        {
            if (!args.DidSizeChange) return;

            var size = AppWindow.Size;
            var minimumWidth = LogicalToPixels(MinimumWidth);
            var minimumHeight = LogicalToPixels(MinimumHeight);
            if (size.Width < minimumWidth || size.Height < minimumHeight)
            {
                AppWindow.Resize(new SizeInt32(
                    Math.Max(size.Width, minimumWidth),
                    Math.Max(size.Height, minimumHeight)));
            }
        };
        Navigation.SelectedItem = Navigation.MenuItems[0];
        ContentFrame.Navigate(typeof(HomePage), _runtime);
    }


    private void Navigation_Loaded(object sender, RoutedEventArgs e)
    {
        if (_initialSizeApplied) return;

        _initialSizeApplied = true;
        Navigation.Loaded -= Navigation_Loaded;
        ResizeToLogicalSize(DefaultWidth, DefaultHeight);
    }

    private void ResizeToLogicalSize(int width, int height)
    {
        AppWindow.Resize(new SizeInt32(LogicalToPixels(width), LogicalToPixels(height)));
    }

    private int LogicalToPixels(int logicalSize)
    {
        var dpi = GetDpiForWindow(_windowHandle);
        if (dpi == 0) dpi = 96;
        return (int)Math.Round(logicalSize * dpi / 96d);
    }

    [DllImport("user32.dll")]
    private static extern uint GetDpiForWindow(IntPtr hwnd);

    private void Navigation_SelectionChanged(NavigationView sender, NavigationViewSelectionChangedEventArgs args)
    {
        if (args.SelectedItemContainer?.Tag is not string tag) return;
        var page = tag switch
        {
            "home" => typeof(HomePage),
            "connections" => typeof(ConnectionsPage),
            "capabilities" => typeof(CapabilitiesPage),
            "activity" => typeof(ActivityPage),
            "settings" => typeof(SettingsPage),
            _ => typeof(HomePage)
        };
        ContentFrame.Navigate(page, _runtime);
    }
}

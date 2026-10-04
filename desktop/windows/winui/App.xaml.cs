using Microsoft.UI.Xaml;

namespace AgentDock.ControlPanel;

public partial class NativeApp : Application
{
    private MainWindow? _window;

    public NativeApp()
    {
        UiText.ConfigureCurrentUICulture();
        InitializeComponent();
    }

    protected override void OnLaunched(LaunchActivatedEventArgs args)
    {
        _window = new MainWindow();
        _window.Activate();
    }
}

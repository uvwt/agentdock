using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Navigation;

namespace AgentDock.ControlPanel;

public sealed partial class PlaceholderPage : Page
{
    public PlaceholderPage() { InitializeComponent(); }
    protected override void OnNavigatedTo(NavigationEventArgs e)
    {
        TitleText.Text = (e.Parameter as string) switch
        {
            "connections" => "连接",
            "capabilities" => "能力",
            "activity" => "活动",
            _ => "AgentDock"
        };
    }
}

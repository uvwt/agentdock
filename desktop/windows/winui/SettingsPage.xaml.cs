using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Navigation;
using Microsoft.UI.Xaml.Shapes;

namespace AgentDock.ControlPanel;

public sealed partial class SettingsPage : Page
{
    private RuntimeService? _runtime;
    private RuntimeSnapshot? _snapshot;

    public SettingsPage() { InitializeComponent(); }

    protected override async void OnNavigatedTo(NavigationEventArgs e)
    {
        _runtime = e.Parameter as RuntimeService;
        if (_runtime is not null) _snapshot = await _runtime.GetSnapshotAsync(includeNexusConnection: false);

        if (SettingsNavigation.SelectedIndex < 0)
        {
            SettingsNavigation.SelectedIndex = 0;
        }
        else if (SettingsNavigation.SelectedItem is ListViewItem item && item.Tag is string tag)
        {
            Render(tag);
        }
    }

    private void SettingsNavigation_SelectionChanged(object sender, SelectionChangedEventArgs e)
    {
        if (SettingsContent is null) return;
        if (SettingsNavigation.SelectedItem is ListViewItem item && item.Tag is string tag) Render(tag);
    }

    private void Render(string tag)
    {
        SettingsContent.Children.Clear();

        var header = new StackPanel { Spacing = 5 };
        header.Children.Add(new TextBlock
        {
            Text = PageTitle(tag),
            FontSize = 20,
            FontWeight = Microsoft.UI.Text.FontWeights.SemiBold
        });
        header.Children.Add(new TextBlock
        {
            Text = PageDetail(tag),
            FontSize = 12.5,
            Opacity = 0.62
        });
        SettingsContent.Children.Add(header);

        var section = new SectionCard { Title = SectionTitle(tag) };
        var rows = new StackPanel();

        if (tag == "runtime")
        {
            rows.Children.Add(Row("状态", _snapshot?.CoreRunning == true ? "正在运行 ●" : "已停止"));
            rows.Children.Add(Divider());
            rows.Children.Add(Row("本地地址", _snapshot?.LocalMcpUrl ?? "—"));
            rows.Children.Add(Divider());

            var action = new Button
            {
                Content = _snapshot?.CoreRunning == true ? "停止" : "启动"
            };
            action.Click += RuntimeAction_Click;
            rows.Children.Add(ActionRow("服务", action));
        }
        else if (tag == "permissions")
        {
            rows.Children.Add(Row("AgentDock 权限", "由 Windows 管理"));
        }
        else if (tag == "startup")
        {
            rows.Children.Add(Row("Core 后台服务", _snapshot?.CoreStartupEnabled == true ? "已启用" : "未启用"));
            rows.Children.Add(Divider());
            rows.Children.Add(Row("托盘应用", _snapshot?.TrayStartupEnabled == true ? "已启用" : "未启用"));
        }
        else
        {
            rows.Children.Add(Row("认证令牌", "已保护"));
            rows.Children.Add(Divider());
            rows.Children.Add(Row("OAuth 密码", "已保护"));
        }

        section.SectionContent = rows;
        SettingsContent.Children.Add(section);
    }

    private async void RuntimeAction_Click(object sender, RoutedEventArgs e)
    {
        if (_runtime is null || sender is not Button button) return;

        button.IsEnabled = false;
        try
        {
            var running = _snapshot?.CoreRunning == true;
            await _runtime.RunCoreActionAsync(running ? "stop" : "start");
            _snapshot = await _runtime.GetSnapshotAsync(includeNexusConnection: false);
            Render("runtime");
        }
        finally
        {
            button.IsEnabled = true;
        }
    }

    private static string PageTitle(string tag) => tag switch
    {
        "permissions" => "Permissions",
        "startup" => "Startup",
        "credentials" => "Access Credentials",
        _ => "Runtime"
    };

    private static string PageDetail(string tag) => tag switch
    {
        "permissions" => "查看 Windows 管理的系统权限。",
        "startup" => "后台服务和托盘应用的启动行为。",
        "credentials" => "凭据继续由现有 AgentDock 配置管理。",
        _ => "本机 AgentDock Runtime 状态与服务控制。"
    };

    private static string SectionTitle(string tag) => tag switch
    {
        "permissions" => "系统权限",
        "startup" => "Startup",
        "credentials" => "Credentials",
        _ => "AgentDock Runtime"
    };

    private static Grid Row(string left, string right)
    {
        var grid = new Grid { Height = 46, Padding = new Thickness(13, 0, 13, 0) };
        grid.Children.Add(new TextBlock { Text = left, VerticalAlignment = VerticalAlignment.Center });
        grid.Children.Add(new TextBlock
        {
            Text = right,
            HorizontalAlignment = HorizontalAlignment.Right,
            VerticalAlignment = VerticalAlignment.Center,
            Opacity = 0.62
        });
        return grid;
    }

    private static Grid ActionRow(string left, UIElement trailing)
    {
        var grid = new Grid { Height = 46, Padding = new Thickness(13, 0, 13, 0) };
        grid.Children.Add(new TextBlock { Text = left, VerticalAlignment = VerticalAlignment.Center });
        if (trailing is FrameworkElement element)
        {
            element.HorizontalAlignment = HorizontalAlignment.Right;
            element.VerticalAlignment = VerticalAlignment.Center;
        }
        grid.Children.Add(trailing);
        return grid;
    }

    private static Rectangle Divider() => new()
    {
        Height = 1,
        Margin = new Thickness(13, 0, 0, 0),
        Fill = new Microsoft.UI.Xaml.Media.SolidColorBrush(Microsoft.UI.Colors.Gray),
        Opacity = 0.18
    };
}

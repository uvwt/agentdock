using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Navigation;

namespace AgentDock.ControlPanel;

public sealed partial class HomePage : Page
{
    private RuntimeService? _runtime;
    private RuntimeSnapshot? _snapshot;

    public HomePage() { InitializeComponent(); }

    protected override async void OnNavigatedTo(NavigationEventArgs e)
    {
        _runtime = e.Parameter as RuntimeService;
        await RefreshAsync();
    }

    private async Task RefreshAsync()
    {
        if (_runtime is null) return;
        _snapshot = await _runtime.GetSnapshotAsync(includeNexusConnection: true);
        var running = _snapshot.CoreRunning;
        RuntimeHeadline.Text = running ? "AgentDock 正在运行" : "AgentDock 已停止";
        RuntimeDescription.Text = running ? "本地服务已准备好，AI 客户端可以连接。" : "启动 AgentDock 后即可接受 AI 客户端连接。";
        RuntimeState.Text = running ? "正在运行 ●" : "已停止";
        McpState.Text = _snapshot.Healthy ? "已准备就绪 ●" : "不可用";
        NexusState.Text = _snapshot.NexusConnected ? "已连接 ●" : "未连接";
        RuntimeAction.Content = running ? "停止" : "启动";
    }

    private async void RuntimeAction_Click(object sender, RoutedEventArgs e)
    {
        if (_runtime is null || _snapshot is null) return;
        RuntimeAction.IsEnabled = false;
        try
        {
            await _runtime.RunCoreActionAsync(_snapshot.CoreRunning ? "stop" : "start");
            await RefreshAsync();
        }
        finally { RuntimeAction.IsEnabled = true; }
    }
}

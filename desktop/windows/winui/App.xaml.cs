using System.Drawing;
using System.Runtime.InteropServices;
using System.Threading;
using Microsoft.UI.Xaml;
using Forms = System.Windows.Forms;

namespace AgentDock.ControlPanel;

public partial class NativeApp : Application
{
    private const string AppUserModelId = "com.uvwt.agentdock.controlpanel";
    private const string MutexName = "Local\\AgentDock.ControlPanel.Singleton";
    private const string ShowEventName = "Local\\AgentDock.ControlPanel.Show";

    private Mutex? _singleInstanceMutex;
    private EventWaitHandle? _showEvent;
    private CancellationTokenSource? _showCancellation;
    private Forms.NotifyIcon? _notifyIcon;
    private Forms.ContextMenuStrip? _trayMenu;
    private RuntimeService? _runtime;
    private MainWindow? _window;
    private bool _ownsMutex;
    private readonly Microsoft.UI.Dispatching.DispatcherQueue _dispatcherQueue = Microsoft.UI.Dispatching.DispatcherQueue.GetForCurrentThread();

    public NativeApp()
    {
        UiText.ConfigureCurrentUICulture();
        InitializeComponent();
    }

    protected override void OnLaunched(LaunchActivatedEventArgs args)
    {
        var arguments = Environment.GetCommandLineArgs().Skip(1).ToArray();
        if (TryRunHelperMode(arguments)) return;

        _ = SetCurrentProcessExplicitAppUserModelID(AppUserModelId);
        var background = arguments.Any(value => string.Equals(value, "--background", StringComparison.OrdinalIgnoreCase));
        _singleInstanceMutex = new Mutex(true, MutexName, out var createdNew);
        _ownsMutex = createdNew;
        if (!createdNew)
        {
            if (!background)
            {
                using var show = new EventWaitHandle(false, EventResetMode.AutoReset, ShowEventName);
                show.Set();
            }
            Exit();
            return;
        }

        _runtime = new RuntimeService();
        CreateTray();
        StartShowListener();
        if (!background) ShowControlPanel();
        _ = ResumeUpdateProgressIfNeededAsync();
    }

    private bool TryRunHelperMode(string[] arguments)
    {
        if (arguments.Any(value => string.Equals(value, "--task-admin", StringComparison.OrdinalIgnoreCase)))
        {
            Environment.Exit(TaskAdminService.Run(arguments));
            return true;
        }

        if (TryGetRuntimeRoot(arguments, "--run-core-task", out var coreRoot))
        {
            RunHelperAndExit(async runtime => await runtime.RunElevatedCoreTaskAsync(), coreRoot);
            return true;
        }
        if (TryGetRuntimeRoot(arguments, "--run-elevated-agentdock", out var elevatedRoot))
        {
            RunHelperAndExit(runtime => runtime.RunElevatedNativeCommandHostAsync(arguments), elevatedRoot);
            return true;
        }
        if (TryGetRuntimeRoot(arguments, "--start-core", out var startCoreRoot))
        {
            RunHelperAndExit(async runtime => { await runtime.RunCoreStartupAsync(); return 0; }, startCoreRoot);
            return true;
        }
        if (TryGetRuntimeRoot(arguments, "--start-tunnel", out var startTunnelRoot))
        {
            RunHelperAndExit(async runtime => { await runtime.RunTunnelStartupAsync(); return 0; }, startTunnelRoot);
            return true;
        }
        return false;
    }

    private static bool TryGetRuntimeRoot(string[] arguments, string flag, out string runtimeRoot)
    {
        runtimeRoot = "";
        if (!arguments.Any(value => string.Equals(value, flag, StringComparison.OrdinalIgnoreCase))) return false;
        for (var index = 0; index < arguments.Length - 1; index++)
        {
            if (!string.Equals(arguments[index], "--runtime-root", StringComparison.OrdinalIgnoreCase)) continue;
            runtimeRoot = arguments[index + 1];
            return !string.IsNullOrWhiteSpace(runtimeRoot);
        }
        return false;
    }

    private static void RunHelperAndExit(Func<RuntimeService, Task<int>> operation, string runtimeRoot)
    {
        // Helper 模式没有窗口、托盘或其他 WinUI 生命周期根。OnLaunched 返回后进程可能立即退出，
        // 所以不能 fire-and-forget；改在线程池执行异步工作并同步等待，避免 UI SynchronizationContext
        // 死锁，同时保证 Core/Tunnel helper 真正完成后宿主进程才退出。
        var exitCode = Task.Run(async () =>
        {
            try
            {
                using var runtime = new RuntimeService(runtimeRoot);
                return await operation(runtime).ConfigureAwait(false);
            }
            catch (Exception ex)
            {
                try
                {
                    ControlPanelDiagnostics.RecordFailure(runtimeRoot, "winui-helper", "startup", ex);
                }
                catch
                {
                    // 诊断写入失败不能覆盖原始 helper 失败；退出码仍保持失败。
                }
                return 1;
            }
        }).GetAwaiter().GetResult();

        Environment.Exit(exitCode);
    }

    private void ShowControlPanel()
    {
        if (_runtime is null) return;
        if (_window is null)
        {
            _window = new MainWindow(_runtime);
        }
        _window.ShowAndActivate();
    }

    private void StartShowListener()
    {
        _showEvent = new EventWaitHandle(false, EventResetMode.AutoReset, ShowEventName);
        _showCancellation = new CancellationTokenSource();
        var token = _showCancellation.Token;
        _ = Task.Run(() =>
        {
            var handles = new WaitHandle[] { _showEvent, token.WaitHandle };
            while (!token.IsCancellationRequested)
            {
                if (WaitHandle.WaitAny(handles) == 0)
                {
                    _dispatcherQueue.TryEnqueue(ShowControlPanel);
                }
            }
        }, token);
    }

    private void CreateTray()
    {
        _trayMenu = new Forms.ContextMenuStrip();
        _trayMenu.Items.Add(UiText.Get("OpenAgentDock"), null, (_, _) => _dispatcherQueue.TryEnqueue(ShowControlPanel));
        _trayMenu.Items.Add(new Forms.ToolStripSeparator());
        _trayMenu.Items.Add(UiText.Get("StartAgentDock"), null, async (_, _) => await RunTrayActionAsync("start"));
        _trayMenu.Items.Add(UiText.Get("StopAgentDock"), null, async (_, _) => await RunTrayActionAsync("stop"));
        _trayMenu.Items.Add(UiText.Get("RestartAgentDock"), null, async (_, _) => await RunTrayActionAsync("restart"));
        _trayMenu.Items.Add(new Forms.ToolStripSeparator());
        _trayMenu.Items.Add(UiText.Get("OpenLogsFolder"), null, (_, _) => _runtime?.OpenLogsDirectory());
        _trayMenu.Items.Add(UiText.Get("OpenConfigFolder"), null, (_, _) => _runtime?.OpenConfigDirectory());
        _trayMenu.Items.Add(new Forms.ToolStripSeparator());
        _trayMenu.Items.Add(UiText.Get("ExitTray"), null, (_, _) => _dispatcherQueue.TryEnqueue(RequestExit));

        _notifyIcon = new Forms.NotifyIcon
        {
            Text = "AgentDock",
            Visible = true,
            Icon = LoadIcon(),
            ContextMenuStrip = _trayMenu
        };
        _notifyIcon.DoubleClick += (_, _) => _dispatcherQueue.TryEnqueue(ShowControlPanel);
    }

    internal void ApplyLanguagePreference(string preference)
    {
        UiText.SetPreference(preference);
        if (_runtime is null) return;

        var previousWindow = _window;
        _window = new MainWindow(_runtime, "settings", "appearance");
        if (previousWindow is not null)
        {
            previousWindow.AllowClose = true;
            previousWindow.Close();
        }

        _notifyIcon?.Dispose();
        _trayMenu?.Dispose();
        CreateTray();
        _window.ShowAndActivate();
    }

    internal void ApplyThemePreference(string preference)
    {
        UiThemePreference.SetPreference(preference);
        _window?.ApplyThemePreference(preference);
    }

    private async Task RunTrayActionAsync(string action)
    {
        if (_runtime is null) return;
        try { await _runtime.RunActionAsync(action); }
        catch (Exception ex)
        {
            _runtime.RecordControlPanelFailure("tray", action, ex);
            _notifyIcon?.ShowBalloonTip(5000, "AgentDock", ex.Message, Forms.ToolTipIcon.Error);
        }
    }

    private async Task ResumeUpdateProgressIfNeededAsync()
    {
        if (_runtime is null) return;
        try
        {
            var transaction = await _runtime.ReadUpdateUiHandoffTransactionAsync();
            if (transaction is null) return;
            var deadline = DateTimeOffset.UtcNow.AddMinutes(4);
            UpdateTerminalResult? result = null;
            while (DateTimeOffset.UtcNow < deadline)
            {
                result = await _runtime.ReadUpdateTerminalResultAsync(transaction.TransactionId);
                if (result is not null) break;
                await Task.Delay(250);
            }
            if (result is null)
            {
                _notifyIcon?.ShowBalloonTip(5000, "AgentDock", UiText.Get("UpdateTransactionResultTimeout"), Forms.ToolTipIcon.Warning);
                return;
            }
            var committed = result.State.Equals("committed", StringComparison.OrdinalIgnoreCase);
            var message = committed ? UiText.Get("UpdateCompleted")
                : result.State.Equals("rolled_back", StringComparison.OrdinalIgnoreCase) ? UiText.Get("UpdateRolledBack")
                : UiText.Get("UpdateFailed");
            _notifyIcon?.ShowBalloonTip(5000, "AgentDock", message, committed ? Forms.ToolTipIcon.Info : Forms.ToolTipIcon.Error);
            await _runtime.AcknowledgeUpdateUiHandoffAsync(transaction.TransactionId);
        }
        catch (Exception ex)
        {
            _runtime.RecordControlPanelFailure("update-handoff", "resume", ex);
        }
    }

    private void RequestExit()
    {
        _showCancellation?.Cancel();
        _notifyIcon?.Dispose();
        _trayMenu?.Dispose();
        if (_window is not null)
        {
            _window.AllowClose = true;
            _window.Close();
        }
        _runtime?.Dispose();
        if (_ownsMutex) _singleInstanceMutex?.ReleaseMutex();
        _singleInstanceMutex?.Dispose();
        Exit();
    }

    private static Icon LoadIcon()
    {
        var path = Path.Combine(AppContext.BaseDirectory, "agentdock.ico");
        return File.Exists(path) ? new Icon(path) : SystemIcons.Application;
    }

    [DllImport("shell32.dll", CharSet = CharSet.Unicode)]
    private static extern int SetCurrentProcessExplicitAppUserModelID(string AppID);
}

using System.Diagnostics;
using System.Drawing;
using System.IO;
using System.Runtime.InteropServices;
using System.Threading;
using System.Windows;
using System.Windows.Threading;
using Application = System.Windows.Application;
using Forms = System.Windows.Forms;

namespace AgentDock.ControlPanel;

public partial class App : System.Windows.Application
{
    private const string MutexName = "Local\\AgentDock.ControlPanel.Singleton";
    private const string ShowEventName = "Local\\AgentDock.ControlPanel.Show";
    private const string AppUserModelId = "com.uvwt.agentdock.controlpanel";

    [DllImport("shell32.dll", CharSet = CharSet.Unicode)]
    private static extern int SetCurrentProcessExplicitAppUserModelID(string appId);

    private Mutex? _singleInstanceMutex;
    private EventWaitHandle? _showEvent;
    private CancellationTokenSource? _showEventCancellation;
    private Forms.NotifyIcon? _notifyIcon;
    private Forms.ContextMenuStrip? _trayMenu;
    private DispatcherTimer? _trayStatusTimer;
    private RuntimeSnapshot? _traySnapshot;
    private bool _traySnapshotRefreshInProgress;
    private bool _updateInProgress;
    private bool _ownsSingleInstanceMutex;
    private bool _exitRequested;

    public RuntimeService Runtime { get; private set; } = null!;
    public MainWindow ControlPanelWindow { get; private set; } = null!;
    public bool ExitRequested => _exitRequested;

    protected override void OnStartup(StartupEventArgs e)
    {
        UiText.ConfigureCurrentUICulture();
        // 使用稳定的 AppUserModelID，避免升级后任务栏沿用旧版本的隐式图标缓存。
        _ = SetCurrentProcessExplicitAppUserModelID(AppUserModelId);
        base.OnStartup(e);

        if (e.Args.Any(argument => string.Equals(argument, "--task-admin", StringComparison.OrdinalIgnoreCase)))
        {
            ShutdownMode = ShutdownMode.OnExplicitShutdown;
            Environment.Exit(TaskAdminService.Run(e.Args));
            return;
        }
        if (e.Args.Any(argument => string.Equals(argument, "--run-core-task", StringComparison.OrdinalIgnoreCase)))
        {
            ShutdownMode = ShutdownMode.OnExplicitShutdown;
            if (TryGetStartupRuntimeRoot(e.Args, "--run-core-task", out var taskRuntimeRoot))
            {
                _ = RunCoreTaskAndExitAsync(taskRuntimeRoot);
            }
            else
            {
                Environment.Exit(2);
            }
            return;
        }
        if (TryGetStartupRuntimeRoot(e.Args, "--start-core", out var coreRuntimeRoot))
        {
            ShutdownMode = ShutdownMode.OnExplicitShutdown;
            _ = StartRuntimeComponentAndExitAsync(coreRuntimeRoot, "core");
            return;
        }
        if (TryGetStartupRuntimeRoot(e.Args, "--start-tunnel", out var tunnelRuntimeRoot))
        {
            ShutdownMode = ShutdownMode.OnExplicitShutdown;
            _ = StartRuntimeComponentAndExitAsync(tunnelRuntimeRoot, "tunnel");
            return;
        }

        _singleInstanceMutex = new Mutex(true, MutexName, out var createdNew);
        _ownsSingleInstanceMutex = createdNew;
        if (!createdNew)
        {
            using var existingEvent = new EventWaitHandle(false, EventResetMode.AutoReset, ShowEventName);
            existingEvent.Set();
            Shutdown();
            return;
        }

        ShutdownMode = ShutdownMode.OnExplicitShutdown;
        Runtime = new RuntimeService();
        ControlPanelWindow = new MainWindow(Runtime);
        MainWindow = ControlPanelWindow;

        CreateNotifyIcon();
        StartShowEventListener();

        var background = e.Args.Any(arg => string.Equals(arg, "--background", StringComparison.OrdinalIgnoreCase));
        if (!background)
        {
            ShowControlPanel();
        }
    }

    private static bool TryGetStartupRuntimeRoot(string[] arguments, string startupFlag, out string runtimeRoot)
    {
        runtimeRoot = "";
        if (!arguments.Any(argument => string.Equals(argument, startupFlag, StringComparison.OrdinalIgnoreCase)))
        {
            return false;
        }

        for (var index = 0; index < arguments.Length - 1; index++)
        {
            if (string.Equals(arguments[index], "--runtime-root", StringComparison.OrdinalIgnoreCase))
            {
                runtimeRoot = arguments[index + 1];
                break;
            }
        }
        return !string.IsNullOrWhiteSpace(runtimeRoot);
    }

    private async Task RunCoreTaskAndExitAsync(string runtimeRoot)
    {
        var exitCode = 1;
        try
        {
            using var runtime = new RuntimeService(runtimeRoot);
            exitCode = await runtime.RunElevatedCoreTaskAsync();
        }
        catch (Exception ex)
        {
            RecordBackgroundStartupFailure(runtimeRoot, "elevated-core", ex);
        }
        finally
        {
            Environment.Exit(exitCode);
        }
    }

    private async Task StartRuntimeComponentAndExitAsync(string runtimeRoot, string component)
    {
        try
        {
            using var runtime = new RuntimeService(runtimeRoot);
            if (component == "core")
            {
                await runtime.RunCoreStartupAsync();
            }
            else
            {
                await runtime.RunTunnelStartupAsync();
            }
        }
        catch (Exception ex)
        {
            RecordBackgroundStartupFailure(runtimeRoot, component, ex);
        }
        finally
        {
            Shutdown();
        }
    }

    private static void RecordBackgroundStartupFailure(string runtimeRoot, string component, Exception exception)
    {
        try
        {
            var logsDirectory = Path.Combine(runtimeRoot, "logs");
            Directory.CreateDirectory(logsDirectory);
            var message = $"{DateTimeOffset.Now:O} {component} startup failed: {exception.Message}{Environment.NewLine}";
            File.AppendAllText(Path.Combine(logsDirectory, "control-panel.err.log"), message, new System.Text.UTF8Encoding(false));
        }
        catch
        {
            // 登录启动必须静默退出；无法写日志时不再制造第二个失败窗口。
        }
    }

    public void ShowControlPanel()
    {
        Dispatcher.Invoke(() =>
        {
            if (!ControlPanelWindow.IsVisible)
            {
                ControlPanelWindow.Show();
            }

            if (ControlPanelWindow.WindowState == WindowState.Minimized)
            {
                ControlPanelWindow.WindowState = WindowState.Normal;
            }

            ControlPanelWindow.Activate();
            ControlPanelWindow.Topmost = true;
            ControlPanelWindow.Topmost = false;
            ControlPanelWindow.Focus();
            _ = ControlPanelWindow.RefreshAsync();
        });
    }

    public async Task ApplyLanguagePreferenceAsync(string preference)
    {
        var previousPreference = UiText.ReadPreference();
        UiText.SetPreference(preference);
        try
        {
            var previousWindow = ControlPanelWindow;
            var wasVisible = previousWindow.IsVisible;
            var previousState = previousWindow.WindowState;
            var left = previousWindow.Left;
            var top = previousWindow.Top;

            var replacement = new MainWindow(Runtime)
            {
                Left = left,
                Top = top,
                WindowState = previousState == WindowState.Minimized ? WindowState.Normal : previousState
            };
            ControlPanelWindow = replacement;
            MainWindow = replacement;
            previousWindow.CloseForReplacement();

            if (_trayMenu is not null && !_trayMenu.Visible)
            {
                PopulateTrayMenu(_trayMenu, _traySnapshot);
            }
            if (wasVisible)
            {
                replacement.Show();
                replacement.Activate();
                await replacement.RefreshAsync();
            }
        }
        catch
        {
            UiText.SetPreference(previousPreference);
            throw;
        }
    }

    public void RequestExit()
    {
        _exitRequested = true;
        _showEventCancellation?.Cancel();
        _trayStatusTimer?.Stop();
        _trayMenu?.Dispose();
        _notifyIcon?.Dispose();
        ControlPanelWindow.Close();
        Shutdown();
    }

    private void StartShowEventListener()
    {
        _showEvent = new EventWaitHandle(false, EventResetMode.AutoReset, ShowEventName);
        _showEventCancellation = new CancellationTokenSource();
        var token = _showEventCancellation.Token;
        _ = Task.Run(() =>
        {
            var handles = new WaitHandle[] { _showEvent, token.WaitHandle };
            while (!token.IsCancellationRequested)
            {
                var signaled = WaitHandle.WaitAny(handles);
                if (signaled == 0)
                {
                    Dispatcher.BeginInvoke(ShowControlPanel);
                }
            }
        }, token);
    }

    private void CreateNotifyIcon()
    {
        _trayMenu = new Forms.ContextMenuStrip();
        PopulateTrayMenu(_trayMenu, null);

        _notifyIcon = new Forms.NotifyIcon
        {
            Text = "AgentDock",
            Visible = true,
            Icon = LoadIcon(),
            ContextMenuStrip = _trayMenu
        };
        _notifyIcon.DoubleClick += (_, _) => ShowControlPanel();

        // 系统托盘菜单必须挂到 NotifyIcon 上，不能手动 Show；否则会产生任务栏图标且失焦后不自动关闭。
        _trayStatusTimer = new DispatcherTimer(DispatcherPriority.Background)
        {
            Interval = TimeSpan.FromSeconds(3)
        };
        _trayStatusTimer.Tick += async (_, _) => await RefreshTraySnapshotAsync();
        _trayStatusTimer.Start();
        _ = RefreshTraySnapshotAsync();
    }

    private async Task RefreshTraySnapshotAsync()
    {
        if (_traySnapshotRefreshInProgress)
        {
            return;
        }

        _traySnapshotRefreshInProgress = true;
        try
        {
            _traySnapshot = await Runtime.GetSnapshotAsync();
            if (_notifyIcon is not null)
            {
                _notifyIcon.Text = TruncateNotifyIconText($"AgentDock: {GetTrayStatusText(_traySnapshot)}");
            }
            if (_trayMenu is not null && !_trayMenu.Visible)
            {
                PopulateTrayMenu(_trayMenu, _traySnapshot);
            }
        }
        catch
        {
            if (_notifyIcon is not null)
            {
                _notifyIcon.Text = $"AgentDock: {UiText.Get("StatusUnavailable")}";
            }
            if (_trayMenu is not null && !_trayMenu.Visible)
            {
                PopulateTrayMenu(_trayMenu, null);
            }
        }
        finally
        {
            _traySnapshotRefreshInProgress = false;
        }
    }

    private void PopulateTrayMenu(Forms.ContextMenuStrip menu, RuntimeSnapshot? snapshot)
    {
        while (menu.Items.Count > 0)
        {
            var oldItem = menu.Items[0];
            menu.Items.RemoveAt(0);
            oldItem.Dispose();
        }

        var statusText = snapshot is null ? UiText.Get("LoadingStatus") : GetTrayStatusText(snapshot);
        var status = new Forms.ToolStripMenuItem($"AgentDock: {statusText}") { Enabled = false };
        menu.Items.Add(status);
        menu.Items.Add(new Forms.ToolStripSeparator());

        menu.Items.Add(CreateMenuItem(UiText.Get("OpenAgentDock"), (_, _) => ShowControlPanel()));
        menu.Items.Add(new Forms.ToolStripSeparator());

        if (snapshot?.CoreRunning == true)
        {
            menu.Items.Add(CreateMenuItem(UiText.Get("StopAgentDock"), async (_, _) => await RunTrayActionAsync("stop")));
            menu.Items.Add(CreateMenuItem(UiText.Get("RestartAgentDock"), async (_, _) => await RunTrayActionAsync("restart")));
        }
        else
        {
            menu.Items.Add(CreateMenuItem(
                UiText.Get("StartAgentDock"),
                async (_, _) => await RunTrayActionAsync("start"),
                snapshot is not null));
        }
        menu.Items.Add(CreateMenuItem(
            _updateInProgress ? UiText.Get("CheckingForUpdates") : UiText.Get("CheckForUpdates"),
            async (_, _) => await RunTrayUpdateAsync(),
            snapshot is not null && !_updateInProgress));
        menu.Items.Add(new Forms.ToolStripSeparator());

        menu.Items.Add(CreateMenuItem(UiText.Get("OpenLogsFolder"), (_, _) => Runtime.OpenLogsDirectory()));
        menu.Items.Add(CreateMenuItem(UiText.Get("OpenConfigFolder"), (_, _) => Runtime.OpenConfigDirectory()));
        menu.Items.Add(CreateMenuItem(UiText.Get("OpenDocumentation"), (_, _) => OpenDocumentation()));
        menu.Items.Add(new Forms.ToolStripSeparator());
        menu.Items.Add(CreateMenuItem(UiText.Get("ExitTray"), (_, _) => Dispatcher.Invoke(RequestExit)));

        if (_notifyIcon is not null)
        {
            _notifyIcon.Text = TruncateNotifyIconText($"AgentDock: {statusText}");
        }
    }

    private static Forms.ToolStripMenuItem CreateMenuItem(string text, EventHandler onClick, bool enabled = true)
    {
        var item = new Forms.ToolStripMenuItem(text) { Enabled = enabled };
        item.Click += onClick;
        return item;
    }

    private static string GetTrayStatusText(RuntimeSnapshot snapshot)
    {
        if (snapshot.Healthy)
        {
            return UiText.Get("RunningNormally");
        }
        return snapshot.CoreRunning ? UiText.Get("ServiceError") : UiText.Get("Stopped");
    }

    private static void OpenDocumentation()
    {
        Process.Start(new ProcessStartInfo("https://uvwt.github.io/agentdock-docs/")
        {
            UseShellExecute = true
        });
    }

    private static string TruncateNotifyIconText(string value) => value.Length <= 63 ? value : value[..63];

    private Task RunTrayUpdateAsync() =>
        CheckForUpdatesAsync(ControlPanelWindow.IsVisible ? ControlPanelWindow : null);

    public async Task CheckForUpdatesAsync(Window? owner = null)
    {
        if (_updateInProgress)
        {
            return;
        }

        _updateInProgress = true;
        ControlPanelWindow.SetUpdateState(true, UiText.Get("PleaseWaitCheckingUpdates"));
        UpdateProgressWindow? progressWindow = null;
        try
        {
            var check = await Runtime.CheckForUpdatesAsync();
            ControlPanelWindow.SetUpdateStatus(check.Message);
            if (!check.UpdateAvailable)
            {
                ShowUpdateMessage(owner, check.Message, MessageBoxButton.OK, MessageBoxImage.Information);
                return;
            }

            var prompt = UiText.Format("NewVersionPrompt", check.CurrentVersion, check.LatestVersion);
            if (ShowUpdateMessage(owner, prompt, MessageBoxButton.YesNo, MessageBoxImage.Question) != MessageBoxResult.Yes)
            {
                ControlPanelWindow.SetUpdateStatus(UiText.Get("UpdateCancelled"));
                return;
            }

            progressWindow = new UpdateProgressWindow(check.CurrentVersion, check.LatestVersion);
            if (owner is { IsVisible: true })
            {
                progressWindow.Owner = owner;
            }
            else
            {
                progressWindow.WindowStartupLocation = WindowStartupLocation.CenterScreen;
            }
            progressWindow.Show();
            var progress = new Progress<UpdateProgress>(progressWindow.Report);

            var output = await Runtime.RunUpdateAsync(progress);
            await ControlPanelWindow.RefreshAsync();
            var completedMessage = LastNonEmptyLine(output, UiText.Get("UpdateCompleted"));
            progressWindow.Complete(completedMessage);
            ControlPanelWindow.SetUpdateStatus(completedMessage);
        }
        catch (Exception ex)
        {
            var message = LastNonEmptyLine(ex.Message, UiText.Get("UpdateCheckFailed"));
            ControlPanelWindow.SetUpdateStatus(message);
            if (progressWindow is null)
            {
                ShowUpdateMessage(owner, message, MessageBoxButton.OK, MessageBoxImage.Error);
            }
            else
            {
                progressWindow.Fail(message);
            }
        }
        finally
        {
            _updateInProgress = false;
            ControlPanelWindow.SetUpdateState(false);
            await RefreshTraySnapshotAsync();
        }
    }

    private static MessageBoxResult ShowUpdateMessage(
        Window? owner,
        string message,
        MessageBoxButton buttons,
        MessageBoxImage image)
    {
        return owner is { IsVisible: true }
            ? System.Windows.MessageBox.Show(owner, message, UiText.Get("UpdateWindowTitle"), buttons, image)
            : System.Windows.MessageBox.Show(message, UiText.Get("UpdateWindowTitle"), buttons, image);
    }

    private async Task RunTrayActionAsync(string action)
    {
        try
        {
            await Runtime.RunActionAsync(action);
            var refreshTask = await Dispatcher.InvokeAsync(ControlPanelWindow.RefreshAsync);
            await refreshTask;
            await RefreshTraySnapshotAsync();
        }
        catch (Exception ex)
        {
            _notifyIcon?.ShowBalloonTip(5000, "AgentDock", LastNonEmptyLine(ex.Message, UiText.Get("OperationFailed")), Forms.ToolTipIcon.Error);
        }
    }

    private static string LastNonEmptyLine(string value, string fallback)
    {
        var line = value
            .Split(['\r', '\n'], StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries)
            .LastOrDefault();
        return string.IsNullOrWhiteSpace(line) ? fallback : line;
    }

    private static Icon LoadIcon()
    {
        var adjacentIcon = Path.Combine(AppContext.BaseDirectory, "agentdock.ico");
        if (File.Exists(adjacentIcon))
        {
            return new Icon(adjacentIcon);
        }

        return Icon.ExtractAssociatedIcon(Environment.ProcessPath!) ?? SystemIcons.Application;
    }

    protected override void OnExit(ExitEventArgs e)
    {
        _showEventCancellation?.Cancel();
        _showEvent?.Dispose();
        _showEventCancellation?.Dispose();
        _trayStatusTimer?.Stop();
        _trayMenu?.Dispose();
        _notifyIcon?.Dispose();
        if (_ownsSingleInstanceMutex)
        {
            _singleInstanceMutex?.ReleaseMutex();
        }
        _singleInstanceMutex?.Dispose();
        Runtime?.Dispose();
        base.OnExit(e);
    }
}

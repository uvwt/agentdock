using System.ComponentModel;
using System.Runtime.InteropServices;

namespace AgentDock.ControlPanel;

internal enum TrayNotificationKind
{
    Info,
    Warning,
    Error
}

internal readonly record struct TrayIconBounds(int Left, int Top, int Right, int Bottom);

internal enum TrayCommand
{
    Open,
    CopyLocalMcp,
    CopyPublicMcp,
    RegenerateQuickTunnel,
    Start,
    Restart,
    CheckUpdates,
    Settings,
    OpenLogs,
    OpenRuntime,
    OpenDocumentation,
    Exit
}

internal sealed record TrayMenuState(
    bool Available,
    bool CoreRunning,
    bool Healthy,
    string Version)
{
    internal static TrayMenuState Unavailable { get; } = new(false, false, false, "");
}

internal sealed class TrayIconHost : IDisposable
{
    private const uint CallbackMessage = 0x8001;
    private const uint NimAdd = 0x00000000;
    private const uint NimModify = 0x00000001;
    private const uint NimDelete = 0x00000002;
    private const uint NifMessage = 0x00000001;
    private const uint NifIcon = 0x00000002;
    private const uint NifTip = 0x00000004;
    private const uint NifInfo = 0x00000010;
    private const uint WmLeftButtonUp = 0x0202;
    private const uint WmRightButtonUp = 0x0205;
    private const uint WmNull = 0x0000;
    private const uint NinSelect = 0x0400;
    private const uint NinKeySelect = 0x0401;
    private const uint ImageIcon = 1;
    private const uint LrLoadFromFile = 0x0010;
    private const uint LrDefaultSize = 0x0040;
    private const uint MfString = 0x0000;
    private const uint MfGrayed = 0x0001;
    private const uint MfDisabled = 0x0002;
    private const uint MfSeparator = 0x0800;
    private const uint TpmRightButton = 0x0002;
    private const uint TpmReturnCmd = 0x0100;
    private const uint OpenCommand = 1;
    private const uint CopyLocalMcpCommand = 2;
    private const uint CopyPublicMcpCommand = 3;
    private const uint RegenerateQuickTunnelCommand = 4;
    private const uint StartCommand = 5;
    private const uint RestartCommand = 6;
    private const uint CheckUpdatesCommand = 7;
    private const uint SettingsCommand = 8;
    private const uint OpenLogsCommand = 9;
    private const uint OpenRuntimeCommand = 10;
    private const uint OpenDocumentationCommand = 11;
    private const uint ExitCommand = 12;

    private readonly string _windowClassName = $"AgentDock.Tray.{Guid.NewGuid():N}";
    private readonly WindowProc _windowProc;
    private readonly IntPtr _instance;
    private readonly uint _iconId = 1;
    private readonly uint _taskbarCreatedMessage;
    private IntPtr _windowHandle;
    private IntPtr _iconHandle;
    private bool _ownsIcon;
    private bool _disposed;
    private TrayMenuState _menuState = TrayMenuState.Unavailable;

    internal event Action? PrimaryInvoked;
    internal event Action<TrayCommand>? CommandInvoked;

    internal TrayIconHost(string iconPath)
    {
        _windowProc = WindowProcedure;
        _instance = GetModuleHandleW(null);

        var windowClass = new WindowClassEx
        {
            cbSize = (uint)Marshal.SizeOf<WindowClassEx>(),
            lpfnWndProc = _windowProc,
            hInstance = _instance,
            lpszClassName = _windowClassName
        };
        if (RegisterClassExW(ref windowClass) == 0)
        {
            throw new Win32Exception(Marshal.GetLastWin32Error(), "Unable to register AgentDock tray window class.");
        }

        // Use a hidden top-level window rather than HWND_MESSAGE. Explorer broadcasts
        // TaskbarCreated only to top-level windows after a shell restart.
        _windowHandle = CreateWindowExW(
            0,
            _windowClassName,
            "AgentDockTrayHost",
            0,
            0,
            0,
            0,
            0,
            IntPtr.Zero,
            IntPtr.Zero,
            _instance,
            IntPtr.Zero);
        if (_windowHandle == IntPtr.Zero)
        {
            var error = Marshal.GetLastWin32Error();
            UnregisterClassW(_windowClassName, _instance);
            throw new Win32Exception(error, "Unable to create AgentDock tray window.");
        }

        _iconHandle = LoadImageW(
            IntPtr.Zero,
            iconPath,
            ImageIcon,
            0,
            0,
            LrLoadFromFile | LrDefaultSize);
        if (_iconHandle != IntPtr.Zero)
        {
            _ownsIcon = true;
        }
        else
        {
            _iconHandle = LoadIconW(IntPtr.Zero, new IntPtr(32512));
        }

        _taskbarCreatedMessage = RegisterWindowMessageW("TaskbarCreated");
        AddTrayIcon(throwOnFailure: true);
    }

    internal bool TryGetBounds(out TrayIconBounds bounds)
    {
        var identifier = new NotifyIconIdentifier
        {
            cbSize = (uint)Marshal.SizeOf<NotifyIconIdentifier>(),
            hWnd = _windowHandle,
            uID = _iconId,
            guidItem = Guid.Empty
        };

        if (Shell_NotifyIconGetRect(ref identifier, out var rect) >= 0)
        {
            bounds = new TrayIconBounds(rect.Left, rect.Top, rect.Right, rect.Bottom);
            return true;
        }

        if (GetCursorPos(out var point))
        {
            bounds = new TrayIconBounds(point.X, point.Y, point.X + 1, point.Y + 1);
            return true;
        }

        bounds = default;
        return false;
    }

    internal void UpdateMenuState(TrayMenuState state)
    {
        _menuState = state;
        if (_disposed || _windowHandle == IntPtr.Zero) return;

        var tooltip = state.Available
            ? state.Healthy ? "AgentDock · " + UiText.Get("RunningNormally")
            : state.CoreRunning ? "AgentDock · " + UiText.Get("ServiceError")
            : "AgentDock · " + UiText.Get("Stopped")
            : "AgentDock · " + UiText.Get("StatusUnavailable");
        if (!string.IsNullOrWhiteSpace(state.Version))
        {
            tooltip += " · " + state.Version.TrimStart('v');
        }

        var data = CreateNotifyIconData(NifTip);
        data.szTip = tooltip.Length > 127 ? tooltip[..127] : tooltip;
        _ = Shell_NotifyIconW(NimModify, ref data);
    }

    internal void ShowContextMenu()
    {
        if (_disposed || _windowHandle == IntPtr.Zero) return;

        var menu = CreatePopupMenu();
        if (menu == IntPtr.Zero) return;

        try
        {
            var state = _menuState;
            var status = state.Available
                ? state.Healthy ? UiText.Get("RunningNormally")
                : state.CoreRunning ? UiText.Get("ServiceError")
                : UiText.Get("Stopped")
                : UiText.Get("StatusUnavailable");
            if (!string.IsNullOrWhiteSpace(state.Version))
            {
                status += " · " + state.Version.TrimStart('v');
            }

            // 1. Status + version
            _ = AppendMenuW(menu, MfString | MfDisabled | MfGrayed, 0, status);
            _ = AppendMenuW(menu, MfSeparator, 0, null);

            // 2. Open AgentDock
            _ = AppendMenuW(menu, MfString, OpenCommand, UiText.Get("TrayShowMainWindow"));

            // 3-4. Core actions
            _ = AppendMenuW(menu, MfSeparator, 0, null);
            AppendCommand(menu, StartCommand, UiText.Get("TrayStartAgentDock"), state.Available && !state.CoreRunning);
            AppendCommand(menu, RestartCommand, UiText.Get("TrayRestartAgentDock"), state.Available);

            // 5-7. Settings, update, exit
            _ = AppendMenuW(menu, MfSeparator, 0, null);
            _ = AppendMenuW(menu, MfString, SettingsCommand, UiText.Get("Settings"));
            _ = AppendMenuW(menu, MfString, CheckUpdatesCommand, UiText.Get("TrayCheckForUpdates"));
            _ = AppendMenuW(menu, MfString, ExitCommand, UiText.Get("ExitTray"));

            if (!GetCursorPos(out var point)) return;

            _ = SetForegroundWindow(_windowHandle);
            var command = TrackPopupMenuEx(
                menu,
                TpmRightButton | TpmReturnCmd,
                point.X,
                point.Y,
                _windowHandle,
                IntPtr.Zero);
            _ = PostMessageW(_windowHandle, WmNull, IntPtr.Zero, IntPtr.Zero);

            var action = command switch
            {
                OpenCommand => TrayCommand.Open,
                StartCommand => TrayCommand.Start,
                RestartCommand => TrayCommand.Restart,
                CheckUpdatesCommand => TrayCommand.CheckUpdates,
                SettingsCommand => TrayCommand.Settings,
                ExitCommand => TrayCommand.Exit,
                _ => (TrayCommand?)null
            };
            if (action is not null)
            {
                CommandInvoked?.Invoke(action.Value);
            }
        }
        finally
        {
            _ = DestroyMenu(menu);
        }
    }

    private static void AppendCommand(IntPtr menu, uint command, string text, bool enabled)
    {
        var flags = enabled ? MfString : MfString | MfDisabled | MfGrayed;
        _ = AppendMenuW(menu, flags, command, text);
    }

    internal void ShowNotification(string title, string message, TrayNotificationKind kind)
    {
        if (_disposed || string.IsNullOrWhiteSpace(message)) return;

        var data = CreateNotifyIconData(NifInfo);
        data.szInfoTitle = title.Length > 63 ? title[..63] : title;
        data.szInfo = message.Length > 255 ? message[..255] : message;
        data.dwInfoFlags = kind switch
        {
            TrayNotificationKind.Warning => 0x00000002,
            TrayNotificationKind.Error => 0x00000003,
            _ => 0x00000001
        };
        _ = Shell_NotifyIconW(NimModify, ref data);
    }

    public void Dispose()
    {
        if (_disposed) return;
        _disposed = true;

        if (_windowHandle != IntPtr.Zero)
        {
            var data = CreateNotifyIconData(0);
            _ = Shell_NotifyIconW(NimDelete, ref data);
        }

        if (_ownsIcon && _iconHandle != IntPtr.Zero)
        {
            _ = DestroyIcon(_iconHandle);
        }

        if (_windowHandle != IntPtr.Zero)
        {
            _ = DestroyWindow(_windowHandle);
            _windowHandle = IntPtr.Zero;
        }

        if (_instance != IntPtr.Zero)
        {
            _ = UnregisterClassW(_windowClassName, _instance);
        }
    }

    private void AddTrayIcon(bool throwOnFailure)
    {
        if (_disposed || _windowHandle == IntPtr.Zero) return;

        // Keep the legacy notification callback contract here. It is the same
        // contract used by AgentDock's proven Win32 tray implementation and
        // gives us WM_LBUTTONUP / WM_RBUTTONUP directly.
        var data = CreateNotifyIconData(NifMessage | NifIcon | NifTip);
        if (Shell_NotifyIconW(NimAdd, ref data)) return;

        if (throwOnFailure)
        {
            throw new Win32Exception(Marshal.GetLastWin32Error(), "Unable to add AgentDock tray icon.");
        }
    }

    private NotifyIconData CreateNotifyIconData(uint flags) => new()
    {
        cbSize = (uint)Marshal.SizeOf<NotifyIconData>(),
        hWnd = _windowHandle,
        uID = _iconId,
        uFlags = flags,
        uCallbackMessage = CallbackMessage,
        hIcon = _iconHandle,
        szTip = "AgentDock",
        szInfo = "",
        szInfoTitle = ""
    };

    private IntPtr WindowProcedure(IntPtr hwnd, uint message, IntPtr wParam, IntPtr lParam)
    {
        if (_taskbarCreatedMessage != 0 && message == _taskbarCreatedMessage)
        {
            AddTrayIcon(throwOnFailure: false);
            return IntPtr.Zero;
        }

        if (message == CallbackMessage)
        {
            var notification = (uint)lParam.ToInt64();
            if (notification is WmLeftButtonUp or NinSelect or NinKeySelect)
            {
                PrimaryInvoked?.Invoke();
                return IntPtr.Zero;
            }

            if (notification == WmRightButtonUp)
            {
                ShowContextMenu();
                return IntPtr.Zero;
            }
        }

        return DefWindowProcW(hwnd, message, wParam, lParam);
    }

    private delegate IntPtr WindowProc(IntPtr hwnd, uint message, IntPtr wParam, IntPtr lParam);

    [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
    private struct WindowClassEx
    {
        public uint cbSize;
        public uint style;
        public WindowProc lpfnWndProc;
        public int cbClsExtra;
        public int cbWndExtra;
        public IntPtr hInstance;
        public IntPtr hIcon;
        public IntPtr hCursor;
        public IntPtr hbrBackground;
        public string? lpszMenuName;
        public string lpszClassName;
        public IntPtr hIconSm;
    }

    [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
    private struct NotifyIconData
    {
        public uint cbSize;
        public IntPtr hWnd;
        public uint uID;
        public uint uFlags;
        public uint uCallbackMessage;
        public IntPtr hIcon;

        [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 128)]
        public string szTip;

        public uint dwState;
        public uint dwStateMask;

        [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 256)]
        public string szInfo;

        public uint uTimeoutOrVersion;

        [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 64)]
        public string szInfoTitle;

        public uint dwInfoFlags;
        public Guid guidItem;
        public IntPtr hBalloonIcon;
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct NotifyIconIdentifier
    {
        public uint cbSize;
        public IntPtr hWnd;
        public uint uID;
        public Guid guidItem;
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct NativeRect
    {
        public int Left;
        public int Top;
        public int Right;
        public int Bottom;
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct NativePoint
    {
        public int X;
        public int Y;
    }

    [DllImport("kernel32.dll", CharSet = CharSet.Unicode)]
    private static extern IntPtr GetModuleHandleW(string? moduleName);

    [DllImport("user32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern ushort RegisterClassExW(ref WindowClassEx windowClass);

    [DllImport("user32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern bool UnregisterClassW(string className, IntPtr instance);

    [DllImport("user32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern IntPtr CreateWindowExW(
        uint extendedStyle,
        string className,
        string windowName,
        uint style,
        int x,
        int y,
        int width,
        int height,
        IntPtr parent,
        IntPtr menu,
        IntPtr instance,
        IntPtr parameter);

    [DllImport("user32.dll")]
    private static extern bool DestroyWindow(IntPtr hwnd);

    [DllImport("user32.dll", CharSet = CharSet.Unicode)]
    private static extern IntPtr DefWindowProcW(IntPtr hwnd, uint message, IntPtr wParam, IntPtr lParam);

    [DllImport("user32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern IntPtr LoadImageW(
        IntPtr instance,
        string name,
        uint type,
        int width,
        int height,
        uint load);

    [DllImport("user32.dll")]
    private static extern IntPtr LoadIconW(IntPtr instance, IntPtr iconName);

    [DllImport("user32.dll")]
    private static extern bool DestroyIcon(IntPtr icon);

    [DllImport("user32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern uint RegisterWindowMessageW(string message);

    [DllImport("shell32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern bool Shell_NotifyIconW(uint message, ref NotifyIconData data);

    [DllImport("shell32.dll")]
    private static extern int Shell_NotifyIconGetRect(ref NotifyIconIdentifier identifier, out NativeRect rect);

    [DllImport("user32.dll")]
    private static extern bool GetCursorPos(out NativePoint point);

    [DllImport("user32.dll")]
    private static extern IntPtr CreatePopupMenu();

    [DllImport("user32.dll", CharSet = CharSet.Unicode)]
    private static extern bool AppendMenuW(IntPtr menu, uint flags, uint commandId, string? text);

    [DllImport("user32.dll")]
    private static extern uint TrackPopupMenuEx(
        IntPtr menu,
        uint flags,
        int x,
        int y,
        IntPtr owner,
        IntPtr parameters);

    [DllImport("user32.dll")]
    private static extern bool DestroyMenu(IntPtr menu);

    [DllImport("user32.dll")]
    private static extern bool SetForegroundWindow(IntPtr hwnd);

    [DllImport("user32.dll", CharSet = CharSet.Unicode)]
    private static extern bool PostMessageW(IntPtr hwnd, uint message, IntPtr wParam, IntPtr lParam);
}

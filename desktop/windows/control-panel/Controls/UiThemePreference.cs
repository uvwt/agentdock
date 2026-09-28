using System.IO;
using System.Runtime.InteropServices;
using System.Windows;
using System.Windows.Interop;

namespace AgentDock.ControlPanel;

// UI preference only. Never reads or writes runtime/service configuration.
internal static class UiThemePreference
{
    internal static string Normalize(string? value) => value?.Trim() == "dark" ? "dark" : "light";
    internal static string Read(string path)
    {
        try { return Normalize(File.ReadAllText(path)); }
        catch (IOException) { return "light"; }
        catch (UnauthorizedAccessException) { return "light"; }
    }
    internal static void Save(string path, string theme)
    {
        Directory.CreateDirectory(Path.GetDirectoryName(path)!);
        var temp = path + "." + Guid.NewGuid().ToString("N") + ".tmp";
        try
        {
            File.WriteAllText(temp, Normalize(theme));
            File.Move(temp, path, overwrite: true);
        }
        finally { if (File.Exists(temp)) File.Delete(temp); }
    }
    internal static void ApplyTitleBar(Window window, bool dark)
    {
        // Native title-bar theming is available on Windows 11. Older systems keep their frame.
        if (!OperatingSystem.IsWindowsVersionAtLeast(10, 0, 22000)) return;
        var handle = new WindowInteropHelper(window).Handle;
        if (handle == IntPtr.Zero) return;
        var enabled = dark ? 1 : 0;
        _ = DwmSetWindowAttribute(handle, 20, ref enabled, sizeof(int));
        var caption = dark ? 0x003D302E : 0x00FFF9F4;
        var text = dark ? 0x00FCF4F2 : 0x00401B11;
        _ = DwmSetWindowAttribute(handle, 35, ref caption, sizeof(int));
        _ = DwmSetWindowAttribute(handle, 36, ref text, sizeof(int));
    }
    [DllImport("dwmapi.dll")]
    private static extern int DwmSetWindowAttribute(IntPtr hwnd, int attribute, ref int value, int size);
}

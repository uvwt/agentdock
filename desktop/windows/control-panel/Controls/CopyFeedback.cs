using System.Runtime.InteropServices;
using System.Windows;

namespace AgentDock.ControlPanel;

public static class CopyFeedback
{
    public static readonly DependencyProperty StateProperty = DependencyProperty.RegisterAttached(
        "State", typeof(string), typeof(CopyFeedback), new PropertyMetadata("idle"));
    public static string GetState(DependencyObject target) => (string)target.GetValue(StateProperty);
    public static void SetState(DependencyObject target, string state) => target.SetValue(StateProperty, state);
}

internal static class UiClipboard
{
    private static readonly SemaphoreSlim Gate = new(1, 1);
    internal static async Task WriteAsync(string text)
    {
        await Gate.WaitAsync();
        try
        {
            // Stay on the WPF STA thread. Retry a briefly busy clipboard without blocking UI.
            for (var attempt = 0; ; attempt++)
            {
                try { System.Windows.Clipboard.SetDataObject(text, true); return; }
                catch (ExternalException) when (attempt < 2) { await Task.Delay(70); }
            }
        }
        finally { Gate.Release(); }
    }
}

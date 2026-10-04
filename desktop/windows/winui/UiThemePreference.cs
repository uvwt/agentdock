using System.IO;
using Microsoft.UI.Xaml;

namespace AgentDock.ControlPanel;

internal static class UiThemePreference
{
    internal const string SystemPreference = "system";
    internal const string LightPreference = "light";
    internal const string DarkPreference = "dark";

    private static string PreferencePath => Path.Combine(
        Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
        "AgentDock",
        "ui-theme");

    internal static string ReadPreference()
    {
        try
        {
            return NormalizePreference(File.ReadAllText(PreferencePath));
        }
        catch (IOException)
        {
            return SystemPreference;
        }
        catch (UnauthorizedAccessException)
        {
            return SystemPreference;
        }
    }

    internal static void SetPreference(string preference)
    {
        var normalized = NormalizePreference(preference);
        if (normalized == SystemPreference)
        {
            File.Delete(PreferencePath);
            return;
        }

        var directory = Path.GetDirectoryName(PreferencePath)
            ?? throw new InvalidOperationException("AgentDock UI preference directory is unavailable.");
        Directory.CreateDirectory(directory);
        File.WriteAllText(PreferencePath, normalized);
    }

    internal static string NormalizePreference(string? value) =>
        value?.Trim().ToLowerInvariant() switch
        {
            LightPreference => LightPreference,
            DarkPreference => DarkPreference,
            _ => SystemPreference
        };

    internal static ElementTheme ToElementTheme(string preference) =>
        NormalizePreference(preference) switch
        {
            LightPreference => ElementTheme.Light,
            DarkPreference => ElementTheme.Dark,
            _ => ElementTheme.Default
        };
}

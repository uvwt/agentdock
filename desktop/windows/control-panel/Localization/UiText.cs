using System.Globalization;
using System.IO;
using System.Resources;
using System.Windows.Markup;

namespace AgentDock.ControlPanel;

internal static class UiText
{
    internal const string SystemPreference = "system";
    internal const string EnglishPreference = "en";
    internal const string SimplifiedChinesePreference = "zh-CN";

    private static readonly string SystemLocale = NormalizeCultureName(CultureInfo.CurrentUICulture.Name);
    private static readonly ResourceManager Resources = new(
        "AgentDock.ControlPanel.Resources.UiStrings",
        typeof(UiText).Assembly);

    private static string PreferencePath => Path.Combine(
        Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
        "AgentDock",
        "ui-language");

    public static void ConfigureCurrentUICulture()
    {
        ApplyPreference(ReadPreference());
    }

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
            ApplyPreference(normalized);
            return;
        }

        var directory = Path.GetDirectoryName(PreferencePath)
            ?? throw new InvalidOperationException("AgentDock UI preference directory is unavailable.");
        Directory.CreateDirectory(directory);
        File.WriteAllText(PreferencePath, normalized);
        ApplyPreference(normalized);
    }

    internal static string NormalizePreference(string? value)
    {
        return value?.Trim() switch
        {
            EnglishPreference => EnglishPreference,
            SimplifiedChinesePreference => SimplifiedChinesePreference,
            _ => SystemPreference
        };
    }

    internal static string ResolveLocale(string preference, string systemCultureName)
    {
        return NormalizePreference(preference) switch
        {
            EnglishPreference => EnglishPreference,
            SimplifiedChinesePreference => SimplifiedChinesePreference,
            _ => NormalizeCultureName(systemCultureName)
        };
    }

    internal static string NormalizeCultureName(string? value)
    {
        var locale = value?.Trim().ToLowerInvariant();
        if (string.IsNullOrEmpty(locale))
        {
            return EnglishPreference;
        }
        if (locale is "zh" or "zh-cn" or "zh-sg" or "zh-hans" || locale.StartsWith("zh-hans-", StringComparison.Ordinal))
        {
            return SimplifiedChinesePreference;
        }
        return EnglishPreference;
    }

    public static string Get(string key)
    {
        return Resources.GetString(key, CultureInfo.CurrentUICulture) ?? key;
    }

    public static string Format(string key, params object?[] args)
    {
        return string.Format(CultureInfo.CurrentCulture, Get(key), args);
    }

    private static void ApplyPreference(string preference)
    {
        var locale = ResolveLocale(preference, SystemLocale);
        var culture = CultureInfo.GetCultureInfo(locale);
        CultureInfo.CurrentUICulture = culture;
        CultureInfo.DefaultThreadCurrentUICulture = culture;
    }
}

[MarkupExtensionReturnType(typeof(string))]
internal sealed class LocExtension : MarkupExtension
{
    public LocExtension(string key)
    {
        Key = key;
    }

    [ConstructorArgument("key")]
    public string Key { get; set; }

    public override object ProvideValue(IServiceProvider serviceProvider)
    {
        return UiText.Get(Key);
    }
}

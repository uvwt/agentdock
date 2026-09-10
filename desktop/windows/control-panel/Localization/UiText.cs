using System.Globalization;
using System.Resources;
using System.Windows.Markup;

namespace AgentDock.ControlPanel;

internal static class UiText
{
    private const string EnglishLocale = "en";
    private const string SimplifiedChineseLocale = "zh-CN";
    private static readonly ResourceManager Resources = new(
        "AgentDock.ControlPanel.Resources.UiStrings",
        typeof(UiText).Assembly);

    public static void ConfigureCurrentUICulture()
    {
        var locale = NormalizeCultureName(CultureInfo.CurrentUICulture.Name);
        var culture = CultureInfo.GetCultureInfo(locale);
        CultureInfo.CurrentUICulture = culture;
        CultureInfo.DefaultThreadCurrentUICulture = culture;
    }

    internal static string NormalizeCultureName(string? value)
    {
        var locale = value?.Trim().ToLowerInvariant();
        if (string.IsNullOrEmpty(locale))
        {
            return EnglishLocale;
        }
        if (locale is "zh" or "zh-cn" or "zh-sg" or "zh-hans" || locale.StartsWith("zh-hans-", StringComparison.Ordinal))
        {
            return SimplifiedChineseLocale;
        }
        return EnglishLocale;
    }

    public static string Get(string key)
    {
        return Resources.GetString(key, CultureInfo.CurrentUICulture) ?? key;
    }

    public static string Format(string key, params object?[] args)
    {
        return string.Format(CultureInfo.CurrentCulture, Get(key), args);
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

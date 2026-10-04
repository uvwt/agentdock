using System.Windows.Markup;

namespace AgentDock.ControlPanel;

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

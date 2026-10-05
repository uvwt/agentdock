using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Markup;

namespace AgentDock.ControlPanel;

[ContentProperty(Name = nameof(SectionContent))]
public sealed partial class SectionCard : UserControl
{
    public static readonly DependencyProperty TitleProperty =
        DependencyProperty.Register(nameof(Title), typeof(string), typeof(SectionCard), new PropertyMetadata(""));
    public static readonly DependencyProperty SectionContentProperty =
        DependencyProperty.Register(nameof(SectionContent), typeof(object), typeof(SectionCard), new PropertyMetadata(null));

    public string Title { get => (string)GetValue(TitleProperty); set => SetValue(TitleProperty, value); }
    public object SectionContent { get => GetValue(SectionContentProperty); set => SetValue(SectionContentProperty, value); }
    public SectionCard() { InitializeComponent(); }
}

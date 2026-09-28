using System.Windows;
using Grid = System.Windows.Controls.Grid;
using Panel = System.Windows.Controls.Panel;
using Size = System.Windows.Size;

namespace AgentDock.ControlPanel;

/// <summary>Reflows cards using available device-independent width, not screen pixels.</summary>
public sealed class ResponsiveDashboardPanel : Panel
{
    public static readonly DependencyProperty TwoColumnMinWidthProperty = DependencyProperty.Register(
        nameof(TwoColumnMinWidth), typeof(double), typeof(ResponsiveDashboardPanel),
        new FrameworkPropertyMetadata(820d, FrameworkPropertyMetadataOptions.AffectsMeasure));
    public static readonly DependencyProperty GapProperty = DependencyProperty.Register(
        nameof(Gap), typeof(double), typeof(ResponsiveDashboardPanel),
        new FrameworkPropertyMetadata(14d, FrameworkPropertyMetadataOptions.AffectsMeasure));
    public static readonly DependencyProperty FullWidthProperty = DependencyProperty.RegisterAttached(
        "FullWidth", typeof(bool), typeof(ResponsiveDashboardPanel),
        new FrameworkPropertyMetadata(false, FrameworkPropertyMetadataOptions.AffectsParentMeasure));
    public double TwoColumnMinWidth { get => (double)GetValue(TwoColumnMinWidthProperty); set => SetValue(TwoColumnMinWidthProperty, value); }
    public double Gap { get => (double)GetValue(GapProperty); set => SetValue(GapProperty, value); }
    public static bool GetFullWidth(DependencyObject value) => (bool)value.GetValue(FullWidthProperty);
    public static void SetFullWidth(DependencyObject value, bool fullWidth) => value.SetValue(FullWidthProperty, fullWidth);
    public int ColumnCount { get; private set; } = 1;
    private readonly Dictionary<UIElement, Rect> _slots = new();
    private double _measuredWidth = -1;

    protected override Size MeasureOverride(Size availableSize) => Layout(availableSize.Width);
    private Size Layout(double availableWidth)
    {
        var width = double.IsFinite(availableWidth) ? Math.Max(0, availableWidth) : TwoColumnMinWidth;
        var gap = Math.Max(0, Gap);
        _measuredWidth = width;
        _slots.Clear();
        ColumnCount = width >= TwoColumnMinWidth ? 2 : 1;
        var children = InternalChildren.Cast<UIElement>().Where(c => c.Visibility != Visibility.Collapsed).ToArray();
        var y = 0d;
        for (var i = 0; i < children.Length;)
        {
            var first = children[i];
            var pair = ColumnCount == 2 && !GetFullWidth(first) && i + 1 < children.Length && !GetFullWidth(children[i + 1]);
            var cellWidth = pair ? Math.Max(0, (width - gap) / 2) : width;
            first.Measure(new Size(cellWidth, double.PositiveInfinity));
            var height = first.DesiredSize.Height;
            if (pair)
            {
                var second = children[i + 1];
                second.Measure(new Size(cellWidth, double.PositiveInfinity));
                height = Math.Max(height, second.DesiredSize.Height);
                _slots[second] = new Rect(cellWidth + gap, y, cellWidth, height);
            }
            _slots[first] = new Rect(0, y, cellWidth, height);
            y += height;
            i += pair ? 2 : 1;
            if (i < children.Length) y += gap;
        }
        return new Size(width, y);
    }
    protected override Size ArrangeOverride(Size finalSize)
    {
        if (Math.Abs(_measuredWidth - finalSize.Width) > 0.1) Layout(finalSize.Width);
        foreach (UIElement child in InternalChildren)
            child.Arrange(_slots.TryGetValue(child, out var slot) ? slot : new Rect(0, 0, 0, 0));
        return finalSize;
    }
}

/// <summary>
/// Preserves existing Grid.Row/Column label-value-action metadata. On narrow widths,
/// labels move above fields and actions wrap when necessary. No business state is held here.
/// </summary>
public sealed class ResponsiveFormPanel : Panel
{
    public static readonly DependencyProperty LabelWidthProperty = DependencyProperty.Register(
        nameof(LabelWidth), typeof(double), typeof(ResponsiveFormPanel),
        new FrameworkPropertyMetadata(150d, FrameworkPropertyMetadataOptions.AffectsMeasure));
    public static readonly DependencyProperty StackBelowWidthProperty = DependencyProperty.Register(
        nameof(StackBelowWidth), typeof(double), typeof(ResponsiveFormPanel),
        new FrameworkPropertyMetadata(500d, FrameworkPropertyMetadataOptions.AffectsMeasure));
    public double LabelWidth { get => (double)GetValue(LabelWidthProperty); set => SetValue(LabelWidthProperty, value); }
    public double StackBelowWidth { get => (double)GetValue(StackBelowWidthProperty); set => SetValue(StackBelowWidthProperty, value); }
    public bool IsStacked { get; private set; }
    private readonly Dictionary<UIElement, Rect> _slots = new();
    private double _measuredWidth = -1;

    protected override Size MeasureOverride(Size availableSize) => Layout(availableSize.Width);
    private Size Layout(double availableWidth)
    {
        var width = double.IsFinite(availableWidth) ? Math.Max(0, availableWidth) : 600;
        _measuredWidth = width;
        IsStacked = width < StackBelowWidth;
        _slots.Clear();
        var y = 0d;
        var rows = InternalChildren.Cast<UIElement>().Where(e => e.Visibility != Visibility.Collapsed)
            .GroupBy(Grid.GetRow).OrderBy(r => r.Key).ToArray();
        foreach (var group in rows)
        {
            var items = group.OrderBy(Grid.GetColumn).ToArray();
            if (items.Length == 1)
            {
                y += Place(items[0], 0, y, width) + 3;
                continue;
            }
            var label = items.FirstOrDefault(e => Grid.GetColumn(e) == 0);
            var value = items.FirstOrDefault(e => Grid.GetColumn(e) == 1);
            var actions = items.Where(e => Grid.GetColumn(e) >= 2).ToArray();
            var x = 0d;
            var labelHeight = 0d;
            if (label is not null)
            {
                if (IsStacked) y += Place(label, 0, y, width);
                else
                {
                    x = Math.Min(LabelWidth, width * 0.45);
                    labelHeight = Place(label, 0, y, x);
                }
            }
            var rest = Math.Max(0, width - x);
            var actionWidth = 0d;
            foreach (var action in actions)
            {
                action.Measure(new Size(rest, double.PositiveInfinity));
                actionWidth += action.DesiredSize.Width;
            }
            var actionWrap = actions.Length > 0 && rest - actionWidth < 145;
            var valueWidth = Math.Max(0, rest - (actionWrap ? 0 : actionWidth));
            var rowHeight = labelHeight;
            if (value is not null) rowHeight = Math.Max(rowHeight, Place(value, x, y, valueWidth));
            if (!actionWrap)
            {
                var actionX = x + valueWidth;
                foreach (var action in actions)
                {
                    var aw = Math.Min(action.DesiredSize.Width, width - actionX);
                    rowHeight = Math.Max(rowHeight, Place(action, actionX, y, aw));
                    actionX += aw;
                }
                // Give every item the full row height so VerticalAlignment=Center
                // aligns labels, fields and action icons instead of aligning their tops.
                foreach (var item in items)
                {
                    if (IsStacked && ReferenceEquals(item, label)) continue;
                    if (_slots.TryGetValue(item, out var rect))
                        _slots[item] = new Rect(rect.X, rect.Y, rect.Width, rowHeight);
                }
                y += rowHeight + 3;
            }
            else
            {
                y += rowHeight;
                foreach (var action in actions) y += Place(action, x, y, rest);
                y += 3;
            }
        }
        return new Size(width, Math.Max(0, y - (rows.Length > 0 ? 3 : 0)));
    }
    private double Place(UIElement element, double x, double y, double width)
    {
        width = Math.Max(0, width);
        element.Measure(new Size(width, double.PositiveInfinity));
        var height = element.DesiredSize.Height;
        _slots[element] = new Rect(x, y, width, height);
        return height;
    }
    protected override Size ArrangeOverride(Size finalSize)
    {
        if (Math.Abs(_measuredWidth - finalSize.Width) > 0.1) Layout(finalSize.Width);
        foreach (UIElement child in InternalChildren)
            child.Arrange(_slots.TryGetValue(child, out var slot) ? slot : new Rect(0, 0, 0, 0));
        return finalSize;
    }
}

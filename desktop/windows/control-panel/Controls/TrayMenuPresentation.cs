using System.Drawing;
using System.Drawing.Drawing2D;
using System.Windows;
using System.Windows.Media;
using Forms = System.Windows.Forms;
using DColor = System.Drawing.Color;
using DPoint = System.Drawing.Point;
using DSize = System.Drawing.Size;
using DPen = System.Drawing.Pen;

namespace AgentDock.ControlPanel;

internal sealed record TrayMenuState(string Status, bool? Running, bool Healthy, bool Updating, bool HasPublicAddress, string Theme);
internal sealed record TrayMenuActions(Action Dashboard, Action Tasks, Func<Task> CopyPublic, Func<Task> PublicCheck,
    Func<string, Task> Service, Func<Task> Update, Action Logs, Action Config, Action<string> Theme, Action Docs, Action Exit);

// Menu composition and drawing only. Application callbacks retain their original behavior.
internal static class TrayMenuPresentation
{
    private static readonly Font MenuFont = new("Microsoft YaHei UI", 9.5f, System.Drawing.FontStyle.Regular);

    internal static void Populate(Forms.ContextMenuStrip menu, TrayMenuState state, TrayMenuActions actions)
    {
        Clear(menu);
        Forms.ToolStripMenuItem Add(Forms.ToolStripItemCollection items, string name, string text, string? glyph, Action? action = null, bool enabled = true)
        {
            var item = new Forms.ToolStripMenuItem(text) { Name = name, Tag = glyph, Enabled = enabled, Padding = new Forms.Padding(8, 5, 12, 5) };
            if (action is not null) item.Click += (_, _) => action();
            items.Add(item);
            return item;
        }
        void Separator() => menu.Items.Add(new Forms.ToolStripSeparator { Margin = new Forms.Padding(6, 4, 6, 4) });
        Add(menu.Items, "status", $"AgentDock · {state.Status}", state.Healthy ? "status-ok" : "status-idle", enabled: false);
        Separator();
        Add(menu.Items, "dashboard", UiText.Get("UiTrayDashboard"), "home", actions.Dashboard);
        Add(menu.Items, "tasks", UiText.Get("UiTrayTaskCenter"), "tasks", actions.Tasks);
        Add(menu.Items, "copy-public", UiText.Get("UiTrayCopyPublic"), "copy", async () => await actions.CopyPublic(), state.HasPublicAddress);
        Add(menu.Items, "public-check", UiText.Get("PublicCheck"), "pulse", async () => await actions.PublicCheck());
        Separator();
        var service = Add(menu.Items, "service", UiText.Get("UiService"), "power");
        if (state.Running == true)
        {
            Add(service.DropDownItems, "stop", UiText.Get("StopAgentDock"), "stop", async () => await actions.Service("stop"));
            Add(service.DropDownItems, "restart", UiText.Get("RestartAgentDock"), "refresh", async () => await actions.Service("restart"));
        }
        else Add(service.DropDownItems, "start", UiText.Get("StartAgentDock"), "power", async () => await actions.Service("start"), state.Running.HasValue);
        var folders = Add(menu.Items, "folders", UiText.Get("UiTrayFolders"), "folder");
        Add(folders.DropDownItems, "logs", UiText.Get("OpenLogsFolder"), "file", actions.Logs);
        Add(folders.DropDownItems, "config", UiText.Get("OpenConfigFolder"), "folder", actions.Config);
        var appearance = Add(menu.Items, "appearance", UiText.Get("UiAppearance"), "theme");
        var light = Add(appearance.DropDownItems, "theme-light", UiText.Get("UiLightTheme"), null, () => actions.Theme("light"));
        var dark = Add(appearance.DropDownItems, "theme-dark", UiText.Get("UiDarkTheme"), null, () => actions.Theme("dark"));
        light.Checked = state.Theme != "dark";
        dark.Checked = state.Theme == "dark";
        Separator();
        var more = Add(menu.Items, "more", UiText.Get("UiTrayMore"), "more");
        Add(more.DropDownItems, "update", UiText.Get(state.Updating ? "CheckingForUpdates" : "CheckForUpdates"), "refresh", async () => await actions.Update(), state.Running.HasValue && !state.Updating);
        Add(more.DropDownItems, "docs", UiText.Get("OpenDocumentation"), "info", actions.Docs);
        var exit = Add(menu.Items, "exit", UiText.Get("ExitTray"), "exit", actions.Exit);
        exit.ToolTipText = UiText.Get("UiTrayExitHint");
        ApplyTheme(menu, state.Theme);
    }

    internal static void Clear(Forms.ToolStrip menu)
    {
        foreach (var item in menu.Items.Cast<Forms.ToolStripItem>().ToArray())
        {
            if (item is Forms.ToolStripDropDownItem parent && parent.HasDropDownItems) Clear(parent.DropDown);
            item.Image?.Dispose();
            item.Image = null;
            menu.Items.Remove(item);
            item.Dispose();
        }
    }

    internal static void ApplyTheme(Forms.ToolStrip menu, string theme)
    {
        var colors = TrayMenuColors.Load(theme);
        var renderer = new TrayMenuRenderer(colors);
        void Visit(Forms.ToolStrip strip)
        {
            strip.Renderer = renderer;
            strip.BackColor = colors.Surface;
            strip.ForeColor = colors.Ink;
            strip.Font = MenuFont;
            strip.Padding = new Forms.Padding(6, 5, 6, 5);
            strip.AutoSize = true;
            var scale = Math.Max(1, strip.DeviceDpi / 96f);
            strip.MinimumSize = new DSize((int)((ReferenceEquals(strip, menu) ? 236 : 180) * scale), 0);
            strip.ImageScalingSize = new DSize((int)(16 * scale), (int)(16 * scale));
            if (strip is Forms.ToolStripDropDownMenu popup)
            {
                popup.ShowImageMargin = true;
                popup.ShowCheckMargin = false;
                popup.DropShadowEnabled = true;
            }
            foreach (Forms.ToolStripItem item in strip.Items)
            {
                item.ForeColor = item.Enabled ? colors.Ink : colors.Muted;
                if (item is Forms.ToolStripMenuItem entry)
                {
                    if (entry.Name == "theme-light") entry.Checked = theme != "dark";
                    if (entry.Name == "theme-dark") entry.Checked = theme == "dark";
                    var oldImage = entry.Image;
                    entry.Image = entry.Tag is string glyph ? DrawIcon(glyph, colors, strip.ImageScalingSize.Width) : null;
                    oldImage?.Dispose();
                    if (entry.HasDropDownItems) Visit(entry.DropDown);
                }
            }
            strip.Invalidate();
        }
        Visit(menu);
    }

    private static Bitmap DrawIcon(string glyph, TrayMenuColors colors, int size)
    {
        var bitmap = new Bitmap(size, size);
        using var g = Graphics.FromImage(bitmap);
        g.SmoothingMode = SmoothingMode.AntiAlias;
        g.ScaleTransform(size / 20f, size / 20f);
        var ink = glyph == "status-ok" ? colors.Success : glyph == "status-idle" ? colors.Muted : colors.Ink;
        using var pen = new DPen(ink, 1.6f) { StartCap = LineCap.Round, EndCap = LineCap.Round, LineJoin = LineJoin.Round };
        void Line(float x, float y, float a, float b) => g.DrawLine(pen, x, y, a, b);
        void Lines(params PointF[] points) => g.DrawLines(pen, points);
        switch (glyph)
        {
            case "status-ok": case "status-idle":
                using (var brush = new SolidBrush(ink)) g.FillEllipse(brush, 6, 6, 8, 8); break;
            case "home": Lines(new(2, 9), new(10, 2), new(18, 9)); Lines(new(4, 8), new(4, 18), new(8, 18), new(8, 12), new(12, 12), new(12, 18), new(16, 18), new(16, 8)); break;
            case "tasks": case "file": g.DrawRectangle(pen, 4, 3, 12, 15); Line(7,7,13,7); Line(7,11,13,11); Line(7,15,11,15); break;
            case "copy": g.DrawRectangle(pen, 7, 6, 10, 12); Lines(new(13,3),new(3,3),new(3,14)); break;
            case "pulse": Lines(new(1,10),new(5,10),new(8,3),new(11,17),new(14,8),new(16,10),new(19,10)); break;
            case "power": g.DrawArc(pen, 3, 3, 14, 14, -55, 290); Line(10,2,10,10); break;
            case "stop": g.DrawRectangle(pen, 4, 4, 12, 12); break;
            case "refresh": g.DrawArc(pen, 3, 3, 14, 14, 35, 290); Lines(new(17,2),new(17,7),new(12,7)); break;
            case "folder": Lines(new(2,6),new(2,3),new(8,3),new(10,6),new(18,6),new(18,17),new(2,17),new(2,6),new(18,6)); break;
            case "theme": g.DrawEllipse(pen, 3, 3, 14, 14); using(var b = new SolidBrush(ink))g.FillPie(b, 5, 5, 10, 10, 90, 180); break;
            case "info": g.DrawEllipse(pen, 2, 2, 16, 16); Line(10,9,10,14); Line(10,6,10,6.2f); break;
            case "exit": Lines(new(8,3),new(3,3),new(3,17),new(8,17)); Line(8,10,18,10); Lines(new(14,6),new(18,10),new(14,14)); break;
            default: using(var b = new SolidBrush(ink)) foreach(var x in new[]{3,9,15})g.FillEllipse(b,x,8,2,2); break;
        }
        return bitmap;
    }
}

internal sealed record TrayMenuColors(DColor Surface, DColor Ink, DColor Muted, DColor Line, DColor Hover, DColor Accent, DColor Success)
{
    internal static TrayMenuColors Load(string theme)
    {
        var resources = new ResourceDictionary { Source = new Uri($"/agentdock-tray;component/Themes/{(theme == "dark" ? "Dark" : "Light")}.xaml", UriKind.Relative) };
        DColor C(string key)
        {
            var c = ((SolidColorBrush)resources[key]).Color;
            return DColor.FromArgb(c.A, c.R, c.G, c.B);
        }
        return new(C("UiSurface"),C("UiInk"),C("UiMuted"),C("UiLine"),C("UiHover"),C("UiAccentInk"),C("UiSuccess"));
    }
}

internal sealed class TrayColorTable(TrayMenuColors c) : Forms.ProfessionalColorTable
{
    public override DColor ToolStripDropDownBackground => c.Surface;
    public override DColor MenuBorder => c.Line;
    public override DColor MenuItemSelected => c.Hover;
    public override DColor MenuItemBorder => c.Hover;
    public override DColor ImageMarginGradientBegin => c.Surface;
    public override DColor ImageMarginGradientMiddle => c.Surface;
    public override DColor ImageMarginGradientEnd => c.Surface;
    public override DColor SeparatorDark => c.Line;
    public override DColor SeparatorLight => c.Line;
}

internal sealed class TrayMenuRenderer(TrayMenuColors colors) : Forms.ToolStripProfessionalRenderer(new TrayColorTable(colors) { UseSystemColors = false })
{
    protected override void OnRenderToolStripBackground(Forms.ToolStripRenderEventArgs e)
    {
        using var brush = new SolidBrush(colors.Surface);
        e.Graphics.FillRectangle(brush, e.AffectedBounds);
    }
    protected override void OnRenderImageMargin(Forms.ToolStripRenderEventArgs e) { }
    protected override void OnRenderMenuItemBackground(Forms.ToolStripItemRenderEventArgs e)
    {
        if (!(e.Item.Selected || e.Item.Pressed) || !e.Item.Enabled) return;
        var rect = new Rectangle(1,1,Math.Max(1,e.Item.Width-2),Math.Max(1,e.Item.Height-2));
        using var path = new GraphicsPath();
        var r = Math.Max(4, (int)(6 * (e.ToolStrip?.DeviceDpi ?? 96) / 96f));
        path.AddArc(rect.Left,rect.Top,r,r,180,90); path.AddArc(rect.Right-r,rect.Top,r,r,270,90);
        path.AddArc(rect.Right-r,rect.Bottom-r,r,r,0,90); path.AddArc(rect.Left,rect.Bottom-r,r,r,90,90);path.CloseFigure();
        using var brush = new SolidBrush(colors.Hover);
        var previous=e.Graphics.SmoothingMode;e.Graphics.SmoothingMode=SmoothingMode.AntiAlias;
        e.Graphics.FillPath(brush,path);e.Graphics.SmoothingMode=previous;
    }
    protected override void OnRenderItemText(Forms.ToolStripItemTextRenderEventArgs e)
    {
        e.TextColor=e.Item.Enabled?colors.Ink:colors.Muted;
        Forms.TextRenderer.DrawText(e.Graphics, e.Text, e.TextFont, e.TextRectangle, e.TextColor, e.TextFormat);
    }
    protected override void OnRenderItemImage(Forms.ToolStripItemImageRenderEventArgs e)
    {
        if (e.Image is not null) e.Graphics.DrawImage(e.Image, e.ImageRectangle);
    }
    protected override void OnRenderArrow(Forms.ToolStripArrowRenderEventArgs e)
    {
        e.ArrowColor=e.Item?.Enabled == true?colors.Ink:colors.Muted;base.OnRenderArrow(e);
    }
    protected override void OnRenderSeparator(Forms.ToolStripSeparatorRenderEventArgs e)
    {
        using var pen=new DPen(colors.Line);e.Graphics.DrawLine(pen,8,e.Item.Height/2,Math.Max(8,e.Item.Width-8),e.Item.Height/2);
    }
    protected override void OnRenderToolStripBorder(Forms.ToolStripRenderEventArgs e)
    {
        using var pen=new DPen(colors.Line);e.Graphics.DrawRectangle(pen,0,0,e.ToolStrip.Width-1,e.ToolStrip.Height-1);
    }
    protected override void OnRenderItemCheck(Forms.ToolStripItemImageRenderEventArgs e)
    {
        var b=e.ImageRectangle;using var pen=new DPen(colors.Accent,2f){StartCap=LineCap.Round,EndCap=LineCap.Round,LineJoin=LineJoin.Round};
        e.Graphics.SmoothingMode=SmoothingMode.AntiAlias;
        e.Graphics.DrawLines(pen,new[]{new DPoint(b.Left+2,b.Top+b.Height/2),new DPoint(b.Left+b.Width/2-1,b.Bottom-4),new DPoint(b.Right-2,b.Top+3)});
    }
}

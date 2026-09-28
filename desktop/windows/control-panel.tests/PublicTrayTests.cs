using System;
using System.IO;
using System.Linq;
using System.Collections.Generic;
using System.Reflection;
using System.Threading;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using AgentDock.ControlPanel;
using Forms = System.Windows.Forms;

internal static partial class Program
{
    private static string TextResource(string key) => (string)typeof(MainWindow).Assembly.GetType("AgentDock.ControlPanel.UiText")!.GetMethod("Get")!.Invoke(null, [key])!;
    private static string DetectionState(MainWindow w) => (string)typeof(MainWindow).GetProperty("CurrentPublicCheckState",PrivateInstance)!.GetValue(w)!;
    private static Task Probe(MainWindow w,string origin="https://example.invalid",bool force=true) => (Task)Invoke(w,"RunPublicCheckAsync",origin,"none",force)!;
    private static void SetProbe(MainWindow w, Func<string,CancellationToken,Task<UrlTestResult>>? probe) => typeof(MainWindow).GetProperty("PublicCheckProbeOverride",PrivateInstance)!.SetValue(w,probe);
    private static async Task VerifyPublicCheckAndTray(MainWindow w)
    {
        var tabs=Find<TabControl>(w,"MainTabs");tabs.SelectedIndex=0;await Idle(w);
        Tests["homepage_heatmap_removed"]=w.FindName("ActivityCard") is null && w.FindName("PublicCheckCard") is Border;
        Tests["public_check_moved_home"]=Logical((TabItem)tabs.Items[0]).OfType<TextBlock>().Any(b=>b.Name=="PublicTestStatusText")
            && !Logical((TabItem)tabs.Items[3]).OfType<TextBlock>().Any(b=>b.Name=="PublicTestStatusText");
        var count=0;
        SetProbe(w,(o,c)=>{count++;return Task.FromResult(new UrlTestResult(true,200,TimeSpan.FromMilliseconds(43),"fixture"));});
        await Probe(w,"");Tests["unconfigured_check_does_not_request"]=count==0 && DetectionState(w)=="empty";
        await Probe(w,"invalid");Tests["invalid_check_does_not_request"]=count==0 && DetectionState(w)=="error";
        await Probe(w);Tests["check_success_state"]=DetectionState(w)=="success" && Find<TextBlock>(w,"PublicCheckHttpText").Text=="200";
        Tests["check_real_elapsed_from_result"]=Find<TextBlock>(w,"PublicCheckElapsedText").Text=="43 ms";
        Tests["check_time_set"]=Find<TextBlock>(w,"PublicCheckTimeText").Text!="—";
        var previous=count;await Probe(w,force:false);Tests["automatic_check_throttles"]=count==previous;
        SetProbe(w,(o,c)=>Task.FromResult(new UrlTestResult(false,503,TimeSpan.FromMilliseconds(65),"fixture")));
        await Probe(w);Tests["check_http_failure"]=DetectionState(w)=="error" && Find<TextBlock>(w,"PublicCheckHttpText").Text=="503";
        SetProbe(w,(o,c)=>Task.FromResult(new UrlTestResult(false,null,TimeSpan.FromSeconds(10),TextResource("AccessTimeout"))));
        await Probe(w);Tests["check_timeout"]=Find<TextBlock>(w,"PublicTestStatusText").Text==TextResource("AccessTimeout");
        SetProbe(w,(o,c)=>Task.FromResult(new UrlTestResult(false,null,TimeSpan.FromMilliseconds(7),"https://private.invalid/secret-fixture")));
        await Probe(w);Tests["check_errors_do_not_echo_private_endpoint"]=!Find<TextBlock>(w,"PublicTestStatusText").Text.Contains("private.invalid");
        SetProbe(w,(o,c)=>Task.FromException<UrlTestResult>(new InvalidOperationException("fixture")));
        await Probe(w);Tests["check_exception_recovers_button"]=DetectionState(w)=="error" && Find<Button>(w,"TestPublicButton").IsEnabled;
        var pending=new TaskCompletionSource<UrlTestResult>();count=0;
        SetProbe(w,(o,c)=>{count++;return pending.Task;});
        var running=Probe(w);Tests["check_loading_state"]=DetectionState(w)=="checking" && !Find<Button>(w,"TestPublicButton").IsEnabled;
        await Probe(w);Tests["duplicate_check_is_not_sent"]=count==1;
        pending.SetResult(new UrlTestResult(true,200,TimeSpan.FromMilliseconds(12),""));await running;
        Tests["check_button_reenabled"]=Find<Button>(w,"TestPublicButton").IsEnabled;
        var stale=new TaskCompletionSource<UrlTestResult>();
        SetProbe(w,(o,c)=>o.EndsWith("old")?stale.Task:Task.FromResult(new UrlTestResult(true,200,TimeSpan.FromMilliseconds(4),"")));
        var old=Probe(w,"https://example.invalid/old");await Probe(w,"https://example.invalid/new");
        stale.SetResult(new UrlTestResult(false,503,TimeSpan.FromMilliseconds(9),""));await old;
        Tests["old_detection_result_cannot_overwrite_new"]=DetectionState(w)=="success" && Find<TextBlock>(w,"PublicCheckHttpText").Text=="200";
        SetProbe(w,null);await Probe(w);Tests["preview_does_not_probe_without_stub"]=DetectionState(w)=="idle";
        SetProbe(w,(o,c)=>Task.FromResult(new UrlTestResult(true,200,TimeSpan.FromMilliseconds(43),"fixture")));await Probe(w);
        Find<Border>(w,"PublicCheckCard").BringIntoView();await Idle(w);Capture(w,"20-home-detection.png");
        await VerifyTrayRendering(w);
        SetProbe(w,null);
    }

    private static async Task VerifyTrayRendering(MainWindow w)
    {
        var asm=typeof(MainWindow).Assembly;
        var factory=asm.GetType("AgentDock.ControlPanel.TrayMenuPresentation")!;
        var flags=BindingFlags.NonPublic|BindingFlags.Static;
        var stateType=asm.GetType("AgentDock.ControlPanel.TrayMenuState")!;
        var actionsType=asm.GetType("AgentDock.ControlPanel.TrayMenuActions")!;
        var calls=new List<string>();
        Action Mark(string name)=>()=>calls.Add(name);
        Func<Task> AsyncMark(string name)=>()=>{calls.Add(name);return Task.CompletedTask;};
        var actions=Activator.CreateInstance(actionsType,new object[]{Mark("dashboard"),Mark("tasks"),AsyncMark("copy"),AsyncMark("check"),
            new Func<string,Task>(s=>{calls.Add(s);return Task.CompletedTask;}),AsyncMark("update"),Mark("logs"),Mark("config"),new Action<string>(s=>calls.Add(s)),Mark("docs"),Mark("exit")})!;
        using var menu=new Forms.ContextMenuStrip();
        object State(bool? running,bool updating,bool hasPublic,string theme)=>Activator.CreateInstance(stateType,new object?[]{TextResource("RunningNormally"),running,running==true,updating,hasPublic,theme})!;
        void Populate(bool? running=true,bool updating=false,bool hasPublic=true,string theme="dark")=>factory.GetMethod("Populate",flags)!.Invoke(null,[menu,State(running,updating,hasPublic,theme),actions]);
        void Apply(string theme)=>factory.GetMethod("ApplyTheme",flags)!.Invoke(null,[menu,theme]);
        Forms.ToolStripMenuItem Item(string name)=>(Forms.ToolStripMenuItem)menu.Items.Find(name,true).Single();
        Populate();
        Tests["tray_top_level_groups"]=new[]{"status","dashboard","tasks","copy-public","public-check","service","folders","appearance","more","exit"}.All(n=>menu.Items.Find(n,false).Length==1);
        Tests["tray_original_actions_preserved"]=new[]{"stop","restart","update","logs","config","docs","exit"}.All(n=>menu.Items.Find(n,true).Length==1);
        foreach(var name in new[]{"dashboard","tasks","copy-public","public-check","stop","restart","logs","config","docs","exit","theme-light","theme-dark"})Item(name).PerformClick();
        Tests["tray_callbacks_route_correctly"]=new[]{"dashboard","tasks","copy","check","stop","restart","logs","config","docs","exit","light","dark"}.SequenceEqual(calls);
        Populate(running:false);Item("start").PerformClick();Tests["tray_stopped_mode_uses_start"]=calls.Last()=="start" && menu.Items.Find("stop",true).Length==0;
        Populate(running:null,hasPublic:false);Tests["tray_unknown_disables_start_and_copy"]=!Item("start").Enabled && !Item("copy-public").Enabled;
        Populate(updating:true);Tests["tray_update_busy_disabled"]=!Item("update").Enabled;
        Populate();
        foreach(var theme in new[]{"light","dark"})
        {
            Invoke(w,"ApplyUiTheme",theme,false);Apply(theme);
            var brush=((SolidColorBrush)w.FindResource("UiSurface")).Color;
            Tests[$"tray_{theme}_matches_window_palette"]=menu.BackColor.R==brush.R && menu.BackColor.G==brush.G && menu.BackColor.B==brush.B;
            Tests[$"tray_{theme}_selected_check"]=Item("theme-"+theme).Checked && !Item("theme-"+(theme=="dark"?"light":"dark")).Checked;
            Tests[$"tray_{theme}_submenus_themed"]=new[]{"service","folders","appearance","more"}.All(n=>Item(n).DropDown.BackColor==menu.BackColor && Item(n).DropDown.Renderer==menu.Renderer);
            var screen=Forms.Screen.PrimaryScreen!.WorkingArea;
            menu.Show(new System.Drawing.Point(screen.Left+60,screen.Top+70));await Task.Delay(80);
            using(var b=new System.Drawing.Bitmap(menu.Width,menu.Height)){menu.DrawToBitmap(b,new System.Drawing.Rectangle(0,0,b.Width,b.Height));b.Save(Path.Combine(Output,$"21-tray-{theme}.png"),System.Drawing.Imaging.ImageFormat.Png);}
            Item("appearance").ShowDropDown();await Task.Delay(60);
            var child=Item("appearance").DropDown;
            using(var b=new System.Drawing.Bitmap(child.Width,child.Height)){child.DrawToBitmap(b,new System.Drawing.Rectangle(0,0,b.Width,b.Height));b.Save(Path.Combine(Output,$"22-tray-{theme}-appearance.png"),System.Drawing.Imaging.ImageFormat.Png);}
            Tests[$"tray_{theme}_has_readable_size"]=menu.Width>=200 && menu.Height>250;
            child.Close();menu.Close();
        }
        Invoke(w,"ApplyUiTheme","dark",false);
        Find<TabControl>(w,"MainTabs").SelectedIndex=0;
        Find<ScrollViewer>(w,"HomeScrollViewer").ScrollToTop();await Idle(w);
        Capture(w,"23-home-final.png");
        factory.GetMethod("Clear",flags)!.Invoke(null,[menu]);
        Tests["tray_menu_items_dispose_cleanly"]=menu.Items.Count==0;
    }
}

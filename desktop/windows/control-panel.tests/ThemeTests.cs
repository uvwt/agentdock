using System.IO;
using System.Reflection;
using System.Runtime.InteropServices;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Controls.Primitives;
using System.Windows.Media;
using System.Windows.Media.Imaging;
using AgentDock.ControlPanel;

internal static partial class Program
{
    private static string BrushColor(object brush) => brush is SolidColorBrush solid ? solid.Color.ToString() : "";
    private static async Task ClickCopy(MainWindow w, Button button)
    {
        button.RaiseEvent(new RoutedEventArgs(Button.ClickEvent));
        await (Task)typeof(MainWindow).GetProperty("LastCopyOperation", PrivateInstance)!.GetValue(w)!;
        await Idle(w);
    }
    private static async Task VerifyAppearanceAndCopy(MainWindow w, bool extended)
    {
        var tabs=Find<TabControl>(w,"MainTabs");
        var themePath=typeof(MainWindow).GetProperty("ThemePreferencePathOverride",PrivateInstance)!;
        var preferenceFile=Path.Combine(Output,"test-state","ui-theme");
        themePath.SetValue(w,preferenceFile);
        tabs.SelectedIndex=4;await Idle(w);
        Find<RadioButton>(w,"DarkThemeRadio").IsChecked=true;await Idle(w);
        Tests["theme_setting_enabled"]=Find<RadioButton>(w,"DarkThemeRadio").IsEnabled;
        Tests["dark_choice_saved"]=File.ReadAllText(preferenceFile)=="dark";
        Tests["dark_surface_updates"]=BrushColor(Find<Border>(w,"RuntimeCard").Background)=="#FF282A36";
        Tests["dark_input_updates"]=BrushColor(Find<TextBox>(w,"LocalMcpTextBox").Background)=="#FF252C3C";
        Capture(w,"11-dark-settings.png");
        Find<RadioButton>(w,"LightThemeRadio").IsChecked=true;await Idle(w);
        Tests["light_choice_saved"]=File.ReadAllText(preferenceFile)=="light";
        Tests["light_surface_restored"]=BrushColor(Find<Border>(w,"RuntimeCard").Background)=="#FFFFFFFF";
        var prefType=typeof(MainWindow).Assembly.GetType("AgentDock.ControlPanel.UiThemePreference")!;
        var read=prefType.GetMethod("Read",BindingFlags.NonPublic|BindingFlags.Static)!;
        Tests["saved_theme_can_reload"]=(string)read.Invoke(null,[preferenceFile])! == "light";
        File.WriteAllText(preferenceFile,"invalid-theme");
        Tests["invalid_theme_falls_back"]=(string)read.Invoke(null,[preferenceFile])! == "light";
        var blocked=Path.Combine(Output,"test-state","directory-not-file");Directory.CreateDirectory(blocked);
        themePath.SetValue(w,blocked);Invoke(w,"ApplyUiTheme","dark",true);
        Tests["preference_write_failure_visible"]=Find<TextBlock>(w,"ThemeStatusText").Text.Contains(_culture=="en"?"could not":"失败");
        themePath.SetValue(w,preferenceFile);
        foreach(var mode in new[]{"light","dark"})
        {
            Invoke(w,"ApplyUiTheme",mode,false);await Idle(w);
            foreach(var width in new[]{640d,800d,1220d})
            {
                w.Width=width;w.Height=760;
                for(var index=0;index<5;index++)
                {
                    tabs.SelectedIndex=index;await Idle(w);
                    Tests[$"{mode}_{width}_tab{index}_no_horizontal_scroll"]=Logical(w).OfType<ScrollViewer>().Where(v=>v.IsVisible).All(v=>v.ScrollableWidth<1);
                }
                tabs.SelectedIndex=0;Find<ScrollViewer>(w,"HomeScrollViewer").ScrollToTop();await Idle(w);
                if(width==800)Capture(w,$"13-{mode}-narrow.png");
            }
            tabs.SelectedIndex=4;await Idle(w);
            var combo=Find<ComboBox>(w,"LogLevelComboBox");combo.IsEnabled=true;combo.IsDropDownOpen=true;await Idle(w);
            var popup=(Popup)combo.Template.FindName("PART_Popup",combo);
            Tests[$"{mode}_combo_popup_opens"]=popup.IsOpen;
            Tests[$"{mode}_combo_popup_themed"]=popup.Child is Border border && BrushColor(border.Background)==BrushColor(w.FindResource("UiSurface"));
            combo.IsDropDownOpen=false;Freeze(w);
        }
        // Inject a recording clipboard endpoint for deterministic UI tests, without disturbing user's clipboard.
        var writer=typeof(MainWindow).GetProperty("ClipboardWriter",PrivateInstance)!;
        var nativeWriter=writer.GetValue(w);
        var recorded=new List<string>();
        writer.SetValue(w,new Func<string,Task>(text=>{recorded.Add(text);return Task.CompletedTask;}));
        tabs.SelectedIndex=0;w.Width=1220;w.Height=760;Find<ScrollViewer>(w,"HomeScrollViewer").ScrollToTop();await Idle(w);w.Activate();
        var local=Find<Button>(w,"CopyLocalButton");var pub=Find<Button>(w,"CopyPublicButton");
        Tests["preview_copy_buttons_enabled"]=local.IsEnabled&&pub.IsEnabled;
        await ClickCopy(w,local);
        Tests["local_address_passed_to_clipboard"]=recorded.Last()==Find<TextBox>(w,"LocalMcpTextBox").Text;
        Tests["copy_success_check_state"]=CopyFeedback.GetState(local)=="success";
        Tests["copy_success_message_visible"]=Find<Popup>(w,"CopyToastPopup").IsOpen && Find<TextBlock>(w,"CopyToastText").Text.Contains(_culture=="en"?"copied":"已复制");
        Tests["copy_feedback_has_no_address"]=!Find<TextBlock>(w,"CopyToastText").Text.Contains("http");
        CaptureWithToast(w,"12-copy-success.png");
        await Task.Delay(1200);await ClickCopy(w,local);await Task.Delay(1150);
        Tests["repeat_copy_restarts_feedback_timer"]=CopyFeedback.GetState(local)=="success";
        await Task.Delay(1150);
        Tests["success_feedback_resets"]=CopyFeedback.GetState(local)=="idle" && !Find<Popup>(w,"CopyToastPopup").IsOpen;
        var publicBox=Find<TextBox>(w,"PublicMcpTextBox");var originalPublic=publicBox.Text;publicBox.Text="https://example.invalid/mcp";
        await ClickCopy(w,pub);
        Tests["public_copy_uses_value_not_mask"]=recorded.Last()=="https://example.invalid/mcp";
        await Task.Delay(1300);await ClickCopy(w,local);await Task.Delay(1000);
        Tests["old_button_does_not_clear_new_toast"]=Find<Popup>(w,"CopyToastPopup").IsOpen && ReferenceEquals(Find<Popup>(w,"CopyToastPopup").PlacementTarget,local);
        writer.SetValue(w,new Func<string,Task>(_=>throw new ExternalException("test busy clipboard")));
        await ClickCopy(w,pub);
        Tests["copy_failure_is_not_success"]=CopyFeedback.GetState(pub)=="error";
        Tests["copy_failure_message"]=Find<TextBlock>(w,"CopyToastText").Text.Contains(_culture=="en"?"failed":"失败");
        CaptureWithToast(w,"14-copy-failure.png");
        writer.SetValue(w,new Func<string,Task>(text=>{recorded.Add(text);return Task.CompletedTask;}));
        publicBox.Text="";var count=recorded.Count;await ClickCopy(w,pub);
        Tests["empty_address_not_written"]=recorded.Count==count && CopyFeedback.GetState(pub)=="error";
        publicBox.Text=originalPublic;writer.SetValue(w,nativeWriter);
        Report["copy_test_scope"]="Real WPF button events and state timers; clipboard endpoint substituted, system clipboard not modified.";
        // Keep all appearance writes in this test run's temporary directory.
        Find<Popup>(w,"CopyToastPopup").IsOpen=false;
        CopyFeedback.SetState(local,"idle");CopyFeedback.SetState(pub,"idle");
        themePath.SetValue(w,preferenceFile);Invoke(w,"ApplyUiTheme","dark",true);
        tabs.SelectedIndex=0;Find<ScrollViewer>(w,"HomeScrollViewer").ScrollToTop();SetFooter(w);
        var area=SystemParameters.WorkArea;w.Width=Math.Max(640,Math.Min(1220,area.Width));w.Height=Math.Max(520,Math.Min(760,area.Height));
        w.Left=Math.Max(area.Left,area.Left+(area.Width-w.Width)/2);w.Top=Math.Max(area.Top,area.Top+(area.Height-w.Height)/2);
        await Idle(w);Capture(w,"10-dark-home.png");
    }
    private static void CaptureWithToast(MainWindow w,string name)
    {
        var view=(FrameworkElement)w.Content;view.UpdateLayout();
        var body=new RenderTargetBitmap((int)Math.Ceiling(view.ActualWidth),(int)Math.Ceiling(view.ActualHeight),96,96,PixelFormats.Pbgra32);body.Render(view);
        var popup=Find<Popup>(w,"CopyToastPopup");var toast=(FrameworkElement)popup.Child;toast.UpdateLayout();
        var overlay=new RenderTargetBitmap(Math.Max(1,(int)Math.Ceiling(toast.ActualWidth)),Math.Max(1,(int)Math.Ceiling(toast.ActualHeight)),96,96,PixelFormats.Pbgra32);overlay.Render(toast);
        var a=view.PointToScreen(new Point());var b=toast.PointToScreen(new Point());var dpi=VisualTreeHelper.GetDpi(w).DpiScaleX;
        Tests[name+"_toast_inside_window"]=(b.X-a.X)/dpi >= -1 && (b.X-a.X)/dpi+toast.ActualWidth <= view.ActualWidth+1;
        var visual=new DrawingVisual();using(var dc=visual.RenderOpen()){dc.DrawImage(body,new Rect(0,0,view.ActualWidth,view.ActualHeight));dc.DrawImage(overlay,new Rect((b.X-a.X)/dpi,(b.Y-a.Y)/dpi,toast.ActualWidth,toast.ActualHeight));}
        var result=new RenderTargetBitmap(body.PixelWidth,body.PixelHeight,96,96,PixelFormats.Pbgra32);result.Render(visual);
        var encoder=new PngBitmapEncoder();encoder.Frames.Add(BitmapFrame.Create(result));using var stream=File.Create(Path.Combine(Output,name));encoder.Save(stream);
    }
}

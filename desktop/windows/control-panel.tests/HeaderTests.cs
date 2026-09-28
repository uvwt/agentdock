using System;
using System.IO;
using System.Linq;
using System.Threading.Tasks;
using System.Security.Cryptography;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using System.Windows.Media.Imaging;
using AgentDock.ControlPanel;

internal static partial class Program
{
    private static byte[] Pixels(BitmapSource image)
    {
        var converted=new FormatConvertedBitmap(image,PixelFormats.Bgra32,null,0);
        var bytes=new byte[converted.PixelWidth*converted.PixelHeight*4];
        converted.CopyPixels(bytes,converted.PixelWidth*4,0);
        return SHA256.HashData(bytes);
    }
    private static async Task VerifyHeaderAndLogo(MainWindow w)
    {
        Find<TabControl>(w,"MainTabs").SelectedIndex=0;await Idle(w);
        var actions=Find<StackPanel>(w,"HeaderActionsPanel");
        var buttons=actions.Children.OfType<Button>().ToArray();
        Tests["header_actions_order"]=buttons.Select(b=>b.Name).SequenceEqual(new[]{"RestartButton","UpdateButton","RefreshButton"});
        Tests["service_card_no_duplicate_restart_update"]=!Logical(Find<Border>(w,"ServiceCard")).OfType<Button>().Any(b=>b.Name is "RestartButton" or "UpdateButton");
        var logo=Find<Image>(w,"BrandLogo");
        var original=new BitmapImage();original.BeginInit();original.UriSource=new Uri(Path.Combine(TestData.RepositoryRoot, "packaging", "assets", "agentdock.png"));original.CacheOption=BitmapCacheOption.OnLoad;original.EndInit();
        Tests["brand_logo_uses_original_bitmap"]=logo.Source is BitmapImage bitmap && bitmap.PixelWidth==1024 && bitmap.PixelHeight==1024;
        Tests["brand_logo_pixels_match_official_original"]=Pixels((BitmapSource)logo.Source).SequenceEqual(Pixels(original));
        var oldContent=Find<Button>(w,"UpdateButton").Content;
        foreach(var theme in new[]{"light","dark"})
        {
            Invoke(w,"ApplyUiTheme",theme,false);
            foreach(var width in new[]{640d,800d,1100d,1220d})
            {
                w.Width=width;w.Height=760;await Idle(w);
                var root=(FrameworkElement)w.Content;
                var rects=buttons.Select(b=>new Rect(b.TranslatePoint(new Point(),root),b.RenderSize)).ToArray();
                Tests[$"header_{theme}_{width}_within_window"]=rects.All(r=>r.Left>=0 && r.Right<=root.ActualWidth+1);
                Tests[$"header_{theme}_{width}_aligned"]=rects.All(r=>Math.Abs(r.Top-rects[0].Top)<1 && Math.Abs(r.Height-rects[0].Height)<1)
                    && rects.Zip(rects.Skip(1),(a,b)=>b.Left>=a.Right).All(v=>v);
                if(width==640){Find<Button>(w,"UpdateButton").Content=TextResource("CheckingForUpdates");await Idle(w);var end=buttons[^1].TranslatePoint(new Point(buttons[^1].ActualWidth,0),root);Tests[$"header_{theme}_pending_label_fits"]=end.X<=root.ActualWidth+1;Find<Button>(w,"UpdateButton").Content=oldContent;}
            }
            await Idle(w);Capture(w,$"30-header-{theme}.png");
        }
        Invoke(w,"ApplyUiTheme","dark",false);
        Find<ScrollViewer>(w,"HomeScrollViewer").ScrollToTop();await Idle(w);Capture(w,"31-final-header.png");
    }
}

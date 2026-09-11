using System.ComponentModel;
using System.Windows;

namespace AgentDock.ControlPanel;

public partial class UpdateProgressWindow : Window
{
    private bool _canClose;

    public UpdateProgressWindow(string currentVersion, string latestVersion)
    {
        InitializeComponent();
        VersionText.Text = $"{currentVersion} → {latestVersion}";
    }

    public void Report(UpdateProgress progress)
    {
        UpdateProgressBar.IsIndeterminate = progress.IsIndeterminate;
        if (progress.Percentage is int percentage)
        {
            UpdateProgressBar.Value = Math.Clamp(percentage, 0, 100);
        }
        StatusText.Text = progress.Message;
    }

    public void Complete(string message)
    {
        _canClose = true;
        UpdateProgressBar.IsIndeterminate = false;
        UpdateProgressBar.Value = 100;
        StatusText.Text = message;
        CloseButton.IsEnabled = true;
        CloseButton.Focus();
    }

    public void Fail(string message)
    {
        _canClose = true;
        StatusText.Text = message;
        CloseButton.IsEnabled = true;
        CloseButton.Focus();
    }

    protected override void OnClosing(CancelEventArgs e)
    {
        if (!_canClose)
        {
            e.Cancel = true;
            return;
        }
        base.OnClosing(e);
    }

    private void CloseButton_Click(object sender, RoutedEventArgs e) => Close();
}

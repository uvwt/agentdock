using System.Text.RegularExpressions;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using Button = System.Windows.Controls.Button;
using CheckBox = System.Windows.Controls.CheckBox;
using HorizontalAlignment = System.Windows.HorizontalAlignment;
using MessageBox = System.Windows.MessageBox;
using Orientation = System.Windows.Controls.Orientation;
using TextBox = System.Windows.Controls.TextBox;
using Brushes = System.Windows.Media.Brushes;
using Color = System.Windows.Media.Color;

namespace AgentDock.ControlPanel;

public partial class MainWindow
{
    private static readonly Regex PluginIdentifierPattern = new(
        "^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$",
        RegexOptions.CultureInvariant | RegexOptions.Compiled);

    private readonly SemaphoreSlim _capabilityGate = new(1, 1);
    private CapabilityInventory _capabilityInventory = new();
    private bool _updatingCapabilities;

    private async Task RefreshCapabilitiesAsync(bool coreAvailable = true, bool showErrors = true)
    {
        if (!await _capabilityGate.WaitAsync(0))
        {
            return;
        }
        try
        {
            if (!coreAvailable)
            {
                _capabilityInventory = new CapabilityInventory();
                RenderCapabilityInventory();
                CapabilityStatusText.Text = UiText.Get("CapabilitiesRequireRunningCore");
                return;
            }
            await LoadCapabilityInventoryCoreAsync();
        }
        catch (Exception ex)
        {
            CapabilityStatusText.Text = ex.Message;
            if (showErrors)
            {
                MessageBox.Show(this, ex.Message, "AgentDock", MessageBoxButton.OK, MessageBoxImage.Error);
            }
        }
        finally
        {
            _capabilityGate.Release();
        }
    }

    private async Task LoadCapabilityInventoryCoreAsync()
    {
        CapabilityStatusText.Text = UiText.Get("LoadingCapabilities");
        _capabilityInventory = await _runtime.GetCapabilityInventoryAsync();
        _capabilityInventory.Plugins ??= [];
        _capabilityInventory.Skills ??= [];
        _capabilityInventory.McpServers ??= [];
        RenderCapabilityInventory();
        CapabilityStatusText.Text = UiText.Format(
            "CapabilitiesLoaded",
            _capabilityInventory.Plugins.Count,
            _capabilityInventory.Skills.Count,
            _capabilityInventory.McpServers.Count);
    }

    private async Task ExecuteCapabilityActionAsync(string pendingText, Func<Task> action)
    {
        if (!await _capabilityGate.WaitAsync(0))
        {
            return;
        }
        try
        {
            CapabilityStatusText.Text = pendingText;
            await action();
            await LoadCapabilityInventoryCoreAsync();
        }
        catch (Exception ex)
        {
            CapabilityStatusText.Text = ex.Message;
            MessageBox.Show(this, ex.Message, "AgentDock", MessageBoxButton.OK, MessageBoxImage.Error);
            RenderCapabilityInventory();
        }
        finally
        {
            _capabilityGate.Release();
        }
    }

    private void RenderCapabilityInventory()
    {
        var previous = _updatingCapabilities;
        _updatingCapabilities = true;
        try
        {
            PluginListPanel.Children.Clear();
            StandaloneSkillListPanel.Children.Clear();
            StandaloneMcpListPanel.Children.Clear();

            var plugins = _capabilityInventory.Plugins
                .OrderBy(plugin => plugin.Name, StringComparer.OrdinalIgnoreCase)
                .ToList();
            foreach (var plugin in plugins)
            {
                PluginListPanel.Children.Add(BuildPluginCard(plugin));
            }
            if (plugins.Count == 0)
            {
                PluginListPanel.Children.Add(BuildEmptyCapabilityText("NoPlugins"));
            }

            var ownedSkills = plugins
                .SelectMany(plugin => plugin.Skills ?? [])
                .ToHashSet(StringComparer.Ordinal);
            var ownedMcpServers = plugins
                .SelectMany(plugin => plugin.McpServers ?? [])
                .ToHashSet(StringComparer.Ordinal);

            var standaloneSkills = _capabilityInventory.Skills
                .Where(skill => !ownedSkills.Contains(skill.Identifier))
                .OrderBy(skill => skill.DisplayName, StringComparer.CurrentCultureIgnoreCase)
                .ToList();
            foreach (var skill in standaloneSkills)
            {
                StandaloneSkillListPanel.Children.Add(BuildSkillCapabilityRow(skill, nested: false));
            }
            if (standaloneSkills.Count == 0)
            {
                StandaloneSkillListPanel.Children.Add(BuildEmptyCapabilityText("NoStandaloneSkills"));
            }

            var standaloneMcp = _capabilityInventory.McpServers
                .Where(server => !ownedMcpServers.Contains(server.Name))
                .OrderBy(server => server.Name, StringComparer.OrdinalIgnoreCase)
                .ToList();
            foreach (var server in standaloneMcp)
            {
                StandaloneMcpListPanel.Children.Add(BuildMcpCapabilityRow(server, nested: false));
            }
            if (standaloneMcp.Count == 0)
            {
                StandaloneMcpListPanel.Children.Add(BuildEmptyCapabilityText("NoStandaloneMcpServers"));
            }
        }
        finally
        {
            _updatingCapabilities = previous;
        }
    }

    private Border BuildPluginCard(PluginCapabilityInfo plugin)
    {
        var content = new StackPanel();
        var header = new Grid();
        header.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        header.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        header.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        header.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });

        var title = new TextBlock
        {
            Text = plugin.Name,
            FontSize = 15,
            FontWeight = FontWeights.SemiBold,
            VerticalAlignment = VerticalAlignment.Center
        };
        var edit = new Button
        {
            Content = UiText.Get("Edit"),
            Tag = plugin.Name,
            MinWidth = 70,
            Margin = new Thickness(8, 0, 0, 0)
        };
        edit.Click += PluginEditButton_Click;
        var remove = new Button
        {
            Content = UiText.Get("Delete"),
            Tag = plugin.Name,
            MinWidth = 70,
            Margin = new Thickness(8, 0, 0, 0)
        };
        remove.Click += PluginRemoveButton_Click;
        var toggle = new CheckBox
        {
            Content = UiText.Get("Enabled"),
            IsChecked = plugin.Enabled,
            Tag = plugin.Name,
            VerticalAlignment = VerticalAlignment.Center,
            Margin = new Thickness(14, 0, 0, 0)
        };
        toggle.Checked += PluginToggle_Changed;
        toggle.Unchecked += PluginToggle_Changed;

        Grid.SetColumn(title, 0);
        Grid.SetColumn(edit, 1);
        Grid.SetColumn(remove, 2);
        Grid.SetColumn(toggle, 3);
        header.Children.Add(title);
        header.Children.Add(edit);
        header.Children.Add(remove);
        header.Children.Add(toggle);
        content.Children.Add(header);
        content.Children.Add(new TextBlock
        {
            Text = plugin.Description,
            Foreground = new SolidColorBrush(Color.FromRgb(102, 112, 133)),
            TextWrapping = TextWrapping.Wrap,
            Margin = new Thickness(0, 6, 0, 10)
        });

        var skillsByName = _capabilityInventory.Skills.ToDictionary(skill => skill.Identifier, StringComparer.Ordinal);
        var mcpByName = _capabilityInventory.McpServers.ToDictionary(server => server.Name, StringComparer.Ordinal);
        if ((plugin.Skills?.Count ?? 0) > 0)
        {
            content.Children.Add(BuildPluginSectionTitle(UiText.Get("PluginSkills")));
            foreach (var name in (plugin.Skills ?? []).OrderBy(value => value, StringComparer.OrdinalIgnoreCase))
            {
                content.Children.Add(skillsByName.TryGetValue(name, out var skill)
                    ? BuildSkillCapabilityRow(skill, nested: true)
                    : BuildUnavailableCapabilityRow("Skill", name));
            }
        }
        if ((plugin.McpServers?.Count ?? 0) > 0)
        {
            content.Children.Add(BuildPluginSectionTitle(UiText.Get("PluginMcpServers")));
            foreach (var name in (plugin.McpServers ?? []).OrderBy(value => value, StringComparer.OrdinalIgnoreCase))
            {
                content.Children.Add(mcpByName.TryGetValue(name, out var server)
                    ? BuildMcpCapabilityRow(server, nested: true)
                    : BuildUnavailableCapabilityRow("MCP", name));
            }
        }

        return new Border
        {
            Child = content,
            BorderBrush = new SolidColorBrush(Color.FromRgb(208, 213, 221)),
            BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(6),
            Padding = new Thickness(14),
            Margin = new Thickness(0, 0, 0, 10),
            Background = Brushes.White
        };
    }

    private static TextBlock BuildPluginSectionTitle(string text) => new()
    {
        Text = text,
        FontWeight = FontWeights.SemiBold,
        Margin = new Thickness(0, 8, 0, 3)
    };

    private Border BuildSkillCapabilityRow(SkillCapabilityInfo skill, bool nested)
    {
        var details = skill.Description;
        var metadata = new List<string>();
        if (!string.IsNullOrWhiteSpace(skill.ActiveVersion))
        {
            metadata.Add(skill.ActiveVersion);
        }
        if (skill.Bundled)
        {
            metadata.Add(UiText.Get("Bundled"));
        }
        if (metadata.Count > 0)
        {
            details = string.IsNullOrWhiteSpace(details)
                ? string.Join(" · ", metadata)
                : details + " · " + string.Join(" · ", metadata);
        }
        var title = skill.DisplayName;
        if (!string.Equals(skill.DisplayName, skill.Identifier, StringComparison.Ordinal))
        {
            title += $" ({skill.Identifier})";
        }
        return BuildCapabilityToggleRow(
            title,
            details,
            skill.Enabled,
            new CapabilityToggleTarget("skill", skill.Identifier),
            nested);
    }

    private Border BuildMcpCapabilityRow(McpCapabilityInfo server, bool nested)
    {
        var metadata = UiText.Format("McpStatusSummary", server.Status, server.ToolCount);
        var details = string.IsNullOrWhiteSpace(server.Description)
            ? metadata
            : server.Description + " · " + metadata;
        if (!string.IsNullOrWhiteSpace(server.LastErrorCode))
        {
            details += " · " + server.LastErrorCode;
        }
        return BuildCapabilityToggleRow(
            server.Name,
            details,
            server.Enabled,
            new CapabilityToggleTarget("mcp", server.Name),
            nested);
    }

    private Border BuildCapabilityToggleRow(
        string title,
        string description,
        bool enabled,
        CapabilityToggleTarget target,
        bool nested)
    {
        var row = new Grid();
        row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        row.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var text = new StackPanel();
        text.Children.Add(new TextBlock
        {
            Text = title,
            FontWeight = FontWeights.Medium,
            TextWrapping = TextWrapping.Wrap
        });
        if (!string.IsNullOrWhiteSpace(description))
        {
            text.Children.Add(new TextBlock
            {
                Text = description,
                Foreground = new SolidColorBrush(Color.FromRgb(102, 112, 133)),
                TextWrapping = TextWrapping.Wrap,
                Margin = new Thickness(0, 2, 14, 0)
            });
        }
        var toggle = new CheckBox
        {
            IsChecked = enabled,
            Tag = target,
            VerticalAlignment = VerticalAlignment.Center,
            ToolTip = enabled ? UiText.Get("DisableCapability") : UiText.Get("EnableCapability")
        };
        toggle.Checked += CapabilityToggle_Changed;
        toggle.Unchecked += CapabilityToggle_Changed;
        Grid.SetColumn(text, 0);
        Grid.SetColumn(toggle, 1);
        row.Children.Add(text);
        row.Children.Add(toggle);
        return new Border
        {
            Child = row,
            BorderBrush = new SolidColorBrush(Color.FromRgb(234, 236, 240)),
            BorderThickness = new Thickness(0, 0, 0, 1),
            Padding = nested ? new Thickness(18, 7, 8, 7) : new Thickness(8, 9, 8, 9)
        };
    }

    private static Border BuildUnavailableCapabilityRow(string kind, string name) => new()
    {
        Child = new TextBlock
        {
            Text = UiText.Format("UnavailablePluginMember", kind, name),
            Foreground = new SolidColorBrush(Color.FromRgb(180, 35, 24)),
            TextWrapping = TextWrapping.Wrap
        },
        BorderBrush = new SolidColorBrush(Color.FromRgb(254, 205, 202)),
        BorderThickness = new Thickness(0, 0, 0, 1),
        Padding = new Thickness(18, 7, 8, 7)
    };

    private static TextBlock BuildEmptyCapabilityText(string resourceKey) => new()
    {
        Text = UiText.Get(resourceKey),
        Foreground = new SolidColorBrush(Color.FromRgb(102, 112, 133)),
        Margin = new Thickness(8),
        TextWrapping = TextWrapping.Wrap
    };

    private async void RefreshCapabilitiesButton_Click(object sender, RoutedEventArgs e) =>
        await RefreshCapabilitiesAsync(_snapshot?.Healthy == true);

    private async void PluginToggle_Changed(object sender, RoutedEventArgs e)
    {
        if (_updatingCapabilities || sender is not CheckBox toggle || toggle.Tag is not string name)
        {
            return;
        }
        var enabled = toggle.IsChecked == true;
        await ExecuteCapabilityActionAsync(
            UiText.Format(enabled ? "EnablingPlugin" : "DisablingPlugin", name),
            () => _runtime.SetPluginEnabledAsync(name, enabled));
    }

    private async void CapabilityToggle_Changed(object sender, RoutedEventArgs e)
    {
        if (_updatingCapabilities || sender is not CheckBox toggle || toggle.Tag is not CapabilityToggleTarget target)
        {
            return;
        }
        var enabled = toggle.IsChecked == true;
        var pendingKey = enabled ? "EnablingCapability" : "DisablingCapability";
        await ExecuteCapabilityActionAsync(
            UiText.Format(pendingKey, target.Name),
            target.Kind == "skill"
                ? () => _runtime.SetSkillEnabledAsync(target.Name, enabled)
                : () => _runtime.SetMcpEnabledAsync(target.Name, enabled));
    }

    private async void AddPluginButton_Click(object sender, RoutedEventArgs e)
    {
        if (_updatingCapabilities)
        {
            return;
        }
        var plugin = ShowPluginDialog(null);
        if (plugin is null)
        {
            return;
        }
        await ExecuteCapabilityActionAsync(
            UiText.Format("SavingPlugin", plugin.Name),
            () => _runtime.UpsertPluginAsync(plugin));
    }

    private async void PluginEditButton_Click(object sender, RoutedEventArgs e)
    {
        if (_updatingCapabilities || sender is not Button button || button.Tag is not string name)
        {
            return;
        }
        var existing = _capabilityInventory.Plugins.FirstOrDefault(plugin => plugin.Name == name);
        if (existing is null)
        {
            return;
        }
        var plugin = ShowPluginDialog(existing);
        if (plugin is null)
        {
            return;
        }
        await ExecuteCapabilityActionAsync(
            UiText.Format("SavingPlugin", plugin.Name),
            () => _runtime.UpsertPluginAsync(plugin));
    }

    private async void PluginRemoveButton_Click(object sender, RoutedEventArgs e)
    {
        if (_updatingCapabilities || sender is not Button button || button.Tag is not string name)
        {
            return;
        }
        var confirm = MessageBox.Show(
            this,
            UiText.Format("ConfirmRemovePlugin", name),
            "AgentDock",
            MessageBoxButton.YesNo,
            MessageBoxImage.Warning);
        if (confirm != MessageBoxResult.Yes)
        {
            return;
        }
        await ExecuteCapabilityActionAsync(
            UiText.Format("RemovingPlugin", name),
            () => _runtime.RemovePluginAsync(name));
    }

    private PluginCapabilityInfo? ShowPluginDialog(PluginCapabilityInfo? existing)
    {
        var editing = existing is not null;
        var dialog = new Window
        {
            Title = UiText.Get(editing ? "EditPlugin" : "AddPlugin"),
            Owner = this,
            Width = 680,
            Height = 720,
            MinWidth = 560,
            MinHeight = 520,
            WindowStartupLocation = WindowStartupLocation.CenterOwner,
            ResizeMode = ResizeMode.CanResize,
            ShowInTaskbar = false
        };

        var nameInput = new TextBox
        {
            Text = existing?.Name ?? "",
            IsReadOnly = editing,
            Margin = new Thickness(0, 5, 0, 10)
        };
        var descriptionInput = new TextBox
        {
            Text = existing?.Description ?? "",
            AcceptsReturn = true,
            TextWrapping = TextWrapping.Wrap,
            MinHeight = 70,
            MaxHeight = 120,
            VerticalScrollBarVisibility = ScrollBarVisibility.Auto,
            Margin = new Thickness(0, 5, 0, 10)
        };
        var enabledInput = new CheckBox
        {
            Content = UiText.Get("EnablePluginAfterSaving"),
            IsChecked = existing?.Enabled ?? true,
            Margin = new Thickness(0, 0, 0, 12)
        };

        var otherPlugins = _capabilityInventory.Plugins
            .Where(plugin => existing is null || plugin.Name != existing.Name)
            .ToList();
        var unavailableSkills = otherPlugins
            .SelectMany(plugin => plugin.Skills ?? [])
            .ToHashSet(StringComparer.Ordinal);
        var unavailableMcp = otherPlugins
            .SelectMany(plugin => plugin.McpServers ?? [])
            .ToHashSet(StringComparer.Ordinal);
        var selectedSkills = (existing?.Skills ?? []).ToHashSet(StringComparer.Ordinal);
        var selectedMcp = (existing?.McpServers ?? []).ToHashSet(StringComparer.Ordinal);
        var skillChecks = new Dictionary<string, CheckBox>(StringComparer.Ordinal);
        var mcpChecks = new Dictionary<string, CheckBox>(StringComparer.Ordinal);

        var members = new StackPanel();
        members.Children.Add(new TextBlock
        {
            Text = UiText.Get("PluginMemberSelectionHelp"),
            Foreground = new SolidColorBrush(Color.FromRgb(102, 112, 133)),
            TextWrapping = TextWrapping.Wrap,
            Margin = new Thickness(0, 0, 0, 10)
        });
        members.Children.Add(new TextBlock { Text = UiText.Get("PluginSkills"), FontWeight = FontWeights.SemiBold });
        foreach (var skill in _capabilityInventory.Skills
                     .Where(skill => !unavailableSkills.Contains(skill.Identifier))
                     .OrderBy(skill => skill.DisplayName, StringComparer.CurrentCultureIgnoreCase))
        {
            var check = new CheckBox
            {
                Content = string.Equals(skill.DisplayName, skill.Identifier, StringComparison.Ordinal)
                    ? skill.DisplayName
                    : $"{skill.DisplayName} ({skill.Identifier})",
                IsChecked = selectedSkills.Contains(skill.Identifier),
                ToolTip = skill.Description,
                Margin = new Thickness(12, 6, 0, 0)
            };
            skillChecks[skill.Identifier] = check;
            members.Children.Add(check);
        }
        members.Children.Add(new TextBlock
        {
            Text = UiText.Get("PluginMcpServers"),
            FontWeight = FontWeights.SemiBold,
            Margin = new Thickness(0, 14, 0, 0)
        });
        foreach (var server in _capabilityInventory.McpServers
                     .Where(server => !unavailableMcp.Contains(server.Name))
                     .OrderBy(server => server.Name, StringComparer.OrdinalIgnoreCase))
        {
            var check = new CheckBox
            {
                Content = server.Name,
                IsChecked = selectedMcp.Contains(server.Name),
                ToolTip = server.Description,
                Margin = new Thickness(12, 6, 0, 0)
            };
            mcpChecks[server.Name] = check;
            members.Children.Add(check);
        }

        var save = new Button
        {
            Content = UiText.Get("Save"),
            IsDefault = true,
            MinWidth = 88,
            Height = 32,
            Margin = new Thickness(8, 0, 0, 0)
        };
        var cancel = new Button
        {
            Content = UiText.Get("Cancel"),
            IsCancel = true,
            MinWidth = 88,
            Height = 32,
            Margin = new Thickness(8, 0, 0, 0)
        };
        save.Click += (_, _) =>
        {
            var name = nameInput.Text.Trim();
            if (!PluginIdentifierPattern.IsMatch(name))
            {
                MessageBox.Show(dialog, UiText.Get("PluginNameInvalid"), "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
                nameInput.Focus();
                return;
            }
            if (!editing && _capabilityInventory.Plugins.Any(plugin => plugin.Name == name))
            {
                MessageBox.Show(dialog, UiText.Get("PluginNameExists"), "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
                nameInput.Focus();
                return;
            }
            if (descriptionInput.Text.Trim().Length == 0)
            {
                MessageBox.Show(dialog, UiText.Get("PluginDescriptionRequired"), "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
                descriptionInput.Focus();
                return;
            }
            if (!skillChecks.Values.Any(check => check.IsChecked == true) &&
                !mcpChecks.Values.Any(check => check.IsChecked == true))
            {
                MessageBox.Show(dialog, UiText.Get("PluginMemberRequired"), "AgentDock", MessageBoxButton.OK, MessageBoxImage.Warning);
                return;
            }
            dialog.DialogResult = true;
        };

        var body = new StackPanel { Margin = new Thickness(18) };
        body.Children.Add(new TextBlock { Text = UiText.Get("PluginName"), FontWeight = FontWeights.SemiBold });
        body.Children.Add(nameInput);
        body.Children.Add(new TextBlock { Text = UiText.Get("PluginDescription"), FontWeight = FontWeights.SemiBold });
        body.Children.Add(descriptionInput);
        body.Children.Add(enabledInput);
        body.Children.Add(members);

        var scroller = new ScrollViewer
        {
            Content = body,
            VerticalScrollBarVisibility = ScrollBarVisibility.Auto
        };
        var rightButtons = new StackPanel
        {
            Orientation = Orientation.Horizontal,
            HorizontalAlignment = HorizontalAlignment.Right,
            Margin = new Thickness(18, 10, 18, 18)
        };
        rightButtons.Children.Add(save);
        rightButtons.Children.Add(cancel);
        var layout = new DockPanel();
        DockPanel.SetDock(rightButtons, Dock.Bottom);
        layout.Children.Add(rightButtons);
        layout.Children.Add(scroller);
        dialog.Content = layout;
        dialog.Loaded += (_, _) => nameInput.Focus();
        if (dialog.ShowDialog() != true)
        {
            return null;
        }

        return new PluginCapabilityInfo
        {
            Name = nameInput.Text.Trim(),
            Description = descriptionInput.Text.Trim(),
            Enabled = enabledInput.IsChecked == true,
            Skills = skillChecks
                .Where(pair => pair.Value.IsChecked == true)
                .Select(pair => pair.Key)
                .OrderBy(value => value, StringComparer.Ordinal)
                .ToList(),
            McpServers = mcpChecks
                .Where(pair => pair.Value.IsChecked == true)
                .Select(pair => pair.Key)
                .OrderBy(value => value, StringComparer.Ordinal)
                .ToList()
        };
    }

    private sealed record CapabilityToggleTarget(string Kind, string Name);
}

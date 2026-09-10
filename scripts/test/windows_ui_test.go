package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsControlPanelPrivilegeModeCopyStaysUserFacing(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "MainWindow.xaml"))
	if err != nil {
		t.Fatalf("read MainWindow.xaml: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		`x:Name="ElevatedCoreCheckBox"`,
		`Content="{local:Loc RunCoreElevated}"`,
		`Click="ElevatedCoreCheckBox_Click"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("Windows privilege mode control missing %q", want)
		}
	}
	if strings.Contains(content, "开启时使用 Windows Highest") {
		t.Fatal("Windows privilege mode control must not expose implementation details")
	}
}

func TestWindowsControlPanelSupportsPersistentLanguagePreference(t *testing.T) {
	root := filepath.Join("..", "..", "desktop", "windows", "control-panel")
	checks := map[string][]string{
		"MainWindow.xaml": {
			`x:Name="LanguageComboBox"`,
			`Tag="system"`,
			`Tag="zh-CN"`,
			`Tag="en"`,
			`SelectionChanged="LanguageComboBox_SelectionChanged"`,
		},
		"MainWindow.xaml.cs": {
			`UiText.ReadPreference()`,
			`UiText.Get("LanguageChangeDiscardWarning")`,
			`MessageBoxButton.YesNo`,
			`SelectUiLanguage(UiText.ReadPreference())`,
			`ApplyLanguagePreferenceAsync(preference)`,
		},
		"App.xaml.cs": {
			`UiText.SetPreference(preference)`,
			`new MainWindow(Runtime)`,
			`previousWindow.CloseForReplacement()`,
		},
		filepath.Join("Localization", "UiText.cs"): {
			`"AgentDock",`,
			`"ui-language"`,
			`File.Delete(PreferencePath)`,
			`ResolveLocale(string preference, string systemCultureName)`,
		},
	}

	for relativePath, wants := range checks {
		data, err := os.ReadFile(filepath.Join(root, relativePath))
		if err != nil {
			t.Fatalf("read %s: %v", relativePath, err)
		}
		content := string(data)
		for _, want := range wants {
			if !strings.Contains(content, want) {
				t.Fatalf("Windows language preference contract missing %q in %s", want, relativePath)
			}
		}
	}
}

func TestWindowsUpdateProgressWindowSizesToContent(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "UpdateProgressWindow.xaml"))
	if err != nil {
		t.Fatalf("read UpdateProgressWindow.xaml: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		`SizeToContent="Height"`,
		`x:Name="CloseButton"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("Windows update progress window missing %q", want)
		}
	}
	if strings.Contains(content, `<RowDefinition Height="*" />`) {
		t.Fatal("Windows update progress button row must size to its content")
	}
}

func TestWindowsControlPanelShowsLiveNexusStatusInsideRuntimeStatus(t *testing.T) {
	root := filepath.Join("..", "..", "desktop", "windows", "control-panel")
	files := map[string][]string{
		"MainWindow.xaml": {
			`Text="{local:Loc HealthCheck}" Grid.Row="1"`,
			`Text="Nexus" Grid.Row="2"`,
			`x:Name="NexusStatusText" Grid.Row="2" Grid.Column="1" Text="{local:Loc NotConfigured}"`,
			`Text="{local:Loc Version}" Grid.Row="3"`,
		},
		"MainWindow.xaml.cs": {
			`NexusStatusText.Text`,
			`UiText.Get("Connected")`,
			`UiText.Get("NotConnected")`,
			`UiText.Get("NotConfigured")`,
			`UiText.Get("ConfigurationError")`,
			`snapshot.NexusConnected`,
			`GetSnapshotAsync(includeNexusConnection: true)`,
		},
		filepath.Join("Models", "RuntimeModels.cs"): {
			`bool NexusConnected`,
			`JsonPropertyName("nexus_connected")`,
		},
		filepath.Join("Services", "RuntimeService.cs"): {
			`bool includeNexusConnection = false`,
			`ReadNexusConnectionAsync`,
			`"service", "status", "--runtime-root", RuntimeRoot`,
		},
	}

	for relativePath, wants := range files {
		data, err := os.ReadFile(filepath.Join(root, relativePath))
		if err != nil {
			t.Fatalf("read %s: %v", relativePath, err)
		}
		content := string(data)
		for _, want := range wants {
			if !strings.Contains(content, want) {
				t.Fatalf("Windows Nexus status contract missing %q in %s", want, relativePath)
			}
		}
	}

	xaml, err := os.ReadFile(filepath.Join(root, "MainWindow.xaml"))
	if err != nil {
		t.Fatalf("read MainWindow.xaml: %v", err)
	}
	content := string(xaml)
	for _, forbidden := range []string{`NexusStatusDot`, `NexusHeaderStatusText`, `Nexus ·`} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("Windows Nexus status must stay plain inside runtime status; found %q", forbidden)
		}
	}
}

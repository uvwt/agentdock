package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacOSAdvancedConnectionGatesCloudflareBehindOptionalComponent(t *testing.T) {
	root := filepath.Join("..", "..", "desktop", "macos", "AgentDockApp")
	data, err := os.ReadFile(filepath.Join(root, "Sources", "NativeControlPanelWindowController.swift"))
	if err != nil {
		t.Fatalf("read NativeControlPanelWindowController.swift: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		`model.settingsPage == .advancedConnection`,
		`await model.refreshCloudflaredComponent()`,
		`SettingsSection(L10n.text("Optional component"))`,
		`L10n.text("Cloudflare Tunnel"),`,
		`ProgressView(value: progress)`,
		`ProgressView()`,
		`cloudflaredComponentOperation?.text ?? cloudflaredComponentDetail`,
		`case "downloading":`,
		`L10n.format("Downloading component… %d%%", percentage)`,
		`model.cloudflaredComponent.state == "broken"`,
		`L10n.text("Repair")`,
		`L10n.text("Install")`,
		`L10n.text("Uninstall")`,
		`if model.cloudflaredComponent.ready {`,
		`L10n.text("Temporary domain")`,
		`L10n.text("Fixed domain")`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("macOS advanced connection missing optional component contract %q", want)
		}
	}

	serviceData, err := os.ReadFile(filepath.Join(root, "Sources", "ServiceController.swift"))
	if err != nil {
		t.Fatalf("read ServiceController.swift: %v", err)
	}
	serviceContent := string(serviceData)
	for _, want := range []string{
		`"--progress-json"`,
		`CloudflaredComponentProgress`,
		`runCloudflaredComponentProcess`,
	} {
		if !strings.Contains(serviceContent, want) {
			t.Fatalf("macOS component service missing progress protocol contract %q", want)
		}
	}

	forbidden := []string{
		`SetupWindowController`,
	}
	for _, value := range forbidden {
		if strings.Contains(content, value) {
			t.Fatalf("active macOS control panel must not reference legacy setup controller %q", value)
		}
	}
}

func TestMacOSLegacySetupControllerRemoved(t *testing.T) {
	path := filepath.Join("..", "..", "desktop", "macos", "AgentDockApp", "Sources", "SetupWindowController.swift")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("legacy SetupWindowController must be removed; stat err=%v", err)
	}
}

func TestMacOSUpdateCheckDoesNotLockUnrelatedControls(t *testing.T) {
	root := filepath.Join("..", "..", "desktop", "macos", "AgentDockApp", "Sources")

	appDelegateData, err := os.ReadFile(filepath.Join(root, "AppDelegate.swift"))
	if err != nil {
		t.Fatalf("read AppDelegate.swift: %v", err)
	}
	appDelegate := string(appDelegateData)
	for _, want := range []string{
		"private var updateActivity: DesktopUpdateActivity = .idle",
		"ApplicationMenu.setQuitEnabled(!activity.locksApplication)",
		"setUpdateActivity(.checking)",
		"updateMenuItem.isEnabled = updateActivity.canCheckForUpdates",
		"NSMenu.popUpContextMenu(makeTrayContextMenu(), with: event, for: sender)",
		"menu.appearance = NSApp.effectiveAppearance",
		"guard !self.trayServiceActionInProgress,",
		"!self.setupWindow.hasActiveServiceOperation else {",
	} {
		if !strings.Contains(appDelegate, want) {
			t.Fatalf("macOS app delegate missing update activity contract %q", want)
		}
	}

	controlPanelData, err := os.ReadFile(filepath.Join(root, "NativeControlPanelWindowController.swift"))
	if err != nil {
		t.Fatalf("read NativeControlPanelWindowController.swift: %v", err)
	}
	controlPanel := string(controlPanelData)
	for _, want := range []string{
		"@Published var updateActivity: DesktopUpdateActivity = .idle",
		"guard !isBusy, !updateActivity.locksApplication else { return }",
		".disabled(model.isBusy || model.updateActivity.locksApplication)",
		".disabled(!model.updateActivity.canCheckForUpdates)",
	} {
		if !strings.Contains(controlPanel, want) {
			t.Fatalf("macOS control panel missing update activity contract %q", want)
		}
	}
}

func TestMacOSPublicEndpointCheckInvalidatesAcrossLifecycleChanges(t *testing.T) {
	root := filepath.Join("..", "..", "desktop", "macos", "AgentDockApp", "Sources")
	controlPanelData, err := os.ReadFile(filepath.Join(root, "NativeControlPanelWindowController.swift"))
	if err != nil {
		t.Fatalf("read NativeControlPanelWindowController.swift: %v", err)
	}
	controlPanel := string(controlPanelData)
	for _, want := range []string{
		"publicEndpointCheckRevision",
		"func invalidatePublicEndpointCheck()",
		".onChange(of: model.publicEndpointCheckRevision)",
		"let lifecycleRevision = model.publicEndpointCheckRevision",
		"model.publicEndpointCheckRevision == lifecycleRevision",
	} {
		if !strings.Contains(controlPanel, want) {
			t.Fatalf("macOS public endpoint lifecycle invalidation missing %q", want)
		}
	}

	appDelegateData, err := os.ReadFile(filepath.Join(root, "AppDelegate.swift"))
	if err != nil {
		t.Fatalf("read AppDelegate.swift: %v", err)
	}
	appDelegate := string(appDelegateData)
	for _, want := range []string{
		"performServiceAction(L10n.text(\"Start\"), recheckPublicEndpointOnSuccess: true)",
		"performServiceAction(L10n.text(\"Restart\"), recheckPublicEndpointOnSuccess: true)",
		"self.setupWindow.invalidatePublicEndpointCheck()",
	} {
		if !strings.Contains(appDelegate, want) {
			t.Fatalf("macOS lifecycle action missing public endpoint invalidation %q", want)
		}
	}
}

package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacOSAdvancedSettingsUsesResponsiveScrollableLayout(t *testing.T) {
	path := filepath.Join("..", "..", "desktop", "macos", "AgentDockApp", "Sources", "AdvancedSettingsWindowController.swift")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read AdvancedSettingsWindowController.swift: %v", err)
	}
	content := string(data)

	for _, want := range []string{
		`contentRect: NSRect(x: 0, y: 0, width: 680, height: 760)`,
		`styleMask: [.titled, .closable, .resizable]`,
		`window.minSize = NSSize(width: 620, height: 520)`,
		`let scrollView = NSScrollView()`,
		`scrollView.hasVerticalScroller = true`,
		`scrollView.documentView = scrollDocumentView`,
		`scrollDocumentView.widthAnchor.constraint(equalTo: scrollView.contentView.widthAnchor)`,
		`root.bottomAnchor.constraint(equalTo: scrollDocumentView.bottomAnchor, constant: -22)`,
		`label.widthAnchor.constraint(equalToConstant: 128)`,
		`browserConnectionMode.widthAnchor.constraint(equalToConstant: 360)`,
		`let visibleFrame = (window.screen ?? NSScreen.main)?.visibleFrame`,
		`let nexusPairRow = NSView()`,
		`nexusDeviceTokenStatus.leadingAnchor.constraint(equalTo: nexusPairRow.leadingAnchor, constant: 140)`,
		"let startupStack = NSStackView(views: [\n            serviceAutostart,\n            menuAutostart,\n            formRow(title: L10n.text(\"Interface language\"), control: languagePreference),",
		"let serviceForm = NSStackView(views: [\n            mcpAppsEnabled,\n            formRow(title: L10n.text(\"Service port\"), control: portField),\n            formRow(title: L10n.text(\"Log level\"), control: logLevel),",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("macOS advanced settings missing responsive layout contract %q", want)
		}
	}

	for _, forbidden := range []string{
		`contentRect: NSRect(x: 0, y: 0, width: 590, height: 850)`,
		`label.widthAnchor.constraint(equalToConstant: 92)`,
		`browserConnectionMode.widthAnchor.constraint(equalToConstant: 290)`,
		`nexusEndpoint.widthAnchor.constraint(equalToConstant: 390)`,
		`nexusPairingCode.widthAnchor.constraint(equalToConstant: 390)`,
		`box.widthAnchor.constraint(equalToConstant: 534)`,
		`formRow(title: "Device Token", control: nexusDeviceTokenStatus`,
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("macOS advanced settings still contains fixed/truncated layout contract %q", forbidden)
		}
	}
}

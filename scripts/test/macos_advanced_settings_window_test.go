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
	content := strings.ReplaceAll(string(data), "\r\n", "\n")

	for _, want := range []string{
		`contentRect: NSRect(x: 0, y: 0, width: 700, height: 760)`,
		`styleMask: [.titled, .closable, .resizable]`,
		`window.minSize = NSSize(width: 700, height: 560)`,
		`private let acpOverviewContainer = NSStackView()`,
		`private let acpDefaultProfileMenu = NSPopUpButton`,
		`formRow(title: L10n.text("Default ACP"), control: acpDefaultProfileMenu)`,
		`#selector(defaultACPProfileChanged)`,
		`#selector(editCustomACPProfile(_:))`,
		`let editGesture = NSClickGestureRecognizer(target: self, action: #selector(editCustomACPProfile(_:)))`,
		`private func addACPOverviewRow(_ row: NSView)`,
		`acpProfileList.addArrangedSubview(row)`,
		`row.widthAnchor.constraint(equalTo: acpProfileList.widthAnchor).isActive = true`,
		`acpProfileList.spacing = 0`,
		`let compactPopUpWidth: CGFloat = 110`,
		`let widePopUpWidth: CGFloat = 220`,
		`let acpChildIndent: CGFloat = 18`,
		`let acpListWidth: CGFloat = 250`,
		`languagePreference.widthAnchor.constraint(equalToConstant: compactPopUpWidth).isActive = true`,
		`logLevel.widthAnchor.constraint(equalToConstant: compactPopUpWidth).isActive = true`,
		`acpDefaultProfileMenu.widthAnchor.constraint(equalToConstant: compactPopUpWidth).isActive = true`,
		`browserConnectionMode.widthAnchor.constraint(equalToConstant: widePopUpWidth).isActive = true`,
		`acpOverviewContainer.edgeInsets = NSEdgeInsets(top: 0, left: acpChildIndent, bottom: 0, right: 0)`,
		`acpProfileList.widthAnchor.constraint(equalToConstant: acpListWidth).isActive = true`,
		`row.heightAnchor.constraint(equalToConstant: 28)`,
		`let panel = NSPanel(`,
		`confirmButton.target = self`,
		`confirmButton.action = #selector(confirmCustomACPDialog(_:))`,
		`dialogCancelButton.action = #selector(cancelCustomACPDialog(_:))`,
		`deleteButton.action = #selector(deleteCustomACPDialog(_:))`,
		`func windowShouldClose(_ sender: NSWindow) -> Bool`,
		`panel.delegate = self`,
		`func windowShouldClose(_ sender: NSWindow) -> Bool`,
		`panel.delegate = self`,
		`NSApp.stopModal(withCode: .cancel)`,
		`let form = NSGridView(views: [`,
		`form.rowSpacing = 10`,
		`form.columnSpacing = 12`,
		`form.column(at: 0).xPlacement = .leading`,
		`form.column(at: 0).width = 72`,
		`buttons.bottomAnchor.constraint(equalTo: contentView.bottomAnchor, constant: -18)`,
		`field.widthAnchor.constraint(equalToConstant: 360).isActive = true`,
		`nameView.addGestureRecognizer(editGesture)`,
		`showCustomACPProfileDialog(existing:`,
		`profile.id 是已有 Session 的稳定身份`,
		`let scrollView = NSScrollView()`,
		`scrollView.hasVerticalScroller = true`,
		`scrollView.documentView = scrollDocumentView`,
		`scrollDocumentView.widthAnchor.constraint(equalTo: scrollView.contentView.widthAnchor)`,
		`root.bottomAnchor.constraint(equalTo: scrollDocumentView.bottomAnchor, constant: -22)`,
		`label.widthAnchor.constraint(equalToConstant: 128)`,
		`let visibleFrame = (window.screen ?? NSScreen.main)?.visibleFrame`,
		`let nexusPairRow = NSView()`,
		`nexusDeviceTokenStatus.leadingAnchor.constraint(equalTo: nexusPairRow.leadingAnchor, constant: 140)`,
		"let startupStack = NSStackView(views: [\n            serviceAutostart,\n            menuAutostart,",
		"let serviceForm = NSStackView(views: [\n            mcpAppsEnabled,\n            formRow(title: L10n.text(\"Service port\"), control: portField),\n            formRow(title: L10n.text(\"Log level\"), control: logLevel),\n            formRow(title: L10n.text(\"Interface language\"), control: languagePreference),",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("macOS advanced settings missing responsive layout contract %q", want)
		}
	}

	for _, forbidden := range []string{
		`contentRect: NSRect(x: 0, y: 0, width: 900, height: 760)`,
		`window.minSize = NSSize(width: 780, height: 560)`,
		`contentRect: NSRect(x: 0, y: 0, width: 590, height: 850)`,
		`label.widthAnchor.constraint(equalToConstant: 92)`,
		`browserConnectionMode.widthAnchor.constraint(equalToConstant: 290)`,
		`browserConnectionMode.widthAnchor.constraint(equalToConstant: 360)`,
		`let compactPopUpWidth: CGFloat = 220`,
		`let widePopUpWidth: CGFloat = 320`,
		`let acpListWidth: CGFloat = 370`,
		`let acpListWidth: CGFloat = 460`,
		`nexusEndpoint.widthAnchor.constraint(equalToConstant: 390)`,
		`nexusPairingCode.widthAnchor.constraint(equalToConstant: 390)`,
		`box.widthAnchor.constraint(equalToConstant: 534)`,
		`formRow(title: "Device Token", control: nexusDeviceTokenStatus`,
		`let acpWorkspace = NSStackView`,
		`Profile ID`,
		`acpProfileID`,
		`acpDetailContainer`,
		`showACPDetail`,
		`showACPOverview`,
		`acpOverviewSectionLabel`,
		`L10n.text("Built-in agents")`,
		`L10n.text("Custom agents")`,
		`L10n.text("No custom agents")`,
		`L10n.text("Not configured")`,
		`labelWithString: "›"`,
		`★`,
		`let editButton = NSButton(title: title`,
		`acpProfileList.spacing = 4`,
		`acpProfileList.widthAnchor.constraint(equalTo: acpOverviewContainer.widthAnchor).isActive = true`,
		`logLevel.widthAnchor.constraint(equalToConstant: 120).isActive = true`,
		`languagePreference.widthAnchor.constraint(equalToConstant: 220).isActive = true`,
		`acpDefaultProfileMenu.widthAnchor.constraint(equalToConstant: 180).isActive = true`,
		`acpDefaultProfileMenu.widthAnchor.constraint(equalToConstant: 260).isActive = true`,
		`defaultProfileRow.widthAnchor.constraint(equalTo: acpOverviewContainer.widthAnchor).isActive = true`,
		`row.heightAnchor.constraint(equalToConstant: 38)`,
		`field.widthAnchor.constraint(equalToConstant: 380).isActive = true`,
		`alert.informativeText = L10n.text("The internal profile ID stays hidden and stable.")`,
		`alert.accessoryView = form`,
		`let alert = NSAlert()`,
		`form.column(at: 0).xPlacement = .trailing`,
		`content.spacing = 30`,
		`private final class ModalActionTarget`,
		`private final class ModalPanelCloseDelegate`,
		`#selector(ModalActionTarget.perform(_:))`,
		"let startupStack = NSStackView(views: [\n            serviceAutostart,\n            menuAutostart,\n            formRow(title: L10n.text(\"Interface language\"), control: languagePreference),",
		`closeButton.target = cancelTarget`,
		`content.spacing = 18`,
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("macOS advanced settings still contains fixed/truncated layout contract %q", forbidden)
		}
	}
}

func TestMacOSACPOverviewRowDoesNotActivateCrossHierarchyConstraintBeforeInsertion(t *testing.T) {
	path := filepath.Join("..", "..", "desktop", "macos", "AgentDockApp", "Sources", "AdvancedSettingsWindowController.swift")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read AdvancedSettingsWindowController.swift: %v", err)
	}
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	start := strings.Index(content, "private func acpOverviewRow(")
	if start < 0 {
		t.Fatal("macOS ACP overview row builder not found")
	}
	end := strings.Index(content[start:], "private func acpDisplayName(")
	if end < 0 {
		t.Fatal("macOS ACP overview row builder terminator not found")
	}
	body := content[start : start+end]
	if strings.Contains(body, "acpProfileList.widthAnchor") {
		t.Fatal("macOS ACP overview row activates a cross-hierarchy constraint before the row is inserted into the stack")
	}
}

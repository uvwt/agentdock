package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacOSSetupWindowUsesResponsiveScrollableLayout(t *testing.T) {
	root := filepath.Join("..", "..", "desktop", "macos", "AgentDockApp")
	setupData, err := os.ReadFile(filepath.Join(root, "Sources", "SetupWindowController.swift"))
	if err != nil {
		t.Fatalf("read SetupWindowController.swift: %v", err)
	}
	setup := string(setupData)

	for _, want := range []string{
		`contentRect: NSRect(x: 0, y: 0, width: 920, height: 700)`,
		`styleMask: [.titled, .closable, .miniaturizable, .resizable]`,
		`window.minSize = NSSize(width: 800, height: 540)`,
		`private let scrollDocumentView = TopAlignedDocumentView()`,
		`let sidebar = NSBox()`,
		`sidebar.fillColor = ControlPanelUI.sidebarColor`,
		`contentBackground.fillColor = ControlPanelUI.canvasColor`,
		`ControlPanelUI.sidebarButton(`,
		`case home`,
		`case connections`,
		`case capabilities`,
		`case activity`,
		`case settings`,
		`let scrollView = NSScrollView()`,
		`scrollView.hasVerticalScroller = true`,
		`scrollView.documentView = scrollDocumentView`,
		`scrollDocumentView.widthAnchor.constraint(equalTo: scrollView.contentView.widthAnchor)`,
		`contentStack.leadingAnchor.constraint(equalTo: scrollDocumentView.leadingAnchor, constant: 30)`,
		`contentStack.bottomAnchor.constraint(equalTo: scrollDocumentView.bottomAnchor, constant: -26)`,
		`ControlPanelUI.hero(content: homeHeroContent)`,
		`ControlPanelUI.summaryTile(`,
		`remoteConfigurationStack.isHidden = true`,
		`@objc private func toggleRemoteConfiguration()`,
		`publicAddress.lineBreakMode = .byCharWrapping`,
		`publicAddress.maximumNumberOfLines = 2`,
		`L10n.text("Check permissions")`,
		`L10n.text("Advanced settings")`,
	} {
		if !strings.Contains(setup, want) {
			t.Fatalf("macOS setup window missing responsive layout contract %q", want)
		}
	}

	componentsData, err := os.ReadFile(filepath.Join(root, "Sources", "PermissionUIComponents.swift"))
	if err != nil {
		t.Fatalf("read PermissionUIComponents.swift: %v", err)
	}
	components := string(componentsData)
	for _, want := range []string{
		`final class TopAlignedDocumentView: NSView`,
		`override var isFlipped: Bool { true }`,
	} {
		if !strings.Contains(components, want) {
			t.Fatalf("macOS setup window missing top-aligned document contract %q", want)
		}
	}

	for _, forbidden := range []string{
		`widthAnchor.constraint(equalToConstant: 564)`,
		`publicCheckStatus.widthAnchor.constraint(equalToConstant: 450)`,
		`serverURLField.widthAnchor.constraint(equalToConstant: 430)`,
		`field.widthAnchor.constraint(equalToConstant: actions.count > 1 ? 310 : 370)`,
		`L10n.text("Check permissions…")`,
		`L10n.text("Advanced settings…")`,
	} {
		if strings.Contains(setup, forbidden) {
			t.Fatalf("macOS setup window still contains fixed/truncated layout contract %q", forbidden)
		}
	}

	resources := map[string][]string{
		filepath.Join("Resources", "en.lproj", "Localizable.strings"): {
			`"Check permissions" = "Check permissions";`,
			`"Advanced settings" = "Advanced settings";`,
		},
		filepath.Join("Resources", "zh-Hans.lproj", "Localizable.strings"): {
			`"Check permissions" = "权限检查";`,
			`"Advanced settings" = "高级设置";`,
		},
	}
	for relativePath, wants := range resources {
		data, err := os.ReadFile(filepath.Join(root, relativePath))
		if err != nil {
			t.Fatalf("read %s: %v", relativePath, err)
		}
		content := string(data)
		for _, want := range wants {
			if !strings.Contains(content, want) {
				t.Fatalf("macOS localization missing %q in %s", want, relativePath)
			}
		}
		if strings.Contains(content, "Check permissions…") || strings.Contains(content, "Advanced settings…") {
			t.Fatalf("macOS localization must not keep ellipsis in setup/menu button labels: %s", relativePath)
		}
	}
}

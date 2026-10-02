package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacOSApplicationMenuWindowShortcuts(t *testing.T) {
	root := filepath.Join("..", "..", "desktop", "macos", "AgentDockApp")
	menuData, err := os.ReadFile(filepath.Join(root, "Sources", "ApplicationMenu.swift"))
	if err != nil {
		t.Fatalf("read ApplicationMenu.swift: %v", err)
	}
	menu := string(menuData)
	for _, want := range []string{
		"let windowMenu = NSMenu(title: L10n.text(\"Window\"))",
		"title: L10n.text(\"Close Window\")",
		"action: #selector(NSWindow.performClose(_:))",
		"keyEquivalent: \"w\"",
		"title: L10n.text(\"Minimize\")",
		"action: #selector(NSWindow.performMiniaturize(_:))",
		"keyEquivalent: \"m\"",
		"NSApp.windowsMenu = windowMenu",
	} {
		if !strings.Contains(menu, want) {
			t.Fatalf("macOS application menu missing window shortcut contract %q", want)
		}
	}

	resources := map[string][]string{
		filepath.Join("Resources", "en.lproj", "Localizable.strings"): {
			"\"Window\" = \"Window\";",
			"\"Close Window\" = \"Close Window\";",
			"\"Minimize\" = \"Minimize\";",
		},
		filepath.Join("Resources", "zh-Hans.lproj", "Localizable.strings"): {
			"\"Window\" = \"窗口\";",
			"\"Close Window\" = \"关闭窗口\";",
			"\"Minimize\" = \"最小化\";",
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
	}
}

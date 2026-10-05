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

	fileStart := strings.Index(menu, `let fileMenu = NSMenu(title: L10n.text("File"))`)
	editStart := strings.Index(menu, `let editMenu = NSMenu(title: L10n.text("Edit"))`)
	windowStart := strings.Index(menu, `let windowMenu = NSMenu(title: L10n.text("Window"))`)
	if fileStart < 0 || editStart < 0 || windowStart < 0 || !(fileStart < editStart && editStart < windowStart) {
		t.Fatalf("macOS application menu must keep File, Edit, Window order")
	}

	fileMenu := menu[fileStart:editStart]
	for _, want := range []string{
		`title: L10n.text("Close Window")`,
		`action: #selector(NSWindow.performClose(_:))`,
		`keyEquivalent: "w"`,
	} {
		if !strings.Contains(fileMenu, want) {
			t.Fatalf("macOS File menu missing close-window contract %q", want)
		}
	}

	windowMenu := menu[windowStart:]
	for _, want := range []string{
		`title: L10n.text("Minimize")`,
		`action: #selector(NSWindow.performMiniaturize(_:))`,
		`keyEquivalent: "m"`,
		`NSApp.windowsMenu = windowMenu`,
	} {
		if !strings.Contains(windowMenu, want) {
			t.Fatalf("macOS Window menu missing minimize contract %q", want)
		}
	}
	if strings.Contains(windowMenu, `title: L10n.text("Close Window")`) {
		t.Fatal("macOS Close Window must stay in File menu, not Window menu")
	}

	resources := map[string][]string{
		filepath.Join("Resources", "en.lproj", "Localizable.strings"): {
			`"File" = "File";`,
			`"Close Window" = "Close Window";`,
			`"Window" = "Window";`,
			`"Minimize" = "Minimize";`,
		},
		filepath.Join("Resources", "zh-Hans.lproj", "Localizable.strings"): {
			`"File" = "文件";`,
			`"Close Window" = "关闭窗口";`,
			`"Window" = "窗口";`,
			`"Minimize" = "最小化";`,
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

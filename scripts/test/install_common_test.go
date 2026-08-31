package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnifiedInstallerEntriesReplaceLegacyNames(t *testing.T) {
	for _, path := range []string{
		"../install/install.sh",
		"../install/install-linux-platform.sh",
		"../install/uninstall-linux.sh",
		"../install/install-macos-platform.sh",
		"../install/install.ps1",
	} {
		if info, err := os.Stat(path); err != nil {
			t.Fatalf("required installer file %s: %v", path, err)
		} else if !info.Mode().IsRegular() {
			t.Fatalf("required installer path is not a regular file: %s", path)
		}
	}

	for _, legacyPath := range []string{
		"install-linux.sh",
		"install-linux-bootstrap.sh",
		"install-macos.sh",
		"install-windows.ps1",
	} {
		if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
			t.Fatalf("legacy installer entry must not exist: %s", legacyPath)
		}
	}

	data, err := os.ReadFile("../install/install.sh")
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}
	entry := string(data)
	for _, want := range []string{
		"install-linux-platform.sh",
		"uninstall-linux.sh",
		"install-macos-platform.sh",
		"AGENTDOCK_INSTALLER_BASE_URL",
		"verify_checksum",
	} {
		if !strings.Contains(entry, want) {
			t.Fatalf("install.sh missing %q", want)
		}
	}
}
func TestDesktopRuntimeSurfacesDoNotUseLegacyLaunchers(t *testing.T) {
	trayData, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "tray", "app_windows.go"))
	if err != nil {
		t.Fatalf("read Windows tray: %v", err)
	}
	tray := string(trayData)
	for _, want := range []string{
		"runNativeAgentDock",
		"procSetClipboardData",
		"procShellExecuteW",
		`"tunnel", "regenerate"`,
		`"service", "restart"`,
	} {
		if !strings.Contains(tray, want) {
			t.Fatalf("Windows tray missing native runtime behavior %q", want)
		}
	}
	for _, forbidden := range []string{
		"powershell.exe",
		"startPowerShellScript",
		"AgentDockLauncher",
		"CloudflaredLauncher",
	} {
		if strings.Contains(tray, forbidden) {
			t.Fatalf("Windows tray still depends on legacy launcher %q", forbidden)
		}
	}

	selfUpdateData, err := os.ReadFile(filepath.Join("..", "..", "internal", "selfupdate", "service_darwin.go"))
	if err != nil {
		t.Fatalf("read macOS self-update service adapter: %v", err)
	}
	selfUpdate := string(selfUpdateData)
	for _, want := range []string{
		`"ProgramArguments.0": paths.binary`,
		`"ProgramArguments.2": "launch-core"`,
		`"ProgramArguments.4": paths.runtimeRoot`,
	} {
		if !strings.Contains(selfUpdate, want) {
			t.Fatalf("macOS self-update adapter missing native LaunchAgent contract %q", want)
		}
	}
	for _, forbidden := range []string{"start-agentdock.sh", "startScript"} {
		if strings.Contains(selfUpdate, forbidden) {
			t.Fatalf("macOS self-update adapter still depends on legacy launcher %q", forbidden)
		}
	}
}

//go:build windows

package selfupdate

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/uvwt/agentdock/internal/desktopruntime"
)

func TestWindowsDesktopUpdateInstallRestoreAndCommit(t *testing.T) {
	runtimeRoot := t.TempDir()
	binDir := filepath.Join(runtimeRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}

	corePath := filepath.Join(binDir, "agentdock.exe")
	trayPath := filepath.Join(binDir, "agentdock-tray.exe")
	iconPath := filepath.Join(binDir, "agentdock.ico")
	for path, content := range map[string]string{
		corePath: "core",
		trayPath: "tray-old",
		iconPath: "icon-old",
	} {
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifest := desktopruntime.Manifest{
		SchemaVersion:   desktopruntime.SchemaVersion,
		InstallRoot:     runtimeRoot,
		AgentDockBinary: corePath,
		TrayBinary:      trayPath,
		Host:            "127.0.0.1",
		Port:            28765,
		LocalMCPURL:     "http://127.0.0.1:28765/mcp",
		TunnelMode:      "none",
		InstallChannel:  "setup",
	}
	if err := desktopruntime.Save(filepath.Join(runtimeRoot, "runtime.json"), manifest); err != nil {
		t.Fatal(err)
	}

	stagedRoot := filepath.Join(t.TempDir(), "desktop")
	if err := os.Mkdir(stagedRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"agentdock-tray.exe":      "tray-new",
		"agentdock.ico":           "icon-new",
		windowsDesktopVersionFile: "v0.7.5\n",
	} {
		if err := os.WriteFile(filepath.Join(stagedRoot, name), []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	update, err := prepareWindowsDesktopUpdate(corePath, runtimeRoot, stagedRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := update.Install(); err != nil {
		t.Fatal(err)
	}
	assertWindowsFileContent(t, trayPath, "tray-new")
	assertWindowsFileContent(t, iconPath, "icon-new")
	assertWindowsFileContent(t, filepath.Join(runtimeRoot, windowsDesktopVersionFile), "v0.7.5\n")

	if err := update.Restore(); err != nil {
		t.Fatal(err)
	}
	assertWindowsFileContent(t, trayPath, "tray-old")
	assertWindowsFileContent(t, iconPath, "icon-old")
	if _, err := os.Stat(filepath.Join(runtimeRoot, windowsDesktopVersionFile)); !os.IsNotExist(err) {
		t.Fatalf("desktop version marker survived rollback: %v", err)
	}
	if err := update.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyWindowsDesktopOnlyWritesVersionMarker(t *testing.T) {
	runtimeRoot := t.TempDir()
	binDir := filepath.Join(runtimeRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	corePath := filepath.Join(binDir, "agentdock.exe")
	trayPath := filepath.Join(binDir, "agentdock-tray.exe")
	for path, content := range map[string]string{
		corePath:                               "core",
		trayPath:                               "tray-old",
		filepath.Join(binDir, "agentdock.ico"): "icon-old",
	} {
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := desktopruntime.Save(filepath.Join(runtimeRoot, "runtime.json"), desktopruntime.Manifest{
		SchemaVersion:   desktopruntime.SchemaVersion,
		InstallRoot:     runtimeRoot,
		AgentDockBinary: corePath,
		TrayBinary:      trayPath,
		Host:            "127.0.0.1",
		Port:            28766,
		LocalMCPURL:     "http://127.0.0.1:28766/mcp",
		TunnelMode:      "none",
		InstallChannel:  "setup",
	}); err != nil {
		t.Fatal(err)
	}
	stagedRoot := filepath.Join(t.TempDir(), "desktop")
	if err := os.Mkdir(stagedRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"agentdock-tray.exe":      "tray-new",
		"agentdock.ico":           "icon-new",
		windowsDesktopVersionFile: "v0.7.5\n",
	} {
		if err := os.WriteFile(filepath.Join(stagedRoot, name), []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	_, err := applyWindowsDesktopOnlyUpdate(context.Background(), applyRequest{
		CurrentPath:       corePath,
		CurrentVersion:    "v0.7.5",
		DesktopTargetPath: runtimeRoot,
		DesktopStagedPath: stagedRoot,
		DesktopOnly:       true,
		TargetVersion:     "v0.7.5",
		Output:            io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertWindowsFileContent(t, trayPath, "tray-new")
	assertWindowsFileContent(t, filepath.Join(runtimeRoot, windowsDesktopVersionFile), "v0.7.5\n")
}

func TestWindowsScheduledTaskPath(t *testing.T) {
	for input, expected := range map[string]string{
		"AgentDock":  `\AgentDock`,
		`\AgentDock`: `\AgentDock`,
		"":           `\AgentDock`,
	} {
		if got := windowsScheduledTaskPath(input); got != expected {
			t.Fatalf("windowsScheduledTaskPath(%q) = %q, want %q", input, got, expected)
		}
	}
}

func assertWindowsFileContent(t *testing.T, path, expected string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != expected {
		t.Fatalf("%s = %q, want %q", path, data, expected)
	}
}

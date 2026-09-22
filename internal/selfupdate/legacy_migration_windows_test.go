//go:build windows

package selfupdate

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsLegacyMigrationNeededOnlyForManagedFlatCore(t *testing.T) {
	root := t.TempDir()
	opts := options{
		CurrentVersion:    "0.8.4",
		DesktopTargetPath: root,
		ExecutablePath:    filepath.Join(root, "bin", "agentdock.exe"),
	}
	if !windowsLegacyMigrationNeeded(opts) {
		t.Fatal("managed flat Core must require one-time generation migration")
	}
	opts.DesktopCurrentVersion = "0.8.3"
	if windowsLegacyMigrationReady(opts) {
		t.Fatal("mixed-version flat Core/Tray must repair the desktop payload before migration")
	}
	opts.DesktopCurrentVersion = "0.8.4"
	if !windowsLegacyMigrationReady(opts) {
		t.Fatal("aligned flat Core/Tray must be ready for generation migration")
	}

	opts.ExecutablePath = filepath.Join(root, "versions", "v0.8.4", "agentdock-core.exe")
	if windowsLegacyMigrationNeeded(opts) {
		t.Fatal("clean generation Core must not re-enter legacy migration")
	}

	compatManager := filepath.Join(root, "installer", "manage-windows.ps1")
	if err := os.MkdirAll(filepath.Dir(compatManager), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(compatManager, []byte("legacy bridge marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !windowsLegacyMigrationNeeded(opts) {
		t.Fatal("generation Core must resume a partially completed legacy migration while the compatibility marker remains")
	}

	if err := os.Remove(compatManager); err != nil {
		t.Fatal(err)
	}
	opts.ExecutablePath = filepath.Join(root, "portable", "agentdock.exe")
	if windowsLegacyMigrationNeeded(opts) {
		t.Fatal("portable Core must stay outside managed desktop migration")
	}
}

func TestValidateWindowsLegacyMigrationPlanRequiresStableEntries(t *testing.T) {
	root := t.TempDir()
	valid := windowsLegacyMigrationPlan{
		ParentPID:   1234,
		RuntimeRoot: root,
		CorePath:    filepath.Join(root, "bin", "agentdock.exe"),
		TrayPath:    filepath.Join(root, "bin", "agentdock-tray.exe"),
		PayloadDir:  filepath.Join(root, "payload"),
		Version:     "v0.8.4",
		CleanupRoot: filepath.Join(root, "cleanup"),
	}
	if err := validateWindowsLegacyMigrationPlan(valid); err != nil {
		t.Fatalf("valid plan rejected: %v", err)
	}

	invalid := valid
	invalid.CorePath = filepath.Join(root, "legacy", "agentdock.exe")
	if err := validateWindowsLegacyMigrationPlan(invalid); err == nil {
		t.Fatal("migration must reject a Core outside the stable managed entry")
	}
}

func TestValidateWindowsLegacyMigrationPayloadRequiresGenerationInfrastructure(t *testing.T) {
	root := t.TempDir()
	files := []string{
		"agentdock-arbiter.exe",
		"agentdock-shim.exe",
		"agentdock-tray-shim.exe",
		filepath.Join("wsl-helper", "manifest.json"),
		filepath.Join("wsl-helper", "agentdock-wsl-helper-linux-amd64"),
		filepath.Join("wsl-helper", "agentdock-wsl-helper-linux-arm64"),
	}
	for _, relative := range files {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(relative), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateWindowsLegacyMigrationPayload(root); err != nil {
		t.Fatalf("complete payload rejected: %v", err)
	}

	if err := os.Remove(filepath.Join(root, "agentdock-arbiter.exe")); err != nil {
		t.Fatal(err)
	}
	if err := validateWindowsLegacyMigrationPayload(root); err == nil {
		t.Fatal("payload without arbiter must be rejected before stable entries are changed")
	}
}

func TestWindowsLegacyMigrationInProgressDetectsHeldMutex(t *testing.T) {
	mutexName, err := windows.UTF16PtrFromString(windowsLegacyMigrationMutexName)
	if err != nil {
		t.Fatal(err)
	}
	mutex, err := windows.CreateMutex(nil, true, mutexName)
	if err != nil && err != windows.ERROR_ALREADY_EXISTS {
		t.Fatal(err)
	}
	if err == windows.ERROR_ALREADY_EXISTS {
		_ = windows.CloseHandle(mutex)
		t.Skip("another legacy migration mutex is already active in this Windows session")
	}
	defer func() {
		_ = windows.ReleaseMutex(mutex)
		_ = windows.CloseHandle(mutex)
	}()

	inProgress, err := windowsLegacyMigrationInProgress()
	if err != nil {
		t.Fatal(err)
	}
	if !inProgress {
		t.Fatal("held migration mutex must suppress immediate recursive migration")
	}
}

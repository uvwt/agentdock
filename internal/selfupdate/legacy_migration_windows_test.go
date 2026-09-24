//go:build windows

package selfupdate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

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
	cleanupRoot, err := os.MkdirTemp("", "agentdock-legacy-migration-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(cleanupRoot) })
	valid := windowsLegacyMigrationPlan{
		ParentPID:   1234,
		RuntimeRoot: root,
		CorePath:    filepath.Join(root, "bin", "agentdock.exe"),
		TrayPath:    filepath.Join(root, "bin", "agentdock-tray.exe"),
		PayloadDir:  filepath.Join(cleanupRoot, "payload"),
		Version:     "v0.8.4",
		CleanupRoot: cleanupRoot,
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

func TestRunWindowsLegacyMigrationHelperCleansMalformedPlanTemp(t *testing.T) {
	cleanupRoot, err := os.MkdirTemp("", "agentdock-legacy-migration-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(cleanupRoot) })

	helperPath := filepath.Join(cleanupRoot, "agentdock-legacy-migration-helper.exe")
	planPath := filepath.Join(cleanupRoot, "migration-plan.json")
	if err := os.WriteFile(planPath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runWindowsLegacyMigrationHelper(context.Background(), helperPath, planPath); err == nil {
		t.Fatal("malformed migration plan must fail")
	}
	waitForWindowsLegacyMigrationCleanup(t, cleanupRoot)
}

func TestRunWindowsLegacyMigrationHelperCleansInvalidPlanTemp(t *testing.T) {
	cleanupRoot, err := os.MkdirTemp("", "agentdock-legacy-migration-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(cleanupRoot) })

	helperPath := filepath.Join(cleanupRoot, "agentdock-legacy-migration-helper.exe")
	planPath := filepath.Join(cleanupRoot, "migration-plan.json")
	plan := windowsLegacyMigrationPlan{
		ParentPID:   0,
		RuntimeRoot: t.TempDir(),
		CorePath:    "invalid",
		TrayPath:    "invalid",
		PayloadDir:  filepath.Join(cleanupRoot, "payload"),
		Version:     "v0.9.0",
		CleanupRoot: cleanupRoot,
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runWindowsLegacyMigrationHelper(context.Background(), helperPath, planPath); err == nil {
		t.Fatal("invalid migration plan must fail")
	}
	waitForWindowsLegacyMigrationCleanup(t, cleanupRoot)
}

func TestWindowsLegacyMigrationCleanupRootRequiresMatchingHelperAndPlan(t *testing.T) {
	cleanupRoot, err := os.MkdirTemp("", "agentdock-legacy-migration-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(cleanupRoot) })

	helperPath := filepath.Join(cleanupRoot, "agentdock-legacy-migration-helper.exe")
	planPath := filepath.Join(cleanupRoot, "migration-plan.json")
	if root, ok := windowsLegacyMigrationCleanupRootForHelper(helperPath, planPath); !ok || !sameWindowsPath(root, cleanupRoot) {
		t.Fatalf("matching helper/plan root = %q, ok=%v", root, ok)
	}
	if _, ok := windowsLegacyMigrationCleanupRootForHelper(filepath.Join(cleanupRoot, "agentdock.exe"), planPath); ok {
		t.Fatal("non-helper executable must not own cleanup")
	}
	otherRoot, err := os.MkdirTemp("", "agentdock-legacy-migration-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(otherRoot) })
	if _, ok := windowsLegacyMigrationCleanupRootForHelper(helperPath, filepath.Join(otherRoot, "migration-plan.json")); ok {
		t.Fatal("plan from another Temp root must not be cleaned")
	}
}

func TestFinalizeWindowsLegacyMigrationCleansTempOnPreflightFailure(t *testing.T) {
	runtimeRoot := t.TempDir()
	cleanupRoot, err := os.MkdirTemp("", "agentdock-legacy-migration-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(cleanupRoot) })

	payloadDir := filepath.Join(cleanupRoot, "payload")
	if err := os.MkdirAll(payloadDir, 0o700); err != nil {
		t.Fatal(err)
	}
	plan := windowsLegacyMigrationPlan{
		ParentPID:   int(^uint32(0) >> 1),
		RuntimeRoot: runtimeRoot,
		CorePath:    filepath.Join(runtimeRoot, "bin", "agentdock.exe"),
		TrayPath:    filepath.Join(runtimeRoot, "bin", "agentdock-tray.exe"),
		PayloadDir:  payloadDir,
		Version:     "v0.9.0",
		CleanupRoot: cleanupRoot,
	}
	if err := finalizeWindowsLegacyMigration(context.Background(), plan); err == nil {
		t.Fatal("incomplete migration payload must fail preflight")
	}
	waitForWindowsLegacyMigrationCleanup(t, cleanupRoot)
}

func TestFinalizeWindowsLegacyMigrationWaitsForActiveMutexBeforeCleanup(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

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
		t.Skip("another legacy migration mutex is already active")
	}

	cleanupRoot, err := os.MkdirTemp("", "agentdock-legacy-migration-*")
	if err != nil {
		_ = windows.ReleaseMutex(mutex)
		_ = windows.CloseHandle(mutex)
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(cleanupRoot) })

	runtimeRoot := t.TempDir()
	plan := windowsLegacyMigrationPlan{
		ParentPID:   int(^uint32(0) >> 1),
		RuntimeRoot: runtimeRoot,
		CorePath:    filepath.Join(runtimeRoot, "bin", "agentdock.exe"),
		TrayPath:    filepath.Join(runtimeRoot, "bin", "agentdock-tray.exe"),
		PayloadDir:  filepath.Join(cleanupRoot, "payload"),
		Version:     "v0.9.0",
		CleanupRoot: cleanupRoot,
	}
	done := make(chan error, 1)
	go func() {
		done <- finalizeWindowsLegacyMigration(context.Background(), plan)
	}()

	select {
	case err := <-done:
		_ = windows.ReleaseMutex(mutex)
		_ = windows.CloseHandle(mutex)
		t.Fatalf("duplicate helper returned before active mutex was released: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if _, err := os.Stat(cleanupRoot); err != nil {
		_ = windows.ReleaseMutex(mutex)
		_ = windows.CloseHandle(mutex)
		t.Fatalf("cleanup root changed while another migration held the mutex: %v", err)
	}

	if err := windows.ReleaseMutex(mutex); err != nil {
		_ = windows.CloseHandle(mutex)
		t.Fatal(err)
	}
	_ = windows.CloseHandle(mutex)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("duplicate helper after mutex release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("duplicate helper did not finish after active mutex was released")
	}
	waitForWindowsLegacyMigrationCleanup(t, cleanupRoot)
}

func TestValidateWindowsLegacyMigrationCleanupRootRejectsArbitraryDirectory(t *testing.T) {
	if err := validateWindowsLegacyMigrationCleanupRoot(t.TempDir()); err == nil {
		t.Fatal("arbitrary Temp child must not be accepted as a migration cleanup root")
	}
}

func waitForWindowsLegacyMigrationCleanup(t *testing.T, root string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, err := os.Stat(root)
		if os.IsNotExist(err) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("legacy migration Temp root was not cleaned: %s", root)
		}
		time.Sleep(100 * time.Millisecond)
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

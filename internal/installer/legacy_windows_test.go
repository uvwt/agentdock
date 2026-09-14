package installer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/fs/processlock"
	"github.com/uvwt/agentdock/internal/updateengine"
)

func TestPrepareWindowsLegacyGenerationSeedsInstallerFallback(t *testing.T) {
	root := t.TempDir()
	legacyCore := filepath.Join(root, "legacy", "agentdock.exe")
	legacyTray := filepath.Join(root, "legacy", "agentdock-tray.exe")
	targetPayload := filepath.Join(root, "payload")
	writeTestFile(t, legacyCore, "legacy-core")
	writeTestFile(t, legacyTray, "legacy-tray")
	writeWindowsMigrationPayload(t, targetPayload, "target")

	prepared, err := PrepareWindowsLegacyGeneration(context.Background(), WindowsLegacyBootstrapRequest{
		InstallRoot: root,
		Version:     "0.8.2",
		CorePath:    legacyCore,
		TrayPath:    legacyTray,
		PayloadDir:  targetPayload,
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Version != "v0.8.2" || prepared.AlreadyReady {
		t.Fatalf("prepared=%+v", prepared)
	}

	layout, err := updateengine.NewWindowsLayout(root)
	if err != nil {
		t.Fatal(err)
	}
	store, err := updateengine.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	active, err := store.ReadActive()
	if err != nil {
		t.Fatal(err)
	}
	if active.State != updateengine.StateCommitted || active.ActiveVersion != "v0.8.2" {
		t.Fatalf("legacy active=%+v", active)
	}
	assertFileBody(t, layout.GenerationCore("v0.8.2"), "legacy-core")
	assertFileBody(t, layout.GenerationTray("v0.8.2"), "legacy-tray")
	assertFileBody(t, layout.GenerationArbiter("v0.8.2"), "target-arbiter")
	if got := existingVersion(Request{InstallRoot: root, RuntimeRoot: root}); got != "v0.8.2" {
		t.Fatalf("existingVersion=%q, want v0.8.2", got)
	}

	// A different-version install must now publish target as trial with the real legacy
	// generation as fallback. This is the state that was missing in the Tianyi v0.8.2 E2E.
	request := Request{
		InstallRoot:  root,
		RuntimeRoot:  root,
		PayloadDir:   targetPayload,
		Version:      "v0.8.3",
		StartService: false,
	}
	journal := newJournal(root, "legacy-upgrade")
	if _, err := stageWindowsPayload(request, journal); err != nil {
		t.Fatal(err)
	}
	active, err = store.ReadActive()
	if err != nil {
		t.Fatal(err)
	}
	if active.State != updateengine.StateTrial || active.ActiveVersion != "v0.8.3" || active.FallbackVersion != "v0.8.2" || active.TransactionID != "legacy-upgrade" {
		t.Fatalf("upgrade active=%+v", active)
	}
	if _, err := os.Stat(layout.GenerationCore("v0.8.2")); err != nil {
		t.Fatalf("legacy fallback generation disappeared: %v", err)
	}
}

func TestPrepareWindowsLegacyGenerationRejectsUnsafeVersion(t *testing.T) {
	root := t.TempDir()
	legacyCore := filepath.Join(root, "legacy", "agentdock.exe")
	legacyTray := filepath.Join(root, "legacy", "agentdock-tray.exe")
	payload := filepath.Join(root, "payload")
	writeTestFile(t, legacyCore, "legacy-core")
	writeTestFile(t, legacyTray, "legacy-tray")
	writeWindowsMigrationPayload(t, payload, "target")

	_, err := PrepareWindowsLegacyGeneration(context.Background(), WindowsLegacyBootstrapRequest{
		InstallRoot: root,
		Version:     `0.8.2\\..\\outside`,
		CorePath:    legacyCore,
		TrayPath:    legacyTray,
		PayloadDir:  payload,
	})
	if err == nil {
		t.Fatal("unsafe legacy version must not become a generation path")
	}
	if _, statErr := os.Stat(filepath.Join(root, "outside")); !os.IsNotExist(statErr) {
		t.Fatalf("unsafe version escaped versions root: %v", statErr)
	}
}

func TestPrepareWindowsLegacyGenerationIsIdempotentButRejectsAnotherActiveVersion(t *testing.T) {
	root := t.TempDir()
	legacyCore := filepath.Join(root, "legacy", "agentdock.exe")
	legacyTray := filepath.Join(root, "legacy", "agentdock-tray.exe")
	payload := filepath.Join(root, "payload")
	writeTestFile(t, legacyCore, "legacy-core")
	writeTestFile(t, legacyTray, "legacy-tray")
	writeWindowsMigrationPayload(t, payload, "target")
	request := WindowsLegacyBootstrapRequest{InstallRoot: root, Version: "v0.8.2", CorePath: legacyCore, TrayPath: legacyTray, PayloadDir: payload}

	if _, err := PrepareWindowsLegacyGeneration(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	second, err := PrepareWindowsLegacyGeneration(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !second.AlreadyReady {
		t.Fatal("second prepare must reuse the committed legacy source generation")
	}

	store, err := updateengine.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteActive(updateengine.ActiveVersion{SchemaVersion: updateengine.SchemaVersion, ActiveVersion: "v0.8.1", State: updateengine.StateCommitted}); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareWindowsLegacyGeneration(context.Background(), request); err == nil {
		t.Fatal("prepare must not overwrite another committed active generation")
	}
}

func TestPrepareWindowsLegacyGenerationUsesInstallerTransactionLock(t *testing.T) {
	root := t.TempDir()
	legacyCore := filepath.Join(root, "legacy", "agentdock.exe")
	legacyTray := filepath.Join(root, "legacy", "agentdock-tray.exe")
	payload := filepath.Join(root, "payload")
	writeTestFile(t, legacyCore, "legacy-core")
	writeTestFile(t, legacyTray, "legacy-tray")
	writeWindowsMigrationPayload(t, payload, "target")

	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	held, err := processlock.Acquire(context.Background(), store.LockPath())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = PrepareWindowsLegacyGeneration(ctx, WindowsLegacyBootstrapRequest{
		InstallRoot: root,
		Version:     "v0.8.2",
		CorePath:    legacyCore,
		TrayPath:    legacyTray,
		PayloadDir:  payload,
	})
	if err == nil {
		t.Fatal("legacy migration must not bypass an active Installer transaction lock")
	}
	updateStore, err := updateengine.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := updateStore.ReadActive(); !os.IsNotExist(err) {
		t.Fatalf("contended migration wrote active pointer: %v", err)
	}
}

func writeWindowsMigrationPayload(t *testing.T, root, prefix string) {
	t.Helper()
	for name, body := range map[string]string{
		"agentdock.exe":                              prefix + "-core",
		"agentdock-tray.exe":                         prefix + "-tray",
		"agentdock-arbiter.exe":                      prefix + "-arbiter",
		filepath.Join("wsl-helper", "manifest.json"): "{}",
		filepath.Join("wsl-helper", "agentdock-wsl-helper-linux-amd64"): "amd64",
		filepath.Join("wsl-helper", "agentdock-wsl-helper-linux-arm64"): "arm64",
	} {
		writeTestFile(t, filepath.Join(root, name), body)
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertFileBody(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s=%q, want %q", path, got, want)
	}
}

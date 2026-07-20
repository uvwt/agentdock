package skillstate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestStoreSupportsMultipleVersionsAndAtomicActivation(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1.0.0", "1.1.0"} {
		path, pathErr := store.InstalledPath("demo-skill", version)
		if pathErr != nil {
			t.Fatal(pathErr)
		}
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Activate(context.Background(), "demo-skill", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := store.Activate(context.Background(), "demo-skill", "1.1.0"); err != nil {
		t.Fatal(err)
	}
	active, err := store.ActiveVersion("demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	if active != "1.1.0" {
		t.Fatalf("active version = %q, want 1.1.0", active)
	}
	previous, err := store.PreviousVersion("demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	if previous != "1.0.0" {
		t.Fatalf("previous version = %q, want 1.0.0", previous)
	}
	resolved, err := store.Resolve("demo-skill", "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(resolved) != "1.1.0" {
		t.Fatalf("active version resolved to %q", resolved)
	}
	explicit, err := store.Resolve("demo-skill", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(explicit) != "1.0.0" {
		t.Fatalf("explicit version resolved to %q", explicit)
	}
	versions, err := store.ListVersions("demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 {
		t.Fatalf("versions = %#v", versions)
	}
}

func TestAcquireRemovesOnlySafeStaleLocks(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(store.Root(), "locks", "demo.lock")
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatal(err)
	}
	staleAt := time.Now().Add(-11 * time.Minute)
	if err := os.Chtimes(lockPath, staleAt, staleAt); err != nil {
		t.Fatal(err)
	}
	release, err := store.acquire(context.Background(), "demo")
	if err != nil {
		t.Fatalf("acquire stale empty lock: %v", err)
	}
	if owner := lockOwnerName(t, lockPath); !strings.HasPrefix(owner, lockOwnerPrefix) {
		release()
		t.Fatalf("owner = %q", owner)
	}
	release()

	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lockPath, "unknown"), []byte("do not delete"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(lockPath, staleAt, staleAt); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if _, err := store.acquire(ctx, "demo"); err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("acquire unknown stale lock error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(lockPath, "unknown")); err != nil {
		t.Fatalf("unknown lock content was removed: %v", err)
	}
}

func TestStaleLockReleaseCannotDeleteReplacementOwner(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	firstRelease, err := store.acquire(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(store.Root(), "locks", "demo.lock")
	firstOwner := lockOwnerName(t, lockPath)
	staleAt := time.Now().Add(-11 * time.Minute)
	if err := os.Chtimes(lockPath, staleAt, staleAt); err != nil {
		firstRelease()
		t.Fatal(err)
	}

	secondRelease, err := store.acquire(context.Background(), "demo")
	if err != nil {
		firstRelease()
		t.Fatal(err)
	}
	secondOwner := lockOwnerName(t, lockPath)
	if firstOwner == secondOwner {
		firstRelease()
		secondRelease()
		t.Fatal("stale lock takeover reused the same owner")
	}

	firstRelease()
	ownerAfterOldRelease := lockOwnerName(t, lockPath)
	if ownerAfterOldRelease != secondOwner {
		secondRelease()
		t.Fatalf("replacement owner changed: %q", ownerAfterOldRelease)
	}
	secondRelease()
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("replacement lock remained after owner release: %v", err)
	}
}

func lockOwnerName(t *testing.T, lockPath string) string {
	t.Helper()
	entries, err := os.ReadDir(lockPath)
	if err != nil {
		t.Fatalf("read lock directory: %v", err)
	}
	if len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), lockOwnerPrefix) {
		t.Fatalf("lock owners = %#v", entries)
	}
	return entries[0].Name()
}

func TestActivateKeepsPreviousStateWhenAtomicSaveFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test requires POSIX directory permissions")
	}
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1.0.0", "2.0.0"} {
		path, pathErr := store.InstalledPath("demo", version)
		if pathErr != nil {
			t.Fatal(pathErr)
		}
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Activate(context.Background(), "demo", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(store.Root(), "state")
	if err := os.Chmod(stateDir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(stateDir, 0o700)
	probe := filepath.Join(stateDir, ".permission-probe")
	if err := os.WriteFile(probe, []byte("probe"), 0o600); err == nil {
		_ = os.Remove(probe)
		t.Skip("filesystem permissions do not block writes for this test user")
	}

	if err := store.Activate(context.Background(), "demo", "2.0.0"); err == nil {
		t.Fatal("Activate() succeeded despite unwritable state directory")
	}
	active, err := store.ActiveVersion("demo")
	if err != nil {
		t.Fatal(err)
	}
	if active != "1.0.0" {
		t.Fatalf("active version = %q, want previous version 1.0.0", active)
	}
}

func TestActivationDoesNotCreateLegacyActiveSymlink(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path, err := store.InstalledPath("demo", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.Activate(context.Background(), "demo", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.Root(), "active")); !os.IsNotExist(err) {
		t.Fatalf("legacy active directory exists: %v", err)
	}
}

func TestActivateRechecksInstalledVersionAfterAcquiringLock(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path, err := store.InstalledPath("demo", "2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}

	release, err := store.acquire(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		result <- store.Activate(context.Background(), "demo", "2.0.0")
	}()
	select {
	case err := <-result:
		release()
		t.Fatalf("Activate() returned before lock release: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := os.RemoveAll(path); err != nil {
		release()
		t.Fatal(err)
	}
	release()

	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "not installed") {
			t.Fatalf("Activate() error = %v, want missing-version error", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Activate() did not finish after lock release")
	}
}

func TestRemoveVersionWaitsForSkillLock(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path, err := store.InstalledPath("demo", "2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}

	release, err := store.acquire(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		result <- store.RemoveVersion(context.Background(), "demo", "2.0.0")
	}()
	select {
	case err := <-result:
		release()
		t.Fatalf("RemoveVersion() ignored skill lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	release()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("RemoveVersion() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("RemoveVersion() did not finish after lock release")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("removed version still exists: %v", err)
	}
}

func TestStoreRejectsRemovingActiveVersion(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path, _ := store.InstalledPath("demo", "1.0.0")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.Activate(context.Background(), "demo", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveVersion(context.Background(), "demo", "1.0.0"); err == nil {
		t.Fatal("RemoveVersion accepted the active version")
	}
}

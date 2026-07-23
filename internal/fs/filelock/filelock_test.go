package filelock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAcquireSerializesIndependentCallers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	firstRelease, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx, path); err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second Acquire() error = %v, want deadline exceeded", err)
	}

	firstRelease()
	secondRelease, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatalf("Acquire() after release: %v", err)
	}
	secondRelease()
}

func TestAcquireOnlyRemovesSafeStaleLocks(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.lock")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-staleAfter - time.Minute)
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatal(err)
	}
	release, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatalf("Acquire() stale empty lock: %v", err)
	}
	release()

	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(path, "do-not-delete")
	if err := os.WriteFile(unknown, []byte("protected"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx, path); err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Acquire() unknown stale lock error = %v", err)
	}
	content, err := os.ReadFile(unknown)
	if err != nil || strings.TrimSpace(string(content)) != "protected" {
		t.Fatalf("unknown stale lock content changed: content=%q err=%v", content, err)
	}
}

func TestAcquireDoesNotStealActiveLockWhenDirectoryTimestampIsOld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	release, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	stale := time.Now().Add(-staleAfter - time.Minute)
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 1 {
		t.Fatalf("lock owner entries = %v, err=%v", entries, err)
	}
	ownerPath := filepath.Join(path, entries[0].Name())
	if err := os.Chtimes(ownerPath, stale, stale); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx, path); err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second Acquire() error = %v, want active lock to remain held", err)
	}
}

func TestAcquireRecoversStaleLockOwnedByDeadProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	const deadPID = 1 << 30
	if processAlive(deadPID) {
		t.Skipf("test PID %d unexpectedly exists", deadPID)
	}
	ownerPath := filepath.Join(path, ownerPrefix+strings.Repeat("a", 32))
	if err := os.WriteFile(ownerPath, []byte("1073741824\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-staleAfter - time.Minute)
	if err := os.Chtimes(ownerPath, stale, stale); err != nil {
		t.Fatal(err)
	}
	release, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestAcquireDoesNotRemoveStaleLockWithInvalidOwnerPID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	ownerPath := filepath.Join(path, ownerPrefix+strings.Repeat("b", 32))
	if err := os.WriteFile(ownerPath, []byte("not-a-pid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-staleAfter - time.Minute)
	if err := os.Chtimes(ownerPath, stale, stale); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx, path); err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Acquire() error = %v, want invalid owner to remain protected", err)
	}
	content, err := os.ReadFile(ownerPath)
	if err != nil || string(content) != "not-a-pid\n" {
		t.Fatalf("invalid owner content = %q, err=%v", content, err)
	}
}

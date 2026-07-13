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

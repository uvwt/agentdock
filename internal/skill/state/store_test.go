package state

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreTreatsManagedDirectoryAsCurrentInstallFact(t *testing.T) {
	root := filepath.Join(t.TempDir(), "skills")
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	skillPath, err := store.SkillPath("demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	if skillPath != filepath.Join(root, "demo-skill") {
		t.Fatalf("SkillPath() = %q", skillPath)
	}
	if err := os.MkdirAll(skillPath, 0o700); err != nil {
		t.Fatal(err)
	}
	installed, err := store.IsInstalled("demo-skill")
	if err != nil || !installed {
		t.Fatalf("IsInstalled() = %v, %v", installed, err)
	}
	resolved, err := store.Resolve("demo-skill")
	if err != nil || resolved != skillPath {
		t.Fatalf("Resolve() = %q, %v", resolved, err)
	}
	names, err := store.ListSkills()
	if err != nil || len(names) != 1 || names[0] != "demo-skill" {
		t.Fatalf("ListSkills() = %#v, %v", names, err)
	}
	if _, err := os.Stat(filepath.Join(root, "state")); !os.IsNotExist(err) {
		t.Fatalf("managed Skill store created forbidden state/ directory: %v", err)
	}
}

func TestWriterWaitsForReaderAndReaderWaitsForWriter(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "skills"))
	if err != nil {
		t.Fatal(err)
	}
	readerRelease, err := store.AcquireRead(context.Background(), "demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	writerAcquired := make(chan func(), 1)
	go func() {
		release, acquireErr := store.AcquireWrite(context.Background(), "demo-skill")
		if acquireErr == nil {
			writerAcquired <- release
		}
	}()
	select {
	case release := <-writerAcquired:
		release()
		readerRelease()
		t.Fatal("writer acquired while reader was active")
	case <-time.After(75 * time.Millisecond):
	}
	readerRelease()
	writerRelease := <-writerAcquired

	readerAcquired := make(chan func(), 1)
	go func() {
		release, acquireErr := store.AcquireRead(context.Background(), "demo-skill")
		if acquireErr == nil {
			readerAcquired <- release
		}
	}()
	select {
	case release := <-readerAcquired:
		release()
		writerRelease()
		t.Fatal("reader acquired while writer was active")
	case <-time.After(75 * time.Millisecond):
	}
	writerRelease()
	(<-readerAcquired)()
}

func TestBlockedWriterRespectsContextCancellation(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "skills"))
	if err != nil {
		t.Fatal(err)
	}
	release, err := store.AcquireRead(context.Background(), "demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if _, err := store.AcquireWrite(ctx, "demo-skill"); err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("AcquireWrite() error = %v, want deadline exceeded", err)
	}
}

func TestRefreshOwnedLockKeepsActiveWriterFresh(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, "writer.lock")
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatal(err)
	}
	owner := "test-owner"
	if err := os.WriteFile(filepath.Join(lockPath, lockOwnerPrefix+owner), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * writerStaleAfter)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}

	if err := refreshOwnedLock(lockPath, owner); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(info.ModTime()) >= writerStaleAfter {
		t.Fatalf("writer lock heartbeat remained stale: %s", info.ModTime())
	}
	if err := refreshOwnedLock(lockPath, "other-owner"); err == nil {
		t.Fatal("writer heartbeat accepted a non-owner token")
	}
}

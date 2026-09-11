package processlock

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestProcessLockIsReleasedImmediately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update.lock")
	first, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if second, ok, err := TryAcquire(path); err != nil || ok || second != nil {
		t.Fatalf("TryAcquire while held = lock:%v ok:%v err:%v", second, ok, err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, ok, err := TryAcquire(path)
	if err != nil || !ok || second == nil {
		t.Fatalf("TryAcquire after release = lock:%v ok:%v err:%v", second, ok, err)
	}
	_ = second.Release()
}

func TestAcquireHonorsContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update.lock")
	first, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx, path); err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Acquire error = %v", err)
	}
}

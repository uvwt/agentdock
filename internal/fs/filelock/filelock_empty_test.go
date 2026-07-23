package filelock

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireRecoversEmptyLockAfterInitializationGrace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-emptyLockGrace - time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	release, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatalf("Acquire() empty lock: %v", err)
	}
	release()
}

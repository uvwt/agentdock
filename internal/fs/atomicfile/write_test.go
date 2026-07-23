package atomicfile

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestWriteCreatesAndReplacesCompleteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	if err := Write(path, []byte("first"), 0o600); err != nil {
		t.Fatalf("Write(first) error = %v", err)
	}
	if err := Write(path, []byte("second"), 0o640); err != nil {
		t.Fatalf("Write(second) error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "second" {
		t.Fatalf("content = %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o, want 640", info.Mode().Perm())
	}
	assertNoAtomicTemps(t, filepath.Dir(path))
}

func TestWriteRejectsEmptyPath(t *testing.T) {
	if err := Write("", []byte("data"), 0o600); err == nil {
		t.Fatal("Write() accepted empty path")
	}
}

func TestWriteCleansTempAfterRenameFailure(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Write(target, []byte("data"), 0o600); err == nil || !strings.Contains(err.Error(), "replace atomic file") {
		t.Fatalf("Write() error = %v, want rename failure", err)
	}
	assertNoAtomicTemps(t, root)
}

func TestWriteConcurrentResultsAreNeverPartial(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.json")
	const writers = 24
	payloads := make([][]byte, writers)
	var wg sync.WaitGroup
	wg.Add(writers)
	for i := range writers {
		payloads[i] = bytes.Repeat([]byte{byte('A' + i)}, 32<<10)
		go func(payload []byte) {
			defer wg.Done()
			if err := Write(path, payload, 0o600); err != nil {
				t.Errorf("Write() error = %v", err)
			}
		}(payloads[i])
	}
	wg.Wait()
	final, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	matched := false
	for _, payload := range payloads {
		if bytes.Equal(final, payload) {
			matched = true
			break
		}
	}
	if !matched {
		t.Fatalf("final file is partial or mixed, length=%d", len(final))
	}
	assertNoAtomicTemps(t, root)
}

func assertNoAtomicTemps(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".agentdock-atomic-") {
			t.Fatalf("temporary file remains: %s", entry.Name())
		}
	}
}

func TestWritePreservesOwnerExecutableMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows permissions use DACL rather than Unix mode bits")
	}
	path := filepath.Join(t.TempDir(), "script.sh")
	if err := Write(path, []byte("#!/bin/sh\necho ok\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("mode = %04o, want 0700", got)
	}
}

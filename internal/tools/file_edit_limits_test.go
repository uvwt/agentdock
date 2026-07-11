package tools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestFileEditRejectsOversizedExistingFile(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	path := filepath.Join(root, "oversized.txt")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxTextFileReadBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name string
		args map[string]any
	}{
		{name: "replace", args: map[string]any{"action": "replace", "path": "oversized.txt", "old": "a", "new": "b"}},
		{name: "overwrite", args: map[string]any{"action": "add", "path": "oversized.txt", "content": "replacement", "overwrite": true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := rt.fileEdit(t.Context(), test.args)
			var toolErr *ToolError
			if !errors.As(err, &toolErr) {
				t.Fatalf("expected ToolError, got %T: %v", err, err)
			}
			if toolErr.Code != "FILE_TOO_LARGE" || toolErr.Details["max_size_bytes"] != maxTextFileReadBytes {
				t.Fatalf("unexpected error: %#v", toolErr)
			}
		})
	}
}

func TestFileEditRejectsMovingDirectoryIntoDescendant(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	if err := os.MkdirAll(filepath.Join(root, "parent"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := rt.fileEdit(t.Context(), map[string]any{
		"action": "move", "path": "parent", "new_path": "parent/child",
	})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "INVALID_MOVE_DESTINATION" {
		t.Fatalf("unexpected error: %#v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "parent")); statErr != nil {
		t.Fatalf("source directory changed: %v", statErr)
	}
}

func TestMovePathWithRollbackRestoresDestinationOnFailure(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source.txt")
	dest := filepath.Join(root, "destination.txt")
	if err := os.WriteFile(src, []byte("new-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	moveErr := errors.New("simulated source rename failure")
	err := movePathWithRollback(src, dest, true, func(oldPath, newPath string) error {
		if oldPath == src && newPath == dest {
			return moveErr
		}
		return os.Rename(oldPath, newPath)
	})
	if !errors.Is(err, moveErr) {
		t.Fatalf("movePathWithRollback() error = %v", err)
	}
	gotDest, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("destination was not restored: %v", err)
	}
	if string(gotDest) != "old-content" {
		t.Fatalf("destination content = %q", gotDest)
	}
	gotSrc, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("source changed after failed move: %v", err)
	}
	if string(gotSrc) != "new-content" {
		t.Fatalf("source content = %q", gotSrc)
	}
	backups, err := filepath.Glob(filepath.Join(root, ".agentdock-move-backup-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Fatalf("move backup directories remain: %v", backups)
	}
}

func TestMovePathWithRollbackReportsRollbackFailure(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source.txt")
	dest := filepath.Join(root, "destination.txt")
	if err := os.WriteFile(src, []byte("new-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	moveErr := errors.New("move failed")
	rollbackErr := errors.New("rollback failed")
	calls := 0
	err := movePathWithRollback(src, dest, true, func(oldPath, newPath string) error {
		calls++
		switch calls {
		case 1:
			return os.Rename(oldPath, newPath)
		case 2:
			return moveErr
		case 3:
			return rollbackErr
		default:
			return fmt.Errorf("unexpected rename %s -> %s", oldPath, newPath)
		}
	})
	if !errors.Is(err, moveErr) || !errors.Is(err, rollbackErr) {
		t.Fatalf("movePathWithRollback() error = %v", err)
	}
	backups, globErr := filepath.Glob(filepath.Join(root, ".agentdock-move-backup-*", "payload"))
	if globErr != nil {
		t.Fatal(globErr)
	}
	if len(backups) != 1 {
		t.Fatalf("recoverable destination backups = %v, want one retained payload", backups)
	}
	retained, readErr := os.ReadFile(backups[0])
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(retained) != "old-content" {
		t.Fatalf("retained backup = %q", retained)
	}
}

func TestFileEditRejectsNegativeExpectedMatches(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("alpha"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := rt.fileEdit(t.Context(), map[string]any{
		"action": "replace", "path": "note.txt", "old": "alpha", "new": "beta", "expected_matches": -1,
	})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "INVALID_EXPECTED_MATCHES" {
		t.Fatalf("unexpected error: %#v", err)
	}
}

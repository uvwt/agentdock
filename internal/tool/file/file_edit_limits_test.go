package file

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
			_, err := rt.Edit(t.Context(), test.args)
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
	_, err := rt.Edit(t.Context(), map[string]any{
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
	rename := func(oldPath, newPath string) error {
		if oldPath == src && newPath == dest {
			return moveErr
		}
		return os.Rename(oldPath, newPath)
	}
	err := movePathWithRollback(src, dest, true, rename, rename)
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
	rename := func(oldPath, newPath string) error {
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
	}
	err := movePathWithRollback(src, dest, true, rename, rename)
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
	_, err := rt.Edit(t.Context(), map[string]any{
		"action": "replace", "path": "note.txt", "old": "alpha", "new": "beta", "expected_matches": -1,
	})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "INVALID_EXPECTED_MATCHES" {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestMovePathWithoutOverwritePreservesConcurrentDestination(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source.txt")
	dest := filepath.Join(root, "destination.txt")
	if err := os.WriteFile(src, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("concurrent"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := movePathWithRollback(src, dest, false, os.Rename, renameNoReplace); err == nil {
		t.Fatal("move without overwrite replaced an existing destination")
	}
	gotDest, err := os.ReadFile(dest)
	if err != nil || string(gotDest) != "concurrent" {
		t.Fatalf("destination = %q, err=%v", gotDest, err)
	}
	gotSrc, err := os.ReadFile(src)
	if err != nil || string(gotSrc) != "source" {
		t.Fatalf("source = %q, err=%v", gotSrc, err)
	}
}

func TestMovePathOverwritePreservesDestinationRecreatedAfterBackup(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source.txt")
	dest := filepath.Join(root, "destination.txt")
	if err := os.WriteFile(src, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("original-destination"), 0o600); err != nil {
		t.Fatal(err)
	}
	install := func(source, target string) error {
		if sameTestPath(source, src) && sameTestPath(target, dest) {
			if err := os.WriteFile(dest, []byte("concurrent"), 0o600); err != nil {
				return err
			}
			return errors.New("destination recreated")
		}
		return renameNoReplace(source, target)
	}
	err := movePathWithRollback(src, dest, true, os.Rename, install)
	if err == nil {
		t.Fatal("move overwrite succeeded after destination recreation")
	}
	gotDest, readErr := os.ReadFile(dest)
	if readErr != nil || string(gotDest) != "concurrent" {
		t.Fatalf("destination = %q, err=%v", gotDest, readErr)
	}
	gotSrc, readErr := os.ReadFile(src)
	if readErr != nil || string(gotSrc) != "source" {
		t.Fatalf("source = %q, err=%v", gotSrc, readErr)
	}
	backups, globErr := filepath.Glob(filepath.Join(root, ".agentdock-move-backup-*", "payload"))
	if globErr != nil {
		t.Fatal(globErr)
	}
	if len(backups) != 1 {
		t.Fatalf("recoverable destination backups = %v, want one", backups)
	}
}

func TestDeletePathPreservesReplacementCreatedBeforeCommit(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "target.txt")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	expected, err := captureFileSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	replaced := false
	rename := func(source, target string) error {
		if sameTestPath(source, path) && !replaced {
			replaced = true
			if err := os.Remove(path); err != nil {
				return err
			}
			if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
				return err
			}
		}
		return os.Rename(source, target)
	}
	err = deletePathSafely(path, expected, false, rename, renameNoReplace)
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "FILE_CHANGED" {
		t.Fatalf("deletePathSafely() error = %#v, want FILE_CHANGED", err)
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil || string(content) != "replacement" {
		t.Fatalf("replacement content = %q, err=%v", content, readErr)
	}
}

func TestMovePathRestoresSourceReplacedBeforeInstall(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source.txt")
	dest := filepath.Join(root, "destination.txt")
	if err := os.WriteFile(src, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	replaced := false
	install := func(source, target string) error {
		if sameTestPath(source, src) && !replaced {
			replaced = true
			if err := os.Remove(src); err != nil {
				return err
			}
			if err := os.WriteFile(src, []byte("replacement"), 0o600); err != nil {
				return err
			}
		}
		return renameNoReplace(source, target)
	}
	err := movePathWithRollback(src, dest, false, os.Rename, install)
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "FILE_CHANGED" {
		t.Fatalf("movePathWithRollback() error = %#v, want FILE_CHANGED", err)
	}
	content, readErr := os.ReadFile(src)
	if readErr != nil || string(content) != "replacement" {
		t.Fatalf("restored source = %q, err=%v", content, readErr)
	}
	if _, statErr := os.Stat(dest); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("destination remains after source conflict: %v", statErr)
	}
}

package file

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRollbackPreservesConcurrentFileAndOriginalBackup(t *testing.T) {
	target := filepath.Join(t.TempDir(), "file.txt")
	original, updated := "original\n", "updated\n"
	if err := os.WriteFile(target, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	info, statErr := os.Stat(target)
	if statErr != nil {
		t.Fatal(statErr)
	}
	staged := map[string]stagedPatchFile{target: {Abs: target, Display: target, Content: &updated, Mode: info.Mode(), Original: []byte(original), OriginalExists: true}}
	injected := false
	backupPath := ""
	install := func(from, to string) error {
		if strings.Contains(filepath.Base(from), ".agentdock-patch-backup-") && to == target {
			injected = true
			backupPath = from
			if err := os.WriteFile(target, []byte("external edit\n"), 0600); err != nil {
				return err
			}
			return renameNoReplace(from, to)
		}
		return errors.New("injected install failure")
	}
	err := commitStagedPatchWithFileOps(staged, os.Rename, install)
	if err == nil {
		t.Fatal("install failure was ignored")
	}
	data, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "external edit\n" {
		t.Errorf("rollback failed to protect a concurrent edit: got %q, restore attempted=%v", data, injected)
	}
	backup, readErr := os.ReadFile(backupPath)
	if readErr != nil || string(backup) != original {
		t.Fatalf("original backup = %q, error = %v", backup, readErr)
	}
	if !strings.Contains(err.Error(), backupPath) {
		t.Fatalf("rollback error omitted recoverable backup path: %v", err)
	}
}

//go:build darwin || linux

package file

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestListDirSkipsUnreadableDescendant(t *testing.T) {
	service, root := newCodeToolsRuntime(t)
	visibleDir := filepath.Join(root, "visible")
	if err := os.MkdirAll(visibleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(visibleDir, "ok.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	blockedDir := filepath.Join(root, "blocked")
	if err := os.MkdirAll(blockedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blockedDir, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blockedDir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blockedDir, 0o755) })

	result, err := service.ListDir(context.Background(), map[string]any{"path": ".", "max_depth": 2, "patterns": []string{"**/*.txt"}, "entry_type": "file"})
	if err != nil {
		t.Fatalf("ListDir returned a descendant permission error: %v", err)
	}
	entries := result["entries"].([]map[string]any)
	if len(entries) != 1 || entries[0]["path"] != "visible/ok.txt" {
		t.Fatalf("entries = %#v", entries)
	}
	if result["partial"] != true {
		t.Fatalf("partial = %#v", result["partial"])
	}
	skipped := result["skipped_paths"].([]string)
	if len(skipped) != 1 || skipped[0] != "blocked" {
		t.Fatalf("skipped_paths = %#v", skipped)
	}
}

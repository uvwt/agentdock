package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFileEditAddCreatesEmptyFile(t *testing.T) {
	runtime, root := newCodeToolsRuntime(t)
	result, err := runtime.Call(context.Background(), "file_edit", map[string]any{
		"action":  "add",
		"path":    "empty.txt",
		"content": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["changed"] != true {
		t.Fatalf("expected empty file creation to report changed: %#v", result)
	}
	info, err := os.Stat(filepath.Join(root, "empty.txt"))
	if err != nil {
		t.Fatalf("expected empty file to be created: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("empty file size = %d", info.Size())
	}
}

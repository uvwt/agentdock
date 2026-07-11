package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestListDirHonorsCanceledContext(t *testing.T) {
	runtime, _ := newCodeToolsRuntime(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := runtime.Call(ctx, "list_dir", map[string]any{"path": "."})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("list_dir error = %v, want context.Canceled", err)
	}
}

func TestListFilesHonorsCanceledContext(t *testing.T) {
	runtime, _ := newCodeToolsRuntime(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := runtime.Call(ctx, "list_files", map[string]any{"path": "."})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("list_files error = %v, want context.Canceled", err)
	}
}

func TestListDirRecursiveAppliesDepthAndHiddenRules(t *testing.T) {
	runtime, root := newCodeToolsRuntime(t)
	for path, content := range map[string]string{
		"visible.txt":          "visible",
		".hidden.txt":          "hidden",
		"nested/child.txt":     "child",
		"nested/deep/file.txt": "deep",
	} {
		absolute := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	result, err := runtime.Call(context.Background(), "list_dir", map[string]any{
		"path": ".", "recursive": true, "max_depth": 2, "max_entries": 100,
	})
	if err != nil {
		t.Fatalf("list_dir error = %v", err)
	}
	entries, ok := result["entries"].([]map[string]any)
	if !ok {
		t.Fatalf("entries type = %T", result["entries"])
	}
	paths := make(map[string]bool, len(entries))
	for _, entry := range entries {
		paths[entry["path"].(string)] = true
	}
	for _, expected := range []string{"visible.txt", "nested", "nested/child.txt", "nested/deep"} {
		if !paths[expected] {
			t.Fatalf("missing path %q in %#v", expected, paths)
		}
	}
	if paths[".hidden.txt"] || paths["nested/deep/file.txt"] {
		t.Fatalf("hidden or over-depth path leaked: %#v", paths)
	}
}

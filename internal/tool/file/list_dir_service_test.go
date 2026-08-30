package file

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestListDirHonorsCanceledContext(t *testing.T) {
	runtime, _ := newFileTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := runtime.listDirTest(ctx, map[string]any{"path": "."})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("list_dir error = %v, want context.Canceled", err)
	}
}

func TestListDirAppliesDepthAndHiddenRules(t *testing.T) {
	runtime, root := newFileTestService(t)
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

	result, err := runtime.listDirTest(context.Background(), map[string]any{
		"path": ".", "max_depth": 2, "max_entries": 100,
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

func TestListDirPatternsAreRelativeToRequestedPath(t *testing.T) {
	runtime, root := newFileTestService(t)
	for path, content := range map[string]string{
		"src/root.go":          "package root\n",
		"src/readme.md":        "docs\n",
		"src/nested/deep.go":   "package nested\n",
		"src/nested/other.txt": "other\n",
	} {
		absolute := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	list := func(pattern string) []map[string]any {
		t.Helper()
		result, err := runtime.listDirTest(context.Background(), map[string]any{
			"path": "src", "max_depth": 2, "entry_type": "file", "patterns": []string{pattern},
		})
		if err != nil {
			t.Fatal(err)
		}
		entries, ok := result["entries"].([]map[string]any)
		if !ok {
			t.Fatalf("entries type = %T", result["entries"])
		}
		return entries
	}

	shallow := list("*.go")
	if len(shallow) != 1 || shallow[0]["path"] != "root.go" {
		t.Fatalf("*.go entries = %#v, want only root.go", shallow)
	}
	deep := list("**/*.go")
	if len(deep) != 2 || deep[0]["path"] != "nested/deep.go" || deep[1]["path"] != "root.go" {
		t.Fatalf("**/*.go entries = %#v, want request-relative Go files", deep)
	}
}

func TestListDirNestedPathKeepsWorkspaceRootIgnoreRules(t *testing.T) {
	runtime, root := newFileTestService(t)
	for path, content := range map[string]string{
		"src/keep.txt":           "keep",
		"src/generated/drop.txt": "drop",
	} {
		absolute := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("/src/generated/\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	list := func(includeIgnored bool) map[string]bool {
		t.Helper()
		result, err := runtime.listDirTest(context.Background(), map[string]any{
			"path": "src", "max_depth": 2, "include_ignored": includeIgnored,
		})
		if err != nil {
			t.Fatal(err)
		}
		paths := map[string]bool{}
		for _, entry := range result["entries"].([]map[string]any) {
			paths[entry["path"].(string)] = true
		}
		return paths
	}

	filtered := list(false)
	if !filtered["keep.txt"] || filtered["generated"] || filtered["generated/drop.txt"] {
		t.Fatalf("workspace root .gitignore not applied from nested path: %#v", filtered)
	}
	included := list(true)
	if !included["generated"] || !included["generated/drop.txt"] {
		t.Fatalf("include_ignored did not restore ignored entries: %#v", included)
	}
}

func TestListDirTruncatedRequiresAdditionalMatchingEntry(t *testing.T) {
	runtime, root := newFileTestService(t)
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	args := map[string]any{"path": ".", "entry_type": "file", "patterns": []string{"*.txt"}, "max_entries": 2}

	exact, err := runtime.listDirTest(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if exact["truncated"] != false {
		t.Fatalf("truncated = %#v with exactly max_entries matches, want false", exact["truncated"])
	}

	if err := os.WriteFile(filepath.Join(root, "c.txt"), []byte("c"), 0o600); err != nil {
		t.Fatal(err)
	}
	overflow, err := runtime.listDirTest(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	entries := overflow["entries"].([]map[string]any)
	if overflow["truncated"] != true || len(entries) != 2 {
		t.Fatalf("overflow result = %#v, want two entries and truncated=true", overflow)
	}
}

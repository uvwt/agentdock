package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFileEditAtomicWriteCreatesAndOverwrites(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	rel := filepath.Join("book", "chapter.md")
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		t.Fatal(err)
	}

	res, err := rt.Call(context.Background(), "file_edit", map[string]any{
		"action": "atomic_write", "path": rel, "content": "# hi\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["atomic"] != true || res["created"] != true || res["action"] != "atomic_write" {
		t.Fatalf("res=%#v", res)
	}
	data, err := os.ReadFile(abs)
	if err != nil || string(data) != "# hi\n" {
		t.Fatalf("data=%q err=%v", data, err)
	}

	res, err = rt.Call(context.Background(), "file_edit", map[string]any{
		"action": "atomic_write", "path": rel, "content": "# full book\nline2\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["overwritten"] != true {
		t.Fatalf("res=%#v", res)
	}
	data, err = os.ReadFile(abs)
	if err != nil || string(data) != "# full book\nline2\n" {
		t.Fatalf("data=%q err=%v", data, err)
	}
}

func TestFileEditAtomicWriteDryRun(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	rel := "dry.md"
	abs := filepath.Join(root, rel)
	res, err := rt.Call(context.Background(), "file_edit", map[string]any{
		"action": "atomic_write", "path": rel, "content": "x\n", "dry_run": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["dry_run"] != true {
		t.Fatalf("%#v", res)
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Fatalf("file should not exist, err=%v", err)
	}
}

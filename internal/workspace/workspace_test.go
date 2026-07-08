package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveExistingAllowsAbsolutePath(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	file := filepath.Join(outside, "repo.txt")
	if err := os.WriteFile(file, []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	realFile, err := filepath.EvalSymlinks(file)
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
	}

	ws, err := New(root)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p, err := ws.ResolveExisting(file)
	if err != nil {
		t.Fatalf("ResolveExisting() error = %v", err)
	}
	if p.Abs != realFile {
		t.Fatalf("ResolveExisting() Abs = %q, want %q", p.Abs, realFile)
	}
	if p.Display != realFile {
		t.Fatalf("ResolveExisting() Display = %q, want %q", p.Display, realFile)
	}
}

func TestResolveExistingExpandsHomePath(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	file := filepath.Join(home, "repo.txt")
	if err := os.WriteFile(file, []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	realFile, err := filepath.EvalSymlinks(file)
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
	}

	ws, err := New(root)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p, err := ws.ResolveExisting("~/repo.txt")
	if err != nil {
		t.Fatalf("ResolveExisting() error = %v", err)
	}
	if p.Abs != realFile {
		t.Fatalf("ResolveExisting() Abs = %q, want %q", p.Abs, realFile)
	}
}

func TestResolveForWriteRelativeUsesDefaultDirectory(t *testing.T) {
	root := t.TempDir()
	ws, err := New(root)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p, err := ws.ResolveForWrite("notes/todo.md")
	if err != nil {
		t.Fatalf("ResolveForWrite() error = %v", err)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
	}
	want := filepath.Join(realRoot, "notes", "todo.md")
	if p.Abs != want {
		t.Fatalf("ResolveForWrite() Abs = %q, want %q", p.Abs, want)
	}
}

package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveExistingWorkspacePolicyDeniesAbsolutePath(t *testing.T) {
	root := t.TempDir()
	ws, err := New(root, false)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = ws.ResolveExisting(root)
	if err == nil {
		t.Fatal("ResolveExisting() expected absolute path to be denied")
	}
}

func TestResolveExistingHostPolicyAllowsAbsolutePath(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	file := filepath.Join(outside, "repo.txt")
	if err := os.WriteFile(file, []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	ws, err := New(root, true)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	p, err := ws.ResolveExisting(file)
	if err != nil {
		t.Fatalf("ResolveExisting() error = %v", err)
	}
	if p.Abs != file {
		t.Fatalf("ResolveExisting() Abs = %q, want %q", p.Abs, file)
	}
	if p.Display != file {
		t.Fatalf("ResolveExisting() Display = %q, want %q", p.Display, file)
	}
}

func TestResolveExistingHostPolicyExpandsHomePath(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	file := filepath.Join(home, "repo.txt")
	if err := os.WriteFile(file, []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	ws, err := New(root, true)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	p, err := ws.ResolveExisting("~/repo.txt")
	if err != nil {
		t.Fatalf("ResolveExisting() error = %v", err)
	}
	if p.Abs != file {
		t.Fatalf("ResolveExisting() Abs = %q, want %q", p.Abs, file)
	}
}

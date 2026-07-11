package workspace

import (
	"os"
	"path/filepath"
	"runtime"
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
	t.Setenv("USERPROFILE", home)
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
	if runtime.GOOS == "windows" {
		windowsPath, err := ws.ResolveExisting(`~\repo.txt`)
		if err != nil {
			t.Fatalf("ResolveExisting(Windows home path) error = %v", err)
		}
		if windowsPath.Abs != realFile {
			t.Fatalf("ResolveExisting(Windows home path) Abs = %q, want %q", windowsPath.Abs, realFile)
		}
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

func TestResolveForWriteFollowsSymlinkParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	ws, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := ws.ResolveForWrite("linked/new/file.txt")
	if err != nil {
		t.Fatalf("ResolveForWrite() error = %v", err)
	}
	realOutside, err := filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(realOutside, "new", "file.txt")
	if resolved.Abs != want {
		t.Fatalf("Abs = %q, want %q", resolved.Abs, want)
	}
	if resolved.Display != want {
		t.Fatalf("Display = %q, want outside absolute path %q", resolved.Display, want)
	}
	if resolved.Exists {
		t.Fatal("Exists = true for a missing write target")
	}
}

func TestRelativeAllowsNamesBeginningWithTwoDots(t *testing.T) {
	root := t.TempDir()
	ws, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ws.Relative(filepath.Join(ws.Root(), "..config", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "..config/settings.json" {
		t.Fatalf("Relative() = %q, want relative child path", got)
	}
}

func TestSetDefaultCWDAndDisplay(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	realProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	display, err := ws.SetDefaultCWD("project")
	if err != nil {
		t.Fatalf("SetDefaultCWD() error = %v", err)
	}
	if display != "project" || ws.DefaultDisplay() != "project" || ws.DefaultCWD() != realProject {
		t.Fatalf("display=%q defaultDisplay=%q defaultCWD=%q want=%q", display, ws.DefaultDisplay(), ws.DefaultCWD(), realProject)
	}
	file := filepath.Join(project, "file.txt")
	if err := os.WriteFile(file, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.SetDefaultCWD("file.txt"); err == nil {
		t.Fatal("SetDefaultCWD() accepted a regular file")
	}
}

func TestWorkspaceLexicalHelpers(t *testing.T) {
	if got, err := Clean(""); err != nil || got != "." {
		t.Fatalf("Clean(empty) = %q, %v", got, err)
	}
	if _, err := Clean("bad\x00path"); err == nil {
		t.Fatal("Clean() accepted NUL byte")
	}
	if !Hidden(".git") || Hidden("git") {
		t.Fatal("Hidden() classification is incorrect")
	}
	ws, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.ResolveExisting("~someone/path"); err == nil {
		t.Fatal("ResolveExisting() accepted unsupported home shorthand")
	}
}

func TestNewRejectsFilesystemRoot(t *testing.T) {
	root := string(filepath.Separator)
	if volume := filepath.VolumeName(t.TempDir()); volume != "" {
		root = volume + string(filepath.Separator)
	}
	if _, err := New(root); err == nil {
		t.Fatal("New() accepted filesystem root")
	}
}

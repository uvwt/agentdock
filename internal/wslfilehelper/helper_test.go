//go:build linux

package wslfilehelper

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWSLFileHelperReadsListsAndSearchesNativeTree(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "keep.txt"), "alpha needle omega\n", 0o640)
	mustWriteFile(t, filepath.Join(root, "nested", "match.txt"), "before\nneedle here\nafter\n", 0o644)
	mustWriteFile(t, filepath.Join(root, "nested", "skip.log"), "needle hidden by glob\n", 0o644)
	mustWriteFile(t, filepath.Join(root, ".hidden.txt"), "needle hidden\n", 0o644)
	mustWriteFile(t, filepath.Join(root, "ignored", "ignored.txt"), "needle ignored\n", 0o644)
	mustWriteFile(t, filepath.Join(root, ".gitignore"), "ignored/\n", 0o644)

	read, err := Dispatch(&Request{Action: "read", Path: filepath.Join(root, "keep.txt")})
	if err != nil {
		t.Fatal(err)
	}
	if read.Exists == nil || !*read.Exists || read.Content != "alpha needle omega\n" || read.Mode == nil || *read.Mode != 0o640 {
		t.Fatalf("read result = %#v", read)
	}

	listed, err := Dispatch(&Request{Action: "list_dir", Path: root, MaxDepth: 3, Patterns: []string{"**/*"}})
	if err != nil {
		t.Fatal(err)
	}
	if listed.Entries == nil {
		t.Fatal("list response omitted entries")
	}
	paths := make([]string, 0, len(*listed.Entries))
	for _, entry := range *listed.Entries {
		paths = append(paths, entry.Path)
	}
	for _, want := range []string{"keep.txt", "nested", "nested/match.txt", "nested/skip.log"} {
		if !contains(paths, want) {
			t.Fatalf("list paths = %#v, missing %q", paths, want)
		}
	}
	for _, unwanted := range []string{".hidden.txt", "ignored", "ignored/ignored.txt"} {
		if contains(paths, unwanted) {
			t.Fatalf("list paths = %#v, unexpectedly contains %q", paths, unwanted)
		}
	}

	searched, err := Dispatch(&Request{
		Action: "search_text", Path: root, Query: "needle", IncludeGlobs: []string{"**/*.txt"},
		ContextLines: 1, MaxResults: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if searched.Engine != "go_wsl" || searched.TotalMatches == nil || *searched.TotalMatches != 2 {
		t.Fatalf("search result = %#v", searched)
	}
	if searched.Matches == nil || len(*searched.Matches) != 2 {
		t.Fatalf("search matches = %#v", searched.Matches)
	}
	nestedMatch := (*searched.Matches)[1]
	if nestedMatch.RelativePath != "nested/match.txt" || nestedMatch.Line != 2 || nestedMatch.Column != 1 {
		t.Fatalf("nested match = %#v", nestedMatch)
	}
	if got := nestedMatch.Before; len(got) != 1 || got[0] != "before" {
		t.Fatalf("before context = %#v", got)
	}
}

func TestWSLFileHelperAtomicWritePreservesModeAndRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.txt")
	mustWriteFile(t, target, "old\n", 0o640)
	newContent := "new\n"
	result, err := Dispatch(&Request{Action: "write_atomic", Path: target, Content: &newContent, MustExist: true, Overwrite: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode == nil || *result.Mode != 0o640 || result.SHA256 == "" {
		t.Fatalf("write result = %#v", result)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o, want 640", info.Mode().Perm())
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != newContent {
		t.Fatalf("content = %q, err=%v", got, err)
	}

	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	_, err = Dispatch(&Request{Action: "write_atomic", Path: link, Content: &newContent, Overwrite: true})
	requireFailureCode(t, err, "SYMLINK_NOT_ALLOWED")

	viaParent := filepath.Join(root, "link-dir", "child.txt")
	if err := os.Symlink(root, filepath.Join(root, "link-dir")); err != nil {
		t.Fatal(err)
	}
	_, err = Dispatch(&Request{Action: "write_atomic", Path: viaParent, Content: &newContent})
	requireFailureCode(t, err, "SYMLINK_NOT_ALLOWED")
}

func TestWSLFileHelperMovesDeletesAndBlocksProtectedPaths(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	destination := filepath.Join(root, "nested", "destination.txt")
	mustWriteFile(t, source, "move me\n", 0o600)

	moved, err := Dispatch(&Request{Action: "move", Path: source, NewPath: destination})
	if err != nil {
		t.Fatal(err)
	}
	if moved.NewPath != destination {
		t.Fatalf("move result = %#v", moved)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatal(err)
	}
	if _, err := Dispatch(&Request{Action: "delete", Path: destination}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination still exists: %v", err)
	}

	content := "blocked\n"
	_, err = Dispatch(&Request{Action: "write_atomic", Path: "/proc/agentdock-test", Content: &content})
	requireFailureCode(t, err, "PROTECTED_WSL_PATH")
}

func requireFailureCode(t *testing.T, err error, code string) *ToolFailure {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s error", code)
	}
	var failure *ToolFailure
	if !errors.As(err, &failure) {
		t.Fatalf("error = %T %v, want ToolFailure %s", err, err, code)
	}
	if failure.Code != code {
		t.Fatalf("error code = %s, want %s; message=%s", failure.Code, code, failure.Message)
	}
	return failure
}

func mustWriteFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

package file

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIgnoreMatcherHonorsGitHierarchyAndExclude(t *testing.T) {
	root := t.TempDir()
	writeIgnoreTestFile(t, root, ".gitignore", "*.tmp\n/root-only.txt\nignored/\n!ignored/keep.txt\na/**/b.txt\n\\#literal\n\\!literal\nspace\\ \n")
	writeIgnoreTestFile(t, root, "src/.gitignore", "!important.tmp\n/generated/\n")
	writeIgnoreTestFile(t, root, ".git/info/exclude", "vendor/\n")

	matcher := loadIgnoreMatcher(root)
	tests := []struct {
		path  string
		isDir bool
		want  bool
	}{
		{path: "root-only.txt", want: true},
		{path: "nested/root-only.txt", want: false},
		{path: "drop.tmp", want: true},
		{path: "src/drop.tmp", want: true},
		{path: "src/important.tmp", want: false},
		{path: "src/generated", isDir: true, want: true},
		{path: "src/generated/file.txt", want: true},
		{path: "generated", isDir: true, want: false},
		{path: "ignored/keep.txt", want: true},
		{path: "vendor", isDir: true, want: true},
		{path: "vendor/file.txt", want: true},
		{path: "a/b.txt", want: true},
		{path: "a/x/y/b.txt", want: true},
		{path: "#literal", want: true},
		{path: "!literal", want: true},
		{path: "space ", want: true},
		{path: "keep.txt", want: false},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			if got := matcher.Ignored(test.path, test.isDir); got != test.want {
				t.Fatalf("Ignored(%q, %t) = %t, want %t", test.path, test.isDir, got, test.want)
			}
		})
	}
}

func TestIgnoreMatcherMatchesGit(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is required for gitignore differential test")
	}
	root := t.TempDir()
	if output, err := exec.Command(git, "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	writeIgnoreTestFile(t, root, ".gitignore", "*.tmp\n/root-only.txt\nignored/\n!ignored/keep.txt\na/**/b.txt\n\\#literal\n\\!literal\nspace\\ \nabc/**\ndocs/*.md\nfile?.txt\n[ab].cfg\n")
	writeIgnoreTestFile(t, root, "src/.gitignore", "!important.tmp\n/generated/\n")
	writeIgnoreTestFile(t, root, ".git/info/exclude", "vendor/\n")

	files := []string{
		"root-only.txt",
		"nested/root-only.txt",
		"drop.tmp",
		"src/drop.tmp",
		"src/important.tmp",
		"src/generated/file.txt",
		"ignored/keep.txt",
		"vendor/file.txt",
		"a/b.txt",
		"a/x/y/b.txt",
		"#literal",
		"!literal",
		"space ",
		"abc/file.txt",
		"docs/a.md",
		"docs/sub/a.md",
		"file1.txt",
		"file10.txt",
		"a.cfg",
		"c.cfg",
		"keep.txt",
	}
	for _, rel := range files {
		writeIgnoreTestFile(t, root, rel, "fixture")
	}
	for _, rel := range []string{"src/generated", "generated", "ignored", "vendor", "abc"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(rel)), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	matcher := loadIgnoreMatcher(root)
	tests := []struct {
		path  string
		isDir bool
	}{
		{path: "root-only.txt"},
		{path: "nested/root-only.txt"},
		{path: "drop.tmp"},
		{path: "src/drop.tmp"},
		{path: "src/important.tmp"},
		{path: "src/generated", isDir: true},
		{path: "src/generated/file.txt"},
		{path: "generated", isDir: true},
		{path: "ignored/keep.txt"},
		{path: "vendor", isDir: true},
		{path: "vendor/file.txt"},
		{path: "a/b.txt"},
		{path: "a/x/y/b.txt"},
		{path: "#literal"},
		{path: "!literal"},
		{path: "space "},
		{path: "abc", isDir: true},
		{path: "abc/file.txt"},
		{path: "docs/a.md"},
		{path: "docs/sub/a.md"},
		{path: "file1.txt"},
		{path: "file10.txt"},
		{path: "a.cfg"},
		{path: "c.cfg"},
		{path: "keep.txt"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			want := gitCheckIgnored(t, git, root, test.path)
			if got := matcher.Ignored(test.path, test.isDir); got != want {
				t.Fatalf("Ignored(%q, %t) = %t, git check-ignore = %t", test.path, test.isDir, got, want)
			}
		})
	}
}

func TestResolveGitInfoExcludeForWorktree(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, "git", "worktrees", "sample")
	commonDir := filepath.Join(root, "git")
	if err := os.MkdirAll(gitDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeIgnoreTestFile(t, root, ".git", "gitdir: git/worktrees/sample\n")
	if err := os.WriteFile(filepath.Join(gitDir, "commondir"), []byte("../..\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(commonDir, "info", "exclude")
	if got := resolveGitInfoExclude(root); got != want {
		t.Fatalf("resolveGitInfoExclude() = %q, want %q", got, want)
	}
}

func gitCheckIgnored(t *testing.T, git, root, rel string) bool {
	t.Helper()
	cmd := exec.Command(git, "-C", root, "check-ignore", "--no-index", "-q", "--", filepath.ToSlash(rel))
	err := cmd.Run()
	if err == nil {
		return true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false
	}
	t.Fatalf("git check-ignore %q: %v", rel, err)
	return false
}

func TestSearchTextGoHonorsNestedGitignoreAndExclude(t *testing.T) {
	runtime, root := newCodeToolsRuntime(t)
	for path, content := range map[string]string{
		"src/keep.txt":           "needle keep\n",
		"src/drop.tmp":           "needle root-ignore\n",
		"src/important.tmp":      "needle nested-include\n",
		"src/vendor/drop.txt":    "needle info-exclude\n",
		"src/generated/drop.txt": "needle nested-ignore\n",
	} {
		writeIgnoreTestFile(t, root, path, content)
	}
	writeIgnoreTestFile(t, root, ".gitignore", "*.tmp\n")
	writeIgnoreTestFile(t, root, "src/.gitignore", "!important.tmp\n/generated/\n")
	writeIgnoreTestFile(t, root, ".git/info/exclude", "src/vendor/\n")

	path, err := runtime.ws.ResolveExisting("src")
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.searchTextGo(context.Background(), path, SearchOptions{
		Query: "needle", CaseSensitive: true, MaxResults: 20,
	})
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]bool{}
	for _, match := range result["matches"].([]map[string]any) {
		got[match["path"].(string)] = true
	}
	if len(got) != 2 || !got["src/keep.txt"] || !got["src/important.tmp"] {
		t.Fatalf("search fallback ignored paths = %#v, want keep.txt and important.tmp only", got)
	}
}

func TestListDirHonorsNestedGitignoreAndExclude(t *testing.T) {
	runtime, root := newFileTestService(t)
	for path, content := range map[string]string{
		"src/keep.txt":           "keep",
		"src/drop.tmp":           "root ignore",
		"src/important.tmp":      "nested include",
		"src/vendor/drop.txt":    "info exclude",
		"src/generated/drop.txt": "nested ignore",
	} {
		writeIgnoreTestFile(t, root, path, content)
	}
	writeIgnoreTestFile(t, root, ".gitignore", "*.tmp\n")
	writeIgnoreTestFile(t, root, "src/.gitignore", "!important.tmp\n/generated/\n")
	writeIgnoreTestFile(t, root, ".git/info/exclude", "src/vendor/\n")

	result, err := runtime.listDirTest(context.Background(), map[string]any{
		"path": "src", "max_depth": 2, "include_hidden": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, entry := range result["entries"].([]map[string]any) {
		got[entry["path"].(string)] = true
	}
	if !got["keep.txt"] || !got["important.tmp"] {
		t.Fatalf("list_dir missing expected entries: %#v", got)
	}
	for _, ignored := range []string{"drop.tmp", "generated", "generated/drop.txt", "vendor", "vendor/drop.txt"} {
		if got[ignored] {
			t.Fatalf("list_dir returned ignored path %q: %#v", ignored, got)
		}
	}
}

func writeIgnoreTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

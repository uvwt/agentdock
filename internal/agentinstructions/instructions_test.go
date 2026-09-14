package agentinstructions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeGuidance(t *testing.T, dir, text string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, Filename)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func loadGuidance(t *testing.T, options Options) Snapshot {
	t.Helper()
	snapshot, err := Load(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func loadedContents(snapshot Snapshot) []string {
	var values []string
	for _, file := range snapshot.Files {
		if file.Status == "loaded" {
			values = append(values, file.Content)
		}
	}
	return values
}

func TestLoadGlobalRootAndNestedInOrder(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	child := filepath.Join(root, "项目 with spaces", "src")
	writeGuidance(t, home, "\ufeff# 全局\r\n不要自动执行 ACP。\r\n")
	writeGuidance(t, root, "# 项目规则")
	writeGuidance(t, filepath.Dir(child), "# 子目录规则")
	writeGuidance(t, child, "# 当前目录规则")
	options := Options{Home: home, DefaultDir: root, Workdir: child}
	snapshot := loadGuidance(t, options)
	got := strings.Join(loadedContents(snapshot), "|")
	want := "# 全局\r\n不要自动执行 ACP。|# 项目规则|# 子目录规则|# 当前目录规则"
	if got != want {
		t.Fatalf("ordered contents = %q, want %q", got, want)
	}
	if snapshot.Workdir != child || snapshot.WorkspaceRoot != root || !snapshot.AutoLoad {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	for _, file := range snapshot.Files {
		if len(file.SHA256) != 64 || file.SizeBytes == 0 {
			t.Fatalf("missing provenance: %#v", file)
		}
	}
	if !strings.Contains(snapshot.Text(), "does not override global safety") {
		t.Fatal("workspace scope is not identified")
	}
}

func TestLoadRefreshesEvenWhenSizeAndModificationTimeAreUnchanged(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	path := writeGuidance(t, root, "old rule")
	options := Options{Home: home, DefaultDir: root, Workdir: root}
	before := loadGuidance(t, options)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	writeGuidance(t, root, "new rule")
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	after := loadGuidance(t, options)
	if got := strings.Join(loadedContents(after), ""); got != "new rule" {
		t.Fatalf("stale guidance: %s", got)
	}
	if before.Files[1].SHA256 == after.Files[1].SHA256 {
		t.Fatal("digest did not change")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	removed := loadGuidance(t, options)
	if removed.Files[1].Status != "not_found" || len(loadedContents(removed)) != 0 {
		t.Fatalf("deleted file was cached: %#v", removed)
	}
	writeGuidance(t, root, "created again")
	if got := strings.Join(loadedContents(loadGuidance(t, options)), ""); got != "created again" {
		t.Fatalf("new file not detected: %s", got)
	}
}

func TestExplicitGlobalOverrideAndDeduplication(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	writeGuidance(t, home, "unused automatic global")
	path := writeGuidance(t, root, "explicit rules")
	snapshot := loadGuidance(t, Options{Home: home, DefaultDir: root, Workdir: root, GlobalFile: path})
	if len(snapshot.Files) != 2 || snapshot.Files[0].Scope != "global" || snapshot.Files[1].Status != "duplicate" || snapshot.Files[1].DuplicateOf != path {
		t.Fatalf("dedup = %#v", snapshot)
	}
	if snapshot.Files[1].Content != "" || strings.Count(snapshot.Text(), "explicit rules") != 1 {
		t.Fatal("same file injected twice")
	}
}

func TestDisableAutoLoadPreservesExplicitInstructions(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	path := writeGuidance(t, home, "explicit global")
	writeGuidance(t, root, "workspace rules")
	options := Options{Home: home, DefaultDir: root, Workdir: root, DisableAutoLoad: true}
	if snapshot := loadGuidance(t, options); snapshot.AutoLoad || len(snapshot.Files) != 0 {
		t.Fatalf("autoload did not disable: %#v", snapshot)
	}
	options.GlobalFile = path
	snapshot := loadGuidance(t, options)
	if got := strings.Join(loadedContents(snapshot), "|"); got != "explicit global" {
		t.Fatalf("explicit global not preserved: %s", got)
	}
}

func TestRepositoryBoundaryDoesNotReadUnrelatedAncestorsOrSiblings(t *testing.T) {
	outer := t.TempDir()
	root, child := filepath.Join(outer, "repo"), filepath.Join(outer, "repo", "src")
	writeGuidance(t, outer, "OUTSIDE")
	writeGuidance(t, root, "ROOT")
	writeGuidance(t, child, "CHILD")
	writeGuidance(t, filepath.Join(root, "sibling"), "SIBLING")
	// Worktrees use a .git file instead of a directory; no Git command is needed.
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot := loadGuidance(t, Options{Home: t.TempDir(), DefaultDir: t.TempDir(), Workdir: child})
	if got := strings.Join(loadedContents(snapshot), "|"); got != "ROOT|CHILD" {
		t.Fatalf("boundary leak: %s", got)
	}
	if snapshot.WorkspaceRoot != root {
		t.Fatalf("root = %q", snapshot.WorkspaceRoot)
	}
	if err := os.Remove(filepath.Join(root, ".git")); err != nil {
		t.Fatal(err)
	}
	snapshot = loadGuidance(t, Options{Home: t.TempDir(), DefaultDir: t.TempDir(), Workdir: child})
	if got := strings.Join(loadedContents(snapshot), "|"); got != "CHILD" {
		t.Fatalf("nonrepository read parent rules: %s", got)
	}
}

func TestNestedRepositoryStopsAtNearestBoundary(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	writeGuidance(t, root, "outer")
	writeGuidance(t, nested, "nested")
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(nested, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	snapshot := loadGuidance(t, Options{Home: t.TempDir(), DefaultDir: root, Workdir: nested})
	if got := strings.Join(loadedContents(snapshot), "|"); got != "nested" {
		t.Fatalf("nested boundary = %s", got)
	}
}

func TestFileValidationNeverReturnsPartialGuidance(t *testing.T) {
	for _, test := range []struct{ name, content, status, reason string }{
		{"empty", " \r\n\t", "empty", ""},
		{"bom_only", "\ufeff", "empty", ""},
		{"invalid_utf8", string([]byte{0xff, 0xfe}), "skipped", "invalid_utf8_text"},
		{"nul", "text\x00text", "skipped", "invalid_utf8_text"},
		{"at_limit", strings.Repeat("x", MaxFileBytes), "loaded", ""},
		{"over_limit", strings.Repeat("x", MaxFileBytes+1), "skipped", "file_size_limit"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeGuidance(t, root, test.content)
			snapshot := loadGuidance(t, Options{DefaultDir: root, Workdir: root})
			file := snapshot.Files[0]
			if file.Status != test.status || file.Reason != test.reason {
				t.Fatalf("file = %#v", file)
			}
			if file.Status != "loaded" && file.Content != "" {
				t.Fatal("returned partial/invalid content")
			}
		})
	}
}

func TestNonRegularFileIsSkipped(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, Filename), 0o700); err != nil {
		t.Fatal(err)
	}
	snapshot := loadGuidance(t, Options{DefaultDir: root, Workdir: root})
	if snapshot.Files[0].Reason != "not_regular_file" {
		t.Fatalf("file = %#v", snapshot.Files[0])
	}
}

func TestAutomaticSymlinkIsNotFollowedButExplicitGlobalIsSupported(t *testing.T) {
	home, root, outside := t.TempDir(), t.TempDir(), t.TempDir()
	target := writeGuidance(t, outside, "outside guidance")
	link := filepath.Join(root, Filename)
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	snapshot := loadGuidance(t, Options{Home: home, DefaultDir: root, Workdir: root})
	if len(loadedContents(snapshot)) != 0 || snapshot.Files[1].Reason != "not_regular_file" {
		t.Fatalf("followed automatic symlink: %#v", snapshot)
	}
	snapshot = loadGuidance(t, Options{Home: home, DefaultDir: root, Workdir: root, GlobalFile: link})
	if snapshot.Files[0].Status != "loaded" || snapshot.Files[0].Path != link {
		t.Fatalf("explicit file semantics changed: %#v", snapshot)
	}
}

func TestHardLinkIsDeduplicated(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	path := writeGuidance(t, home, "one physical file")
	if err := os.Link(path, filepath.Join(root, Filename)); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	snapshot := loadGuidance(t, Options{Home: home, DefaultDir: root, Workdir: root})
	if snapshot.Files[1].Status != "duplicate" || len(loadedContents(snapshot)) != 1 {
		t.Fatalf("hardlink repeated: %#v", snapshot)
	}
}

func TestTotalBudgetSkipsWholeFiles(t *testing.T) {
	root := t.TempDir()
	dir := root
	for range 5 {
		writeGuidance(t, dir, strings.Repeat("x", MaxFileBytes))
		dir = filepath.Join(dir, "child")
	}
	workdir := filepath.Dir(dir)
	snapshot := loadGuidance(t, Options{DefaultDir: root, Workdir: workdir})
	if len(loadedContents(snapshot)) != MaxTotalBytes/MaxFileBytes || snapshot.Files[4].Reason != "total_size_limit" || snapshot.Files[4].Content != "" {
		t.Fatalf("budget not enforced: statuses=%v", func() []string {
			var s []string
			for _, f := range snapshot.Files {
				s = append(s, f.Status+":"+f.Reason)
			}
			return s
		}())
	}
}

func TestLoadRejectsInvalidSelectionAndHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	file := writeGuidance(t, root, "rules")
	for _, workdir := range []string{"relative", file, filepath.Join(root, "missing")} {
		if _, err := Load(t.Context(), Options{Workdir: workdir}); err == nil {
			t.Fatalf("accepted workdir %q", workdir)
		}
	}
	if _, err := Load(t.Context(), Options{Workdir: root, GlobalFile: "relative.md"}); err == nil {
		t.Fatal("accepted relative explicit file")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Load(ctx, Options{DefaultDir: root, Workdir: root}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
}

func TestWorkspaceDepthIsBounded(t *testing.T) {
	root := t.TempDir()
	dir := root
	for range MaxDirectories {
		dir = filepath.Join(dir, "a")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(t.Context(), Options{DefaultDir: root, Workdir: dir}); err == nil {
		t.Fatal("unbounded directory traversal")
	}
}

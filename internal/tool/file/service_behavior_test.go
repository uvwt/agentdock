package file

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestReadFileReturnsNextStartLineOnTruncation(t *testing.T) {
	rt, root := newFileTestService(t)
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("第一行\n第二行\n第三行\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := rt.readFileTest(context.Background(), map[string]any{"path": "notes.txt", "max_bytes": 13})
	if err != nil {
		t.Fatal(err)
	}
	content := result["content"].(string)
	if !utf8.ValidString(content) {
		t.Fatalf("content is invalid UTF-8: %q", content)
	}
	if result["truncated"] != true || result["truncated_reason"] != "max_bytes" {
		t.Fatalf("expected truncation metadata, got %#v", result)
	}
	if _, ok := result["next_start_line"].(int); !ok {
		t.Fatalf("expected next_start_line, got %#v", result)
	}
}

func TestEditFileReplacesSingleMatch(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	rt, root := newFileTestService(t)
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc main() {}\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	result, err := rt.editFileTest(map[string]any{"path": "main.go", "old": "func main() {}", "new": "func main() { println(\"ok\") }"})
	if err != nil {
		t.Fatal(err)
	}
	if result["changed"] != true || result["matches"] != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "println") {
		t.Fatalf("file was not edited: %s", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); runtime.GOOS != "windows" && got != 0o640 {
		t.Fatalf("mode = %v, want 0640", got)
	}
}

func TestEditFileDryRunDoesNotWrite(t *testing.T) {
	rt, root := newFileTestService(t)
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := rt.editFileTest(map[string]any{"path": "main.go", "old": "alpha", "new": "beta", "dry_run": true})
	if err != nil {
		t.Fatal(err)
	}
	if result["changed"] != true || !strings.Contains(result["diff_preview"].(string), "beta") {
		t.Fatalf("unexpected dry-run result: %#v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "alpha\n" {
		t.Fatalf("dry-run wrote file: %q", data)
	}
}

func TestEditFileRejectsUnexpectedMatchCounts(t *testing.T) {
	rt, root := newFileTestService(t)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("alpha\nalpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.editFileTest(map[string]any{"path": "main.go", "old": "alpha", "new": "beta"}); err == nil {
		t.Fatalf("expected multi-match error")
	}
	if _, err := rt.editFileTest(map[string]any{"path": "main.go", "old": "missing", "new": "beta"}); err == nil {
		t.Fatalf("expected zero-match error")
	}
}

func TestEditFileExpectedZeroSucceedsWhenTextIsAbsent(t *testing.T) {
	rt, root := newFileTestService(t)
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := rt.editFileTest(map[string]any{
		"path":             "main.go",
		"old":              "missing",
		"new":              "beta",
		"expected_matches": 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["matches"] != 0 || result["changed"] != false {
		t.Fatalf("unexpected zero-match assertion result: %#v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "alpha\n" {
		t.Fatalf("zero-match assertion wrote file: %q", data)
	}
}

func TestEditFileReplaceAll(t *testing.T) {
	rt, root := newFileTestService(t)
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("alpha\nalpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.editFileTest(map[string]any{"path": "main.go", "old": "alpha", "new": "beta", "replace_all": true, "expected_matches": 2}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "beta\nbeta\n" {
		t.Fatalf("replace_all result = %q", data)
	}
}

func TestEditFileRejectsBinaryAndInvalidUTF8(t *testing.T) {
	rt, root := newFileTestService(t)
	if err := os.WriteFile(filepath.Join(root, "bin.dat"), []byte{0, 1, 2}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.editFileTest(map[string]any{"path": "bin.dat", "old": "x", "new": "y"}); err == nil {
		t.Fatalf("expected binary rejection")
	}
	if err := os.WriteFile(filepath.Join(root, "bad.txt"), []byte{0xff, 'x'}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.editFileTest(map[string]any{"path": "bad.txt", "old": "x", "new": "y"}); err == nil {
		t.Fatalf("expected invalid UTF-8 rejection")
	}
}

func TestSearchTextGoFallbackIncludesColumnsAndContext(t *testing.T) {
	rt, root := newFileTestService(t)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("one\nTwo needle\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := rt.ws.ResolveExisting(".")
	if err != nil {
		t.Fatal(err)
	}
	result, err := rt.searchTextGo(context.Background(), p, SearchOptions{Query: "needle", MaxResults: 10, ContextLines: 1})
	if err != nil {
		t.Fatal(err)
	}
	matches := result["matches"].([]map[string]any)
	if len(matches) != 1 {
		t.Fatalf("matches = %#v", matches)
	}
	if matches[0]["column"] != 5 || matches[0]["match_text"] != "needle" {
		t.Fatalf("missing column/match_text: %#v", matches[0])
	}
	if matches[0]["context_start_line"] != 1 || matches[0]["context_end_line"] != 3 {
		t.Fatalf("missing context range: %#v", matches[0])
	}
}

func TestParseRGJSONIncludesColumnsAndContext(t *testing.T) {
	rt, root := newFileTestService(t)
	abs := filepath.Join(root, "main.go")
	escapedAbs := strings.ReplaceAll(abs, `\`, `\\`)
	output := strings.Join([]string{
		`{"type":"context","data":{"path":{"text":"` + escapedAbs + `"},"lines":{"text":"before\n"},"line_number":1}}`,
		`{"type":"match","data":{"path":{"text":"` + escapedAbs + `"},"lines":{"text":"needle here\n"},"line_number":2,"submatches":[{"match":{"text":"needle"},"start":0,"end":6}]}}`,
		`{"type":"context","data":{"path":{"text":"` + escapedAbs + `"},"lines":{"text":"after\n"},"line_number":3}}`,
	}, "\n")
	matches, truncated, ok := rt.parseRGJSON([]byte(output), rt.ws.Root(), SearchOptions{Query: "needle", MaxResults: 10, ContextLines: 1})
	if !ok || truncated || len(matches) != 1 {
		t.Fatalf("parseRGJSON = matches=%#v truncated=%v ok=%v", matches, truncated, ok)
	}
	if !strings.HasSuffix(matches[0]["path"].(string), "main.go") || matches[0]["column"] != 1 || matches[0]["match_text"] != "needle" {
		t.Fatalf("missing rg fields: %#v", matches[0])
	}
	if matches[0]["context_start_line"] != 1 || matches[0]["context_end_line"] != 3 {
		t.Fatalf("missing rg context range: %#v", matches[0])
	}
}

func TestApplyEnvelopePatchDryRunAndDiagnostics(t *testing.T) {
	rt, root := newFileTestService(t)
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("alpha\nbeta\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	patch := "*** Begin Patch\n*** Update File: main.go\n@@\n-alpha\n+ALPHA\n*** End Patch"
	result, err := rt.applyPatchTest(context.Background(), map[string]any{"patch": patch, "dry_run": true})
	if err != nil {
		t.Fatal(err)
	}
	if result["dry_run"] != true || !strings.Contains(result["diff_preview"].(string), "ALPHA") {
		t.Fatalf("unexpected patch dry-run: %#v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "alpha\nbeta\n" {
		t.Fatalf("dry-run wrote file: %q", data)
	}

	_, err = rt.applyPatchTest(context.Background(), map[string]any{"patch": "*** Begin Patch\n*** Update File: main.go\n@@\n-missing\n+value\n*** End Patch"})
	if err == nil {
		t.Fatalf("expected context diagnostic")
	}
	if toolErr, ok := err.(*ToolError); !ok || toolErr.Details["diagnostic"] == nil {
		t.Fatalf("missing diagnostic: %#v", err)
	}
}

func TestApplyEnvelopePatchContextAnchorDisambiguates(t *testing.T) {
	rt, root := newFileTestService(t)
	path := filepath.Join(root, "main.go")
	content := "func first() {\n\treturn nil\n}\n\nfunc second() {\n\treturn nil\n}\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	patch := "*** Begin Patch\n*** Update File: main.go\n@@ func second() {\n-\treturn nil\n+\treturn secondErr\n*** End Patch"
	if _, err := rt.applyPatchTest(context.Background(), map[string]any{"patch": patch}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "func first() {\n\treturn nil\n}\n\nfunc second() {\n\treturn secondErr\n}\n"
	if string(got) != want {
		t.Fatalf("anchored patch content = %q, want %q", got, want)
	}
}

func TestApplyEnvelopePatchContextAnchorSearchesForward(t *testing.T) {
	rt, root := newFileTestService(t)
	path := filepath.Join(root, "main.go")
	content := "func target() {\n\tprepare()\n\twork()\n\treturn nil\n}\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	patch := "*** Begin Patch\n*** Update File: main.go\n@@ func target() {\n-\treturn nil\n+\treturn targetErr\n*** End Patch"
	if _, err := rt.applyPatchTest(context.Background(), map[string]any{"patch": patch}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "func target() {\n\tprepare()\n\twork()\n\treturn targetErr\n}\n"
	if string(got) != want {
		t.Fatalf("forward anchored patch content = %q, want %q", got, want)
	}
}

func TestApplyEnvelopePatchAcceptsBlankContextLine(t *testing.T) {
	rt, root := newFileTestService(t)
	path := filepath.Join(root, "main.txt")
	if err := os.WriteFile(path, []byte("alpha\n\nbeta\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	patch := "*** Begin Patch\n*** Update File: main.txt\n@@\n alpha\n\n-beta\n+BETA\n*** End Patch"
	if _, err := rt.applyPatchTest(context.Background(), map[string]any{"patch": patch}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "alpha\n\nBETA\n" {
		t.Fatalf("blank-context patch content = %q", got)
	}
}

func TestApplyEnvelopePatchEndOfFileAnchorsLastMatch(t *testing.T) {
	rt, root := newFileTestService(t)
	path := filepath.Join(root, "main.txt")
	if err := os.WriteFile(path, []byte("alpha\nomega\nalpha\nomega\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	patch := "*** Begin Patch\n*** Update File: main.txt\n@@\n-alpha\n-omega\n+ALPHA\n+OMEGA\n*** End of File\n*** End Patch"
	if _, err := rt.applyPatchTest(context.Background(), map[string]any{"patch": patch}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "alpha\nomega\nALPHA\nOMEGA\n" {
		t.Fatalf("EOF-anchored patch content = %q", got)
	}
}

func TestApplyEnvelopePatchEndOfFilePureInsertAppends(t *testing.T) {
	rt, root := newFileTestService(t)
	path := filepath.Join(root, "main.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	patch := "*** Begin Patch\n*** Update File: main.txt\n@@\n+omega\n*** End of File\n*** End Patch"
	if _, err := rt.applyPatchTest(context.Background(), map[string]any{"patch": patch}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "alpha\nomega\n" {
		t.Fatalf("EOF pure insert content = %q", got)
	}
}

func TestApplyEnvelopePatchAnchorPureInsertFollowsAnchor(t *testing.T) {
	rt, root := newFileTestService(t)
	path := filepath.Join(root, "main.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	patch := "*** Begin Patch\n*** Update File: main.txt\n@@ beta\n+inserted\n*** End Patch"
	if _, err := rt.applyPatchTest(context.Background(), map[string]any{"patch": patch}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "alpha\nbeta\ninserted\ngamma\n" {
		t.Fatalf("anchored pure insert content = %q, want insert after anchor", got)
	}
}

func TestApplyEnvelopePatchPreservesMissingTrailingNewline(t *testing.T) {
	rt, root := newFileTestService(t)
	path := filepath.Join(root, "main.txt")
	if err := os.WriteFile(path, []byte("alpha"), 0o600); err != nil {
		t.Fatal(err)
	}
	patch := "*** Begin Patch\n*** Update File: main.txt\n@@\n-alpha\n+beta\n*** End Patch"
	if _, err := rt.applyPatchTest(context.Background(), map[string]any{"patch": patch}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "beta" {
		t.Fatalf("patch added an unexpected trailing newline: %q", got)
	}
}

func TestApplyUpdateHunksRStripFallbackRequiresUniqueMatch(t *testing.T) {
	t.Run("rstrip fallback", func(t *testing.T) {
		got, err := applyUpdateHunks("alpha   \nbeta\n", []patchUpdateChunk{{Lines: []string{"-alpha", "+ALPHA"}}}, "main.txt")
		if err != nil {
			t.Fatal(err)
		}
		if got != "ALPHA\nbeta\n" {
			t.Fatalf("rstrip patch content = %q", got)
		}
	})

	t.Run("rstrip keeps unchanged context text", func(t *testing.T) {
		got, err := applyUpdateHunks("alpha   \nbeta\n", []patchUpdateChunk{{Lines: []string{" alpha", "-beta", "+BETA"}}}, "main.txt")
		if err != nil {
			t.Fatal(err)
		}
		if got != "alpha   \nBETA\n" {
			t.Fatalf("rstrip context was rewritten: %q", got)
		}
	})

	t.Run("rstrip ambiguity", func(t *testing.T) {
		_, err := applyUpdateHunks("alpha \nalpha\t\n", []patchUpdateChunk{{Lines: []string{"-alpha", "+ALPHA"}}}, "main.txt")
		var toolErr *ToolError
		if !errors.As(err, &toolErr) || toolErr.Code != "PATCH_FAILED" {
			t.Fatalf("ambiguous rstrip error = %#v", err)
		}
		diagnostic, _ := toolErr.Details["diagnostic"].(map[string]any)
		if diagnostic["code"] != "AMBIGUOUS_CONTEXT" {
			t.Fatalf("ambiguous rstrip diagnostic = %#v", diagnostic)
		}
	})

	t.Run("leading whitespace remains significant", func(t *testing.T) {
		_, err := applyUpdateHunks("    alpha\n", []patchUpdateChunk{{Lines: []string{"-  alpha", "+ALPHA"}}}, "main.txt")
		var toolErr *ToolError
		if !errors.As(err, &toolErr) || toolErr.Code != "PATCH_FAILED" {
			t.Fatalf("leading-whitespace error = %#v", err)
		}
		diagnostic, _ := toolErr.Details["diagnostic"].(map[string]any)
		if diagnostic["code"] != "CONTEXT_NOT_FOUND" {
			t.Fatalf("leading-whitespace diagnostic = %#v", diagnostic)
		}
	})
}

func TestApplyEnvelopePatchDoesNotWriteEarlierFileWhenLaterContextFails(t *testing.T) {
	rt, root := newFileTestService(t)
	first := filepath.Join(root, "first.txt")
	second := filepath.Join(root, "second.txt")
	if err := os.WriteFile(first, []byte("first old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	patch := strings.Join([]string{
		"*** Begin Patch",
		"*** Update File: first.txt",
		"@@",
		"-first old",
		"+first new",
		"*** Update File: second.txt",
		"@@",
		"-missing",
		"+second new",
		"*** End Patch",
	}, "\n")
	if _, err := rt.applyPatchTest(context.Background(), map[string]any{"patch": patch}); err == nil {
		t.Fatal("expected the second file context to fail")
	}
	got, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first old\n" {
		t.Fatalf("first file was written before full patch validation: %q", got)
	}
}

func TestApplyUnifiedDiffDryRunDoesNotWrite(t *testing.T) {
	rt, root := newFileTestService(t)
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	patch := "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-alpha\n+beta\n"
	result, err := rt.applyPatchTest(context.Background(), map[string]any{"patch": patch, "dry_run": true})
	if err != nil {
		t.Fatal(err)
	}
	if result["dry_run"] != true || result["insertions"] != 1 || result["deletions"] != 1 {
		t.Fatalf("unexpected unified dry-run result: %#v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "alpha\n" {
		t.Fatalf("dry-run wrote file: %q", data)
	}
}

func TestReadFileUsesCanonicalEncodingErrorCode(t *testing.T) {
	rt, root := newFileTestService(t)
	path := filepath.Join(root, "invalid-utf8.txt")
	if err := os.WriteFile(path, []byte{0xff, 0xfe}, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := rt.readFileTest(context.Background(), map[string]any{"path": "invalid-utf8.txt"})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "ENCODING_UNSUPPORTED" {
		t.Fatalf("readFile() error = %#v, want ENCODING_UNSUPPORTED", err)
	}
}

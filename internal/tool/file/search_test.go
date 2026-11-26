package file

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/workspace"
)

func TestBoundedInt(t *testing.T) {
	tests := []struct {
		name             string
		value, fallback  int
		minimum, maximum int
		want             int
	}{
		{name: "below minimum", value: -1, fallback: 100, minimum: 1, maximum: 1000, want: 100},
		{name: "zero below positive minimum", value: 0, fallback: 100, minimum: 1, maximum: 1000, want: 100},
		{name: "zero allowed", value: 0, fallback: 0, minimum: 0, maximum: 20, want: 0},
		{name: "inside", value: 12, fallback: 0, minimum: 0, maximum: 20, want: 12},
		{name: "capped", value: 50, fallback: 0, minimum: 0, maximum: 20, want: 20},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := boundedInt(test.value, test.fallback, test.minimum, test.maximum); got != test.want {
				t.Fatalf("boundedInt() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestSearchTextGoKeepsUnicodeByteOffsets(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	if err := os.WriteFile(filepath.Join(root, "sample.txt"), []byte("İX\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := rt.ws.ResolveExisting(".")
	if err != nil {
		t.Fatal(err)
	}

	result, err := rt.searchTextGo(context.Background(), path, SearchOptions{
		Query:         "x",
		CaseSensitive: false,
		MaxResults:    10,
	})
	if err != nil {
		t.Fatal(err)
	}
	matches, ok := result["matches"].([]map[string]any)
	if !ok || len(matches) != 1 {
		t.Fatalf("matches = %#v, want one match", result["matches"])
	}
	if got := matches[0]["column"]; got != 3 {
		t.Fatalf("column = %#v, want UTF-8 byte column 3", got)
	}
	if got := matches[0]["match_text"]; got != "X" {
		t.Fatalf("match_text = %#v, want X", got)
	}
}

func TestSearchTextGoQuotesCaseInsensitiveLiteral(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	if err := os.WriteFile(filepath.Join(root, "sample.txt"), []byte("Alpha.X\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := rt.ws.ResolveExisting(".")
	if err != nil {
		t.Fatal(err)
	}

	result, err := rt.searchTextGo(context.Background(), path, SearchOptions{
		Query:         ".",
		CaseSensitive: false,
		MaxResults:    10,
	})
	if err != nil {
		t.Fatal(err)
	}
	matches, ok := result["matches"].([]map[string]any)
	if !ok || len(matches) != 1 {
		t.Fatalf("matches = %#v, want one literal dot match", result["matches"])
	}
	if got := matches[0]["match_text"]; got != "." {
		t.Fatalf("match_text = %#v, want literal dot", got)
	}
}

func TestSearchTextGoNestedPathKeepsWorkspaceRootIgnoreRules(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	for path, content := range map[string]string{
		"src/keep.txt":           "needle keep\n",
		"src/generated/drop.txt": "needle drop\n",
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
	path, err := rt.ws.ResolveExisting("src")
	if err != nil {
		t.Fatal(err)
	}

	search := func(includeIgnored bool) []map[string]any {
		t.Helper()
		result, err := rt.searchTextGo(context.Background(), path, SearchOptions{
			Query: "needle", CaseSensitive: true, IncludeIgnored: includeIgnored, MaxResults: 10,
		})
		if err != nil {
			t.Fatal(err)
		}
		return result["matches"].([]map[string]any)
	}

	filtered := search(false)
	if len(filtered) != 1 || filtered[0]["path"] != "src/keep.txt" {
		t.Fatalf("workspace root .gitignore not applied from nested search path: %#v", filtered)
	}
	included := search(true)
	if len(included) != 2 {
		t.Fatalf("include_ignored matches = %#v, want 2", included)
	}
}

func TestSearchTextHonorsCanceledRequestContext(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	if err := os.WriteFile(filepath.Join(root, "sample.txt"), []byte("content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := rt.searchTextTest(ctx, SearchRequest{Path: ".", Query: "content"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "SEARCH_CANCELED" {
		t.Fatalf("error = %#v, want structured SEARCH_CANCELED", err)
	}
}

func TestSearchTextGoFallbackSkipsOversizedFiles(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte("needle in a large file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "small.txt"), []byte("needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := rt.ws.ResolveExisting(".")
	if err != nil {
		t.Fatal(err)
	}

	result, err := rt.searchTextGoWithLimits(context.Background(), path, SearchOptions{Query: "needle", CaseSensitive: true, MaxResults: 10}, searchFallbackLimits{
		MaxEntries: 10, MaxFiles: 10, MaxFileBytes: 10, MaxTotalBytes: 100, Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["partial"] != true || result["skipped_large_files"] != 1 || result["files_scanned"] != 1 {
		t.Fatalf("fallback accounting = %#v", result)
	}
	matches := result["matches"].([]map[string]any)
	if len(matches) != 1 || matches[0]["path"] != "small.txt" {
		t.Fatalf("matches = %#v, want only small.txt", matches)
	}
}

func TestSearchTextGoFallbackReturnsStructuredResourceLimit(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	for _, name := range []string{"one.txt", "two.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("haystack\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path, err := rt.ws.ResolveExisting(".")
	if err != nil {
		t.Fatal(err)
	}

	_, err = rt.searchTextGoWithLimits(context.Background(), path, SearchOptions{Query: "needle", CaseSensitive: true, MaxResults: 10}, searchFallbackLimits{
		MaxEntries: 10, MaxFiles: 1, MaxFileBytes: 100, MaxTotalBytes: 100, Timeout: time.Second,
	})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "RESOURCE_LIMIT" || toolErr.Details["resource"] != "files" {
		t.Fatalf("error = %#v, want file RESOURCE_LIMIT", err)
	}
}

func TestSearchTextGoFallbackReturnsStructuredRootFailure(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	missing := workspace.Path{Abs: filepath.Join(root, "missing"), Display: "missing"}

	_, err := rt.searchTextGo(context.Background(), missing, SearchOptions{Query: "needle", CaseSensitive: true, MaxResults: 10})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "SEARCH_FAILED" || toolErr.Details["engine"] != "go_fallback" {
		t.Fatalf("error = %#v, want structured SEARCH_FAILED", err)
	}
}

func TestSearchTextGlobPatternsAreRelativeToRequestedPath(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	for name, content := range map[string]string{
		"src/root.go":        "needle root\n",
		"src/nested/deep.go": "needle deep\n",
		"src/nested/note.md": "needle note\n",
	} {
		absolute := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	assertMatches := func(pattern string, want int) {
		t.Helper()
		result, err := rt.searchTextTest(context.Background(), SearchRequest{Path: "src", Query: "needle", IncludeGlobs: []string{pattern}, MaxResults: intPtrForTest(10)})
		if err != nil {
			t.Fatal(err)
		}
		matches, ok := result["matches"].([]map[string]any)
		if !ok {
			t.Fatalf("matches type = %T", result["matches"])
		}
		if len(matches) != want {
			t.Fatalf("pattern %q matches = %#v, want %d", pattern, matches, want)
		}
	}

	assertMatches("*.go", 1)
	assertMatches("**/*.go", 2)
}

func TestSearchTextGoGlobPatternsAreRelativeToRequestedPath(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	for name := range map[string]struct{}{
		"src/root.go":        {},
		"src/nested/deep.go": {},
	} {
		absolute := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte("needle\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path, err := rt.ws.ResolveExisting("src")
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		pattern string
		want    int
	}{
		{pattern: "*.go", want: 1},
		{pattern: "**/*.go", want: 2},
	} {
		result, err := rt.searchTextGo(context.Background(), path, SearchOptions{
			Query: "needle", CaseSensitive: true, IncludeGlobs: []string{test.pattern}, MaxResults: 10,
		})
		if err != nil {
			t.Fatal(err)
		}
		matches := result["matches"].([]map[string]any)
		if len(matches) != test.want {
			t.Fatalf("Go fallback pattern %q matches = %#v, want %d", test.pattern, matches, test.want)
		}
	}
}

func TestSearchTextOmitsEmptyContextFieldsAcrossEngines(t *testing.T) {
	for _, engine := range []string{"rg", "go_fallback"} {
		t.Run(engine, func(t *testing.T) {
			if engine == "rg" {
				if _, err := exec.LookPath("rg"); err != nil {
					t.Skip("rg is not installed")
				}
			} else {
				t.Setenv("PATH", t.TempDir())
			}

			tests := []struct {
				name         string
				content      string
				contextLines int
				wantBefore   []string
				wantAfter    []string
				wantStart    int
				wantEnd      int
			}{
				{name: "context_lines_zero", content: "before\nneedle\nafter\n", contextLines: 0},
				{name: "single_line_with_context", content: "needle\n", contextLines: 2},
				{name: "both_before_and_after", content: "prev\nneedle\nnext\n", contextLines: 1, wantBefore: []string{"prev"}, wantAfter: []string{"next"}, wantStart: 1, wantEnd: 3},
				{name: "only_after_at_first_line", content: "needle\nnext\n", contextLines: 1, wantAfter: []string{"next"}, wantStart: 1, wantEnd: 2},
				{name: "only_before_at_last_line", content: "prev\nneedle", contextLines: 1, wantBefore: []string{"prev"}, wantStart: 1, wantEnd: 2},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					svc, root := newCodeToolsRuntime(t)
					if err := os.WriteFile(filepath.Join(root, "sample.txt"), []byte(tt.content), 0o600); err != nil {
						t.Fatal(err)
					}
					result, err := svc.SearchText(context.Background(), SearchRequest{
						Path:         "sample.txt",
						Query:        "needle",
						ContextLines: &tt.contextLines,
					})
					if err != nil {
						t.Fatal(err)
					}
					if result["engine"] != engine {
						t.Fatalf("engine = %#v, want %s", result["engine"], engine)
					}
					matches := result["matches"].([]map[string]any)
					if len(matches) != 1 {
						t.Fatalf("matches = %#v, want 1 match", matches)
					}
					match := matches[0]

					if len(tt.wantBefore) == 0 {
						if _, exists := match["before"]; exists {
							t.Fatalf("expected before to be omitted: %#v", match)
						}
					} else {
						got, _ := match["before"].([]string)
						if strings.Join(got, "\n") != strings.Join(tt.wantBefore, "\n") {
							t.Fatalf("before = %#v, want %#v", got, tt.wantBefore)
						}
					}

					if len(tt.wantAfter) == 0 {
						if _, exists := match["after"]; exists {
							t.Fatalf("expected after to be omitted: %#v", match)
						}
					} else {
						got, _ := match["after"].([]string)
						if strings.Join(got, "\n") != strings.Join(tt.wantAfter, "\n") {
							t.Fatalf("after = %#v, want %#v", got, tt.wantAfter)
						}
					}

					if tt.wantStart == 0 {
						if _, exists := match["context_start_line"]; exists {
							t.Fatalf("expected context_start_line to be omitted: %#v", match)
						}
						if _, exists := match["context_end_line"]; exists {
							t.Fatalf("expected context_end_line to be omitted: %#v", match)
						}
					} else if match["context_start_line"] != tt.wantStart || match["context_end_line"] != tt.wantEnd {
						t.Fatalf("context range = %v..%v, want %d..%d (match=%#v)", match["context_start_line"], match["context_end_line"], tt.wantStart, tt.wantEnd, match)
					}
				})
			}
		})
	}
}

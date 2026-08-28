package file

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
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

	_, err := rt.SearchText(ctx, map[string]any{"path": ".", "query": "content"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
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
		result, err := rt.SearchText(context.Background(), map[string]any{
			"path": "src", "query": "needle", "include_globs": []string{pattern}, "max_results": 10,
		})
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

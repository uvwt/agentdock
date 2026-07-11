package tools

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

	result, err := rt.searchTextGo(context.Background(), path, searchOptions{
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

	result, err := rt.searchTextGo(context.Background(), path, searchOptions{
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

func TestSearchTextHonorsCanceledRequestContext(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	if err := os.WriteFile(filepath.Join(root, "sample.txt"), []byte("content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := rt.Call(ctx, "search_text", map[string]any{"path": ".", "query": "content"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

package file

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSearchTextResultCapAndTrailingContext(t *testing.T) {
	for _, engine := range []string{"rg", "go_fallback"} {
		t.Run(engine, func(t *testing.T) {
			if engine == "rg" {
				if _, err := exec.LookPath("rg"); err != nil {
					t.Skip("rg is not installed")
				}
			} else {
				t.Setenv("PATH", t.TempDir())
			}
			for _, tt := range []struct {
				name, content string
				cap, context  int
				count         int
				truncated     bool
				after         []string
				end           int
			}{
				{"no matches", "before\nother", 1, 1, 0, false, nil, 0},
				{"below cap", "before\nneedle\nafter", 2, 1, 1, false, []string{"after"}, 3},
				{"exact cap", "needle\nneedle\nafter", 2, 1, 2, false, []string{"after"}, 3},
				{"above cap", "needle\nneedle\nnear\nfar\nneedle\nlast", 2, 1, 2, true, []string{"near"}, 3},
				{"single exact cap", "before\nneedle\nafter", 1, 1, 1, false, []string{"after"}, 3},
				{"no context", "before\nneedle\nafter", 1, 0, 1, false, nil, 2},
				{"EOF", "needle", 1, 3, 1, false, nil, 1},
				{"short tail", "needle\nlast", 1, 3, 1, false, []string{"last"}, 2},
				{"adjacent overflow", "needle\nneedle\nlast", 1, 2, 1, true, []string{"needle", "last"}, 3},
				{"far context", "needle\nnear\nfar\nfar\nneedle\nlast", 1, 1, 1, true, []string{"near"}, 2},
			} {
				t.Run(tt.name, func(t *testing.T) {
					svc, root := newCodeToolsRuntime(t)
					if err := os.WriteFile(filepath.Join(root, "sample.txt"), []byte(tt.content), 0600); err != nil {
						t.Fatal(err)
					}
					result, err := svc.SearchText(context.Background(), SearchRequest{Path: "sample.txt", Query: "needle", MaxResults: &tt.cap, ContextLines: &tt.context})
					if err != nil {
						t.Fatal(err)
					}
					if result["engine"] != engine || result["truncated"] != tt.truncated || result["total_matches"] != tt.count {
						t.Errorf("result = %#v; want engine=%s truncated=%v count=%d", result, engine, tt.truncated, tt.count)
					}
					matches := result["matches"].([]map[string]any)
					if len(matches) != tt.count {
						t.Fatalf("matches = %#v", matches)
					}
					if len(matches) == 0 {
						return
					}
					last := matches[len(matches)-1]
					after, _ := last["after"].([]string)
					if len(after) != len(tt.after) || strings.Join(after, "\n") != strings.Join(tt.after, "\n") || last["context_end_line"] != tt.end {
						t.Errorf("last match=%#v; want after=%#v end=%d", last, tt.after, tt.end)
					}
				})
			}
			t.Run("filters before overflow", func(t *testing.T) {
				svc, root := newCodeToolsRuntime(t)
				for _, name := range []string{"keep.txt", "skip.txt", "skip.log"} {
					if err := os.WriteFile(filepath.Join(root, name), []byte("before\nneedle\nafter"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				cap, contextLines := 1, 1
				result, err := svc.SearchText(context.Background(), SearchRequest{Query: "needle", IncludeGlobs: []string{"*.txt"}, ExcludeGlobs: []string{"skip.txt"}, MaxResults: &cap, ContextLines: &contextLines})
				if err != nil {
					t.Fatal(err)
				}
				matches := result["matches"].([]map[string]any)
				if result["truncated"] != false || len(matches) != 1 || matches[0]["path"] != "keep.txt" {
					t.Fatalf("filtered result = %#v", result)
				}
			})
			t.Run("cross-file overflow", func(t *testing.T) {
				svc, root := newCodeToolsRuntime(t)
				for _, name := range []string{"a", "b"} {
					if err := os.WriteFile(filepath.Join(root, name+".txt"), []byte("needle\n"+name+" tail"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				cap, contextLines := 1, 1
				result, err := svc.SearchText(context.Background(), SearchRequest{Query: "needle", MaxResults: &cap, ContextLines: &contextLines})
				if err != nil {
					t.Fatal(err)
				}
				matches := result["matches"].([]map[string]any)
				if result["truncated"] != true || len(matches) != 1 {
					t.Fatalf("cross-file result = %#v", result)
				}
				name := strings.TrimSuffix(fmt.Sprint(matches[0]["path"]), ".txt")
				if got := matches[0]["after"].([]string); len(got) != 1 || got[0] != name+" tail" {
					t.Fatalf("cross-file context = %#v", matches[0])
				}
			})
		})
	}
}

func TestSearchTextGoCapStillHonorsResourceBudget(t *testing.T) {
	svc, root := newCodeToolsRuntime(t)
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("needle"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path, err := svc.ws.ResolveExisting(".")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.searchTextGoWithLimits(context.Background(), path, SearchOptions{Query: "needle", MaxResults: 1}, searchFallbackLimits{
		MaxEntries: 10, MaxFiles: 1, MaxFileBytes: 100, MaxTotalBytes: 100, Timeout: time.Second,
	})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "RESOURCE_LIMIT" || toolErr.Details["resource"] != "files" {
		t.Fatalf("error = %#v, want file RESOURCE_LIMIT", err)
	}
}

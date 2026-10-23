package file

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSearchTextRGAcceptsFileAndLeadingHyphenQuery(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep is not installed")
	}
	service, root := newFileTestService(t)
	if err := os.WriteFile(filepath.Join(root, "sample.txt"), []byte("needle --needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, path, query string
	}{
		{"single file", "sample.txt", "needle"},
		{"leading hyphen", ".", "--needle"},
		{"known option as text", ".", "--files"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := service.SearchText(context.Background(), SearchRequest{Path: test.path, Query: test.query})
			if err != nil {
				t.Fatal(err)
			}
			matches := result["matches"].([]map[string]any)
			want := 1
			if test.query == "--files" {
				want = 0
			}
			if result["engine"] != "rg" || len(matches) != want {
				t.Fatalf("result = %#v, want rg with %d matches", result, want)
			}
			if want > 0 && matches[0]["path"] != "sample.txt" {
				t.Fatalf("match path = %#v, want sample.txt", matches[0]["path"])
			}
		})
	}
}

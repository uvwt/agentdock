package file

import (
	"reflect"
	"testing"
)

func TestApplyUpdateHunksPreservesUnicodeLineBoundaries(t *testing.T) {
	content := "alpha\u2028beta\nend\n"
	updated, err := applyUpdateHunks(content, [][]string{{" alpha\u2028beta", "-end", "+done"}}, "unicode.txt")
	if err != nil {
		t.Fatal(err)
	}
	if updated != "alpha\u2028beta\ndone\n" {
		t.Fatalf("updated content = %q", updated)
	}
}

func TestApplyUpdateHunksFinalNewlineIsControlledByHunk(t *testing.T) {
	tests := []struct {
		name    string
		content string
		hunk    []string
		want    string
	}{
		{
			name:    "remove final newline",
			content: "before\n",
			hunk:    []string{"-before", "+after", "-"},
			want:    "after",
		},
		{
			name:    "add final newline",
			content: "before",
			hunk:    []string{"-before", "+after", "+"},
			want:    "after\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updated, err := applyUpdateHunks(tt.content, [][]string{tt.hunk}, "newline.txt")
			if err != nil {
				t.Fatal(err)
			}
			if updated != tt.want {
				t.Fatalf("updated content = %q, want %q", updated, tt.want)
			}
		})
	}
}

func TestParseUpdateHunkAcceptsStrippedEmptyContextLine(t *testing.T) {
	oldLines, newLines, err := parseUpdateHunk([]string{"", "-before", "+after"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(oldLines, []string{"", "before"}) {
		t.Fatalf("old lines = %#v", oldLines)
	}
	if !reflect.DeepEqual(newLines, []string{"", "after"}) {
		t.Fatalf("new lines = %#v", newLines)
	}
}

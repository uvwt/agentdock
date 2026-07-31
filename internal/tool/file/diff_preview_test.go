package file

import (
	"strings"
	"testing"
)

func TestUnifiedDiffPreviewWithoutExternalDiff(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	preview, truncated, stats, err := unifiedDiffPreview(
		"example.txt",
		"alpha\nkeep\n",
		"beta\nkeep\n",
		65536,
	)
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Fatal("small diff was truncated")
	}
	for _, want := range []string{
		"--- a/example.txt\n",
		"+++ b/example.txt\n",
		"-alpha\n",
		"+beta\n",
	} {
		if !strings.Contains(preview, want) {
			t.Fatalf("preview does not contain %q:\n%s", want, preview)
		}
	}
	if strings.HasPrefix(preview, "diff ") {
		t.Fatalf("preview contains an unexpected command header:\n%s", preview)
	}
	if stats != (diffStats{FilesChanged: 1, Insertions: 1, Deletions: 1}) {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestUnifiedDiffPreviewPreservesMissingNewlineMarkers(t *testing.T) {
	preview, _, _, err := unifiedDiffPreview("example.txt", "alpha", "beta", 65536)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(preview, "\\ No newline at end of file"); got != 2 {
		t.Fatalf("missing newline markers = %d, want 2:\n%s", got, preview)
	}
}

func TestUnifiedDiffPreviewUsesEmptyRanges(t *testing.T) {
	tests := []struct {
		name string
		old  string
		new  string
		want string
	}{
		{name: "add", new: "alpha\n", want: "@@ -0,0 +1,1 @@"},
		{name: "delete", old: "alpha\n", want: "@@ -1,1 +0,0 @@"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			preview, _, _, err := unifiedDiffPreview("example.txt", test.old, test.new, 65536)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(preview, test.want) {
				t.Fatalf("preview does not contain %q:\n%s", test.want, preview)
			}
		})
	}
}

func TestUnifiedDiffPreviewTruncatesAfterCollectingStats(t *testing.T) {
	preview, truncated, stats, err := unifiedDiffPreview("example.txt", "alpha\n", "beta\n", 16)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len([]byte(preview)) > 16 {
		t.Fatalf("preview length = %d, truncated = %v", len([]byte(preview)), truncated)
	}
	if stats != (diffStats{FilesChanged: 1, Insertions: 1, Deletions: 1}) {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestUnifiedDiffPreviewReturnsEmptyForIdenticalContent(t *testing.T) {
	preview, truncated, stats, err := unifiedDiffPreview("example.txt", "same\n", "same\n", 65536)
	if err != nil {
		t.Fatal(err)
	}
	if preview != "" || truncated || stats != (diffStats{}) {
		t.Fatalf("preview = %q, truncated = %v, stats = %#v", preview, truncated, stats)
	}
}

package recall

import (
	"testing"
	"unicode/utf8"
)

func TestMemoryUnifiedDiffTruncatesAtUTF8Boundary(t *testing.T) {
	full := MemoryUnifiedDiff("note.md", "# 标题\n旧内容", "# 标题\n新内容", 0)
	if full == "" {
		t.Fatal("MemoryUnifiedDiff() returned empty diff")
	}
	for maxBytes := 1; maxBytes < len(full); maxBytes++ {
		got := MemoryUnifiedDiff("note.md", "# 标题\n旧内容", "# 标题\n新内容", maxBytes)
		if !utf8.ValidString(got) {
			t.Fatalf("maxBytes=%d returned invalid UTF-8: %q", maxBytes, got)
		}
		if len(got) > maxBytes {
			t.Fatalf("maxBytes=%d returned %d bytes", maxBytes, len(got))
		}
	}
}

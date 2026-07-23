package file

import "testing"
import "unicode/utf8"

func TestMatchesAnyWithDoubleStar(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		patterns []string
		want     bool
	}{
		{name: "root go file", path: "main.go", patterns: []string{"**/*.go"}, want: true},
		{name: "nested go file", path: "internal/tool/file/text.go", patterns: []string{"**/*.go"}, want: true},
		{name: "scoped go file", path: "internal/tool/file/text.go", patterns: []string{"internal/**/*.go"}, want: true},
		{name: "wrong extension", path: "internal/tool/file/text.md", patterns: []string{"**/*.go"}, want: false},
		{name: "empty pattern ignored", path: "internal/tool/file/text.go", patterns: []string{"", "**/*.go"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesAny(tt.path, tt.patterns); got != tt.want {
				t.Fatalf("matchesAny(%q, %v) = %v, want %v", tt.path, tt.patterns, got, tt.want)
			}
		})
	}
}

func TestSliceTextTruncatesAtUTF8Boundary(t *testing.T) {
	content := "第一行\n第二行\n第三行"
	got, meta := sliceText(content, 1, 3, len("第一行\n第")+1)
	if !utf8.ValidString(got) {
		t.Fatalf("sliceText returned invalid UTF-8: %q", got)
	}
	if !meta.Truncated || meta.TruncatedReason != "max_bytes" {
		t.Fatalf("expected max_bytes truncation, got %#v", meta)
	}
	if meta.NextStartLine == 0 {
		t.Fatalf("expected next_start_line on truncation, got %#v", meta)
	}
}

func TestTruncateStringPreservesUTF8(t *testing.T) {
	got := truncateString("你好世界", 5)
	if !utf8.ValidString(got) {
		t.Fatalf("truncateString returned invalid UTF-8: %q", got)
	}
	if got != "你" {
		t.Fatalf("truncateString = %q, want first complete rune", got)
	}
}

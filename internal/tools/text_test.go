package tools

import "testing"

func TestMatchesAnyWithDoubleStar(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		patterns []string
		want     bool
	}{
		{name: "root go file", path: "main.go", patterns: []string{"**/*.go"}, want: true},
		{name: "nested go file", path: "internal/tools/text.go", patterns: []string{"**/*.go"}, want: true},
		{name: "scoped go file", path: "internal/tools/text.go", patterns: []string{"internal/**/*.go"}, want: true},
		{name: "wrong extension", path: "internal/tools/text.md", patterns: []string{"**/*.go"}, want: false},
		{name: "empty pattern ignored", path: "internal/tools/text.go", patterns: []string{"", "**/*.go"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesAny(tt.path, tt.patterns); got != tt.want {
				t.Fatalf("matchesAny(%q, %v) = %v, want %v", tt.path, tt.patterns, got, tt.want)
			}
		})
	}
}

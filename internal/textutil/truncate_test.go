package textutil

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSafeTruncateStringRespectsUTF8Boundaries(t *testing.T) {
	const value = "你a"
	tests := []struct {
		name      string
		maxBytes  int
		text      string
		truncated bool
		omitted   int
	}{
		{name: "unlimited zero", maxBytes: 0, text: value},
		{name: "unlimited negative", maxBytes: -1, text: value},
		{name: "inside multibyte rune", maxBytes: 2, text: "", truncated: true, omitted: 4},
		{name: "exact rune boundary", maxBytes: 3, text: "你", truncated: true, omitted: 1},
		{name: "exact full length", maxBytes: 4, text: value},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := SafeTruncateString(value, test.maxBytes)
			if got.Text != test.text || got.Truncated != test.truncated || got.Omitted != test.omitted {
				t.Fatalf("SafeTruncateString() = %#v, want text=%q truncated=%v omitted=%d", got, test.text, test.truncated, test.omitted)
			}
			if !utf8.ValidString(got.Text) {
				t.Fatalf("result is invalid UTF-8: %q", got.Text)
			}
		})
	}
}

func TestSafeTruncateBytesReportsOmittedBytes(t *testing.T) {
	data := []byte("alpha-世界")
	got := SafeTruncateBytes(data, len("alpha-世"))
	if got.Text != "alpha-世" {
		t.Fatalf("text = %q, want alpha-世", got.Text)
	}
	if !got.Truncated {
		t.Fatal("Truncated = false, want true")
	}
	if got.Omitted != len("界") {
		t.Fatalf("Omitted = %d, want %d", got.Omitted, len("界"))
	}
}

func TestSafeTruncateBytesDoesNotSplitRune(t *testing.T) {
	data := []byte("a界b")
	got := SafeTruncateBytes(data, 3)
	if got.Text != "a" {
		t.Fatalf("text = %q, want a", got.Text)
	}
	if got.Omitted != len(data)-1 {
		t.Fatalf("Omitted = %d, want %d", got.Omitted, len(data)-1)
	}
}

func FuzzSafeTruncateString(f *testing.F) {
	for _, seed := range []string{"", "ascii", "你a", "emoji-🙂-tail", "e\u0301"} {
		f.Add(seed, uint16(3))
	}
	f.Fuzz(func(t *testing.T, value string, rawMax uint16) {
		if !utf8.ValidString(value) {
			t.Skip()
		}
		maxBytes := int(rawMax % 256)
		got := SafeTruncateString(value, maxBytes)
		if !utf8.ValidString(got.Text) {
			t.Fatalf("invalid UTF-8 result for %q at %d bytes", value, maxBytes)
		}
		if !strings.HasPrefix(value, got.Text) {
			t.Fatalf("result %q is not a prefix of %q", got.Text, value)
		}
		if got.Omitted != len(value)-len(got.Text) {
			t.Fatalf("omitted=%d, want %d", got.Omitted, len(value)-len(got.Text))
		}
		wantTruncated := maxBytes > 0 && len(value) > maxBytes
		if got.Truncated != wantTruncated {
			t.Fatalf("truncated=%v, want %v", got.Truncated, wantTruncated)
		}
		if wantTruncated && len(got.Text) > maxBytes {
			t.Fatalf("result bytes=%d exceed max=%d", len(got.Text), maxBytes)
		}
	})
}

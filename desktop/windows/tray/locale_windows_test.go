//go:build windows

package tray

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsSimplifiedChineseLocale(t *testing.T) {
	tests := []struct {
		locale string
		want   bool
	}{
		{locale: "zh", want: true},
		{locale: "zh-CN", want: true},
		{locale: "zh-SG", want: true},
		{locale: "zh-Hans", want: true},
		{locale: "zh-Hans-CN", want: true},
		{locale: "zh-TW", want: false},
		{locale: "zh-HK", want: false},
		{locale: "zh-Hant", want: false},
		{locale: "en-US", want: false},
		{locale: "", want: false},
	}

	for _, test := range tests {
		t.Run(test.locale, func(t *testing.T) {
			if got := isSimplifiedChineseLocale(test.locale); got != test.want {
				t.Fatalf("isSimplifiedChineseLocale(%q)=%v want=%v", test.locale, got, test.want)
			}
		})
	}
}

func TestResolveTrayLocaleHonorsExplicitPreference(t *testing.T) {
	tests := []struct {
		name         string
		preference   string
		systemLocale string
		want         string
	}{
		{name: "explicit English overrides Chinese system", preference: "en", systemLocale: "zh-CN", want: "en"},
		{name: "explicit Chinese overrides English system", preference: "zh-CN", systemLocale: "en-US", want: "zh-CN"},
		{name: "system follows simplified Chinese", preference: "system", systemLocale: "zh-SG", want: "zh-CN"},
		{name: "system follows English", preference: "system", systemLocale: "en-US", want: "en"},
		{name: "invalid preference follows system", preference: "invalid", systemLocale: "zh-Hans", want: "zh-CN"},
		{name: "traditional Chinese falls back to English", preference: "system", systemLocale: "zh-TW", want: "en"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resolveTrayLocale(test.preference, test.systemLocale); got != test.want {
				t.Fatalf("resolveTrayLocale(%q, %q)=%q want=%q", test.preference, test.systemLocale, got, test.want)
			}
		})
	}
}

func TestReadTrayLanguagePreference(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", root)
	if got := readTrayLanguagePreference(); got != "system" {
		t.Fatalf("missing preference=%q want=system", got)
	}

	preferenceDir := filepath.Join(root, "AgentDock")
	if err := os.MkdirAll(preferenceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(preferenceDir, "ui-language"), []byte("zh-CN\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readTrayLanguagePreference(); got != "zh-CN" {
		t.Fatalf("stored preference=%q want=zh-CN", got)
	}
}

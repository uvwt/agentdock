//go:build windows

package tray

import "testing"

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

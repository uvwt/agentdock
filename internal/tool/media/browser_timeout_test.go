package media

import (
	"testing"
	"time"
)

func TestBrowserRunnerTimeoutIncludesProtocolAndActionMargin(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		want time.Duration
	}{
		{name: "default operation margin", args: map[string]any{}, want: 32 * time.Second},
		{name: "explicit operation margin", args: map[string]any{"timeout_ms": 10000}, want: 12 * time.Second},
		{
			name: "long action controls runner timeout",
			args: map[string]any{
				"timeout_ms": 10000,
				"actions":    []any{map[string]any{"action": "wait_for_text", "timeout_ms": 40000}},
			},
			want: 45 * time.Second,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := browserRunnerTimeout(test.args); got != test.want {
				t.Fatalf("browserRunnerTimeout() = %s, want %s", got, test.want)
			}
		})
	}
}

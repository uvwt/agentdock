package desktopruntime

import (
	"testing"
	"time"
)

func TestNextTunnelRetryDelay(t *testing.T) {
	tests := []struct {
		name        string
		previous    time.Duration
		runDuration time.Duration
		want        time.Duration
	}{
		{name: "first failure", want: 5 * time.Second},
		{name: "second short failure", previous: 5 * time.Second, want: 10 * time.Second},
		{name: "exponential backoff", previous: 10 * time.Second, want: 20 * time.Second},
		{name: "cap", previous: 40 * time.Second, want: time.Minute},
		{name: "stable run resets", previous: time.Minute, runDuration: 2 * time.Minute, want: 5 * time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := nextTunnelRetryDelay(test.previous, test.runDuration); got != test.want {
				t.Fatalf("nextTunnelRetryDelay(%s, %s) = %s, want %s", test.previous, test.runDuration, got, test.want)
			}
		})
	}
}

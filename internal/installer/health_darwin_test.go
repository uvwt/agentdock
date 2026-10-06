//go:build darwin

package installer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitHealthyCurlRequiresConsecutiveTargetVersionSuccesses(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		call := calls.Add(1)
		version := "1.0.0"
		if call == 2 {
			version = "0.9.1"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "version": version})
	}))
	defer server.Close()

	if err := waitHealthyCurl(context.Background(), server.URL, "v1.0.0", 2*time.Second); err != nil {
		t.Fatalf("curl health should pass after two consecutive target-version responses: %v", err)
	}
	if got := calls.Load(); got < 4 {
		t.Fatalf("curl health accepted non-consecutive successes after %d calls", got)
	}
}

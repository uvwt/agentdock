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

func TestWaitHealthyRequiresConsecutiveOKResponses(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		call := calls.Add(1)
		ok := call != 2
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": ok})
	}))
	defer server.Close()

	if err := waitHealthy(context.Background(), server.URL, 2*time.Second); err != nil {
		t.Fatalf("health should pass after two consecutive ok responses: %v", err)
	}
	if got := calls.Load(); got < 4 {
		t.Fatalf("health accepted non-consecutive successes after %d calls", got)
	}
}

package selfupdate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitForVersionRequiresConsecutiveHealthyResponses(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		call := calls.Add(1)
		version := "v0.4.5"
		if call == 2 {
			version = "v0.4.4"
		}
		_ = json.NewEncoder(w).Encode(healthResponse{OK: true, Version: version})
	}))
	defer server.Close()

	if err := waitForVersion(context.Background(), []string{server.URL}, "v0.4.5", 4*time.Second); err != nil {
		t.Fatal(err)
	}
	if calls.Load() < 4 {
		t.Fatalf("health check returned before two consecutive successes: %d calls", calls.Load())
	}
}

func TestLocalHealthURLSupportsIPv6(t *testing.T) {
	if got := localHealthURL("::1", 8765); got != "http://[::1]:8765/healthz" {
		t.Fatalf("unexpected IPv6 health URL: %s", got)
	}
}

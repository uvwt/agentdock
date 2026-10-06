package updateengine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitForVersionRejectsWrongVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(HealthResponse{OK: true, Version: "v0.9.1"})
	}))
	defer server.Close()

	err := WaitForVersion(context.Background(), []string{server.URL}, "v1.0.0", 600*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "reports version v0.9.1") {
		t.Fatalf("wrong-version health must fail, got: %v", err)
	}
}

func TestWaitForVersionRequiresConsecutiveTargetVersionSuccesses(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		call := calls.Add(1)
		version := "v1.0.0"
		if call == 2 {
			version = "v0.9.1"
		}
		_ = json.NewEncoder(w).Encode(HealthResponse{OK: true, Version: version})
	}))
	defer server.Close()

	if err := WaitForVersion(context.Background(), []string{server.URL}, "1.0.0", 3*time.Second); err != nil {
		t.Fatalf("health should pass after two consecutive target-version responses: %v", err)
	}
	if got := calls.Load(); got < 4 {
		t.Fatalf("health accepted non-consecutive successes after %d calls", got)
	}
}

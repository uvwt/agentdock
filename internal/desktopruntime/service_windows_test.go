//go:build windows

package desktopruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWindowsCoreStartTimeoutKeepsColdStartHeadroom(t *testing.T) {
	if WindowsCoreStartTimeout != 60*time.Second {
		t.Fatalf("WindowsCoreStartTimeout = %s, want 1m", WindowsCoreStartTimeout)
	}
}

func TestWaitForHealthRetriesUntilHealthy(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests < 3 {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := waitForHealth(context.Background(), server.URL, 2*time.Second); err != nil {
		t.Fatalf("waitForHealth() rejected a service that became healthy on retry: %v", err)
	}
	if requests != 3 {
		t.Fatalf("health requests = %d, want 3", requests)
	}
}

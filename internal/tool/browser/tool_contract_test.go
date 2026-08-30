package browser

import (
	"context"
	"testing"
)

func TestBrowserSessionAcceptsAdvertisedTimeoutForCloseAndCleanup(t *testing.T) {
	service := New(Config{AgentDockHome: t.TempDir()}, nil)
	t.Cleanup(func() { _ = service.Close() })

	closed, err := service.HandleSession(context.Background(), map[string]any{
		"action": "close", "session_id": "missing", "timeout_ms": 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if closed["code"] != ErrSessionNotFound {
		t.Fatalf("close result = %#v", closed)
	}

	cleaned, err := service.HandleSession(context.Background(), map[string]any{
		"action": "cleanup_stale", "max_age_ms": 1000, "timeout_ms": 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cleaned["browser_ok"] != true {
		t.Fatalf("cleanup result = %#v", cleaned)
	}
}

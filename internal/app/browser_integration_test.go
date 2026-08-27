//go:build browser_integration

package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/config"
	toolbrowser "github.com/uvwt/agentdock/internal/tool/browser"
)

func appBrowserIntegrationRuntime(t *testing.T) (*Runtime, string) {
	t.Helper()
	executable := strings.TrimSpace(os.Getenv("AGENTDOCK_BROWSER_EXECUTABLE_PATH"))
	if executable == "" {
		t.Fatal("browser_integration requires AGENTDOCK_BROWSER_EXECUTABLE_PATH and must not skip")
	}
	home := filepath.Join(t.TempDir(), ".agentdock")
	cfg := config.Config{
		AgentDockHome:         home,
		AgentDockDefaultDir:   t.TempDir(),
		BrowserEnabled:        true,
		BrowserExecutablePath: executable,
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return runtime, home
}

func appBrowserIntegrationServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<title>Runtime Browser</title><main id="ready">runtime-ready</main>`)
	}))
}

func startRuntimeBrowser(t *testing.T, runtime *Runtime, url string) string {
	t.Helper()
	result, err := runtime.browserSession(context.Background(), map[string]any{
		"action": "start", "url": url, "browser": "auto", "headless": true, "timeout_ms": 60_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["browser_ok"] != true {
		t.Fatalf("browser_session result = %#v", result)
	}
	sessionID, _ := result["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("browser_session missing session id: %#v", result)
	}
	return sessionID
}

func TestBrowserIntegrationCloseAfterWaitsForArtifactPublication(t *testing.T) {
	server := appBrowserIntegrationServer(t)
	defer server.Close()
	runtime, _ := appBrowserIntegrationRuntime(t)
	defer runtime.Close()
	sessionID := startRuntimeBrowser(t, runtime, server.URL)

	result, err := runtime.browserAct(context.Background(), map[string]any{
		"session_id": sessionID,
		"actions": []any{map[string]any{
			"action": "wait_for_text", "text": "runtime-ready", "exact": true, "state": "visible", "timeout_ms": 5_000,
		}},
		"close_after": true,
		"timeout_ms":  10_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["browser_ok"] != true || result["closed"] != true {
		t.Fatalf("browser_act close_after result = %#v", result)
	}
	screenshot, ok := result["screenshot"].(map[string]any)
	if !ok || screenshot["artifact_id"] == "" {
		t.Fatalf("browser_act screenshot artifact = %#v", result["screenshot"])
	}
	_, err = runtime.browser.Snapshot(context.Background(), toolbrowser.SnapshotRequest{SessionID: sessionID, Timeout: time.Second})
	var browserErr *toolbrowser.Error
	if !errors.As(err, &browserErr) || browserErr.Code != toolbrowser.ErrSessionNotFound {
		t.Fatalf("session after successful close_after = %v", err)
	}
}

func TestBrowserIntegrationArtifactFailureRetainsSession(t *testing.T) {
	server := appBrowserIntegrationServer(t)
	defer server.Close()
	runtime, home := appBrowserIntegrationRuntime(t)
	defer runtime.Close()
	sessionID := startRuntimeBrowser(t, runtime, server.URL)

	if err := os.WriteFile(filepath.Join(home, "public-artifacts"), []byte("block directory creation"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := runtime.browserAct(context.Background(), map[string]any{
		"session_id":  sessionID,
		"actions":     []any{map[string]any{"action": "wait", "value": 1}},
		"close_after": true,
		"timeout_ms":  10_000,
	})
	if err == nil {
		t.Fatal("browser_act unexpectedly published artifact through obstructed public-artifacts path")
	}
	if _, snapshotErr := runtime.browser.Snapshot(context.Background(), toolbrowser.SnapshotRequest{SessionID: sessionID, Timeout: 5 * time.Second}); snapshotErr != nil {
		t.Fatalf("session was closed after artifact publication failure: %v", snapshotErr)
	}
}

func TestBrowserIntegrationRuntimeCloseClosesBrowserService(t *testing.T) {
	server := appBrowserIntegrationServer(t)
	defer server.Close()
	runtime, _ := appBrowserIntegrationRuntime(t)
	sessionID := startRuntimeBrowser(t, runtime, server.URL)
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := runtime.browser.Snapshot(context.Background(), toolbrowser.SnapshotRequest{SessionID: sessionID, Timeout: time.Second})
	var browserErr *toolbrowser.Error
	if !errors.As(err, &browserErr) || browserErr.Code != toolbrowser.ErrSessionNotFound {
		t.Fatalf("runtime close left browser session addressable: %v", err)
	}
}

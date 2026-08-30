//go:build browser_integration

package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
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
	result, err := runtime.Call(context.Background(), "browser_session", map[string]any{
		"action": "start", "url": url, "browser": "auto", "headless": true, "timeout_ms": 60_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertToolResultMatchesOutputSchema(t, "browser_session", result)
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

	snapshot, err := runtime.Call(context.Background(), "browser_snapshot", map[string]any{"session_id": sessionID})
	if err != nil {
		t.Fatal(err)
	}
	assertToolResultMatchesOutputSchema(t, "browser_snapshot", snapshot)

	result, err := runtime.Call(context.Background(), "browser_act", map[string]any{
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
	assertToolResultMatchesOutputSchema(t, "browser_act", result)
	if result["browser_ok"] != true || result["closed"] != true {
		t.Fatalf("browser_act close_after result = %#v", result)
	}
	screenshot, ok := result["screenshot"].(map[string]any)
	if !ok || screenshot["artifact_id"] == "" {
		t.Fatalf("browser_act screenshot artifact = %#v", result["screenshot"])
	}
	afterClose, err := runtime.Call(context.Background(), "browser_snapshot", map[string]any{"session_id": sessionID, "timeout_ms": 1000})
	if err != nil {
		t.Fatal(err)
	}
	if afterClose["browser_ok"] != false || afterClose["code"] != "SESSION_NOT_FOUND" {
		t.Fatalf("session after successful close_after = %#v", afterClose)
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
	_, err := runtime.Call(context.Background(), "browser_act", map[string]any{
		"session_id":  sessionID,
		"actions":     []any{map[string]any{"action": "wait", "value": 1}},
		"close_after": true,
		"timeout_ms":  10_000,
	})
	if err == nil {
		t.Fatal("browser_act unexpectedly published artifact through obstructed public-artifacts path")
	}
	closed, closeErr := runtime.Call(context.Background(), "browser_session", map[string]any{
		"action": "close", "session_id": sessionID, "timeout_ms": 5000,
	})
	if closeErr != nil || closed["browser_ok"] != true {
		t.Fatalf("session was not retained after artifact publication failure: result=%#v err=%v", closed, closeErr)
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
	afterClose, err := runtime.Call(context.Background(), "browser_snapshot", map[string]any{"session_id": sessionID, "timeout_ms": 1000})
	if err != nil {
		t.Fatal(err)
	}
	if afterClose["browser_ok"] != false || afterClose["code"] != "SESSION_NOT_FOUND" {
		t.Fatalf("runtime close left browser session addressable: %#v", afterClose)
	}
}

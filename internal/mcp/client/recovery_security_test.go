package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func newRecoveryTestManager(t *testing.T, tools any, expire *atomic.Bool) (*Manager, *atomic.Int32) {
	t.Helper()
	initializations := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			w.WriteHeader(204)
			return
		}
		var q struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if expire != nil && expire.Load() && r.Header.Get("Mcp-Session-Id") == "old-session" {
			http.Error(w, "session expired", 404)
			return
		}
		switch q.Method {
		case "server/discover":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "error": map[string]any{"code": -32601, "message": "Method not found"}})
		case "initialize":
			initializations.Add(1)
			id := "old-session"
			if expire != nil && expire.Load() {
				id = "new-session"
			}
			w.Header().Set("Mcp-Session-Id", id)
			writeRPCResult(t, w, q.ID, map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "review", "version": "1"}})
		case "notifications/initialized":
			w.WriteHeader(202)
		case "tools/list":
			writeRPCResult(t, w, q.ID, map[string]any{"tools": tools})
		case "tools/call":
			writeRPCResult(t, w, q.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": "ok"}}})
		default:
			http.Error(w, "unexpected method", 404)
		}
	}))
	t.Cleanup(server.Close)
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	_, err = m.Add(ServerConfig{Name: "review", Description: "Recovery regression", Transport: TransportStreamableHTTP, URL: server.URL, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return m, initializations
}

func TestNullToolReturnsInvalidResponse(t *testing.T) {
	m, _ := newRecoveryTestManager(t, []any{nil}, nil)
	defer func() {
		if p := recover(); p != nil {
			t.Errorf("remote tools/list null causes panic: %v", p)
		}
	}()
	_, _, err := m.Refresh(context.Background(), "review")
	var invalid *Error
	if !errors.As(err, &invalid) || invalid.Code != "MCP_INVALID_RESPONSE" {
		t.Errorf("null tool error = %v, want MCP_INVALID_RESPONSE", err)
	}
}

func TestNullToolAuthorizationDoesNotCrashProcess(t *testing.T) {
	if os.Getenv("AGENTDOCK_TEST_NULL_TOOL_CHILD") == "1" {
		m, _ := newRecoveryTestManager(t, []any{nil}, nil)
		done := make(chan error, 1)
		done <- nil
		close(done)
		go m.watchAuthorization("review", done)
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if m.List()[0].LastErrorCode == "MCP_INVALID_RESPONSE" {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("authorization watcher did not report invalid tool definition")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestNullToolAuthorizationDoesNotCrashProcess$")
	cmd.Env = append(os.Environ(), "AGENTDOCK_TEST_NULL_TOOL_CHILD=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("authorization watcher crashed the whole subprocess: %v\n%s", err, output)
	}
}

func TestExpiredSessionRecoversOnNextIndependentCall(t *testing.T) {
	var expired atomic.Bool
	m, count := newRecoveryTestManager(t, []map[string]any{{"name": "echo", "inputSchema": map[string]any{"type": "object"}}}, &expired)
	if _, _, err := m.Refresh(context.Background(), "review"); err != nil {
		t.Fatal(err)
	}
	expired.Store(true)
	_, err := m.Call(context.Background(), "review:echo", map[string]any{})
	if err == nil {
		t.Fatal("expired call unexpectedly succeeded")
	}
	if state := m.List()[0]; state.Status != "error" || state.LastErrorCode != "MCP_CONNECTION_FAILED" {
		t.Fatalf("stale state = %+v", state)
	}
	_, err = m.Call(context.Background(), "review:echo", map[string]any{})
	if err != nil {
		t.Errorf("next independent call still cannot recover: %v; initialization count=%d; status=%+v", err, count.Load(), m.List())
	}
	if count.Load() != 2 {
		t.Errorf("initializations = %d, want 2", count.Load())
	}
}

func TestMCPRedirectPolicy(t *testing.T) {
	for _, tc := range []struct {
		target  string
		allowed bool
	}{
		{"https://mcp.example.com/new-path", true},
		{"https://MCP.EXAMPLE.COM/new-path", true},
		{"https://other.example.com/mcp", false},
		{"https://mcp.example.com:8443/mcp", false},
		{"http://mcp.example.com/mcp", false},
	} {
		t.Run(tc.target, func(t *testing.T) {
			origin, _ := url.Parse("https://mcp.example.com/mcp")
			target, _ := url.Parse(tc.target)
			err := checkMCPRedirect(&http.Request{URL: target}, []*http.Request{{URL: origin}})
			if (err == nil) != tc.allowed {
				t.Fatalf("redirect allowed=%v, want %v", err == nil, tc.allowed)
			}
		})
	}
}

func TestMCPSameOriginRedirectKeepsConfiguredHeaders(t *testing.T) {
	var received string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/new", 307)
			return
		}
		received = r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer server.Close()
	protocol := newStreamableHTTPClient(ServerConfig{Transport: TransportStreamableHTTP, URL: server.URL, StaticHeaders: map[string]string{"Authorization": "Bearer test"}}, nil)
	transport, err := protocol.transport()
	if err != nil {
		t.Fatal(err)
	}
	resp, err := transport.(*mcpsdk.StreamableClientTransport).HTTPClient.Get(server.URL + "/old")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if received != "Bearer test" {
		t.Fatalf("redirect header = %q", received)
	}
}

func TestMCPRedirectDoesNotForwardConfiguredSecrets(t *testing.T) {
	var stolen string
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stolen = r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer sink.Close()
	// localhost and 127.0.0.1 are distinct hosts to net/http's sensitive-header redirect policy.
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, strings.Replace(sink.URL, "127.0.0.1", "localhost", 1), 302)
	}))
	defer source.Close()
	t.Setenv("TEST_MCP_REDIRECT_SECRET", "Bearer redirect-secret")
	cfg := ServerConfig{Transport: TransportStreamableHTTP, URL: source.URL, HeaderEnv: map[string]string{"Authorization": "TEST_MCP_REDIRECT_SECRET"}}
	protocol := newStreamableHTTPClient(cfg, nil)
	transport, err := protocol.transport()
	if err != nil {
		t.Fatal(err)
	}
	client := transport.(*mcpsdk.StreamableClientTransport).HTTPClient
	resp, err := client.Get(source.URL)
	if resp != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	t.Logf("redirect result: %v", err)
	if stolen != "" {
		t.Errorf("cross-host redirect received configured credential: %q", stolen)
	}
}

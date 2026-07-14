package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
)

func TestPrivateNoteManageProxiesNexusAPI(t *testing.T) {
	type expectedCall struct {
		path    string
		payload map[string]any
	}
	calls := []expectedCall{
		{path: "/v1/private-notes/search", payload: map[string]any{"query": "恢复", "max_results": float64(5)}},
		{path: "/v1/private-notes/read", payload: map[string]any{"path": "notes/recovery/demo.md", "max_bytes": float64(2048)}},
		{path: "/v1/private-notes/write", payload: map[string]any{"path": "notes/recovery/demo.md", "summary": "安全简介", "tags": []any{"recovery"}, "content": "secret body", "confirmed": true, "overwrite": true}},
		{path: "/v1/private-notes/delete", payload: map[string]any{"path": "notes/recovery/demo.md", "confirmed": true}},
		{path: "/v1/private-notes/status", payload: map[string]any{"action": "list"}},
		{path: "/v1/private-notes/maintenance", payload: map[string]any{"action": "encrypt-all"}},
	}
	index := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if index >= len(calls) {
			t.Fatalf("unexpected extra request: %s", r.URL.Path)
		}
		want := calls[index]
		index++
		if r.Method != http.MethodPost || r.URL.Path != want.path {
			t.Fatalf("request = %s %s, want POST %s", r.Method, r.URL.Path, want.path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer nexus-secret" {
			t.Fatalf("authorization = %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(payload, want.payload) {
			t.Fatalf("payload = %#v, want %#v", payload, want.payload)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "action": "done", "root": "/recall/private-notes"})
	}))
	defer server.Close()

	rt := newPrivateNoteProxyRuntime(t, server.URL, "nexus-secret")
	requests := []map[string]any{
		{"action": "search", "query": "恢复", "max_results": 5},
		{"action": "read", "path": "notes/recovery/demo.md", "max_bytes": 2048},
		{"action": "write", "path": "notes/recovery/demo.md", "summary": "安全简介", "tags": []any{"recovery"}, "content": "secret body", "confirmed": true, "overwrite": true},
		{"action": "delete", "path": "notes/recovery/demo.md", "confirmed": true},
		{"action": "status", "status_action": "list"},
		{"action": "maintain", "maintenance_action": "encrypt-all"},
	}
	for _, request := range requests {
		result, err := rt.privateNoteManage(context.Background(), request)
		if err != nil {
			t.Fatalf("privateNoteManage(%v): %v", request["action"], err)
		}
		if result["private_note_store"] != "NexusDock Private Notes" || result["recall_endpoint"] != server.URL {
			t.Fatalf("unexpected proxy result: %#v", result)
		}
	}
	if index != len(calls) {
		t.Fatalf("request count = %d, want %d", index, len(calls))
	}
}

func TestPrivateNoteManageRejectsUnknownActionWithoutRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("unknown action must not call Nexus")
	}))
	defer server.Close()
	rt := newPrivateNoteProxyRuntime(t, server.URL, "")
	if _, err := rt.privateNoteManage(context.Background(), map[string]any{"action": "unknown"}); err == nil {
		t.Fatal("unknown private note action succeeded")
	}
}

func newPrivateNoteProxyRuntime(t *testing.T, endpoint, token string) *Runtime {
	t.Helper()
	home := t.TempDir()
	cfg := config.Config{
		AgentDockDefaultDir: t.TempDir(),
		AgentDockHome:       filepath.Join(home, ".agentdock"),
		NexusEndpoint:       endpoint,
		NexusToken:          token,
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	return rt
}

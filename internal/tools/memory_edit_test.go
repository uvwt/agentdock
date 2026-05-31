package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
)

func newMemoryTestRuntime(t *testing.T, store map[string]string) (*Runtime, func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/memories/"):
			p := strings.TrimPrefix(r.URL.Path, "/v1/memories/")
			content, ok := store[p]
			if !ok {
				http.Error(w, `{"error":{"message":"not found"}}`, http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "memory": map[string]any{"path": p, "content": content}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/memories":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			p, _ := payload["path"].(string)
			content, _ := payload["content"].(string)
			store[p] = content
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "memory": map[string]any{"path": p, "content": content}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/memories":
			entries := []map[string]any{}
			for p := range store {
				entries = append(entries, map[string]any{"path": p})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "entries": entries, "count": len(entries)})
		default:
			http.NotFound(w, r)
		}
	}))
	cfg := config.Config{Workspace: t.TempDir(), MemoryEndpoint: server.URL, MemoryTimeoutMS: 30000, ToolProfile: config.ProfileFull}
	cfg.Normalize()
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return rt, server.Close
}

func TestMemoryDiffAndPatchDryRun(t *testing.T) {
	store := map[string]string{"devices/test.md": "# Test\nkey：old\n"}
	rt, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()
	res, err := rt.memoryDiff(context.Background(), map[string]any{"path": "devices/test.md", "old": "old", "new": "new"})
	if err != nil {
		t.Fatal(err)
	}
	if changed, _ := res["changed"].(bool); !changed {
		t.Fatalf("expected changed result: %#v", res)
	}
	res, err = rt.memoryPatch(context.Background(), map[string]any{"path": "devices/test.md", "old": "old", "new": "new"})
	if err != nil {
		t.Fatal(err)
	}
	if dryRun, _ := res["dry_run"].(bool); !dryRun {
		t.Fatalf("expected dry-run by default: %#v", res)
	}
	if store["devices/test.md"] != "# Test\nkey：old\n" {
		t.Fatalf("dry-run wrote content: %q", store["devices/test.md"])
	}
}

func TestMemoryPatchConfirmedWrites(t *testing.T) {
	store := map[string]string{"devices/test.md": "# Test\nkey：old\n"}
	rt, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()
	_, err := rt.memoryPatch(context.Background(), map[string]any{"path": "devices/test.md", "old": "old", "new": "new", "confirmed": true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(store["devices/test.md"], "key：new") {
		t.Fatalf("expected write, got: %q", store["devices/test.md"])
	}
}

func TestMemoryUpdateFactAndLint(t *testing.T) {
	store := map[string]string{
		"devices/test.md": "# Device\nplugin_dir：old\n",
		"ops/test.md":     "# Ops\nNo forbidden terms.\n",
	}
	rt, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()
	res, err := rt.memoryUpdateFact(context.Background(), map[string]any{"path": "devices/test.md", "key": "plugin_dir", "value": "plugins", "confirmed": true})
	if err != nil {
		t.Fatal(err)
	}
	if changed, _ := res["changed"].(bool); !changed {
		t.Fatalf("expected fact update change: %#v", res)
	}
	if !strings.Contains(store["devices/test.md"], "plugin_dir：plugins") {
		t.Fatalf("fact was not written: %q", store["devices/test.md"])
	}
	res, err = rt.memoryLint(context.Background(), map[string]any{"terms": []any{"plugin_dir"}, "max_entries": 10})
	if err != nil {
		t.Fatal(err)
	}
	if count, _ := res["finding_count"].(int); count == 0 {
		t.Fatalf("expected lint finding: %#v", res)
	}
}

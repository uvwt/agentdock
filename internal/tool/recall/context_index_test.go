package recall

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
)

func TestMemoryContextIndexUsesOnlyContextIndexEndpoint(t *testing.T) {
	contextCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/recall/context-index":
			contextCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"context_index": map[string]any{
					"project": "agentdock",
					"items": []any{map[string]any{
						"kind": "profile", "path": "profile.md", "title": "Profile", "summary": "Compact startup context.",
					}},
					"total_bytes": 120, "max_bytes": 3000, "truncated": false,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	svc := New(func() config.Config {
		return config.Config{NexusEndpoint: server.URL, NexusDeviceToken: "test-token"}
	})
	result, err := svc.memoryContextIndex(context.Background(), 3000)
	if err != nil {
		t.Fatal(err)
	}
	if contextCalls != 1 {
		t.Fatalf("context-index calls=%d", contextCalls)
	}
	index, ok := result["context_index"].(map[string]any)
	if !ok {
		t.Fatalf("missing context_index: %#v", result)
	}
	items, _ := index["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("unexpected context items: %#v", index)
	}
}

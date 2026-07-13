package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/config"
)

func TestRecallWriteDeleteRequiresConfirmationLocally(t *testing.T) {
	store := map[string]string{"devices/test.md": "# Test\n"}
	rt, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()

	_, err := rt.recallWrite(context.Background(), map[string]any{
		"target": "markdown",
		"action": "delete",
		"path":   "devices/test.md",
	})
	if err == nil {
		t.Fatal("expected delete to require confirmed=true before calling NexusDock Recall")
	}
	if _, ok := store["devices/test.md"]; !ok {
		t.Fatalf("unconfirmed delete must not mutate store")
	}
}

func TestRecallWriteRequiresExplicitTargetAction(t *testing.T) {
	store := map[string]string{}
	rt, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()

	_, err := rt.recallWrite(context.Background(), map[string]any{
		"title":   "缺少 target/action",
		"content": "没有显式选择 target/action 时应该返回校验错误，而不是静默走 auto。",
	})
	if err == nil {
		t.Fatal("expected missing target/action to fail")
	}
	if len(store) != 0 {
		t.Fatalf("missing target/action must not write, store=%#v", store)
	}
}

func TestRecallWriteCardPlanDoesNotWrite(t *testing.T) {
	store := map[string]string{}
	rt, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()

	res, err := rt.recallWrite(context.Background(), map[string]any{
		"target":  "card",
		"action":  "plan",
		"title":   "直接执行偏好",
		"content": "用户偏好直接执行可以自动完成的操作，不要反复确认或让用户代替完成。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(store) != 0 {
		t.Fatalf("card plan must not write, store=%#v", store)
	}
	if got, _ := res["recall_target"].(string); got != "card" {
		t.Fatalf("expected card target, got %#v", res)
	}
	if got, _ := res["recall_action"].(string); got != "plan" {
		t.Fatalf("expected plan action, got %#v", res)
	}
}

func TestRecallWriteMarkdownDiffDoesNotWrite(t *testing.T) {
	store := map[string]string{"projects/demo/project.md": "# Demo\nold\n"}
	rt, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()

	res, err := rt.recallWrite(context.Background(), map[string]any{
		"target":  "markdown",
		"action":  "diff",
		"path":    "projects/demo/project.md",
		"content": "# Demo\nnew\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if store["projects/demo/project.md"] != "# Demo\nold\n" {
		t.Fatalf("diff must not write, store=%#v", store)
	}
	if got, _ := res["recall_action"].(string); got != "diff" {
		t.Fatalf("expected diff action, got %#v", res)
	}
}

func TestRecallMaintainReindexCardsUsesCanonicalPrefix(t *testing.T) {
	var gotPrefix string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/embeddings/reindex" {
			http.NotFound(w, r)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		gotPrefix, _ = payload["prefix"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "count": 1})
	}))
	defer server.Close()

	cfg := config.Config{AgentDockDefaultDir: t.TempDir(), AgentDockHome: filepath.Join(t.TempDir(), ".agentdock"), NexusEndpoint: server.URL}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := rt.recallMaintain(context.Background(), map[string]any{"action": "reindex_cards"}); err != nil {
		t.Fatal(err)
	}
	if gotPrefix != "recall/managed/cards" {
		t.Fatalf("unexpected reindex prefix %q", gotPrefix)
	}
}

func TestRecallSearchCardsUsesCanonicalPrefix(t *testing.T) {
	var gotPrefix string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/recall/search" {
			http.NotFound(w, r)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		gotPrefix, _ = payload["prefix"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "results": []any{}, "count": 0})
	}))
	defer server.Close()

	cfg := config.Config{AgentDockDefaultDir: t.TempDir(), AgentDockHome: filepath.Join(t.TempDir(), ".agentdock"), NexusEndpoint: server.URL}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := rt.recallSearch(context.Background(), map[string]any{"kind": "card", "query": "deployment"}); err != nil {
		t.Fatal(err)
	}
	if gotPrefix != "recall/managed/cards" {
		t.Fatalf("unexpected card search prefix %q", gotPrefix)
	}
}

func TestRecallRequestTimeoutUsesLongWindowForEmbeddingReindex(t *testing.T) {
	if got := recallRequestTimeout("/v1/embeddings/reindex"); got != 180*time.Second {
		t.Fatalf("unexpected embedding reindex timeout %s", got)
	}
	defaultTimeout := time.Duration(config.RecallTimeoutMS) * time.Millisecond
	if got := recallRequestTimeout("/v1/recall/search"); got != defaultTimeout {
		t.Fatalf("unexpected default recall timeout %s", got)
	}
}

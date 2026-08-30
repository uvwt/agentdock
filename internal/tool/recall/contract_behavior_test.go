package recall

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uvwt/agentdock-protocol/mcpcontract"
	"github.com/uvwt/agentdock/internal/config"
	toolcore "github.com/uvwt/agentdock/internal/tool/core"
)

func TestRecallWriteMatchesCanonicalBehaviorCases(t *testing.T) {
	for _, behavior := range mcpcontract.RecallWriteBehaviorCases() {
		t.Run(behavior.Name, func(t *testing.T) {
			runtime, store, closeRuntime := newRecallWriteBehaviorService(t)
			defer closeRuntime()
			args := recallWriteBehaviorArgs(behavior)
			before, existedBefore := prepareRecallWriteBehaviorFixture(store, behavior)

			result, err := runtime.Write(context.Background(), args)
			got := classifyRecallWriteOutcome(behavior, result, err)
			if got != behavior.Expected {
				t.Fatalf("outcome=%s want=%s result=%#v err=%v", got, behavior.Expected, result, err)
			}
			assertRecallWriteBehaviorState(t, store, behavior, result, before, existedBefore)
		})
	}
}

func recallWriteBehaviorArgs(behavior mcpcontract.RecallWriteBehaviorCase) map[string]any {
	args := map[string]any{
		"target": behavior.Target, "action": behavior.Action,
		"confirmed": behavior.Confirmed,
	}
	if behavior.DryRun {
		args["dry_run"] = true
	}
	if behavior.Path != "" {
		args["path"] = behavior.Path
	}
	switch behavior.Target {
	case "card":
		args["title"] = "Canonical behavior card"
		args["content"] = "A reusable canonical behavior statement with enough detail to pass card validation without warnings."
	case "markdown":
		switch behavior.Action {
		case "append":
			args["content"] = "appended line\n"
		case "patch":
			args["old"] = "value: old"
			args["new"] = "value: new"
		case "update_fact":
			args["key"] = "value"
			args["value"] = "new"
		case "diff":
			args["content"] = "# Existing\n\nvalue: new\n"
		case "delete":
		default:
			args["content"] = "# Canonical\n\nvalue: new\n"
		}
	}
	return args
}

func prepareRecallWriteBehaviorFixture(store map[string]string, behavior mcpcontract.RecallWriteBehaviorCase) (string, bool) {
	if behavior.Target != "markdown" || behavior.Path == "" || !behavior.Existing {
		return "", false
	}
	content := "# Existing\n\nvalue: old\n"
	store[behavior.Path] = content
	return content, true
}

func classifyRecallWriteOutcome(behavior mcpcontract.RecallWriteBehaviorCase, result toolcore.Result, err error) mcpcontract.RecallWriteOutcome {
	if err != nil {
		return mcpcontract.RecallWriteError
	}
	if dryRun, _ := result["dry_run"].(bool); dryRun {
		return mcpcontract.RecallWritePreview
	}
	if behavior.Action == "diff" {
		return mcpcontract.RecallWriteReadOnly
	}
	return mcpcontract.RecallWriteMutation
}

func assertRecallWriteBehaviorState(t *testing.T, store map[string]string, behavior mcpcontract.RecallWriteBehaviorCase, result toolcore.Result, before string, existedBefore bool) {
	t.Helper()
	if behavior.Target == "card" {
		if behavior.Expected == mcpcontract.RecallWriteMutation && len(store) == 0 {
			t.Fatalf("card mutation did not persist any Recall entry: %#v", result)
		}
		if behavior.Expected != mcpcontract.RecallWriteMutation && len(store) != 0 {
			t.Fatalf("non-mutating card behavior persisted Recall entries: %#v", store)
		}
		return
	}
	if behavior.Path == "" {
		return
	}
	after, existsAfter := store[behavior.Path]
	if behavior.Expected == mcpcontract.RecallWriteMutation {
		if behavior.Action == "delete" {
			if existsAfter {
				t.Fatalf("delete mutation left %q", behavior.Path)
			}
			return
		}
		if !existsAfter {
			t.Fatalf("mutation did not persist %q", behavior.Path)
		}
		return
	}
	if existedBefore {
		if !existsAfter {
			t.Fatalf("non-mutating behavior removed %q", behavior.Path)
		}
		if after != before {
			t.Fatalf("non-mutating behavior changed %q: got %q want %q", behavior.Path, after, before)
		}
		return
	}
	if existsAfter {
		t.Fatalf("non-mutating behavior created %q", behavior.Path)
	}
}

func newRecallWriteBehaviorService(t *testing.T) (*Service, map[string]string, func()) {
	t.Helper()
	store := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-device-token" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/recall/"):
			path := strings.TrimPrefix(r.URL.Path, "/v1/recall/")
			content, ok := store[path]
			if !ok {
				writeRecallBehaviorJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{"message": "not found"}})
				return
			}
			writeRecallBehaviorJSON(w, http.StatusOK, map[string]any{"ok": true, "recall": memoryTestDocument(path, content)})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/recall/search":
			writeRecallBehaviorJSON(w, http.StatusOK, map[string]any{"ok": true, "results": []any{}, "count": 0})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/recall/preview":
			payload := decodeRecallBehaviorPayload(t, r)
			path, _ := payload["path"].(string)
			content, _ := payload["content"].(string)
			overwrite, _ := payload["overwrite"].(bool)
			if _, exists := store[path]; exists && !overwrite {
				writeRecallBehaviorJSON(w, http.StatusConflict, map[string]any{"error": map[string]any{"message": "recall file exists; set overwrite=true to replace"}})
				return
			}
			writeRecallBehaviorJSON(w, http.StatusOK, map[string]any{
				"ok": true, "path": path, "proposed_content": content, "overwrite": overwrite,
				"dry_run": true, "confirmed": boolValue(payload["confirmed"]),
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/recall":
			payload := decodeRecallBehaviorPayload(t, r)
			path, _ := payload["path"].(string)
			content, _ := payload["content"].(string)
			confirmed, overwrite := boolValue(payload["confirmed"]), boolValue(payload["overwrite"])
			if !strings.HasPrefix(path, "recall/docs/inbox/") && !confirmed {
				writeRecallBehaviorJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "writing outside inbox requires confirmed=true"}})
				return
			}
			if _, exists := store[path]; exists && !overwrite {
				writeRecallBehaviorJSON(w, http.StatusConflict, map[string]any{"error": map[string]any{"message": "recall file exists; set overwrite=true to replace"}})
				return
			}
			store[path] = content
			writeRecallBehaviorJSON(w, http.StatusOK, map[string]any{"ok": true, "recall": memoryTestDocument(path, content)})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v1/recall/"):
			path := strings.TrimPrefix(r.URL.Path, "/v1/recall/")
			if r.URL.Query().Get("confirmed") != "true" {
				writeRecallBehaviorJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "confirmation required"}})
				return
			}
			delete(store, path)
			writeRecallBehaviorJSON(w, http.StatusOK, map[string]any{"ok": true, "path": path})
		default:
			http.NotFound(w, r)
		}
	}))
	root := t.TempDir()
	cfg := config.Config{
		AgentDockDefaultDir: root,
		AgentDockHome:       filepath.Join(root, ".agentdock"),
		NexusEndpoint:       server.URL,
		NexusDeviceToken:    "test-device-token",
	}
	if err := cfg.Normalize(); err != nil {
		server.Close()
		t.Fatal(err)
	}
	return New(func() config.Config { return cfg }), store, server.Close
}

func decodeRecallBehaviorPayload(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func writeRecallBehaviorJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}

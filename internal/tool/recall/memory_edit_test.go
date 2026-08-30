package recall

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
)

func newMemoryTestService(t *testing.T, store map[string]string) (*Service, func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-device-token" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/recall/"):
			p := strings.TrimPrefix(r.URL.Path, "/v1/recall/")
			content, ok := store[p]
			if !ok {
				http.Error(w, `{"error":{"message":"not found"}}`, http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "recall": memoryTestDocument(p, content)})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/recall/context-index":
			items := []any{}
			for p, content := range store {
				kind := "project"
				if p == "profile.md" {
					kind = "profile"
				}
				items = append(items, map[string]any{"kind": kind, "path": p, "title": filepath.Base(p), "summary": memoryTestBody(content)})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":            true,
				"context_index": map[string]any{"project": "agentdock", "items": items, "max_bytes": 3000, "truncated": false},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/recall/search":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			query := strings.ToLower(fmt.Sprint(payload["query"]))
			prefix, _ := payload["prefix"].(string)
			prefix = strings.TrimSpace(prefix)
			excludePrefix, _ := payload["exclude_prefix"].(string)
			excludePrefix = strings.TrimSpace(excludePrefix)
			terms := strings.Fields(query)
			results := []map[string]any{}
			for p, content := range store {
				if prefix != "" && !strings.HasPrefix(p, prefix) {
					continue
				}
				if excludePrefix != "" && (p == excludePrefix || strings.HasPrefix(p, excludePrefix+"/")) {
					continue
				}
				haystack := strings.ToLower(p + "\n" + content)
				matched := query == "" || strings.Contains(haystack, query)
				if !matched {
					for _, term := range terms {
						if strings.Contains(haystack, term) {
							matched = true
							break
						}
					}
				}
				if matched {
					results = append(results, map[string]any{"path": p, "score": 1, "snippet": memoryTestBody(content)})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "query": query, "results": results, "count": len(results), "requested_max_results": payload["max_results"]})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/recall":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			p, _ := payload["path"].(string)
			content, _ := payload["content"].(string)
			store[p] = content
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "recall": memoryTestDocument(p, content)})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/recall":
			entries := []map[string]any{{"path": "devices", "type": "directory"}}
			for p := range store {
				entries = append(entries, map[string]any{"path": p, "type": "file"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "entries": entries, "count": len(entries)})
		default:
			http.NotFound(w, r)
		}
	}))
	cfg := config.Config{
		AgentDockDefaultDir: t.TempDir(), AgentDockHome: filepath.Join(t.TempDir(), ".agentdock"),
		NexusEndpoint: server.URL, NexusDeviceToken: "test-device-token",
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	return New(func() config.Config { return cfg }), server.Close
}

func memoryTestDocument(p, content string) map[string]any {
	return map[string]any{
		"path":        p,
		"content":     content,
		"body":        memoryTestBody(content),
		"frontmatter": map[string]any{"type": "test"},
		"size_bytes":  len(content),
	}
}

func memoryTestBody(content string) string {
	separator := "\n---\n\n"
	if strings.HasPrefix(content, "---\n") {
		if index := strings.Index(content, separator); index >= 0 {
			return content[index+len(separator):]
		}
	}
	return content
}

func TestMemoryReadCompactsRawMarkdownByDefault(t *testing.T) {
	full := "---\ntype: test\n---\n\n# Test\n正文\n"
	store := map[string]string{"devices/test.md": full}
	rt, closeServer := newMemoryTestService(t, store)
	defer closeServer()

	res, err := rt.memoryRead(context.Background(), map[string]any{"path": "devices/test.md"})
	if err != nil {
		t.Fatal(err)
	}
	recallDoc := res["recall"].(map[string]any)
	if _, ok := recallDoc["content"]; ok {
		t.Fatalf("content should be hidden by default: %#v", recallDoc)
	}
	if _, ok := recallDoc["raw_content"]; ok {
		t.Fatalf("raw_content should be hidden by default: %#v", recallDoc)
	}
	if body, _ := recallDoc["body"].(string); body != "# Test\n正文\n" {
		t.Fatalf("unexpected body: %#v", recallDoc)
	}

	res, err = rt.memoryRead(context.Background(), map[string]any{"path": "devices/test.md", "include_content": true})
	if err != nil {
		t.Fatal(err)
	}
	recallDoc = res["recall"].(map[string]any)
	if _, ok := recallDoc["raw_content"]; ok {
		t.Fatalf("undocumented include_content should not expose raw_content: %#v", recallDoc)
	}

	res, err = rt.memoryRead(context.Background(), map[string]any{"path": "devices/test.md", "include_raw": true})
	if err != nil {
		t.Fatal(err)
	}
	recallDoc = res["recall"].(map[string]any)
	if raw, _ := recallDoc["raw_content"].(string); raw != full {
		t.Fatalf("raw_content should contain full Markdown: %#v", recallDoc)
	}
	if _, ok := recallDoc["content"]; ok {
		t.Fatalf("include_raw should expose raw_content, not content: %#v", recallDoc)
	}
}

func TestRecallContextIndexUsesContextIndexEndpoint(t *testing.T) {
	store := map[string]string{"profile.md": "# Profile\n\nCompact Nexus context.\n"}
	rt, closeServer := newMemoryTestService(t, store)
	defer closeServer()

	res, err := rt.ContextIndex(context.Background(), 3000)
	if err != nil {
		t.Fatal(err)
	}
	index := res["context_index"].(map[string]any)
	items := index["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("context index should return one compact item: %#v", res)
	}
	item := items[0].(map[string]any)
	if item["path"] != "profile.md" || item["kind"] != "profile" {
		t.Fatalf("unexpected context item: %#v", item)
	}
}

func TestMemoryDiffAndPatchDryRun(t *testing.T) {
	store := map[string]string{"devices/test.md": "# Test\nkey：old\n"}
	rt, closeServer := newMemoryTestService(t, store)
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
	rt, closeServer := newMemoryTestService(t, store)
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
		"devices/test.md":                        "---\ntype: test\n---\n\n# Device\nplugin_dir：old\n",
		"ops/test.md":                            "# Ops\nNo forbidden terms.\n",
		"private-notes/encrypted/example.md.age": "not utf-8 recall content",
	}
	rt, closeServer := newMemoryTestService(t, store)
	defer closeServer()
	res, err := rt.memoryUpdateFact(context.Background(), map[string]any{"path": "devices/test.md", "key": "plugin_dir", "value": "plugins", "confirmed": true})
	if err != nil {
		t.Fatal(err)
	}
	if changed, _ := res["changed"].(bool); !changed {
		t.Fatalf("expected fact update change: %#v", res)
	}
	if !strings.Contains(store["devices/test.md"], "---\ntype: test\n---") {
		t.Fatalf("frontmatter was not preserved: %q", store["devices/test.md"])
	}
	if !strings.Contains(store["devices/test.md"], "plugin_dir：plugins") {
		t.Fatalf("fact was not written: %q", store["devices/test.md"])
	}
	res, err = rt.memoryUpdateFact(context.Background(), map[string]any{"path": "devices/test.md", "key": "missing_fact", "value": "created", "append_if_missing": true, "confirmed": true})
	if err != nil {
		t.Fatal(err)
	}
	if changed, _ := res["changed"].(bool); !changed {
		t.Fatalf("expected missing fact append to change content: %#v", res)
	}
	if !strings.Contains(store["devices/test.md"], "missing_fact：created") {
		t.Fatalf("missing fact was not appended: %q", store["devices/test.md"])
	}
	res, err = rt.memoryLint(context.Background(), map[string]any{"terms": []any{"plugin_dir"}, "max_entries": 10})
	if err != nil {
		t.Fatal(err)
	}
	if count, _ := res["finding_count"].(int); count == 0 {
		t.Fatalf("expected lint finding: %#v", res)
	}
	if scanned, _ := res["files_scanned"].(int); scanned != 2 {
		t.Fatalf("recall lint should scan only markdown/text files, got %d: %#v", scanned, res)
	}
	for _, item := range res["findings"].([]memoryLintFinding) {
		if item.Term == "READ_ERROR" {
			t.Fatalf("recall lint should skip directory entries, got: %#v", res)
		}
	}
}

func TestRecallSearchDefaultsToEightResults(t *testing.T) {
	store := map[string]string{"profile.md": "# Profile\nsearchable memory\n"}
	rt, closeServer := newMemoryTestService(t, store)
	defer closeServer()

	res, err := rt.Search(context.Background(), map[string]any{"query": "searchable"})
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(res["requested_max_results"]); got != "8" {
		t.Fatalf("default max_results = %s, want 8: %#v", got, res)
	}

	res, err = rt.Search(context.Background(), map[string]any{"query": "searchable", "max_results": 3})
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(res["requested_max_results"]); got != "3" {
		t.Fatalf("explicit max_results = %s, want 3: %#v", got, res)
	}
}

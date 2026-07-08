package tools

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

func newMemoryTestRuntime(t *testing.T, store map[string]string) (*Runtime, func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		case r.Method == http.MethodPost && r.URL.Path == "/v1/recall/pack":
			sections := []any{}
			for p, content := range store {
				sections = append(sections, memoryTestDocument(p, content))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "project": "agentdock", "sections": sections, "count": len(sections)})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/recall/search":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			query := strings.ToLower(fmt.Sprint(payload["query"]))
			prefix := strings.TrimSpace(fmt.Sprint(payload["prefix"]))
			terms := strings.Fields(query)
			results := []map[string]any{}
			for p, content := range store {
				if prefix != "" && !strings.HasPrefix(p, prefix) {
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
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "query": query, "results": results, "count": len(results)})
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
	cfg := config.Config{AgentDockDefaultDir: t.TempDir(), AgentDockHome: filepath.Join(t.TempDir(), ".agentdock"), RecallEndpoint: server.URL, RecallTimeoutMS: 30000}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return rt, server.Close
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
	rt, closeServer := newMemoryTestRuntime(t, store)
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

func TestRecallBootstrapCompactsSectionRawMarkdown(t *testing.T) {
	full := "---\ntype: test\n---\n\n# Packed\n"
	store := map[string]string{"projects/agentdock/project.md": full}
	rt, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()

	res, err := rt.memoryBootstrap(context.Background(), map[string]any{"project": "agentdock"})
	if err != nil {
		t.Fatal(err)
	}
	sections := res["sections"].([]any)
	section := sections[0].(map[string]any)
	if _, ok := section["content"]; ok {
		t.Fatalf("section content should be hidden by default: %#v", section)
	}
	if _, ok := section["raw_content"]; ok {
		t.Fatalf("section raw_content should be hidden by default: %#v", section)
	}

	res, err = rt.memoryBootstrap(context.Background(), map[string]any{"project": "agentdock", "include_raw": true})
	if err != nil {
		t.Fatal(err)
	}
	sections = res["sections"].([]any)
	section = sections[0].(map[string]any)
	if raw, _ := section["raw_content"].(string); raw != full {
		t.Fatalf("section raw_content should contain full Markdown: %#v", section)
	}
	if _, ok := section["content"]; ok {
		t.Fatalf("include_raw should expose raw_content, not content: %#v", section)
	}
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
		"devices/test.md": "---\ntype: test\n---\n\n# Device\nplugin_dir：old\n",
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
	for _, item := range res["findings"].([]memoryLintFinding) {
		if item.Term == "READ_ERROR" {
			t.Fatalf("recall lint should skip directory entries, got: %#v", res)
		}
	}
}

func TestMemoryBootstrapCompactByDefault(t *testing.T) {
	store := map[string]string{"projects/agentdock/project.md": "# Bootstrap\n正文正文正文正文正文\n"}
	rt, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()
	res, err := rt.memoryBootstrap(context.Background(), map[string]any{"project": "agentdock", "max_bytes": 12000})
	if err != nil {
		t.Fatal(err)
	}
	section := res["sections"].([]any)[0].(map[string]any)
	if _, ok := section["body"]; ok {
		t.Fatalf("bootstrap should not include body unless include_body=true, even when max_bytes is explicit: %#v", section)
	}
	if _, ok := section["body_excerpt"]; !ok {
		t.Fatalf("default bootstrap should include excerpt: %#v", section)
	}

	res, err = rt.memoryBootstrap(context.Background(), map[string]any{"project": "agentdock", "include_body": true})
	if err != nil {
		t.Fatal(err)
	}
	section = res["sections"].([]any)[0].(map[string]any)
	if body, _ := section["body"].(string); body == "" {
		t.Fatalf("include_body should expose section body: %#v", section)
	}
}

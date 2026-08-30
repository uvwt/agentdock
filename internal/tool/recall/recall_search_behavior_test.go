package recall

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

func TestRecallSearchKeepsNativeFieldsAndAddsSourceIdentity(t *testing.T) {
	const path = "recall/docs/example.md"
	service, closeServer := newMemoryTestService(t, map[string]string{
		path: "# Example\n\nAgentDock citation source\n",
	})
	defer closeServer()

	got, err := service.searchTest(context.Background(), map[string]any{"query": "AgentDock", "kind": "markdown"})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"count", "query", "recall_endpoint", "recall_kind", "recall_store", "results"} {
		if _, ok := got[field]; !ok {
			t.Fatalf("recall_search missing top-level field %q: %#v", field, got)
		}
	}

	items, ok := got["results"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("unexpected recall_search results: %#v", got["results"])
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected recall_search item: %#v", items[0])
	}
	if item["path"] != path || item["id"] != path {
		t.Fatalf("path/id = %#v/%#v, want %q", item["path"], item["id"], path)
	}
	if item["title"] != "example" {
		t.Fatalf("title = %#v, want fallback title example", item["title"])
	}
	if item["score"] != float64(1) || !strings.Contains(item["snippet"].(string), "AgentDock citation source") {
		t.Fatalf("native search fields were not preserved: %#v", item)
	}

	sourceURL, err := url.Parse(item["url"].(string))
	if err != nil || !sourceURL.IsAbs() {
		t.Fatalf("source url = %#v, err=%v", item["url"], err)
	}
	if sourceURL.Query().Get("path") != path || sourceURL.Fragment != "recall/library" {
		t.Fatalf("source url = %q", sourceURL.String())
	}
}

func TestRecallSearchKindFiltersCardsFromMarkdown(t *testing.T) {
	store := map[string]string{
		"profile.md": "# Profile\n\nshared recall term\n",
		"recall/managed/cards/agentdock/inbox/runbook/shared.md": "# Shared Card\n\nshared recall term\n",
	}
	service, closeServer := newMemoryTestService(t, store)
	defer closeServer()

	tests := []struct {
		kind      string
		wantPaths map[string]bool
	}{
		{kind: "all", wantPaths: map[string]bool{"profile.md": true, "recall/managed/cards/agentdock/inbox/runbook/shared.md": true}},
		{kind: "markdown", wantPaths: map[string]bool{"profile.md": true}},
		{kind: "card", wantPaths: map[string]bool{"recall/managed/cards/agentdock/inbox/runbook/shared.md": true}},
	}

	for _, test := range tests {
		t.Run(test.kind, func(t *testing.T) {
			result, err := service.searchTest(context.Background(), map[string]any{
				"query": "shared recall term",
				"kind":  test.kind,
			})
			if err != nil {
				t.Fatal(err)
			}
			items, ok := result["results"].([]any)
			if !ok {
				t.Fatalf("unexpected results: %#v", result["results"])
			}
			gotPaths := make(map[string]bool, len(items))
			for _, raw := range items {
				item, ok := raw.(map[string]any)
				if !ok {
					t.Fatalf("unexpected result item: %#v", raw)
				}
				path, _ := item["path"].(string)
				gotPaths[path] = true
			}
			if len(gotPaths) != len(test.wantPaths) {
				t.Fatalf("kind=%s paths=%#v want=%#v", test.kind, gotPaths, test.wantPaths)
			}
			for path := range test.wantPaths {
				if !gotPaths[path] {
					t.Fatalf("kind=%s missing path %q in %#v", test.kind, path, gotPaths)
				}
			}
		})
	}
}

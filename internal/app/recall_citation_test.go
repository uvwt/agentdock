package app

import (
	"net/url"
	"strings"
	"testing"
)

func TestRecallSearchKeepsNativeFieldsAndAddsSourceIdentity(t *testing.T) {
	const path = "recall/docs/example.md"
	rt, closeServer := newMemoryTestRuntime(t, map[string]string{
		path: "# Example\n\nAgentDock citation source\n",
	})
	defer closeServer()

	got, err := rt.Call(t.Context(), "recall_search", map[string]any{"query": "AgentDock", "kind": "markdown"})
	if err != nil {
		t.Fatal(err)
	}
	assertToolResultMatchesOutputSchema(t, "recall_search", got)
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

func TestRecallSearchOutputSchemaDeclaresCitationIdentityWithoutDroppingNativeFields(t *testing.T) {
	schema := OutputSchema("recall_search")
	props := schema["properties"].(map[string]any)
	for _, field := range []string{"count", "query", "recall_endpoint", "recall_kind", "recall_store", "results"} {
		if _, ok := props[field]; !ok {
			t.Fatalf("recall_search output schema missing %q", field)
		}
	}

	results := props["results"].(map[string]any)
	item := results["items"].(map[string]any)
	itemProps := item["properties"].(map[string]any)
	for _, field := range []string{"frontmatter", "matched_fields", "matched_terms", "path", "snippet", "id", "title", "url"} {
		if _, ok := itemProps[field]; !ok {
			t.Fatalf("recall_search result schema missing %q", field)
		}
	}
	required := item["required"].([]string)
	if strings.Join(required, ",") != "id,title,url" {
		t.Fatalf("recall_search result required = %#v", required)
	}
	if additional, _ := item["additionalProperties"].(bool); !additional {
		t.Fatalf("recall_search result schema must keep native backend fields: %#v", item)
	}
}

package app

import (
	"strings"
	"testing"
)

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

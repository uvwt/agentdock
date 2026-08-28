package app

import (
	"context"
	"testing"
)

func TestRecallSearchKindFiltersCardsFromMarkdown(t *testing.T) {
	store := map[string]string{
		"profile.md": "# Profile\n\nshared recall term\n",
		"recall/managed/cards/agentdock/inbox/runbook/shared.md": "# Shared Card\n\nshared recall term\n",
	}
	rt, closeServer := newMemoryTestRuntime(t, store)
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
			result, err := rt.Call(context.Background(), "recall_search", map[string]any{
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

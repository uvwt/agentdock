package acp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHistoryCollectorTruncatesOversizedUpdateAsValidJSON(t *testing.T) {
	collector := &historyCollector{}
	update, err := json.Marshal(map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"content": map[string]any{
			"type": "text",
			"text": strings.Repeat("x", maxHistoryUpdateBytes+1024),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	collector.append("agent_message_chunk", update)
	replay := collector.snapshot()
	if len(replay.Events) != 1 {
		t.Fatalf("history events = %d, want 1", len(replay.Events))
	}
	event := replay.Events[0]
	if event.Source != "acp" || !event.UpdateTruncated || event.OriginalUpdateBytes != len(update) || !replay.Truncated {
		t.Fatalf("truncated history event = %#v replay=%#v", event, replay)
	}
	if !json.Valid(event.Update) {
		t.Fatalf("truncated history update is invalid JSON: %q", event.Update)
	}
}

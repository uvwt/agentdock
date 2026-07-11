package tools

import "testing"

func TestInputSchemaPublishesRuntimeBounds(t *testing.T) {
	tests := []struct {
		tool, property   string
		minimum, maximum int
	}{
		{tool: "read_file", property: "max_bytes", minimum: 1, maximum: maxTextOutputBytes},
		{tool: "list_dir", property: "max_depth", minimum: 1, maximum: 20},
		{tool: "list_dir", property: "max_entries", minimum: 1, maximum: 2000},
		{tool: "list_files", property: "max_results", minimum: 1, maximum: 5000},
		{tool: "search_text", property: "context_lines", minimum: 0, maximum: 20},
		{tool: "search_text", property: "max_results", minimum: 1, maximum: 1000},
		{tool: "exec_command", property: "timeout_ms", minimum: 1, maximum: 86400000},
		{tool: "exec_command", property: "yield_time_ms", minimum: 0, maximum: 30000},
		{tool: "exec_command", property: "max_output_bytes", minimum: 1, maximum: maxCommandOutputBytes},
		{tool: "session_observe", property: "max_output_bytes", minimum: 1, maximum: maxCommandOutputBytes},
		{tool: "session_act", property: "max_output_bytes", minimum: 1, maximum: maxCommandOutputBytes},
		{tool: "browser_session", property: "timeout_ms", minimum: 1, maximum: 300000},
		{tool: "browser_act", property: "timeout_ms", minimum: 1, maximum: 300000},
		{tool: "browser_snapshot", property: "timeout_ms", minimum: 1, maximum: 300000},
		{tool: "git_read", property: "timeout_ms", minimum: 1, maximum: 120000},
		{tool: "private_note_manage", property: "max_results", minimum: 1, maximum: maxPrivateNoteSearchResults},
		{tool: "private_note_manage", property: "max_bytes", minimum: 1, maximum: maxPrivateNoteReadBytes},
	}
	for _, test := range tests {
		t.Run(test.tool+"/"+test.property, func(t *testing.T) {
			schema := InputSchema(test.tool)
			properties, ok := schema["properties"].(map[string]any)
			if !ok {
				t.Fatalf("properties type = %T", schema["properties"])
			}
			property, ok := properties[test.property].(map[string]any)
			if !ok {
				t.Fatalf("property %s = %#v", test.property, properties[test.property])
			}
			if property["minimum"] != test.minimum || property["maximum"] != test.maximum {
				t.Fatalf("bounds = min:%#v max:%#v, want %d..%d", property["minimum"], property["maximum"], test.minimum, test.maximum)
			}
		})
	}
}

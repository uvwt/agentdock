package app

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

func TestExecCommandInputSchemaPublishesExecutionModes(t *testing.T) {
	schema := InputSchema("exec_command")
	properties := schema["properties"].(map[string]any)
	mode, ok := properties["execution_mode"].(map[string]any)
	if !ok {
		t.Fatalf("execution_mode schema = %#v", properties["execution_mode"])
	}
	values, ok := mode["enum"].([]string)
	if !ok || len(values) != 3 || values[0] != "auto" || values[1] != "sync" || values[2] != "async" {
		t.Fatalf("execution_mode enum = %#v", mode["enum"])
	}
	if _, exists := properties["wait_until_exit"]; exists {
		t.Fatal("exec_command schema still exposes wait_until_exit")
	}
	if schema["additionalProperties"] != false {
		t.Fatalf("exec_command additionalProperties = %#v, want false", schema["additionalProperties"])
	}
}

func TestExecCommandOutputSchemaPublishesSessionGuidance(t *testing.T) {
	properties := OutputSchema("exec_command")["properties"].(map[string]any)
	for _, property := range []string{"session_reason", "observe_after_ms"} {
		if _, exists := properties[property]; !exists {
			t.Fatalf("exec_command output schema missing %s", property)
		}
	}
	sessionProperties := OutputSchema("session_observe")["properties"].(map[string]any)
	if _, exists := sessionProperties["session_reason"]; exists {
		t.Fatal("session_observe output schema unexpectedly exposes exec_command session guidance")
	}
}

func TestBrowserInputSchemaPublishesPageWaitAndStorageStateFields(t *testing.T) {
	for _, test := range []struct {
		tool       string
		properties []string
	}{
		{tool: "browser_session", properties: []string{"storage_state_path"}},
		{tool: "browser_act", properties: []string{"page_id", "storage_state_path"}},
		{tool: "browser_snapshot", properties: []string{"page_id", "storage_state_path"}},
	} {
		t.Run(test.tool, func(t *testing.T) {
			properties := InputSchema(test.tool)["properties"].(map[string]any)
			for _, property := range test.properties {
				if _, ok := properties[property]; !ok {
					t.Fatalf("%s schema missing property %s", test.tool, property)
				}
			}
		})
	}

	actions := InputSchema("browser_act")["properties"].(map[string]any)["actions"].(map[string]any)
	items := actions["items"].(map[string]any)
	properties := items["properties"].(map[string]any)
	action := properties["action"].(map[string]any)
	values := action["enum"].([]string)
	for _, expected := range []string{"wait_for_url", "wait_for_text", "wait_for_response"} {
		found := false
		for _, value := range values {
			if value == expected {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("browser action enum missing %s: %#v", expected, values)
		}
	}
}

func TestViewImageInputSchemaDeclaresObjectTypeForEveryOneOfBranch(t *testing.T) {
	schema := InputSchema("view_image")
	if schema["type"] != "object" {
		t.Fatalf("root type = %#v, want object", schema["type"])
	}

	branches, ok := schema["oneOf"].([]map[string]any)
	if !ok {
		t.Fatalf("oneOf type = %T", schema["oneOf"])
	}
	if len(branches) != 3 {
		t.Fatalf("oneOf branches = %d, want 3", len(branches))
	}

	for index, branch := range branches {
		if branch["type"] != "object" {
			t.Fatalf("oneOf[%d] type = %#v, want object", index, branch["type"])
		}
		required, ok := branch["required"].([]string)
		if !ok || len(required) != 1 || required[0] == "" {
			t.Fatalf("oneOf[%d] required = %#v", index, branch["required"])
		}
	}
}

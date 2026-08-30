package app

import (
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	toolcommand "github.com/uvwt/agentdock/internal/tool/command"
	toolfile "github.com/uvwt/agentdock/internal/tool/file"
	toolrecall "github.com/uvwt/agentdock/internal/tool/recall"
)

func TestTaskManageSchemaHidesNexusExtensionsWithoutNexus(t *testing.T) {
	inputProps := testInputSchemaForConfig("task_manage", config.Config{})["properties"].(map[string]any)
	for _, hidden := range []string{"template_id", "source_template_ids", "learning_checks"} {
		if _, ok := inputProps[hidden]; ok {
			t.Fatalf("task_manage input schema should hide %s without Nexus", hidden)
		}
	}
	for _, field := range []string{"project", "device", "steps"} {
		description, _ := inputProps[field].(map[string]any)["description"].(string)
		if description == "" || containsNexusExtensionTerm(description) {
			t.Fatalf("task_manage %s description should stay neutral without Nexus: %q", field, description)
		}
	}

	outputProps := testOutputSchemaForConfig("task_manage", config.Config{})["properties"].(map[string]any)
	for _, hidden := range []string{"guidance_context", "review_revision", "evolution_candidates", "evolution_warning"} {
		if _, ok := outputProps[hidden]; ok {
			t.Fatalf("task_manage output schema should hide %s without Nexus", hidden)
		}
	}
	nextDescription, _ := outputProps["next_required_action"].(map[string]any)["description"].(string)
	if nextDescription == "" || containsNexusExtensionTerm(nextDescription) {
		t.Fatalf("task_manage next_required_action description should stay neutral without Nexus: %q", nextDescription)
	}
}

func TestTaskManageSchemaKeepsNexusExtensionsWithNexus(t *testing.T) {
	cfg := config.Config{NexusEndpoint: "http://127.0.0.1:18777"}
	inputProps := testInputSchemaForConfig("task_manage", cfg)["properties"].(map[string]any)
	for _, field := range []string{"template_id", "source_template_ids", "learning_checks"} {
		if _, ok := inputProps[field]; !ok {
			t.Fatalf("task_manage input schema should expose %s with Nexus", field)
		}
	}
	outputProps := testOutputSchemaForConfig("task_manage", cfg)["properties"].(map[string]any)
	for _, field := range []string{"guidance_context", "review_revision", "evolution_candidates", "evolution_warning"} {
		if _, ok := outputProps[field]; !ok {
			t.Fatalf("task_manage output schema should expose %s with Nexus", field)
		}
	}
}

func containsNexusExtensionTerm(value string) bool {
	for _, term := range []string{"Evolution", "evolution", "template"} {
		if strings.Contains(value, term) {
			return true
		}
	}
	return false
}

func TestInputSchemaPublishesRuntimeBounds(t *testing.T) {
	tests := []struct {
		tool, property   string
		minimum, maximum int
	}{
		{tool: "read_file", property: "max_bytes", minimum: 1, maximum: toolfile.MaxTextOutputBytes},
		{tool: "list_dir", property: "max_depth", minimum: 1, maximum: 20},
		{tool: "list_dir", property: "max_entries", minimum: 1, maximum: 5000},
		{tool: "search_text", property: "context_lines", minimum: 0, maximum: 20},
		{tool: "search_text", property: "max_results", minimum: 1, maximum: 1000},
		{tool: "exec_command", property: "timeout_ms", minimum: 1, maximum: 86400000},
		{tool: "exec_command", property: "yield_time_ms", minimum: 0, maximum: 30000},
		{tool: "exec_command", property: "max_output_bytes", minimum: 1, maximum: toolcommand.MaxOutputBytes},
		{tool: "session_observe", property: "max_output_bytes", minimum: 1, maximum: toolcommand.MaxOutputBytes},
		{tool: "session_act", property: "max_output_bytes", minimum: 1, maximum: toolcommand.MaxOutputBytes},
		{tool: "task_manage", property: "limit", minimum: 1, maximum: 200},
		{tool: "view_image", property: "max_source_bytes", minimum: 1, maximum: 100 * 1024 * 1024},
		{tool: "view_image", property: "max_bytes", minimum: 1, maximum: 2 * 1024 * 1024},
		{tool: "view_image", property: "quality", minimum: 35, maximum: 95},
		{tool: "file_publish", property: "retention_seconds", minimum: 0, maximum: 7 * 24 * 60 * 60},
		{tool: "browser_session", property: "timeout_ms", minimum: 1, maximum: 300000},
		{tool: "browser_act", property: "timeout_ms", minimum: 1, maximum: 300000},
		{tool: "browser_snapshot", property: "timeout_ms", minimum: 1, maximum: 300000},
		{tool: "private_note_manage", property: "max_results", minimum: 1, maximum: toolrecall.MaxPrivateNoteSearchResults},
		{tool: "private_note_manage", property: "max_bytes", minimum: 1, maximum: toolrecall.MaxPrivateNoteReadBytes},
	}
	for _, test := range tests {
		t.Run(test.tool+"/"+test.property, func(t *testing.T) {
			schema := testInputSchema(test.tool)
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
	schema := testInputSchema("exec_command")
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
	properties := testOutputSchema("exec_command")["properties"].(map[string]any)
	for _, property := range []string{"session_reason", "observe_after_ms"} {
		if _, exists := properties[property]; !exists {
			t.Fatalf("exec_command output schema missing %s", property)
		}
	}
	sessionProperties := testOutputSchema("session_observe")["properties"].(map[string]any)
	if _, exists := sessionProperties["session_reason"]; exists {
		t.Fatal("session_observe output schema unexpectedly exposes exec_command session guidance")
	}
}

func TestBrowserSchemasUseNativeCDPContract(t *testing.T) {
	for _, tool := range []string{"browser_session", "browser_act", "browser_snapshot"} {
		schema := testInputSchema(tool)
		if schema["additionalProperties"] != false {
			t.Fatalf("%s additionalProperties = %#v, want false", tool, schema["additionalProperties"])
		}
		properties := schema["properties"].(map[string]any)
		for _, forbidden := range []string{"backend", "channel", "storage_state", "storage_state_path", "save_storage_state"} {
			if _, exists := properties[forbidden]; exists {
				t.Fatalf("%s input schema still exposes %s", tool, forbidden)
			}
		}
		output := testOutputSchema(tool)["properties"].(map[string]any)
		for _, forbidden := range []string{"backend", "cdp_url", "browser_ownership", "browser_launch", "suggested_retry", "stdout", "stderr", "storage_state_path"} {
			if _, exists := output[forbidden]; exists {
				t.Fatalf("%s output schema still exposes %s", tool, forbidden)
			}
		}
	}
	sessionProperties := testInputSchema("browser_session")["properties"].(map[string]any)
	if _, exists := sessionProperties["cdp_url"]; !exists {
		t.Fatal("browser_session input schema missing cdp_url")
	}
	if _, exists := sessionProperties["reuse_existing_cdp"]; exists {
		t.Fatal("browser_session must not let a tool call enable automatic CDP reuse; it is a user configuration")
	}
	for _, tool := range []string{"browser_act", "browser_snapshot"} {
		properties := testInputSchema(tool)["properties"].(map[string]any)
		if _, exists := properties["cdp_url"]; exists {
			t.Fatalf("%s must not expose cdp_url; CDP attachment belongs to browser_session", tool)
		}
	}
}

func TestBrowserSessionSchemaPublishesSupportedBrowsers(t *testing.T) {
	properties := testInputSchema("browser_session")["properties"].(map[string]any)
	browser := properties["browser"].(map[string]any)
	values, ok := browser["enum"].([]string)
	want := []string{"auto", "chrome", "chromium", "edge"}
	if !ok || len(values) != len(want) {
		t.Fatalf("browser enum = %#v", browser["enum"])
	}
	for index := range want {
		if values[index] != want[index] {
			t.Fatalf("browser enum = %#v, want %#v", values, want)
		}
	}
}

func TestBrowserActionSchemaIsStrictPerAction(t *testing.T) {
	actions := testInputSchema("browser_act")["properties"].(map[string]any)["actions"].(map[string]any)
	items := actions["items"].(map[string]any)
	branches, ok := items["oneOf"].([]map[string]any)
	if !ok || len(branches) != 14 {
		t.Fatalf("browser action oneOf = %#v", items["oneOf"])
	}
	seen := map[string]map[string]any{}
	for _, branch := range branches {
		if branch["additionalProperties"] != false {
			t.Fatalf("browser action branch is not strict: %#v", branch)
		}
		properties := branch["properties"].(map[string]any)
		action := properties["action"].(map[string]any)["const"].(string)
		seen[action] = properties
	}
	for _, action := range []string{"wait_for_url", "wait_for_text", "wait_for_response", "reload", "back", "forward"} {
		if seen[action] == nil {
			t.Fatalf("browser action schema missing %s", action)
		}
	}
	if _, exists := seen["wait_for_text"]["value"]; exists {
		t.Fatal("wait_for_text still exposes legacy value fallback")
	}
	state := seen["wait_for_text"]["state"].(map[string]any)
	values := state["enum"].([]string)
	if len(values) != 4 || values[0] != "visible" || values[1] != "hidden" || values[2] != "attached" || values[3] != "detached" {
		t.Fatalf("wait_for_text state enum = %#v", values)
	}
}

func TestViewImageInputSchemaDeclaresObjectTypeForEveryOneOfBranch(t *testing.T) {
	schema := testInputSchema("view_image")
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

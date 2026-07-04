package mcp

import "testing"

func TestDefaultTaskSchemaHidesRecoveryFields(t *testing.T) {
	props := schemaProperties(t, "task_manage")
	for _, key := range []string{"step_evidence", "step_completions", "condition_evidence", "phase_checkpoint", "complete_step", "record_attempt", "template_save"} {
		if _, exists := props[key]; exists {
			t.Fatalf("default task_manage schema should hide %s", key)
		}
	}
	actions := enumStrings(t, props["action"])
	if containsString(actions, "phase_checkpoint") || containsString(actions, "complete_step") || containsString(actions, "template_save") {
		t.Fatalf("default task_manage actions should be simple only: %#v", actions)
	}
	for _, action := range []string{"create", "list", "get", "block", "resume", "final_review", "complete_after_review", "template_match"} {
		if !containsString(actions, action) {
			t.Fatalf("default task_manage schema missing action %s: %#v", action, actions)
		}
	}
}

func TestRecoveryTaskSchemaKeepsLegacyFields(t *testing.T) {
	props := schemaProperties(t, "task_manage_recovery")
	for _, key := range []string{"step_evidence", "step_completions", "condition_evidence", "template", "template_status"} {
		if _, exists := props[key]; !exists {
			t.Fatalf("recovery schema should keep %s", key)
		}
	}
	actions := enumStrings(t, props["action"])
	for _, action := range []string{"phase_checkpoint", "complete_step", "record_attempt", "template_save"} {
		if !containsString(actions, action) {
			t.Fatalf("recovery schema missing action %s: %#v", action, actions)
		}
	}
}

func schemaProperties(t *testing.T, name string) map[string]any {
	t.Helper()
	schema := inputSchema(name)
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties missing for %s: %#v", name, schema)
	}
	return props
}

func enumStrings(t *testing.T, value any) []string {
	t.Helper()
	prop, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("action property missing: %#v", value)
	}
	raw, ok := prop["enum"].([]string)
	if ok {
		return raw
	}
	rawAny, ok := prop["enum"].([]any)
	if !ok {
		t.Fatalf("action enum missing: %#v", prop)
	}
	out := make([]string, 0, len(rawAny))
	for _, item := range rawAny {
		out = append(out, item.(string))
	}
	return out
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

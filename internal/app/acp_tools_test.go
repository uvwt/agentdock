package app

import (
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
)

func TestACPToolsAreFeatureGatedAndUseStrictSchemas(t *testing.T) {
	disabled := config.Config{AgentDockHome: t.TempDir(), AgentDockDefaultDir: t.TempDir()}
	if err := disabled.Normalize(); err != nil {
		t.Fatal(err)
	}
	disabledRuntime, err := NewRuntime(disabled)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range disabledRuntime.ToolNames() {
		if name == "acp_session" || name == "acp_prompt" || name == "acp_interaction" {
			disabledRuntime.Close()
			t.Fatalf("ACP tool %s exposed while disabled", name)
		}
	}
	if err := disabledRuntime.Close(); err != nil {
		t.Fatal(err)
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	enabled := config.Config{
		AgentDockHome: t.TempDir(), AgentDockDefaultDir: root,
		ACPEnabled: true, ACPAgentName: "helper", ACPCommand: executable,
	}
	if err := enabled.Normalize(); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(enabled)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runtime.Close() }()

	for _, name := range []string{"acp_session", "acp_prompt", "acp_interaction"} {
		found := false
		for _, available := range runtime.ToolNames() {
			if available == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("ACP tool %s is not exposed", name)
		}
		schema := InputSchema(name)
		if schema["additionalProperties"] != false {
			t.Fatalf("%s additionalProperties = %#v", name, schema["additionalProperties"])
		}
	}

	sessionProperties := InputSchema("acp_session")["properties"].(map[string]any)
	actions := sessionProperties["action"].(map[string]any)["enum"].([]string)
	expectedActions := []string{"info", "authenticate", "new", "load", "resume", "fork", "set_mode", "set_config", "list", "inspect", "close", "delete"}
	if !reflect.DeepEqual(actions, expectedActions) {
		t.Fatalf("acp_session actions = %#v, want %#v", actions, expectedActions)
	}
	sessionOutputProperties := OutputSchema("acp_session")["properties"].(map[string]any)
	for _, property := range []string{"context_policy", "event_policy", "interaction_policy", "steering_policy"} {
		if _, exists := sessionOutputProperties[property]; !exists {
			t.Fatalf("acp_session output schema missing %s", property)
		}
	}

	promptProperties := OutputSchema("acp_prompt")["properties"].(map[string]any)
	for _, property := range []string{"next_seq", "first_seq", "latest_seq", "dropped_count", "has_more", "truncated"} {
		if _, exists := promptProperties[property]; !exists {
			t.Fatalf("acp_prompt output schema missing %s", property)
		}
	}

	result, err := runtime.Call(context.Background(), "acp_session", map[string]any{"action": "list"})
	if err != nil {
		t.Fatal(err)
	}
	if result["count"] != 0 {
		t.Fatalf("empty ACP session count = %#v", result["count"])
	}
	contextResult, err := runtime.Call(context.Background(), "agentdock_context", nil)
	if err != nil {
		t.Fatal(err)
	}
	var contextData capabilityContext
	if err := remarshal(contextResult, &contextData); err != nil {
		t.Fatal(err)
	}
	if contextData.ACP == nil || !contextData.ACP.Enabled || contextData.ACP.Agent != "helper" {
		t.Fatalf("context ACP metadata = %#v", contextData.ACP)
	}
}

func TestACPToolDefinitionsPublishConservativeAnnotations(t *testing.T) {
	definitions := ToolDefinitions()
	for _, name := range []string{"acp_session", "acp_prompt", "acp_interaction"} {
		var annotations *ToolAnnotations
		for _, definition := range definitions {
			if definition.Name == name {
				annotations = definition.Annotations
				break
			}
		}
		if annotations == nil || annotations.ReadOnlyHint || annotations.DestructiveHint == nil || !*annotations.DestructiveHint || annotations.OpenWorldHint == nil || !*annotations.OpenWorldHint {
			t.Fatalf("%s annotations = %#v", name, annotations)
		}
	}
}

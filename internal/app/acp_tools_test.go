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
		ACPEnabled:        true,
		ACPProfiles:       []config.ACPProfile{{ID: "helper", Kind: "custom", Command: executable, Enabled: true}},
		ACPDefaultProfile: "helper",
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
		schema := testInputSchema(name)
		if schema["additionalProperties"] != false {
			t.Fatalf("%s additionalProperties = %#v", name, schema["additionalProperties"])
		}
		properties := schema["properties"].(map[string]any)
		if _, exists := properties["profile_id"]; !exists {
			t.Fatalf("%s input schema missing profile_id", name)
		}
	}

	sessionProperties := testInputSchema("acp_session")["properties"].(map[string]any)
	actions := sessionProperties["action"].(map[string]any)["enum"].([]string)
	expectedActions := []string{"info", "new", "list", "inspect", "open", "update", "close", "delete"}
	if !reflect.DeepEqual(actions, expectedActions) {
		t.Fatalf("acp_session actions = %#v, want %#v", actions, expectedActions)
	}
	sessionOutputProperties := testOutputSchema("acp_session")["properties"].(map[string]any)
	for _, property := range []string{"profile_id", "context_policy", "event_policy", "interaction_policy", "steering_policy"} {
		if _, exists := sessionOutputProperties[property]; !exists {
			t.Fatalf("acp_session output schema missing %s", property)
		}
	}

	promptProperties := testOutputSchema("acp_prompt")["properties"].(map[string]any)
	for _, property := range []string{"profile_id", "next_seq", "first_seq", "latest_seq", "dropped_count", "has_more", "truncated"} {
		if _, exists := promptProperties[property]; !exists {
			t.Fatalf("acp_prompt output schema missing %s", property)
		}
	}
	interactionProperties := testOutputSchema("acp_interaction")["properties"].(map[string]any)
	if _, exists := interactionProperties["profile_id"]; !exists {
		t.Fatal("acp_interaction output schema missing profile_id")
	}

	contextResult, err := runtime.Call(context.Background(), "agentdock_context", nil)
	if err != nil {
		t.Fatal(err)
	}
	var contextData capabilityContext
	if err := remarshal(contextResult, &contextData); err != nil {
		t.Fatal(err)
	}
	if contextData.ACP == nil || !contextData.ACP.Enabled || contextData.ACP.DefaultProfile != "helper" {
		t.Fatalf("context ACP metadata = %#v", contextData.ACP)
	}
}

func TestACPContextListsConfiguredProfiles(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		AgentDockHome: t.TempDir(), AgentDockDefaultDir: t.TempDir(), ACPEnabled: true,
		ACPProfiles: []config.ACPProfile{
			{ID: "zcode", Kind: "custom", Command: executable, Enabled: true},
			{ID: "agy", Kind: "custom", Command: executable, Enabled: true},
		},
		ACPDefaultProfile: "zcode",
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runtime.Close() }()

	contextResult, err := runtime.Call(context.Background(), "agentdock_context", nil)
	if err != nil {
		t.Fatal(err)
	}
	var contextData capabilityContext
	if err := remarshal(contextResult, &contextData); err != nil {
		t.Fatal(err)
	}
	if contextData.ACP == nil || contextData.ACP.DefaultProfile != "zcode" {
		t.Fatalf("context ACP metadata = %#v", contextData.ACP)
	}
	if acpData, ok := contextResult["acp"].(map[string]any); ok {
		if _, exists := acpData["agent"]; exists {
			t.Fatalf("context ACP metadata still contains legacy agent alias: %#v", acpData)
		}
	}
	if len(contextData.ACP.Profiles) != 2 || contextData.ACP.Profiles[0].ID != "zcode" || contextData.ACP.Profiles[1].ID != "agy" {
		t.Fatalf("context ACP profiles = %#v", contextData.ACP.Profiles)
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

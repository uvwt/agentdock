package app

import (
	"maps"
	"reflect"
	"testing"

	"github.com/uvwt/agentdock-protocol/mcpcontract"
)

func TestCanonicalToolDefinitionsMatchSharedContract(t *testing.T) {
	definitions := make(map[string]ToolDefinition, len(mcpcontract.ToolNames()))
	for _, definition := range ToolDefinitions() {
		if mcpcontract.IsCanonicalTool(definition.Name) {
			definitions[definition.Name] = definition
		}
	}
	if len(definitions) != len(mcpcontract.ToolNames()) {
		t.Fatalf("canonical tool count=%d want=%d", len(definitions), len(mcpcontract.ToolNames()))
	}

	for _, name := range mcpcontract.ToolNames() {
		definition, ok := definitions[name]
		if !ok {
			t.Fatalf("canonical tool %s missing", name)
		}
		wantInput, _ := mcpcontract.InputSchema(name)
		actualInput, actualOutput := definition.InputSchema, definition.OutputSchema
		if name == mcpcontract.ToolAgentDockContext {
			// Standalone AgentDock adds only optional local context fields. Compare
			// every remaining field against the unchanged shared protocol contract.
			actualInput = withoutLocalContextProperty(t, actualInput, "workdir")
			actualOutput = withoutLocalContextProperty(t, actualOutput, "instruction_files")
			actualOutput = withoutLocalContextProperty(t, actualOutput, "plugins")
		}
		if !reflect.DeepEqual(actualInput, wantInput) {
			t.Fatalf("%s input schema drifted from shared contract", name)
		}
		var wantOutput map[string]any
		if name == mcpcontract.ToolAgentDockContext {
			wantOutput = mcpcontract.LocalAgentDockContextOutputSchema()
		} else {
			wantOutput, _ = mcpcontract.OutputSchema(name)
		}
		if !reflect.DeepEqual(actualOutput, wantOutput) {
			t.Fatalf("%s output schema drifted from shared contract", name)
		}

		wantAnnotations, _ := mcpcontract.AnnotationContract(name)
		annotations := definition.Annotations
		if annotations == nil ||
			annotations.ReadOnlyHint != wantAnnotations.ReadOnlyHint ||
			!reflect.DeepEqual(annotations.DestructiveHint, wantAnnotations.DestructiveHint) ||
			!reflect.DeepEqual(annotations.OpenWorldHint, wantAnnotations.OpenWorldHint) {
			t.Fatalf("%s annotations drifted: got=%#v want=%#v", name, annotations, wantAnnotations)
		}
		wantIdempotent := wantAnnotations.IdempotentHint != nil && *wantAnnotations.IdempotentHint
		if annotations.IdempotentHint != wantIdempotent {
			t.Fatalf("%s idempotentHint=%v want=%v", name, annotations.IdempotentHint, wantIdempotent)
		}
	}
}

func withoutLocalContextProperty(t *testing.T, schema map[string]any, name string) map[string]any {
	t.Helper()
	copy := maps.Clone(schema)
	properties := maps.Clone(schema["properties"].(map[string]any))
	if properties[name] == nil {
		t.Fatalf("local context extension %q missing", name)
	}
	requiredFields, _ := schema["required"].([]string)
	for _, required := range requiredFields {
		if required == name {
			t.Fatalf("local extension %q must remain optional", name)
		}
	}
	delete(properties, name)
	copy["properties"] = properties
	return copy
}

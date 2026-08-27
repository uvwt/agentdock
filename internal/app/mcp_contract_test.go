package app

import (
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
		if !reflect.DeepEqual(definition.InputSchema, wantInput) {
			t.Fatalf("%s input schema drifted from shared contract", name)
		}
		var wantOutput map[string]any
		if name == mcpcontract.ToolAgentDockContext {
			wantOutput = mcpcontract.LocalAgentDockContextOutputSchema()
		} else {
			wantOutput, _ = mcpcontract.OutputSchema(name)
		}
		if !reflect.DeepEqual(definition.OutputSchema, wantOutput) {
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

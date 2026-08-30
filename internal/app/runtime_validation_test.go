package app

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
)

func TestValidateTopLevelArgumentsRejectsUnknownFieldsForStrictTools(t *testing.T) {
	spec, ok := toolSpecByName("exec_command")
	if !ok {
		t.Fatal("exec_command spec not found")
	}
	err := validateTopLevelArguments(spec, map[string]any{
		"cmd":     "true",
		"z_wrong": true,
		"a_wrong": true,
	})
	toolErr, ok := err.(*ToolError)
	if !ok {
		t.Fatalf("error = %T %v, want *ToolError", err, err)
	}
	if toolErr.Code != "INVALID_ARGUMENT" || toolErr.Category != "validation" {
		t.Fatalf("error = %#v", toolErr)
	}
	if got, want := toolErr.Details["fields"], []string{"a_wrong", "z_wrong"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("fields = %#v, want %#v", got, want)
	}
}

func TestRuntimeCallRejectsUnknownStrictArgumentsBeforeHandler(t *testing.T) {
	runtime := newRuntimeValidationTestRuntime(t)
	_, err := runtime.Call(context.Background(), "exec_command", map[string]any{"typo": true})
	toolErr, ok := err.(*ToolError)
	if !ok {
		t.Fatalf("error = %T %v, want *ToolError", err, err)
	}
	if toolErr.Code != "INVALID_ARGUMENT" {
		t.Fatalf("error code = %q, want INVALID_ARGUMENT", toolErr.Code)
	}
	if got, want := toolErr.Details["fields"], []string{"typo"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("fields = %#v, want %#v", got, want)
	}
}

func TestRuntimeCallAllowsUnknownArgumentsForPermissiveNonCanonicalTools(t *testing.T) {
	runtime := newRuntimeValidationTestRuntime(t)
	if _, err := runtime.Call(context.Background(), "search_text", map[string]any{"path": ".", "query": "never", "future_field": true}); err != nil {
		t.Fatalf("permissive tool rejected unknown field: %v", err)
	}
}

func TestRuntimeCallRejectsWrongDeclaredArgumentTypes(t *testing.T) {
	runtime := newRuntimeValidationTestRuntime(t)
	tests := []struct {
		name string
		tool string
		args map[string]any
	}{
		{name: "command integer as string", tool: "exec_command", args: map[string]any{"cmd": "true", "timeout_ms": "12"}},
		{name: "command env non string value", tool: "exec_command", args: map[string]any{"cmd": "true", "env": map[string]any{"COUNT": 1}}},
		{name: "file integer as string", tool: "read_file", args: map[string]any{"path": "missing.txt", "max_bytes": "128"}},
		{name: "file bool as string", tool: "file_edit", args: map[string]any{"action": "replace", "path": "missing.txt", "replace_all": "true"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := runtime.Call(context.Background(), test.tool, test.args)
			toolErr, ok := err.(*ToolError)
			if !ok || toolErr.Code != "INVALID_ARGUMENT" || toolErr.Category != "validation" {
				t.Fatalf("error = %T %#v, want INVALID_ARGUMENT validation error", err, err)
			}
		})
	}
}

func TestRuntimeCallRejectsDeclaredNullArguments(t *testing.T) {
	runtime := newRuntimeValidationTestRuntime(t)
	_, err := runtime.Call(context.Background(), "exec_command", map[string]any{"cmd": "true", "timeout_ms": nil})
	toolErr, ok := err.(*ToolError)
	if !ok || toolErr.Code != "INVALID_ARGUMENT" || toolErr.Category != "validation" {
		t.Fatalf("error = %T %#v, want INVALID_ARGUMENT validation error", err, err)
	}
	if got, want := toolErr.Details["fields"], []string{"timeout_ms"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("fields = %#v, want %#v", got, want)
	}
}

func TestDecodeToolInputAllowsNullInsideDynamicLeaf(t *testing.T) {
	var request struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	err := decodeToolInput("mcp_tool_call", map[string]any{
		"name":      "demo:tool",
		"arguments": map[string]any{"nullable_downstream_value": nil},
	}, &request)
	if err != nil {
		t.Fatalf("dynamic leaf null rejected: %v", err)
	}
	if value, exists := request.Arguments["nullable_downstream_value"]; !exists || value != nil {
		t.Fatalf("arguments = %#v, want preserved nested null", request.Arguments)
	}
}

func TestRuntimeCallRejectsRemovedListDirArguments(t *testing.T) {
	runtime := newRuntimeValidationTestRuntime(t)
	for _, field := range []string{"recursive", "glob", "max_results"} {
		_, err := runtime.Call(context.Background(), "list_dir", map[string]any{"path": ".", field: true})
		toolErr, ok := err.(*ToolError)
		if !ok || toolErr.Code != "INVALID_ARGUMENT" {
			t.Fatalf("removed list_dir field %q error = %T %v, want INVALID_ARGUMENT", field, err, err)
		}
	}
}

func TestRuntimeCallRejectsUnknownArgumentsForCanonicalTools(t *testing.T) {
	runtime := newRuntimeValidationTestRuntime(t)
	_, err := runtime.Call(context.Background(), "agentdock_context", map[string]any{"future_field": true})
	if err == nil {
		t.Fatal("canonical tool accepted unknown field")
	}
}

func newRuntimeValidationTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	root := t.TempDir()
	cfg := config.Config{
		AgentDockDefaultDir: root,
		AgentDockHome:       filepath.Join(root, ".agentdock"),
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			t.Errorf("close runtime: %v", err)
		}
	})
	return runtime
}

package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
)

func TestRuntimeAvailableToolInputsAreStrictAtRoot(t *testing.T) {
	runtime := newRuntimeValidationTestRuntime(t)
	for _, definition := range runtime.ToolDefinitions() {
		if got := definition.InputSchema["additionalProperties"]; got != false {
			t.Fatalf("%s additionalProperties = %#v, want false", definition.Name, got)
		}
	}
}

func TestRuntimeCallRejectsUnknownArgumentsForFormerlyPermissiveTools(t *testing.T) {
	runtime := newRuntimeValidationTestRuntime(t)
	for _, test := range []struct {
		tool string
		args map[string]any
	}{
		{tool: "search_text", args: map[string]any{"query": "never", "future_field": true}},
		{tool: "read_file", args: map[string]any{"path": "missing.txt", "future_field": true}},
		{tool: "file_edit", args: map[string]any{"action": "replace", "future_field": true}},
		{tool: "task_manage", args: map[string]any{"action": "list", "future_field": true}},
		{tool: "skill_package", args: map[string]any{"action": "env_list", "future_field": true}},
		{tool: "view_image", args: map[string]any{"path": "missing.png", "future_field": true}},
		{tool: "file_publish", args: map[string]any{"path": "missing.txt", "future_field": true}},
	} {
		t.Run(test.tool, func(t *testing.T) {
			assertInvalidToolArguments(t, runtime, test.tool, test.args)
		})
	}
}

func TestRuntimeCallEnforcesRequiredEnumBoundsAndOneOf(t *testing.T) {
	runtime := newRuntimeValidationTestRuntime(t)
	for _, test := range []struct {
		name string
		tool string
		args map[string]any
	}{
		{name: "required", tool: "exec_command", args: map[string]any{}},
		{name: "enum", tool: "exec_command", args: map[string]any{"cmd": "true", "execution_mode": "eventually"}},
		{name: "minimum", tool: "exec_command", args: map[string]any{"cmd": "true", "timeout_ms": 0}},
		{name: "maximum", tool: "mcp_tool_search", args: map[string]any{"query": "x", "limit": 101}},
		{name: "task_limit", tool: "task_manage", args: map[string]any{"action": "list", "limit": 201}},
		{name: "image_quality", tool: "view_image", args: map[string]any{"path": "a.png", "quality": 96}},
		{name: "image_format", tool: "view_image", args: map[string]any{"path": "a.png", "format": "webp"}},
		{name: "one_of", tool: "view_image", args: map[string]any{"path": "a.png", "url": "https://example.invalid/a.png"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertInvalidToolArguments(t, runtime, test.tool, test.args)
		})
	}
}

func TestRuntimeCallRejectsNestedUnknownFields(t *testing.T) {
	runtime := newRuntimeValidationTestRuntime(t)
	assertInvalidToolArguments(t, runtime, "task_manage", map[string]any{
		"action": "create",
		"title":  "schema test",
		"goal":   "schema test",
		"steps": []any{
			map[string]any{"id": "step-1", "title": "test", "future_field": true},
		},
	})
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
			assertInvalidToolArguments(t, runtime, test.tool, test.args)
		})
	}
}

func TestRuntimeCallRejectsDeclaredNullArguments(t *testing.T) {
	runtime := newRuntimeValidationTestRuntime(t)
	assertInvalidToolArguments(t, runtime, "exec_command", map[string]any{"cmd": "true", "timeout_ms": nil})
}

func TestRuntimeValidationAllowsNullInsideDynamicMCPArguments(t *testing.T) {
	runtime := newRuntimeValidationTestRuntime(t)
	args := map[string]any{
		"name":      "demo:tool",
		"arguments": map[string]any{"nullable_downstream_value": nil},
	}
	if err := runtime.validateToolArguments("mcp_tool_call", args); err != nil {
		t.Fatalf("dynamic leaf null rejected by schema: %v", err)
	}
	var request struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := decodeToolInput("mcp_tool_call", args, &request); err != nil {
		t.Fatalf("dynamic leaf null rejected by decoder: %v", err)
	}
	if value, exists := request.Arguments["nullable_downstream_value"]; !exists || value != nil {
		t.Fatalf("arguments = %#v, want preserved nested null", request.Arguments)
	}
}

func TestRuntimeCallRejectsRemovedListDirArguments(t *testing.T) {
	runtime := newRuntimeValidationTestRuntime(t)
	for _, field := range []string{"recursive", "glob", "max_results"} {
		assertInvalidToolArguments(t, runtime, "list_dir", map[string]any{"path": ".", field: true})
	}
}

func TestRuntimeCallRejectsUnknownArgumentsForCanonicalTools(t *testing.T) {
	runtime := newRuntimeValidationTestRuntime(t)
	assertInvalidToolArguments(t, runtime, "agentdock_context", map[string]any{"future_field": true})
}

func assertInvalidToolArguments(t *testing.T, runtime *Runtime, tool string, args map[string]any) {
	t.Helper()
	_, err := runtime.Call(context.Background(), tool, args)
	toolErr, ok := err.(*ToolError)
	if !ok || toolErr.Code != "INVALID_ARGUMENT" || toolErr.Category != "validation" {
		t.Fatalf("%s error = %T %#v, want INVALID_ARGUMENT validation error", tool, err, err)
	}
	reason, _ := toolErr.Details["reason"].(string)
	if strings.TrimSpace(reason) == "" {
		t.Fatalf("%s validation error has no reason: %#v", tool, toolErr)
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

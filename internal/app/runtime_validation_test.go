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
	if _, err := runtime.Call(context.Background(), "server_info", map[string]any{"future_field": true}); err != nil {
		t.Fatalf("permissive tool rejected unknown field: %v", err)
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

package mcp

import (
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/tools"
)

func TestToolRegistryHasNoDuplicates(t *testing.T) {
	seen := map[string]bool{}
	for _, def := range toolRegistry {
		if def.Name == "" {
			t.Fatalf("tool registry contains empty name")
		}
		if seen[def.Name] {
			t.Fatalf("duplicate tool definition: %s", def.Name)
		}
		seen[def.Name] = true
		if def.ReadOnly && def.Destructive {
			t.Fatalf("tool cannot be both read-only and destructive: %s", def.Name)
		}
	}
}

func TestRuntimeToolsHaveRegistryDefinitionsAndSchemas(t *testing.T) {
	cfg := config.Config{
		Workspace:       t.TempDir(),
		ToolProfile:     config.ProfileUnified,
		Mode:            config.ModeSandboxed,
		PathPolicy:      config.PathPolicyWorkspace,
		AgentDockDir:    "AgentDock",
		MemoryEndpoint:  "http://127.0.0.1:18777",
		BrowserEnabled:  true,
		DesktopEnabled:  true,
		EnableViewImage: true,
	}
	cfg.Normalize()
	rt, err := tools.NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range rt.ToolNames() {
		def, ok := toolDefinition(name)
		if !ok {
			t.Fatalf("runtime exposes tool without registry definition: %s", name)
		}
		if def.Name != name {
			t.Fatalf("registry lookup mismatch: got %q want %q", def.Name, name)
		}
		assertObjectSchema(t, name, "input", inputSchema(name))
		assertObjectSchema(t, name, "output", outputSchema(name))
	}
}

func TestReadOnlyProfileExcludesDestructiveTools(t *testing.T) {
	cfg := config.Config{
		Workspace:       t.TempDir(),
		ToolProfile:     config.ProfileReadOnly,
		Mode:            config.ModeSandboxed,
		PathPolicy:      config.PathPolicyWorkspace,
		AgentDockDir:    "AgentDock",
		MemoryEndpoint:  "http://127.0.0.1:18777",
		BrowserEnabled:  true,
		DesktopEnabled:  true,
		EnableViewImage: true,
	}
	cfg.Normalize()
	rt, err := tools.NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, name := range rt.ToolNames() {
		seen[name] = true
		def, ok := toolDefinition(name)
		if !ok {
			t.Fatalf("read-only runtime exposes unregistered tool: %s", name)
		}
		if def.Destructive {
			t.Fatalf("read-only profile exposes destructive tool: %s", name)
		}
	}
	if !seen["desktop_observe"] {
		t.Fatalf("read-only desktop profile should expose unified observation tool desktop_observe")
	}
	if seen["desktop_act"] || seen["desktop_click"] || seen["desktop_type"] || seen["desktop_set_value"] {
		t.Fatalf("read-only desktop profile exposed mutating desktop tools")
	}
	if seen["memory_edit"] || seen["memory_write"] || seen["memory_patch"] || seen["memory_delete"] {
		t.Fatalf("read-only profile exposed mutating memory tools")
	}
	if seen["edit_file"] {
		t.Fatalf("read-only profile exposed edit_file")
	}
}

func TestSkillManageSchemaIncludesValidate(t *testing.T) {
	schema := inputSchema("skill_manage")
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("skill_manage input schema properties missing")
	}
	action, ok := props["action"].(map[string]any)
	if !ok {
		t.Fatal("skill_manage action schema missing")
	}
	values, ok := action["enum"].([]string)
	if !ok {
		t.Fatalf("skill_manage action enum has unexpected type: %#v", action["enum"])
	}
	seen := map[string]bool{}
	for _, value := range values {
		seen[value] = true
	}
	for _, value := range []string{"list", "inspect", "validate", "install", "run", "rollback"} {
		if !seen[value] {
			t.Fatalf("skill_manage action enum missing %q: %#v", value, values)
		}
	}
	outputProps, ok := outputSchema("skill_manage")["properties"].(map[string]any)
	if !ok {
		t.Fatal("skill_manage output schema properties missing")
	}
	for _, name := range []string{"valid", "source", "digest", "env", "commands", "issues", "requires_no_env_confirm"} {
		if _, ok := outputProps[name]; !ok {
			t.Fatalf("skill_manage output schema missing %q", name)
		}
	}
}

func assertObjectSchema(t *testing.T, name, kind string, schema map[string]any) {
	t.Helper()
	if schema == nil {
		t.Fatalf("%s schema for %s is nil", kind, name)
	}
	if got := schema["type"]; got != "object" {
		t.Fatalf("%s schema for %s has type %v, want object", kind, name, got)
	}
	if _, ok := schema["properties"].(map[string]any); !ok {
		t.Fatalf("%s schema for %s missing object properties", kind, name)
	}
}

func TestTaskManageSchemaExposesLifecycleActions(t *testing.T) {
	schema := inputSchema("task_manage")
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("task_manage input schema properties missing")
	}
	action, ok := props["action"].(map[string]any)
	if !ok {
		t.Fatal("task_manage action schema missing")
	}
	values, ok := action["enum"].([]string)
	if !ok {
		t.Fatalf("task_manage action enum has unexpected type: %#v", action["enum"])
	}
	seen := map[string]bool{}
	for _, value := range values {
		seen[value] = true
	}
	for _, value := range []string{"create", "list", "get", "add_condition", "add_evidence", "advance", "record_attempt", "block", "resume", "complete"} {
		if !seen[value] {
			t.Fatalf("task_manage action enum missing %q: %#v", value, values)
		}
	}
	for _, name := range []string{"completion_conditions", "condition_id", "strategy", "outcome", "diagnosis", "evidence"} {
		if _, ok := props[name]; !ok {
			t.Fatalf("task_manage input schema missing %q", name)
		}
	}
	outputProps, ok := outputSchema("task_manage")["properties"].(map[string]any)
	if !ok {
		t.Fatal("task_manage output schema properties missing")
	}
	for _, name := range []string{"task", "tasks", "count", "state_dir"} {
		if _, ok := outputProps[name]; !ok {
			t.Fatalf("task_manage output schema missing %q", name)
		}
	}
}

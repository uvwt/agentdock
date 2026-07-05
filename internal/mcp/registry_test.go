package mcp

import (
	"strings"
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
		RecallEndpoint:  "http://127.0.0.1:18777",
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
		RecallEndpoint:  "http://127.0.0.1:18777",
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
	if seen["recall_write"] {
		t.Fatalf("read-only profile exposed mutating recall tool")
	}
	if !seen["recall_bootstrap"] || !seen["recall_search"] || !seen["recall_read"] || !seen["recall_maintain"] {
		t.Fatalf("read-only profile should expose read-only RecallDock tools")
	}
	if seen["edit_file"] {
		t.Fatalf("read-only profile exposed edit_file")
	}
}

func TestRecallDockToolNamesHideLegacyMemoryTools(t *testing.T) {
	cfg := config.Config{
		Workspace:       t.TempDir(),
		ToolProfile:     config.ProfileUnified,
		Mode:            config.ModeSandboxed,
		PathPolicy:      config.PathPolicyWorkspace,
		AgentDockDir:    "AgentDock",
		RecallEndpoint:  "http://127.0.0.1:18777",
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
	}
	for _, name := range []string{"recall_bootstrap", "recall_search", "recall_read", "recall_write", "recall_maintain"} {
		if !seen[name] {
			t.Fatalf("unified profile missing RecallDock tool %q", name)
		}
	}
	oldPrefixes := []string{"mem" + "ory_", "notes_"}
	for _, prefix := range oldPrefixes {
		for name := range seen {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("unified profile still exposes legacy recall predecessor tool %q", name)
			}
		}
	}
}

func TestRecallBootstrapSchemaSeparatesPackBudgetFromBody(t *testing.T) {
	schema := inputSchema("recall_bootstrap")
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("recall_bootstrap input schema properties missing")
	}
	if _, ok := props["include_body"]; !ok {
		t.Fatalf("recall_bootstrap schema missing include_body: %#v", props)
	}
	maxBytes, ok := props["max_bytes"].(map[string]any)
	if !ok {
		t.Fatalf("recall_bootstrap max_bytes schema missing: %#v", props["max_bytes"])
	}
	desc, _ := maxBytes["description"].(string)
	if !strings.Contains(desc, "Does not expose section bodies") {
		t.Fatalf("max_bytes description should not imply body exposure: %q", desc)
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
	for _, value := range []string{"create", "list", "get", "block", "resume", "final_review", "complete_after_review", "template_match"} {
		if !seen[value] {
			t.Fatalf("task_manage action enum missing %q: %#v", value, values)
		}
	}
	for _, value := range []string{"phase_checkpoint", "complete_step", "record_attempt", "template_save"} {
		if seen[value] {
			t.Fatalf("task_manage action enum should hide recovery action %q: %#v", value, values)
		}
	}
	for _, name := range []string{"completion_conditions", "review_status", "verified_facts", "open_risks", "missing_checks", "evidence"} {
		if _, ok := props[name]; !ok {
			t.Fatalf("task_manage input schema missing %q", name)
		}
	}
	for _, name := range []string{"step_completions", "condition_evidence", "advance_phase", "complete_task", "strategy", "outcome", "diagnosis"} {
		if _, ok := props[name]; ok {
			t.Fatalf("task_manage input schema should hide %q", name)
		}
	}
	recoveryProps, ok := inputSchema("task_manage_recovery")["properties"].(map[string]any)
	if !ok {
		t.Fatal("task_manage_recovery input schema properties missing")
	}
	for _, name := range []string{"step_completions", "condition_evidence", "advance_phase", "complete_task", "strategy", "outcome", "diagnosis"} {
		if _, ok := recoveryProps[name]; !ok {
			t.Fatalf("task_manage_recovery input schema missing %q", name)
		}
	}
	outputProps, ok := outputSchema("task_manage")["properties"].(map[string]any)
	if !ok {
		t.Fatal("task_manage output schema properties missing")
	}
	for _, name := range []string{"task_id", "task", "task_summary", "next_required_action", "tasks", "count", "state_dir", "recommended", "best_candidate_score"} {
		if _, ok := outputProps[name]; !ok {
			t.Fatalf("task_manage output schema missing %q", name)
		}
	}
}

func TestRecallWriteSchemaExposesCardsNotesAndEditFields(t *testing.T) {
	inputProps, ok := inputSchema("recall_write")["properties"].(map[string]any)
	if !ok {
		t.Fatal("recall_write input schema properties missing")
	}
	for _, name := range []string{"kind", "title", "content", "type", "scope", "status", "confidence", "confirmed", "allow_warnings", "path", "overwrite", "old", "new", "facts"} {
		if _, ok := inputProps[name]; !ok {
			t.Fatalf("recall_write input schema missing %q", name)
		}
	}
	if _, ok := inputProps["project"]; ok {
		t.Fatal("recall_write input schema should not expose project; project is an internal metadata/backward-compat field")
	}
	outputProps, ok := outputSchema("recall_write")["properties"].(map[string]any)
	if !ok {
		t.Fatal("recall_write output schema properties missing")
	}
	for _, name := range []string{"recall_kind", "card", "warnings", "capture_plan", "similar_results", "recall", "diff", "updates"} {
		if _, ok := outputProps[name]; !ok {
			t.Fatalf("recall_write output schema missing %q", name)
		}
	}
}

func TestRecallBootstrapSchemaHidesProjectSelector(t *testing.T) {
	inputProps, ok := inputSchema("recall_bootstrap")["properties"].(map[string]any)
	if !ok {
		t.Fatal("recall_bootstrap input schema properties missing")
	}
	if _, ok := inputProps["project"]; ok {
		t.Fatal("recall_bootstrap input schema should not expose project; backend keeps the default context")
	}
	for _, name := range []string{"max_bytes", "include_raw", "include_body"} {
		if _, ok := inputProps[name]; !ok {
			t.Fatalf("recall_bootstrap input schema missing %q", name)
		}
	}
}

func TestRecallSearchSchemasExposeNotesCompactionControls(t *testing.T) {
	inputProps, ok := inputSchema("recall_search")["properties"].(map[string]any)
	if !ok {
		t.Fatal("recall_search input schema properties missing")
	}
	if _, ok := inputProps["include_search_results"]; !ok {
		t.Fatal("recall_search input schema missing include_search_results")
	}
	outputProps, ok := outputSchema("recall_search")["properties"].(map[string]any)
	if !ok {
		t.Fatal("recall_search output schema properties missing")
	}
	for _, name := range []string{"candidate_paths", "candidates", "search_result_count", "search_results"} {
		if _, ok := outputProps[name]; !ok {
			t.Fatalf("recall_search output schema missing %q", name)
		}
	}
}

func TestDesktopCommandSchemasExposeVerificationControls(t *testing.T) {
	for _, tool := range []string{
		"desktop_act",
		"desktop_click",
		"desktop_double_click",
		"desktop_move",
		"desktop_scroll",
		"desktop_drag",
		"desktop_type",
		"desktop_hotkey",
	} {
		schema := inputSchema(tool)
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s input schema properties missing", tool)
		}
		for _, name := range []string{"verify", "before_snapshot", "after_snapshot", "verify_region", "wait_ms"} {
			if _, ok := props[name]; !ok {
				t.Fatalf("%s input schema missing %q", tool, name)
			}
		}
	}
}

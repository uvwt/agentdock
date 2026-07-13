package mcp

import (
	"path/filepath"
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
	}
}

func TestRuntimeToolsHaveRegistryDefinitionsAndSchemas(t *testing.T) {
	cfg := config.Config{
		AgentDockDefaultDir: t.TempDir(), AgentDockHome: filepath.Join(t.TempDir(), ".agentdock"),
		NexusEndpoint:  "http://127.0.0.1:18777",
		BrowserEnabled: true,
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
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

func TestRuntimeExposesSingleToolSet(t *testing.T) {
	cfg := config.Config{
		AgentDockDefaultDir: t.TempDir(), AgentDockHome: filepath.Join(t.TempDir(), ".agentdock"),
		NexusEndpoint:  "http://127.0.0.1:18777",
		BrowserEnabled: true,
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	rt, err := tools.NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, name := range rt.ToolNames() {
		seen[name] = true
	}
	for _, name := range []string{"agentdock_context", "git_read", "git_write", "session_observe", "session_act", "recall_read", "recall_write", "skill_package", "mcp_manage", "mcp_tool_search", "mcp_tool_inspect", "mcp_tool_call"} {
		if !seen[name] {
			t.Fatalf("single tool set missing %s: %#v", name, seen)
		}
	}
	for _, removed := range []string{"skill_read", "skill_run", "skill_env_manage"} {
		if seen[removed] {
			t.Fatalf("removed model-facing Skill tool still exposed: %s", removed)
		}
	}
}

func TestAgentDockContextSchemaIsModelFacingEntrypoint(t *testing.T) {
	def, ok := toolDefinition("agentdock_context")
	if !ok {
		t.Fatal("agentdock_context definition missing")
	}
	if !strings.Contains(def.Description, "clients that cannot inject system prompt context") {
		t.Fatalf("agentdock_context description should explain model-facing use: %q", def.Description)
	}

	inputProps := schemaProperties(t, "agentdock_context")
	if len(inputProps) != 0 {
		t.Fatalf("agentdock_context input schema should not expose legacy refresh: %#v", inputProps)
	}
	outputProps, ok := outputSchema("agentdock_context")["properties"].(map[string]any)
	if !ok {
		t.Fatal("agentdock_context output schema properties missing")
	}
	if _, ok := outputProps["context"]; !ok {
		t.Fatal("agentdock_context output schema missing context")
	}
	if len(outputProps) != 1 {
		t.Fatalf("agentdock_context output schema should expose only context: %#v", outputProps)
	}
	for _, name := range []string{"ok", "skills", "dynamic_mcp", "generated_at", "summary", "counts", "base_tools", "task_templates", "memory", "rules"} {
		if _, ok := outputProps[name]; ok {
			t.Fatalf("agentdock_context output schema should not expose non-context field %q", name)
		}
	}
	required, ok := outputSchema("agentdock_context")["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "context" {
		t.Fatalf("agentdock_context output schema required = %#v, want [context]", required)
	}

}

func TestNexusDockRecallToolNamesHideLegacyMemoryTools(t *testing.T) {
	cfg := config.Config{
		AgentDockDefaultDir: t.TempDir(), AgentDockHome: filepath.Join(t.TempDir(), ".agentdock"),
		NexusEndpoint: "http://127.0.0.1:18777",
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	rt, err := tools.NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, name := range rt.ToolNames() {
		seen[name] = true
	}
	for _, name := range []string{"recall_bootstrap", "recall_search", "recall_read", "recall_write", "recall_maintain", "private_note_manage"} {
		if !seen[name] {
			t.Fatalf("full profile missing memory tool %q", name)
		}
	}
	oldPrefixes := []string{"mem" + "ory_", "notes_", "private_notes_"}
	for _, prefix := range oldPrefixes {
		for name := range seen {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("full profile still exposes legacy memory/private-notes tool %q", name)
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

func TestSkillPackageSchemaAndRemovedRuntimeTools(t *testing.T) {
	packageProps := schemaProperties(t, "skill_package")
	assertSameStrings(t, enumStrings(t, packageProps["action"]), []string{"validate", "install", "rollback", "env_set", "env_unset", "env_list"})
	for _, name := range []string{"source", "digest", "activate", "max_bytes", "skill", "channel", "key", "value"} {
		if _, ok := packageProps[name]; !ok {
			t.Fatalf("skill_package input schema missing %q", name)
		}
	}

	for _, removed := range []string{"skill_read", "skill_run", "skill_env_manage"} {
		if _, ok := toolDefinition(removed); ok {
			t.Fatalf("removed model-facing tool still has definition: %s", removed)
		}
	}

	packageOutputProps, ok := outputSchema("skill_package")["properties"].(map[string]any)
	if !ok {
		t.Fatal("skill_package output schema properties missing")
	}
	for _, name := range []string{"valid", "source", "digest", "document", "issues"} {
		if _, ok := packageOutputProps[name]; !ok {
			t.Fatalf("skill_package output schema missing %q", name)
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
	props := schemaProperties(t, "task_manage")
	assertSameStrings(t, enumStrings(t, props["action"]), []string{"create", "list", "get", "checkpoint", "block", "resume", "final_review", "complete"})
	for _, name := range []string{"completion_conditions", "steps", "template_id", "source_template_ids", "step_id", "completed_step_ids", "current_step_id", "status", "summary", "verified", "risks"} {
		if _, ok := props[name]; !ok {
			t.Fatalf("task_manage input schema missing %q", name)
		}
	}
	for _, removed := range []string{"template_version", "selected_reason", "template_candidates", "blocker", "evidence", "review_status", "verified_facts", "open_risks", "missing_checks"} {
		if _, ok := props[removed]; ok {
			t.Fatalf("task_manage input schema still exposes removed field %q", removed)
		}
	}

	templateProps := schemaProperties(t, "workflow_template_manage")
	assertSameStrings(t, enumStrings(t, templateProps["action"]), []string{"save", "validate", "publish", "retire", "list", "get", "get_many", "match", "vector_index"})
	for _, name := range []string{"template", "template_id", "template_ids", "template_version", "template_status", "allow_long_template", "long_template_reason", "goal", "device", "type"} {
		if _, ok := templateProps[name]; !ok {
			t.Fatalf("workflow_template_manage input schema missing %q", name)
		}
	}

	outputProps, ok := outputSchema("task_manage")["properties"].(map[string]any)
	if !ok {
		t.Fatal("task_manage output schema properties missing")
	}
	for _, name := range []string{"task_id", "task", "task_summary", "next_required_action", "tasks", "count", "state_dir"} {
		if _, ok := outputProps[name]; !ok {
			t.Fatalf("task_manage output schema missing %q", name)
		}
	}

	workflowOutputProps, ok := outputSchema("workflow_template_manage")["properties"].(map[string]any)
	if !ok {
		t.Fatal("workflow_template_manage output schema properties missing")
	}
	for _, name := range []string{"candidates", "recommended", "best_candidate_score", "score_thresholds", "composition_required", "next_required_action"} {
		if _, ok := workflowOutputProps[name]; !ok {
			t.Fatalf("workflow_template_manage output schema missing %q", name)
		}
	}
}

func TestFileEditAndGitUnifiedSchemas(t *testing.T) {
	fileProps := schemaProperties(t, "file_edit")
	assertSameStrings(t, enumStrings(t, fileProps["action"]), []string{"replace", "patch", "add", "delete", "move"})
	for _, name := range []string{"path", "old", "new", "patch", "dry_run", "expected_matches", "replace_all", "content", "new_path", "overwrite", "recursive"} {
		if _, ok := fileProps[name]; !ok {
			t.Fatalf("file_edit input schema missing %q", name)
		}
	}

	gitReadProps := schemaProperties(t, "git_read")
	assertSameStrings(t, enumStrings(t, gitReadProps["action"]), []string{"repos", "status", "diff", "log", "show", "blame", "github_repo_access"})
	for _, name := range []string{"repo_path", "path", "paths", "rev", "limit", "max_bytes", "repo", "timeout_ms"} {
		if _, ok := gitReadProps[name]; !ok {
			t.Fatalf("git_read input schema missing %q", name)
		}
	}

	gitWriteProps := schemaProperties(t, "git_write")
	assertSameStrings(t, enumStrings(t, gitWriteProps["action"]), []string{"clone", "commit", "fetch", "pull", "push"})
	for _, name := range []string{"repo_path", "url", "dest", "message", "remote", "branch", "max_bytes"} {
		if _, ok := gitWriteProps[name]; !ok {
			t.Fatalf("git_write input schema missing %q", name)
		}
	}
}

func TestLegacyModelEntrypointsAreRemoved(t *testing.T) {
	for _, name := range []string{"apply_patch", "edit_file", "workspace_repos", "git_status", "git_diff", "git_log", "git_inspect", "git_remote", "git_clone", "git_commit", "check_github_repo_access", "browser_profile", "private_notes_search", "private_notes_read", "private_notes_write", "private_notes_status", "private_notes_maintain"} {
		if _, ok := toolDefinition(name); ok {
			t.Fatalf("legacy tool should not be model-facing: %s", name)
		}
		props := schemaProperties(t, name)
		if len(props) != 0 {
			t.Fatalf("legacy tool schema should be empty for %s: %#v", name, props)
		}
	}
}

func TestRecallModelChoiceFieldsUseEnums(t *testing.T) {
	searchProps := schemaProperties(t, "recall_search")
	for _, want := range []string{"all", "markdown", "card", "note"} {
		if !containsString(enumStrings(t, searchProps["kind"]), want) {
			t.Fatalf("recall_search kind enum missing %s: %#v", want, searchProps["kind"])
		}
	}
	for _, want := range []string{"questions", "github-learning"} {
		if !containsString(enumStrings(t, searchProps["note_scope"]), want) {
			t.Fatalf("recall_search note_scope enum missing %s: %#v", want, searchProps["note_scope"])
		}
	}

	writeProps := schemaProperties(t, "recall_write")
	for _, want := range []string{"card", "note", "markdown"} {
		if !containsString(enumStrings(t, writeProps["target"]), want) {
			t.Fatalf("recall_write target enum missing %s: %#v", want, writeProps["target"])
		}
	}
	for _, want := range []string{"plan", "create", "replace", "append", "patch", "update_fact", "diff", "delete"} {
		if !containsString(enumStrings(t, writeProps["action"]), want) {
			t.Fatalf("recall_write action enum missing %s: %#v", want, writeProps["action"])
		}
	}
	for _, want := range []string{"questions", "github-learning"} {
		if !containsString(enumStrings(t, writeProps["note_scope"]), want) {
			t.Fatalf("recall_write note_scope enum missing %s: %#v", want, writeProps["note_scope"])
		}
	}

	maintainProps := schemaProperties(t, "recall_maintain")
	for _, want := range []string{"sync_status", "list", "lint", "embedding_status", "reindex", "reindex_cards"} {
		if !containsString(enumStrings(t, maintainProps["action"]), want) {
			t.Fatalf("recall_maintain action enum missing %s: %#v", want, maintainProps["action"])
		}
	}
}

func TestRecallPublicSchemasAreClosedForModelFacingArgs(t *testing.T) {
	for _, name := range []string{"recall_bootstrap", "recall_search", "recall_read", "recall_write", "recall_maintain", "private_note_manage"} {
		schema := inputSchema(name)
		if got, _ := schema["additionalProperties"].(bool); got {
			t.Fatalf("%s input schema should be closed to keep hidden compatibility args out of model-facing schema: %#v", name, schema)
		}
	}
}

func TestPrivateNoteManageModelEntrypoint(t *testing.T) {
	def, ok := toolDefinition("private_note_manage")
	if !ok {
		t.Fatal("private_note_manage definition missing")
	}
	for _, text := range []string{"Do not use by default", "private/local-only/non-synced", "Actions: search, read, write, status, or maintain"} {
		if !strings.Contains(def.Description, text) {
			t.Fatalf("private_note_manage description missing %q: %q", text, def.Description)
		}
	}

	props := schemaProperties(t, "private_note_manage")
	assertSameStrings(t, enumStrings(t, props["action"]), []string{"search", "read", "write", "status", "maintain"})
	for _, name := range []string{"query", "path", "category", "title", "content", "confirmed", "overwrite", "status_action", "maintenance_action"} {
		if _, ok := props[name]; !ok {
			t.Fatalf("private_note_manage input schema missing %q", name)
		}
	}
	for _, name := range []string{"private_notes_search", "private_notes_read", "private_notes_write", "private_notes_status", "private_notes_maintain"} {
		if _, ok := toolDefinition(name); ok {
			t.Fatalf("legacy private notes tool should not be model-facing: %s", name)
		}
	}
}

func TestRecallToolDescriptionsMatchCompactModelEntrypoints(t *testing.T) {
	searchDef, ok := toolDefinition("recall_search")
	if !ok {
		t.Fatal("recall_search definition missing")
	}
	if strings.Contains(searchDef.Description, "use kind or prefix") || strings.Contains(searchDef.Description, "use prefix") {
		t.Fatalf("recall_search description should not tell the model to choose prefix: %q", searchDef.Description)
	}
	writeDef, ok := toolDefinition("recall_write")
	if !ok {
		t.Fatal("recall_write definition missing")
	}
	for _, legacy := range []string{"append_note", "kind=", "target=auto"} {
		if strings.Contains(writeDef.Description, legacy) {
			t.Fatalf("recall_write description should not advertise legacy alias %q: %q", legacy, writeDef.Description)
		}
	}
	for _, required := range []string{"target=card/note/markdown", "action"} {
		if !strings.Contains(writeDef.Description, required) {
			t.Fatalf("recall_write description missing %q: %q", required, writeDef.Description)
		}
	}
}

func TestRecallWriteSchemaExposesCompactCoreFields(t *testing.T) {
	schema := inputSchema("recall_write")
	inputProps, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("recall_write input schema properties missing")
	}
	for _, name := range []string{"target", "action", "title", "content", "summary", "query", "note_scope", "confirmed", "path", "overwrite", "max_bytes", "old", "new", "append", "section", "section_content", "key", "value", "facts", "append_if_missing", "allow_warnings", "conclusion", "open_questions"} {
		if _, ok := inputProps[name]; !ok {
			t.Fatalf("recall_write input schema missing compact core field %q", name)
		}
	}
	for _, name := range []string{"kind", "project", "prefix", "scope", "status", "confidence", "source", "evidence", "boundary", "pattern", "replacement", "operations", "dry_run"} {
		if _, ok := inputProps[name]; ok {
			t.Fatalf("recall_write input schema should hide advanced/internal field %q", name)
		}
	}
	required, _ := schema["required"].([]string)
	if len(required) != 2 || required[0] != "target" || required[1] != "action" {
		t.Fatalf("recall_write should require model-selected target/action, got %#v", schema["required"])
	}
	outputProps, ok := outputSchema("recall_write")["properties"].(map[string]any)
	if !ok {
		t.Fatal("recall_write output schema properties missing")
	}
	for _, name := range []string{"recall_target", "recall_action", "card", "warnings", "capture_plan", "similar_results", "recall", "diff", "updates"} {
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
	outputProps, ok := outputSchema("recall_bootstrap")["properties"].(map[string]any)
	if !ok {
		t.Fatal("recall_bootstrap output schema properties missing")
	}
	projectProp, ok := outputProps["project"].(map[string]any)
	if !ok {
		t.Fatal("recall_bootstrap output schema should include actual backend project/context field")
	}
	projectDesc, _ := projectProp["description"].(string)
	if strings.Contains(projectDesc, "input selector") && !strings.Contains(projectDesc, "not an input selector") {
		t.Fatalf("recall_bootstrap output project description is ambiguous: %q", projectDesc)
	}
	if projectDesc == "Project key." {
		t.Fatal("recall_bootstrap output project description should not look like a model-selected project parameter")
	}
}

func TestRecallSearchSchemaHidesInternalRoutingFields(t *testing.T) {
	inputProps, ok := inputSchema("recall_search")["properties"].(map[string]any)
	if !ok {
		t.Fatal("recall_search input schema properties missing")
	}
	if _, ok := inputProps["note_scope"]; !ok {
		t.Fatal("recall_search input schema should expose note_scope for questions/github-learning notes")
	}
	for _, name := range []string{"prefix", "scope", "include_search_results"} {
		if _, ok := inputProps[name]; ok {
			t.Fatalf("recall_search input schema should hide internal field %q", name)
		}
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
	obj, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("schema property is not object: %#v", value)
	}
	raw, ok := obj["enum"]
	if !ok {
		t.Fatalf("enum missing: %#v", obj)
	}
	items, ok := raw.([]string)
	if !ok {
		t.Fatalf("enum has unexpected type: %#v", raw)
	}
	return items
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func assertSameStrings(t *testing.T, actual, expected []string) {
	t.Helper()
	if len(actual) != len(expected) {
		t.Fatalf("unexpected enum length: got %#v want %#v", actual, expected)
	}
	seen := map[string]bool{}
	for _, item := range actual {
		seen[item] = true
	}
	for _, item := range expected {
		if !seen[item] {
			t.Fatalf("enum missing %q: got %#v want %#v", item, actual, expected)
		}
	}
}

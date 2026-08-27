package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
)

func TestAgentDockContextToolReturnsStructuredRuntimeIndex(t *testing.T) {
	cfg := config.Config{
		AgentDockDefaultDir: t.TempDir(),
		AgentDockHome:       filepath.Join(t.TempDir(), ".agentdock"),
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	installDocumentSkillForTest(t, rt, "demo-skill", "1.0.0", "Use this Skill for context index tests.")

	result, err := rt.Call(context.Background(), "agentdock_context", map[string]any{})
	if err != nil {
		t.Fatalf("agentdock_context call failed: %v", err)
	}
	if _, legacy := result["context"]; legacy {
		t.Fatalf("agentdock_context still exposes legacy Markdown context: %#v", result)
	}
	var got capabilityContext
	if err := remarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	var demo *capabilitySkillItem
	for index := range got.Skills {
		if got.Skills[index].Name == "demo-skill" {
			demo = &got.Skills[index]
			break
		}
	}
	if demo == nil || demo.Description != "Use this Skill for context index tests." || demo.File != "skill://demo-skill/SKILL.md" {
		t.Fatalf("structured Skill index missing demo-skill: %#v", got.Skills)
	}
	if got.DynamicMCP == nil || got.WorkflowTemplates == nil || got.Rules == nil {
		t.Fatalf("required structured context fields must be arrays: %#v", got)
	}
	rules := strings.Join(got.Rules, "\n")
	for _, want := range []string{"AgentDock 自带工具直接调用", "task_manage checkpoint"} {
		if !strings.Contains(rules, want) {
			t.Fatalf("context rules missing %q: %s", want, rules)
		}
	}
	for _, removed := range []string{"skill_read", "skill_run"} {
		if strings.Contains(rules, removed) {
			t.Fatalf("context rules still reference removed tool %q: %s", removed, rules)
		}
	}
}

func TestAgentDockContextExposesShortACPOrientationWhenEnabled(t *testing.T) {
	disabled := config.Config{
		AgentDockDefaultDir: t.TempDir(),
		AgentDockHome:       filepath.Join(t.TempDir(), ".agentdock"),
	}
	if err := disabled.Normalize(); err != nil {
		t.Fatal(err)
	}
	disabledRuntime, err := NewRuntime(disabled)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = disabledRuntime.Close() })

	disabledResult, err := disabledRuntime.Call(context.Background(), "agentdock_context", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var disabledContext capabilityContext
	if err := remarshal(disabledResult, &disabledContext); err != nil {
		t.Fatal(err)
	}
	if disabledContext.ACP != nil {
		t.Fatalf("context should omit ACP while disabled: %#v", disabledContext.ACP)
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	enabled := config.Config{
		AgentDockDefaultDir: root,
		AgentDockHome:       filepath.Join(t.TempDir(), ".agentdock"),
		ACPEnabled:          true,
		ACPAgentName:        "helper",
		ACPCommand:          executable,
	}
	if err := enabled.Normalize(); err != nil {
		t.Fatal(err)
	}
	enabledRuntime, err := NewRuntime(enabled)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = enabledRuntime.Close() })

	enabledResult, err := enabledRuntime.Call(context.Background(), "agentdock_context", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var enabledContext capabilityContext
	if err := remarshal(enabledResult, &enabledContext); err != nil {
		t.Fatal(err)
	}
	if enabledContext.ACP == nil || !enabledContext.ACP.Enabled || enabledContext.ACP.Agent != "helper" {
		t.Fatalf("ACP context = %#v", enabledContext.ACP)
	}
	for _, want := range []string{"Agent Client Protocol", "仅当用户明确要求时使用", "独特见解", "编排任务", "不是动态 MCP", "mcp_tool_*"} {
		if !strings.Contains(enabledContext.ACP.Description, want) {
			t.Fatalf("ACP description missing %q: %s", want, enabledContext.ACP.Description)
		}
	}
	combined := enabledContext.ACP.Description + "\n" + strings.Join(enabledContext.Rules, "\n")
	for _, operational := range []string{"acp_session", "acp_prompt", "acp_interaction", "option_id", "always", "JSON-RPC", "wire protocol"} {
		if strings.Contains(combined, operational) {
			t.Fatalf("ACP bootstrap should leave %q to tool descriptions: %s", operational, combined)
		}
	}
}

func TestNexusUnavailableHidesWorkflowTemplateCapability(t *testing.T) {
	cfg := config.Config{
		AgentDockDefaultDir: t.TempDir(),
		AgentDockHome:       filepath.Join(t.TempDir(), ".agentdock"),
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}

	toolNames := strings.Join(rt.ToolNames(), "\n")
	for _, hiddenTool := range []string{"workflow_template_manage", "evolve", "recall_bootstrap", "recall_search", "recall_read", "recall_write", "recall_maintain"} {
		if strings.Contains(toolNames, hiddenTool) {
			t.Fatalf("%s should be hidden without Nexus: %s", hiddenTool, toolNames)
		}
	}
	if !strings.Contains(toolNames, "task_manage") {
		t.Fatalf("task_manage should remain available without Nexus: %s", toolNames)
	}

	result, err := rt.Call(context.Background(), "agentdock_context", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var got capabilityContext
	if err := remarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.WorkflowTemplates) != 0 || got.Recall != nil {
		t.Fatalf("Nexus-only context should be absent without Nexus: %#v", got)
	}
	rules := strings.Join(got.Rules, "\n")
	for _, hidden := range []string{"workflow_template_manage", "source_template_ids", "recall_search", "recall_read"} {
		if strings.Contains(rules, hidden) {
			t.Fatalf("context rules should hide %q without Nexus: %s", hidden, rules)
		}
	}
	if !strings.Contains(rules, "task_manage") {
		t.Fatalf("context should keep task_manage without Nexus: %s", rules)
	}
}

func TestNexusAvailableExposesWorkflowAndRecallContext(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)

	toolNames := strings.Join(rt.ToolNames(), "\n")
	if !strings.Contains(toolNames, "workflow_template_manage") {
		t.Fatalf("workflow_template_manage should be available with Nexus: %s", toolNames)
	}

	result, err := rt.Call(context.Background(), "agentdock_context", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var got capabilityContext
	if err := remarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	if got.Recall == nil || !got.Recall.Enabled {
		t.Fatalf("Nexus context should declare Recall enabled: %#v", got.Recall)
	}
	rules := strings.Join(got.Rules, "\n")
	for _, want := range []string{"workflow_template_manage match", "source_template_ids", "recall_search", "recall_read"} {
		if !strings.Contains(rules, want) {
			t.Fatalf("context rules missing %q with Nexus: %s", want, rules)
		}
	}
}

func TestAgentDockLocalContextSkipsSharedNexusLookups(t *testing.T) {
	root := t.TempDir()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected Nexus request", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	cfg := config.Config{
		AgentDockDefaultDir: root,
		AgentDockHome:       filepath.Join(root, ".agentdock"),
		NexusEndpoint:       server.URL,
		NexusDeviceToken:    "test-device-token",
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	result, err := rt.AgentDockLocalContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("local-only context made %d Nexus requests", got)
	}
	var contextResult capabilityContext
	if err := remarshal(result, &contextResult); err != nil {
		t.Fatal(err)
	}
	if contextResult.WorkflowTemplates == nil || len(contextResult.WorkflowTemplates) != 0 || contextResult.Recall != nil {
		t.Fatalf("local-only context leaked shared Nexus data: %#v", contextResult)
	}
	rules := strings.Join(contextResult.Rules, "\n")
	for _, sharedRule := range []string{"workflow_template_manage", "source_template_ids", "recall_search", "recall_read", "private_note_manage"} {
		if strings.Contains(rules, sharedRule) {
			t.Fatalf("local-only context leaked shared rule %q: %s", sharedRule, rules)
		}
	}
	if !strings.Contains(rules, "task_manage checkpoint") {
		t.Fatalf("local-only context lost device rule: %s", rules)
	}
}

func TestCapabilitySkillItemExposesOnlyLightweightIndexFields(t *testing.T) {
	data, err := json.Marshal(capabilitySkillItem{
		Name:        "desktop",
		Description: "Desktop automation.",
		File:        "skill://desktop/SKILL.md",
		Bundled:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{`"name"`, `"description"`, `"file"`, `"bundled"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("Skill index JSON missing %s: %s", want, text)
		}
	}
	for _, unwanted := range []string{`"active_version"`, `"updated_at"`, `"operation_count"`, `"version"`, `"path"`, `"manifest"`} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("Skill index JSON should not expose %s: %s", unwanted, text)
		}
	}
}

func installDocumentSkillForTest(t *testing.T, rt *Runtime, name, version, description string) string {
	t.Helper()
	packageDir, err := rt.skills.InstalledPath(name, version)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(packageDir, 0o700); err != nil {
		t.Fatal(err)
	}
	doc := "---\nname: " + name + "\ndescription: " + description + "\nversion: " + version + "\n---\n\n# Test Skill\n\nFollow the workflow.\n"
	if err := os.WriteFile(filepath.Join(packageDir, "SKILL.md"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rt.skills.Activate(context.Background(), name, version); err != nil {
		t.Fatal(err)
	}
	return packageDir
}

func TestSkillCapabilityIndexOmitsLegacyExecutableSkills(t *testing.T) {
	cfg := config.Config{
		AgentDockDefaultDir: t.TempDir(),
		AgentDockHome:       filepath.Join(t.TempDir(), ".agentdock"),
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	installDocumentSkillForTest(t, rt, "document-skill", "1.0.0", "A document-only Skill.")

	legacyDir, err := rt.skills.InstalledPath("legacy-skill", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyDoc := "---\nname: legacy-skill\ndescription: A legacy executable Skill.\n---\n\n# Legacy\n"
	if err := os.WriteFile(filepath.Join(legacyDir, "SKILL.md"), []byte(legacyDoc), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := `apiVersion: agentdock.dev/v1
kind: Skill
metadata:
  name: legacy-skill
  version: 1.0.0
  displayName: Legacy Skill
  description: A legacy executable Skill.
spec:
  entrypoint: run.sh
  operations:
    - name: run
      description: Run the legacy entrypoint.
      inputSchema: {"type":"object","additionalProperties":false}
      outputSchema: {"type":"object","additionalProperties":true}
      timeoutSeconds: 5
  compatibility:
    platforms: [darwin]
    architectures: [arm64]
    agentdock: ">=1.0.0"
  permissions:
    filesystem: []
    network: []
    commands: []
`
	if err := os.WriteFile(filepath.Join(legacyDir, "agentdock.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "run.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := rt.skills.Activate(context.Background(), "legacy-skill", "1.0.0"); err != nil {
		t.Fatal(err)
	}

	items, err := rt.skillCapabilityIndex()
	if err != nil {
		t.Fatalf("skillCapabilityIndex error = %v", err)
	}
	if len(items) != 1 || items[0].Name != "document-skill" {
		t.Fatalf("legacy executable Skill should be omitted from model index: %#v", items)
	}
}

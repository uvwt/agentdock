package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/skillstate"
)

func TestAgentDockContextToolReturnsRuntimeIndex(t *testing.T) {
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
	if result["ok"] != true {
		t.Fatalf("agentdock_context ok = %#v, want true", result["ok"])
	}
	contextText, _ := result["context"].(string)
	if contextText == "" {
		t.Fatalf("agentdock_context returned empty context: %#v", result)
	}
	for _, want := range []string{"demo-skill", "Use this Skill for context index tests.", "skill://demo-skill/SKILL.md"} {
		if !strings.Contains(contextText, want) {
			t.Fatalf("context missing %q: %s", want, contextText)
		}
	}
	for _, removed := range []string{"skill_read", "skill_run"} {
		if strings.Contains(contextText, removed) {
			t.Fatalf("context still references removed model-facing tool %q: %s", removed, contextText)
		}
	}

	skills, ok := result["skills"].([]capabilitySkillItem)
	if !ok || len(skills) != 1 {
		t.Fatalf("unexpected lightweight Skill index: %#v", result["skills"])
	}
	if got := skills[0]; got.Name != "demo-skill" || got.Description == "" || got.File != "skill://demo-skill/SKILL.md" {
		t.Fatalf("unexpected Skill index item: %#v", got)
	}
	for _, name := range []string{"generated_at", "summary", "counts", "base_tools", "task_templates", "memory", "rules"} {
		if _, ok := result[name]; ok {
			t.Fatalf("agentdock_context exposed unexpected field %q", name)
		}
	}
}

func TestCapabilitySkillItemExposesOnlyLightweightIndexFields(t *testing.T) {
	data, err := json.Marshal(capabilitySkillItem{
		Name:        "desktop",
		Description: "Desktop automation.",
		File:        "skill://desktop/SKILL.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{`"name"`, `"description"`, `"file"`} {
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
	packageDir, err := rt.skills.state.InstalledPath(name, version)
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
	if err := rt.skills.state.Activate(context.Background(), name, version, skillstate.ChannelStable); err != nil {
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

	legacyDir, err := rt.skills.state.InstalledPath("legacy-skill", "1.0.0")
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
	if err := rt.skills.state.Activate(context.Background(), "legacy-skill", "1.0.0", skillstate.ChannelStable); err != nil {
		t.Fatal(err)
	}

	items, _, errText := rt.skillCapabilityIndex()
	if errText != "" {
		t.Fatalf("skillCapabilityIndex error = %s", errText)
	}
	if len(items) != 1 || items[0].Name != "document-skill" {
		t.Fatalf("legacy executable Skill should be omitted from model index: %#v", items)
	}
}

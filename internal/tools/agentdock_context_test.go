package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/skillruntime"
)

func TestSkillManifestDescriptionReadsNestedManifest(t *testing.T) {
	description := skillManifestDescription(Result{"manifest": skillruntime.Manifest{
		Metadata: skillruntime.Metadata{
			Name:        "demo-skill",
			DisplayName: "Demo Skill",
			Description: "Use this demo Skill for capability context tests.",
		},
	}})

	if description != "Use this demo Skill for capability context tests." {
		t.Fatalf("description not read from metadata.description: %q", description)
	}
}

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
	for _, name := range []string{"generated_at", "summary", "counts", "base_tools", "skills", "task_templates", "memory", "rules"} {
		if _, ok := result[name]; ok {
			t.Fatalf("agentdock_context should only expose context payload; unexpected field %q", name)
		}
	}
}

func TestAgentDockContextItemsExposeOnlyNameAndDescription(t *testing.T) {
	items := []any{
		capabilityBaseToolItem{Name: "exec_command", Description: "Run commands."},
		capabilitySkillItem{Name: "desktop", Description: "Desktop automation."},
		capabilityTemplateItem{Name: "deploy", Description: "Deployment workflow."},
		capabilityMemoryItem{Name: "project/rules", Description: "Project rules."},
	}
	data, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{`"name"`, `"description"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("capability item json missing %s: %s", want, text)
		}
	}
	for _, unwanted := range []string{`"summary"`, `"skill"`, `"active_version"`, `"updated_at"`, `"operation_count"`, `"version"`, `"path"`, `"title"`, `"excerpt"`} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("capability item json should not expose %s: %s", unwanted, text)
		}
	}
}

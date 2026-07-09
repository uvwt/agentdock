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

func TestCapabilityContextToolReturnsRuntimeIndex(t *testing.T) {
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

	result, err := rt.Call(context.Background(), "capability_context", map[string]any{"refresh": true})
	if err != nil {
		t.Fatalf("capability_context call failed: %v", err)
	}
	if result["ok"] != true {
		t.Fatalf("capability_context ok = %#v, want true", result["ok"])
	}
	if result["refreshed"] != true {
		t.Fatalf("capability_context refreshed = %#v, want true", result["refreshed"])
	}
	contextText, _ := result["context"].(string)
	if contextText == "" {
		t.Fatalf("capability_context returned empty context: %#v", result)
	}
	summary, _ := result["summary"].(string)
	if summary == "" || summary == contextText {
		t.Fatalf("capability_context summary should be compact and distinct from context: %#v", result)
	}
	baseTools, ok := result["base_tools"].(capabilityBaseToolBlock)
	if !ok {
		t.Fatalf("capability_context base_tools has unexpected shape: %#v", result["base_tools"])
	}
	if len(baseTools.Items) == 0 {
		t.Fatalf("capability_context base_tools missing items: %#v", baseTools)
	}
	if strings.Contains(baseTools.Summary, baseTools.Items[0].Description) {
		t.Fatalf("base_tools summary should not duplicate item details: %#v", baseTools)
	}
}

func TestCapabilityContextItemsExposeOnlyNameAndDescription(t *testing.T) {
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

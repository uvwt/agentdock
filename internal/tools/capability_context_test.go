package tools

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/skillruntime"
)

func TestMergeSkillManifestSummaryReadsNestedManifest(t *testing.T) {
	item := capabilitySkillItem{Skill: "demo-skill"}
	mergeSkillManifestSummary(&item, Result{"manifest": skillruntime.Manifest{
		Metadata: skillruntime.Metadata{
			Name:        "demo-skill",
			DisplayName: "Demo Skill",
			Description: "Use this demo Skill for capability context tests.",
		},
		Spec: skillruntime.Spec{Operations: []skillruntime.Operation{
			{Name: "echo", Description: "Echo input"},
			{Name: "status", Description: "Check status"},
		}},
	}})

	if item.Summary != "Use this demo Skill for capability context tests." {
		t.Fatalf("summary not read from metadata.description: %#v", item)
	}
	if len(item.Operations) != 2 || item.Operations[0] != "echo" || item.Operations[1] != "status" {
		t.Fatalf("operations not read from spec.operations: %#v", item)
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
}

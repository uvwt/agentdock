package tools

import (
	"testing"

	"github.com/uvwt/agentdock/internal/skillruntime"
)

func TestMergeSkillManifestSummaryReadsNestedManifest(t *testing.T) {
	item := map[string]any{"skill": "demo-skill"}
	mergeSkillManifestSummary(item, Result{"manifest": skillruntime.Manifest{
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

	if item["summary"] != "Use this demo Skill for capability context tests." {
		t.Fatalf("summary not read from metadata.description: %#v", item)
	}
	ops, ok := item["operations"].([]string)
	if !ok || len(ops) != 2 || ops[0] != "echo" || ops[1] != "status" {
		t.Fatalf("operations not read from spec.operations: %#v", item)
	}
}

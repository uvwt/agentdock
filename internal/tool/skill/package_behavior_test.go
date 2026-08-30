package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/uvwt/agentdock/internal/envstore"
	skills "github.com/uvwt/agentdock/internal/skill"
)

func TestSkillPackageValidateInstallInspectAndList(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "demo-skill")
	if err := os.MkdirAll(filepath.Join(source, "references"), 0o700); err != nil {
		t.Fatal(err)
	}
	doc := `---
name: demo-skill
description: Use this Skill to test document-only package management.
version: 1.0.0
---

# Demo Skill

Read references and call existing tools.
`
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "references", "guide.md"), []byte("# Guide\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	rt, _ := newSkillTestServiceAtRoot(t, root)

	validated, err := rt.packageTest(context.Background(), map[string]any{
		"action": "validate",
		"source": "demo-skill",
	})
	if err != nil {
		t.Fatal(err)
	}
	if validated["valid"] != true {
		t.Fatalf("validation failed: %#v", validated)
	}
	issues, ok := validated["issues"].([]skills.ValidateIssue)
	if !ok || len(issues) != 0 {
		t.Fatalf("successful validation issues = %#v, want []", validated["issues"])
	}
	document, ok := validated["document"].(skills.SkillDocument)
	if !ok || document.Name != "demo-skill" || document.Version != "1.0.0" {
		t.Fatalf("unexpected document: %#v", validated["document"])
	}

	installed, err := rt.packageTest(context.Background(), map[string]any{
		"action":   "install",
		"source":   "demo-skill",
		"activate": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, ok := installed["result"].(skills.InstallResult)
	if !ok || result.Skill != "demo-skill" || !result.Activated {
		t.Fatalf("unexpected install result: %#v", installed["result"])
	}

	environment, err := rt.packageTest(context.Background(), map[string]any{
		"action": "env_list", "skill": "demo-skill",
	})
	if err != nil {
		t.Fatal(err)
	}
	environmentItems, ok := environment["items"].([]envstore.Entry)
	if !ok || len(environmentItems) != 0 {
		t.Fatalf("empty Skill environment items = %#v, want []", environment["items"])
	}
	if err := rt.state.ReplaceBundledSkills(context.Background(), []string{"demo-skill"}); err != nil {
		t.Fatal(err)
	}
	inspected, err := rt.inspectTest(map[string]any{"skill": "demo-skill"})
	if err != nil {
		t.Fatal(err)
	}
	if inspected["version"] != "1.0.0" {
		t.Fatalf("unexpected inspected version: %#v", inspected)
	}
	if inspected["bundled"] != true {
		t.Fatalf("inspect did not expose bundled Skill: %#v", inspected)
	}
	listed, err := rt.list()
	if err != nil {
		t.Fatal(err)
	}
	if listed["count"] != 1 {
		t.Fatalf("unexpected list: %#v", listed)
	}
	items, ok := listed["skills"].([]map[string]any)
	if !ok || len(items) != 1 || items[0]["bundled"] != true {
		t.Fatalf("list did not expose bundled Skill: %#v", listed)
	}
}

func TestSkillPackageRejectsLegacyManifest(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "legacy-skill")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("---\nname: legacy-skill\ndescription: Legacy package.\nversion: 1.0.0\n---\n\n# Legacy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "agentdock.yaml"), []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt, _ := newSkillTestServiceAtRoot(t, root)
	validated, err := rt.packageTest(context.Background(), map[string]any{"action": "validate", "source": "legacy-skill"})
	if err != nil {
		t.Fatal(err)
	}
	if validated["valid"] != false {
		t.Fatalf("legacy package unexpectedly valid: %#v", validated)
	}
	issues, ok := validated["issues"].([]skills.ValidateIssue)
	if !ok || len(issues) == 0 || issues[0].Stage != "package.legacy_manifest" {
		t.Fatalf("unexpected legacy validation issues: %#v", validated["issues"])
	}
}

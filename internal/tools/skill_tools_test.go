package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/skills"
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

	cfg := config.Config{AgentDockDefaultDir: root, AgentDockHome: filepath.Join(root, ".agentdock")}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}

	validated, err := rt.Call(context.Background(), "skill_package", map[string]any{
		"action": "validate",
		"source": "demo-skill",
	})
	if err != nil {
		t.Fatal(err)
	}
	if validated["valid"] != true {
		t.Fatalf("validation failed: %#v", validated)
	}
	document, ok := validated["document"].(skills.SkillDocument)
	if !ok || document.Name != "demo-skill" || document.Version != "1.0.0" {
		t.Fatalf("unexpected document: %#v", validated["document"])
	}

	installed, err := rt.Call(context.Background(), "skill_package", map[string]any{
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
	inspected, err := rt.skillInspect(map[string]any{"skill": "demo-skill"})
	if err != nil {
		t.Fatal(err)
	}
	if inspected["version"] != "1.0.0" {
		t.Fatalf("unexpected inspected version: %#v", inspected)
	}
	listed, err := rt.skillList()
	if err != nil {
		t.Fatal(err)
	}
	if listed["count"] != 1 {
		t.Fatalf("unexpected list: %#v", listed)
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
	cfg := config.Config{AgentDockDefaultDir: root, AgentDockHome: filepath.Join(root, ".agentdock")}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	validated, err := rt.Call(context.Background(), "skill_package", map[string]any{"action": "validate", "source": "legacy-skill"})
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

func TestRemovedSkillRuntimeToolsAreUnavailable(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{AgentDockDefaultDir: root, AgentDockHome: filepath.Join(root, ".agentdock")}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"skill_read", "skill_run", "skill_env_manage"} {
		_, err := rt.Call(context.Background(), name, map[string]any{})
		var toolErr *ToolError
		if !errors.As(err, &toolErr) || toolErr.Code != "UNKNOWN_TOOL" {
			t.Fatalf("%s should be unavailable, got %T %v", name, err, err)
		}
	}
}

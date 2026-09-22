package skill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/envstore"
	skills "github.com/uvwt/agentdock/internal/skill"
)

func TestSkillManageInstallUpdateAndEnvironment(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "demo-skill")
	writeToolSkillPackage(t, source, "First")

	runtime, _ := newSkillTestServiceAtRoot(t, root)
	installed, err := runtime.manageTest(context.Background(), map[string]any{
		"action": "install", "source": "demo-skill",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, ok := installed["result"].(skills.InstallResult)
	if !ok || result.Skill != "demo-skill" || !result.Changed || result.ContentDigest == "" {
		t.Fatalf("unexpected install result: %#v", installed)
	}
	if result.Path != filepath.Join(root, ".agentdock", "skills", "demo-skill") {
		t.Fatalf("managed Skill path = %q", result.Path)
	}

	repeat, err := runtime.manageTest(context.Background(), map[string]any{"action": "install", "source": "demo-skill"})
	if err != nil {
		t.Fatal(err)
	}
	if repeat["changed"] != false {
		t.Fatalf("repeat install should be no-op: %#v", repeat)
	}

	dataPath := filepath.Join(root, ".agentdock", "data", "skills", "demo-skill", "state.json")
	if err := os.MkdirAll(filepath.Dir(dataPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataPath, []byte("persistent"), 0o600); err != nil {
		t.Fatal(err)
	}

	writeToolSkillPackage(t, source, "Second")
	updated, err := runtime.manageTest(context.Background(), map[string]any{"action": "install", "source": "demo-skill"})
	if err != nil {
		t.Fatal(err)
	}
	if updated["changed"] != true || updated["content_digest"] == result.ContentDigest {
		t.Fatalf("changed install did not update current content: %#v", updated)
	}
	if data, err := os.ReadFile(dataPath); err != nil || string(data) != "persistent" {
		t.Fatalf("managed Skill data changed during content update: data=%q err=%v", data, err)
	}

	secret := "configured-value"
	if _, err := runtime.manageTest(context.Background(), map[string]any{
		"action": "env_set", "skill": "demo-skill", "key": "TOKEN", "value": secret,
	}); err != nil {
		t.Fatal(err)
	}
	environment, err := runtime.manageTest(context.Background(), map[string]any{
		"action": "env_list", "skill": "demo-skill",
	})
	if err != nil {
		t.Fatal(err)
	}
	items, ok := environment["items"].([]envstore.Entry)
	if !ok || len(items) != 1 || items[0].Key != "TOKEN" || !items[0].Configured {
		t.Fatalf("unexpected environment list: %#v", environment)
	}
	text := strings.TrimSpace(items[0].Key)
	if strings.Contains(text, secret) {
		t.Fatalf("env_list leaked secret value: %#v", environment)
	}
}

func TestSkillManageRejectsSettingReservedDataDirButAllowsCleanup(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "demo-skill")
	writeToolSkillPackage(t, source, "Demo")
	runtime, _ := newSkillTestServiceAtRoot(t, root)
	if _, err := runtime.manageTest(context.Background(), map[string]any{"action": "install", "source": "demo-skill"}); err != nil {
		t.Fatal(err)
	}

	if _, err := runtime.manageTest(context.Background(), map[string]any{
		"action": "env_set", "skill": "demo-skill", "key": "SKILL_DATA_DIR", "value": "/tmp/override",
	}); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("reserved env_set error = %v", err)
	}

	scope := envstore.Scope{Kind: envstore.ScopeSkill, Name: "demo-skill"}
	if err := runtime.envs.Set(scope, "SKILL_DATA_DIR", "/tmp/legacy"); err != nil {
		t.Fatal(err)
	}
	result, err := runtime.manageTest(context.Background(), map[string]any{
		"action": "env_unset", "skill": "demo-skill", "key": "SKILL_DATA_DIR",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["removed"] != true {
		t.Fatalf("reserved env_unset did not clean stale value: %#v", result)
	}
}

func TestSkillManageRejectsRemovedVersionActions(t *testing.T) {
	runtime, _ := newSkillTestService(t)
	for _, action := range []string{"validate", "list", "inspect", "update", "activate", "rollback", "uninstall"} {
		if _, err := runtime.manageTest(context.Background(), map[string]any{"action": action}); err == nil {
			t.Fatalf("removed skill_manage action %q unexpectedly succeeded", action)
		}
	}
}

func TestRuntimeSkillCapabilityUsesExactManagedReference(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "demo-skill")
	writeToolSkillPackage(t, source, "Managed")
	runtime, _ := newSkillTestServiceAtRoot(t, root)
	if _, err := runtime.manageTest(context.Background(), map[string]any{"action": "install", "source": "demo-skill"}); err != nil {
		t.Fatal(err)
	}

	items, err := runtime.CapabilityItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("capability items = %#v", items)
	}
	item := items[0]
	if item.SkillRef != "skill://managed/demo-skill" || item.File != "skill://managed/demo-skill/SKILL.md" ||
		item.SourceType != "managed" || item.SourceID != "demo-skill" || item.ContentDigest == "" {
		t.Fatalf("managed capability provenance = %#v", item)
	}
}

func TestWorkspaceSkillRefMustBeIssuedByCurrentRuntime(t *testing.T) {
	runtime, _ := newSkillTestService(t)
	workspaceRoot := t.TempDir()
	packageRoot := filepath.Join(workspaceRoot, ".agents", "skills", "workspace-skill")
	if err := os.MkdirAll(packageRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	document := "---\nname: workspace-skill\ndescription: Workspace Skill.\n---\n\n# Workspace\n"
	if err := os.WriteFile(filepath.Join(packageRoot, "SKILL.md"), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}

	skillRef, sourceID, err := runtime.WorkspaceSkillRef(workspaceRoot, "workspace-skill")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(skillRef, workspaceRoot) || sourceID == "" {
		t.Fatalf("workspace skill_ref leaks host path or source id is empty: ref=%q id=%q", skillRef, sourceID)
	}
	secondRef, secondID, err := runtime.WorkspaceSkillRef(workspaceRoot, "workspace-skill")
	if err != nil {
		t.Fatal(err)
	}
	if secondRef != skillRef || secondID != sourceID {
		t.Fatalf("same runtime/workspace did not reuse source id: first=%q/%q second=%q/%q", skillRef, sourceID, secondRef, secondID)
	}

	resolved, release, err := runtime.Acquire(context.Background(), skillRef)
	if err != nil {
		t.Fatal(err)
	}
	release()
	resolvedInfo, err := os.Stat(resolved.Root)
	if err != nil {
		t.Fatal(err)
	}
	packageInfo, err := os.Stat(packageRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(resolvedInfo, packageInfo) || resolved.SourceType != "workspace" {
		t.Fatalf("workspace skill_ref resolved incorrectly: %#v", resolved)
	}

	forged := "skill://workspace/AAAAAAAAAAAAAAAAAAAAAAAA/workspace-skill"
	if _, _, err := runtime.Acquire(context.Background(), forged); err == nil || !strings.Contains(err.Error(), "was not issued") {
		t.Fatalf("forged workspace skill_ref was accepted: %v", err)
	}
	forgedName := "skill://workspace/" + sourceID + "/other-skill"
	if _, _, err := runtime.Acquire(context.Background(), forgedName); err == nil || !strings.Contains(err.Error(), "was not issued") {
		t.Fatalf("unissued workspace Skill name was accepted for a valid source id: %v", err)
	}
}

func writeToolSkillPackage(t *testing.T, root, heading string) {
	t.Helper()
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "references"), 0o700); err != nil {
		t.Fatal(err)
	}
	document := "---\nname: demo-skill\ndescription: Test managed Skill.\n---\n\n# " + heading + "\n"
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "references", "guide.md"), []byte("# Guide\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

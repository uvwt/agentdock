package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/uvwt/agentdock/internal/envstore"
	skills "github.com/uvwt/agentdock/internal/skill"
)

func TestSkillPackageUninstallPreservesEnvironmentAndData(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "demo-skill")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	document := "---\nname: demo-skill\ndescription: Demo Skill.\nversion: 1.0.0\n---\n\n# Demo\n"
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}

	runtime, _ := newSkillTestServiceAtRoot(t, root)
	if _, err := runtime.packageTest(context.Background(), map[string]any{
		"action": "install", "source": "demo-skill", "activate": true,
	}); err != nil {
		t.Fatal(err)
	}
	secret := "configured-value"
	if _, err := runtime.packageTest(context.Background(), map[string]any{
		"action": "env_set", "skill": "demo-skill", "key": "TOKEN", "value": secret,
	}); err != nil {
		t.Fatal(err)
	}
	dataPath := filepath.Join(root, ".agentdock", "skill-data", "demo-skill", "state.json")
	if err := os.MkdirAll(filepath.Dir(dataPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataPath, []byte("persistent"), 0o600); err != nil {
		t.Fatal(err)
	}

	response, err := runtime.packageTest(context.Background(), map[string]any{
		"action": "uninstall", "skill": "demo-skill",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, ok := response["result"].(skills.UninstallResult)
	if !ok || result.Skill != "demo-skill" || len(result.RemovedVersions) != 1 || result.RemovedVersions[0] != "1.0.0" ||
		!result.PreservedEnvironment || !result.PreservedData {
		t.Fatalf("unexpected uninstall result: %#v", response)
	}
	if names, err := runtime.state.ListSkills(); err != nil || len(names) != 0 {
		t.Fatalf("installed Skills after uninstall = %#v, err=%v", names, err)
	}
	environment, err := runtime.packageTest(context.Background(), map[string]any{
		"action": "env_list", "skill": "demo-skill",
	})
	if err != nil {
		t.Fatal(err)
	}
	items, ok := environment["items"].([]envstore.Entry)
	if !ok || len(items) != 1 || items[0].Key != "TOKEN" || !items[0].Configured {
		t.Fatalf("Skill environment was not preserved: %#v", environment)
	}
	if data, err := os.ReadFile(dataPath); err != nil || string(data) != "persistent" {
		t.Fatalf("Skill data was not preserved: data=%q err=%v", data, err)
	}
}

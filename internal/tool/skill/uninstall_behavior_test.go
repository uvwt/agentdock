package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/uvwt/agentdock/internal/envstore"
	skills "github.com/uvwt/agentdock/internal/skill"
)

func TestSkillManageRemovePreservesEnvironmentAndDataByDefault(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "demo-skill")
	writeToolSkillPackage(t, source, "Demo")
	runtime, _ := newSkillTestServiceAtRoot(t, root)
	if _, err := runtime.manageTest(context.Background(), map[string]any{"action": "install", "source": "demo-skill"}); err != nil {
		t.Fatal(err)
	}
	secret := "configured-value"
	if _, err := runtime.manageTest(context.Background(), map[string]any{
		"action": "env_set", "skill": "demo-skill", "key": "TOKEN", "value": secret,
	}); err != nil {
		t.Fatal(err)
	}
	dataPath := filepath.Join(root, ".agentdock", "data", "skills", "demo-skill", "state.json")
	if err := os.MkdirAll(filepath.Dir(dataPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataPath, []byte("persistent"), 0o600); err != nil {
		t.Fatal(err)
	}

	response, err := runtime.manageTest(context.Background(), map[string]any{"action": "remove", "skill": "demo-skill"})
	if err != nil {
		t.Fatal(err)
	}
	result, ok := response["result"].(skills.RemoveResult)
	if !ok || result.Skill != "demo-skill" || !result.Removed || !result.PreservedEnvironment || !result.PreservedData {
		t.Fatalf("unexpected remove result: %#v", response)
	}
	if names, err := runtime.state.ListSkills(); err != nil || len(names) != 0 {
		t.Fatalf("installed Skills after remove = %#v, err=%v", names, err)
	}
	items, err := runtime.envs.List(envstore.Scope{Kind: envstore.ScopeSkill, Name: "demo-skill"})
	if err != nil || len(items) != 1 || items[0].Key != "TOKEN" || !items[0].Configured {
		t.Fatalf("Skill environment was not preserved: items=%#v err=%v", items, err)
	}
	if data, err := os.ReadFile(dataPath); err != nil || string(data) != "persistent" {
		t.Fatalf("Skill data was not preserved: data=%q err=%v", data, err)
	}
}

func TestSkillManageRemovePurgeDeletesEnvironmentAndData(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "demo-skill")
	writeToolSkillPackage(t, source, "Demo")
	runtime, _ := newSkillTestServiceAtRoot(t, root)
	if _, err := runtime.manageTest(context.Background(), map[string]any{"action": "install", "source": "demo-skill"}); err != nil {
		t.Fatal(err)
	}
	value := "secret"
	if _, err := runtime.manageTest(context.Background(), map[string]any{
		"action": "env_set", "skill": "demo-skill", "key": "TOKEN", "value": value,
	}); err != nil {
		t.Fatal(err)
	}
	dataPath := filepath.Join(root, ".agentdock", "data", "skills", "demo-skill")
	if err := os.MkdirAll(dataPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataPath, "state.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	response, err := runtime.manageTest(context.Background(), map[string]any{
		"action": "remove", "skill": "demo-skill", "purge": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response["purged"] != true {
		t.Fatalf("purge result = %#v", response)
	}
	envPath, err := runtime.envs.Path(envstore.Scope{Kind: envstore.ScopeSkill, Name: "demo-skill"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(envPath); !os.IsNotExist(err) {
		t.Fatalf("Skill environment survived purge: %v", err)
	}
	if _, err := os.Stat(dataPath); !os.IsNotExist(err) {
		t.Fatalf("Skill data survived purge: %v", err)
	}
}

func TestSkillManagePurgeAfterKeepRemovalIsIdempotent(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "demo-skill")
	writeToolSkillPackage(t, source, "Demo")
	runtime, _ := newSkillTestServiceAtRoot(t, root)
	if _, err := runtime.manageTest(context.Background(), map[string]any{"action": "install", "source": "demo-skill"}); err != nil {
		t.Fatal(err)
	}
	value := "secret"
	if _, err := runtime.manageTest(context.Background(), map[string]any{
		"action": "env_set", "skill": "demo-skill", "key": "TOKEN", "value": value,
	}); err != nil {
		t.Fatal(err)
	}
	dataPath := filepath.Join(root, ".agentdock", "data", "skills", "demo-skill")
	if err := os.MkdirAll(dataPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataPath, "state.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.manageTest(context.Background(), map[string]any{
		"action": "remove", "skill": "demo-skill",
	}); err != nil {
		t.Fatal(err)
	}

	response, err := runtime.manageTest(context.Background(), map[string]any{
		"action": "remove", "skill": "demo-skill", "purge": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response["removed"] != false || response["purged"] != true {
		t.Fatalf("idempotent purge result = %#v", response)
	}
	envPath, err := runtime.envs.Path(envstore.Scope{Kind: envstore.ScopeSkill, Name: "demo-skill"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(envPath); !os.IsNotExist(err) {
		t.Fatalf("Skill environment survived delayed purge: %v", err)
	}
	if _, err := os.Stat(dataPath); !os.IsNotExist(err) {
		t.Fatalf("Skill data survived delayed purge: %v", err)
	}
}

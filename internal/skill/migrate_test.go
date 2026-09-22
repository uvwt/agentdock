package skill

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	skillstate "github.com/uvwt/agentdock/internal/skill/state"
)

func TestMigrateLegacyLayoutKeepsOnlyActiveContentAndMovesData(t *testing.T) {
	home := t.TempDir()
	managerState, err := newStateAt(filepath.Join(home, "skills"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(managerState)
	if err != nil {
		t.Fatal(err)
	}

	legacyStore := filepath.Join(home, "skill-store")
	for version, heading := range map[string]string{"1.0.0": "Old", "2.0.0": "Active"} {
		root := filepath.Join(legacyStore, "installed", "demo-skill", version)
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		doc := "---\nname: demo-skill\ndescription: Legacy package.\nversion: " + version + "\n---\n\n# " + heading + "\n"
		if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".agentdock-install.json"), []byte(`{"digest":"legacy"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(legacyStore, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	stateJSON, _ := json.Marshal(map[string]any{"active_version": "2.0.0", "history": []string{"1.0.0"}})
	if err := os.WriteFile(filepath.Join(legacyStore, "state", "demo-skill.json"), stateJSON, 0o600); err != nil {
		t.Fatal(err)
	}

	legacyData := filepath.Join(home, "skill-data", "demo-skill")
	if err := os.MkdirAll(legacyData, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyData, "state.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := MigrateLegacyLayout(context.Background(), home, manager)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.MigratedSkills) != 1 || result.MigratedSkills[0] != "demo-skill" {
		t.Fatalf("migrated Skills = %#v", result.MigratedSkills)
	}
	if len(result.MigratedData) != 1 || result.MigratedData[0] != "demo-skill" {
		t.Fatalf("migrated data = %#v", result.MigratedData)
	}
	current, err := os.ReadFile(filepath.Join(home, "skills", "demo-skill", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(current), "# Active") || strings.Contains(string(current), "# Old") {
		t.Fatalf("migration did not select active legacy content: %s", current)
	}
	if _, err := os.Stat(filepath.Join(home, "skills", "demo-skill", ".agentdock-install.json")); !os.IsNotExist(err) {
		t.Fatalf("legacy install receipt leaked into current managed content: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "skills", "demo-skill", "2.0.0")); !os.IsNotExist(err) {
		t.Fatalf("migration preserved a version directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "data", "skills", "demo-skill", "state.json")); err != nil {
		t.Fatalf("legacy data was not moved: %v", err)
	}
	for _, legacy := range []string{"skill-store", "skill-data"} {
		if _, err := os.Stat(filepath.Join(home, legacy)); !os.IsNotExist(err) {
			t.Fatalf("legacy root %s still participates in runtime layout: %v", legacy, err)
		}
	}
	if result.BackupPath == "" {
		t.Fatal("one-time migration did not archive legacy roots")
	}
	if _, err := os.Stat(filepath.Join(result.BackupPath, "skill-store")); err != nil {
		t.Fatalf("legacy store backup missing: %v", err)
	}
}

func newStateAt(path string) (*skillstate.Store, error) {
	return skillstate.New(path)
}

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

func TestMigrateLegacyLayoutPreparesRollbackSafeContentAndFinalizes(t *testing.T) {
	home, manager := legacyMigrationFixture(t)

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
	if result.PendingPath == "" {
		t.Fatal("migration did not persist pending transaction marker")
	}
	if _, err := os.Stat(result.PendingPath); err != nil {
		t.Fatalf("pending migration marker missing: %v", err)
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
		t.Fatalf("legacy data was not copied: %v", err)
	}

	// Prepare is deliberately non-destructive. An old updater can still restore
	// the old binary at this point without understanding the new migration model.
	for _, legacy := range []string{"skill-store", "skill-data"} {
		if _, err := os.Stat(filepath.Join(home, legacy)); err != nil {
			t.Fatalf("legacy rollback root %s was removed before outer commit: %v", legacy, err)
		}
	}
	legacyState, err := os.ReadFile(filepath.Join(home, "skill-data", "demo-skill", "state.json"))
	if err != nil || string(legacyState) != "{}\n" {
		t.Fatalf("legacy Skill data changed during prepare: data=%q err=%v", legacyState, err)
	}

	// Re-entry before the outer transaction commits must be idempotent and must
	// not turn a second process (for example skill bootstrap) into an early commit.
	repeat, err := MigrateLegacyLayout(context.Background(), home, manager)
	if err != nil {
		t.Fatal(err)
	}
	if repeat.PendingPath != result.PendingPath {
		t.Fatalf("pending migration changed across re-entry: %q != %q", repeat.PendingPath, result.PendingPath)
	}
	for _, legacy := range []string{"skill-store", "skill-data"} {
		if _, err := os.Stat(filepath.Join(home, legacy)); err != nil {
			t.Fatalf("legacy rollback root %s disappeared on re-entry: %v", legacy, err)
		}
	}

	backup, err := FinalizeLegacyMigration(home)
	if err != nil {
		t.Fatal(err)
	}
	if backup == "" {
		t.Fatal("finalize did not archive legacy roots")
	}
	if _, err := os.Stat(result.PendingPath); !os.IsNotExist(err) {
		t.Fatalf("pending marker survived finalize: %v", err)
	}
	for _, legacy := range []string{"skill-store", "skill-data"} {
		if _, err := os.Stat(filepath.Join(home, legacy)); !os.IsNotExist(err) {
			t.Fatalf("legacy root %s still participates after commit: %v", legacy, err)
		}
		if _, err := os.Stat(filepath.Join(backup, legacy)); err != nil {
			t.Fatalf("legacy backup %s missing after commit: %v", legacy, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "data", "skills", "demo-skill", "state.json")); err != nil {
		t.Fatalf("current Skill data changed during finalize: %v", err)
	}
}

func TestPreparedLegacyMigrationKeepsOldLayoutUsableWhenOuterUpdateRollsBack(t *testing.T) {
	home, manager := legacyMigrationFixture(t)

	if _, err := MigrateLegacyLayout(context.Background(), home, manager); err != nil {
		t.Fatal(err)
	}

	selection, err := readLegacySelection(filepath.Join(home, "skill-store"), "demo-skill")
	if err != nil {
		t.Fatalf("old binary could not read legacy selection after prepare: %v", err)
	}
	if selection.ActiveVersion != "2.0.0" {
		t.Fatalf("legacy active version = %q, want 2.0.0", selection.ActiveVersion)
	}
	activeDoc, err := os.ReadFile(filepath.Join(home, "skill-store", "installed", "demo-skill", selection.ActiveVersion, "SKILL.md"))
	if err != nil {
		t.Fatalf("old binary could not read active legacy Skill after prepare: %v", err)
	}
	if !strings.Contains(string(activeDoc), "# Active") {
		t.Fatalf("legacy active content changed during prepare: %s", activeDoc)
	}
	if _, err := os.Stat(filepath.Join(home, "skill-data", "demo-skill", "state.json")); err != nil {
		t.Fatalf("old binary could not read legacy data after prepare: %v", err)
	}
}

func TestPendingLegacyMigrationRefreshesLegacyChangesAfterRollback(t *testing.T) {
	home, manager := legacyMigrationFixture(t)

	if _, err := MigrateLegacyLayout(context.Background(), home, manager); err != nil {
		t.Fatal(err)
	}
	legacyData := filepath.Join(home, "skill-data", "demo-skill", "state.json")
	currentData := filepath.Join(home, "data", "skills", "demo-skill", "state.json")
	if err := os.WriteFile(legacyData, []byte("{\"legacy\":2}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	legacyDoc := filepath.Join(home, "skill-store", "installed", "demo-skill", "2.0.0", "SKILL.md")
	if err := os.WriteFile(legacyDoc, []byte("---\nname: demo-skill\ndescription: Legacy package.\nversion: 2.0.0\n---\n\n# Active Retry\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Normal runtime construction must leave the prepared current copy alone.
	if _, err := MigrateLegacyLayout(context.Background(), home, manager); err != nil {
		t.Fatal(err)
	}
	beforeRefresh, err := os.ReadFile(currentData)
	if err != nil {
		t.Fatal(err)
	}
	if string(beforeRefresh) != "{}\n" {
		t.Fatalf("normal runtime refresh overwrote current Skill data: %q", beforeRefresh)
	}

	// Only an update/bootstrap retry may reconcile the pending bridge.
	if _, err := MigrateLegacyLayoutForUpdate(context.Background(), home, manager); err != nil {
		t.Fatal(err)
	}
	refreshedData, err := os.ReadFile(currentData)
	if err != nil {
		t.Fatal(err)
	}
	if string(refreshedData) != "{\"legacy\":2}\n" {
		t.Fatalf("pending retry did not refresh legacy data: %q", refreshedData)
	}
	refreshedDoc, err := os.ReadFile(filepath.Join(home, "skills", "demo-skill", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(refreshedDoc), "# Active Retry") {
		t.Fatalf("pending retry did not refresh legacy active Skill: %s", refreshedDoc)
	}
}

func TestPendingLegacyMigrationPreservesCurrentChangesFromSuccessfulUse(t *testing.T) {
	home, manager := legacyMigrationFixture(t)

	if _, err := MigrateLegacyLayout(context.Background(), home, manager); err != nil {
		t.Fatal(err)
	}
	currentData := filepath.Join(home, "data", "skills", "demo-skill", "state.json")
	if err := os.WriteFile(currentData, []byte("{\"current\":2}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	currentDoc := filepath.Join(home, "skills", "demo-skill", "SKILL.md")
	if err := os.WriteFile(currentDoc, []byte("---\nname: demo-skill\ndescription: Legacy package.\nversion: 2.0.0\n---\n\n# Current Use\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := MigrateLegacyLayoutForUpdate(context.Background(), home, manager); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(currentData)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{\"current\":2}\n" {
		t.Fatalf("pending migration overwrote current Skill data: %q", data)
	}
	doc, err := os.ReadFile(currentDoc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), "# Current Use") {
		t.Fatalf("pending migration overwrote current Skill content: %s", doc)
	}
}

func TestPendingLegacyMigrationRejectsDivergedOldAndCurrentData(t *testing.T) {
	home, manager := legacyMigrationFixture(t)

	if _, err := MigrateLegacyLayout(context.Background(), home, manager); err != nil {
		t.Fatal(err)
	}
	legacyData := filepath.Join(home, "skill-data", "demo-skill", "state.json")
	currentData := filepath.Join(home, "data", "skills", "demo-skill", "state.json")
	if err := os.WriteFile(legacyData, []byte("{\"legacy\":2}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(currentData, []byte("{\"current\":2}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := MigrateLegacyLayoutForUpdate(context.Background(), home, manager)
	if err == nil || !strings.Contains(err.Error(), "changed on both old and current layouts") {
		t.Fatalf("diverged pending migration error = %v", err)
	}
	legacyAfter, readErr := os.ReadFile(legacyData)
	if readErr != nil {
		t.Fatal(readErr)
	}
	currentAfter, readErr := os.ReadFile(currentData)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(legacyAfter) != "{\"legacy\":2}\n" || string(currentAfter) != "{\"current\":2}\n" {
		t.Fatalf("conflict handling changed data: legacy=%q current=%q", legacyAfter, currentAfter)
	}
}

func TestFinalizeLegacyMigrationRequiresPendingMarker(t *testing.T) {
	home, _ := legacyMigrationFixture(t)

	backup, err := FinalizeLegacyMigration(home)
	if err != nil {
		t.Fatal(err)
	}
	if backup != "" {
		t.Fatalf("finalize archived unprepared legacy roots: %s", backup)
	}
	for _, legacy := range []string{"skill-store", "skill-data"} {
		if _, err := os.Stat(filepath.Join(home, legacy)); err != nil {
			t.Fatalf("unprepared legacy root %s changed: %v", legacy, err)
		}
	}
}

func legacyMigrationFixture(t *testing.T) (string, *Manager) {
	t.Helper()
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
	return home, manager
}

func newStateAt(path string) (*skillstate.Store, error) {
	return skillstate.New(path)
}

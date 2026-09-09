package state

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestUninstallVersionPrunesHistoryAndKeepsActiveVersion(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	installStateTestVersions(t, store, "demo", "1.0.0", "2.0.0", "3.0.0")
	for _, version := range []string{"1.0.0", "2.0.0", "3.0.0"} {
		if err := store.Activate(context.Background(), "demo", version); err != nil {
			t.Fatal(err)
		}
	}

	result, err := store.Uninstall(context.Background(), "demo", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.RemovedVersions, []string{"1.0.0"}) || result.ActiveVersion != "3.0.0" {
		t.Fatalf("unexpected uninstall result: %#v", result)
	}
	installed, err := store.IsInstalled("demo", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if installed {
		t.Fatal("removed version is still installed")
	}
	selection, err := store.Snapshot("demo")
	if err != nil {
		t.Fatal(err)
	}
	if selection.ActiveVersion != "3.0.0" || !reflect.DeepEqual(selection.History, []string{"2.0.0"}) {
		t.Fatalf("selection after uninstall = %#v", selection)
	}
}

func TestUninstallRejectsActiveVersionWithoutChangingState(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	installStateTestVersions(t, store, "demo", "1.0.0", "2.0.0")
	if err := store.Activate(context.Background(), "demo", "1.0.0"); err != nil {
		t.Fatal(err)
	}

	_, err = store.Uninstall(context.Background(), "demo", "1.0.0")
	if err == nil || !strings.Contains(err.Error(), "active skill version") {
		t.Fatalf("active version uninstall error = %v", err)
	}
	installed, err := store.IsInstalled("demo", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Fatal("active version was removed after rejected uninstall")
	}
	selection, err := store.Snapshot("demo")
	if err != nil {
		t.Fatal(err)
	}
	if selection.ActiveVersion != "1.0.0" {
		t.Fatalf("active version changed after rejected uninstall: %#v", selection)
	}
}

func TestUninstallWholeSkillRemovesPackagesAndSelection(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	installStateTestVersions(t, store, "demo", "1.0.0", "2.0.0")
	if err := store.Activate(context.Background(), "demo", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := store.Activate(context.Background(), "demo", "2.0.0"); err != nil {
		t.Fatal(err)
	}

	result, err := store.Uninstall(context.Background(), "demo", "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.RemovedVersions, []string{"1.0.0", "2.0.0"}) || result.ActiveVersion != "2.0.0" {
		t.Fatalf("unexpected whole Skill uninstall result: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(store.Root(), "installed", "demo")); !os.IsNotExist(err) {
		t.Fatalf("installed Skill directory still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.Root(), "state", "demo.json")); !os.IsNotExist(err) {
		t.Fatalf("Skill selection state still exists: %v", err)
	}
	selection, err := store.Snapshot("demo")
	if err != nil {
		t.Fatal(err)
	}
	if selection.ActiveVersion != "" || len(selection.History) != 0 {
		t.Fatalf("selection after whole Skill uninstall = %#v", selection)
	}
}

func TestUninstallLastInactiveVersionRemovesEmptySkillEntry(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	installStateTestVersions(t, store, "demo", "1.0.0")

	if _, err := store.Uninstall(context.Background(), "demo", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	names, err := store.ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 0 {
		t.Fatalf("ListSkills() after removing last version = %#v, want empty", names)
	}
}

func installStateTestVersions(t *testing.T, store *Store, skill string, versions ...string) {
	t.Helper()
	for _, version := range versions {
		path, err := store.InstalledPath(skill, version)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

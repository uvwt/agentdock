package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	skillstate "github.com/uvwt/agentdock/internal/skill/state"
)

func TestActivateSwitchesToInstalledVersion(t *testing.T) {
	state, err := skillstate.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1.0.0", "1.1.0"} {
		path, err := state.InstalledPath("demo-skill", version)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		document := "---\nname: demo-skill\ndescription: Demo Skill.\nversion: " + version + "\n---\n\n# Demo\n"
		if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte(document), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := state.Activate(context.Background(), "demo-skill", "1.0.0"); err != nil {
		t.Fatal(err)
	}

	result, err := manager.Activate(context.Background(), "demo-skill", "1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if result.FromVersion != "1.0.0" || result.ToVersion != "1.1.0" {
		t.Fatalf("Activate() = %#v", result)
	}
	active, err := state.ActiveVersion("demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	if active != "1.1.0" {
		t.Fatalf("active version = %q, want 1.1.0", active)
	}
}

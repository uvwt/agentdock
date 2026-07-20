package skillstate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyChannelsAreIgnoredAndRemovedOnNextSave(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1.0.0", "1.1.0"} {
		path, err := store.InstalledPath("demo-skill", version)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	statePath := filepath.Join(store.Root(), "state", "demo-skill.json")
	legacy := `{
  "active_version": "1.0.0",
  "channels": {"stable": "1.0.0", "canary": "1.1.0"},
  "history": [],
  "updated_at": "2026-07-17T04:51:35Z"
}`
	if err := os.WriteFile(statePath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	active, err := store.ActiveVersion("demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	if active != "1.0.0" {
		t.Fatalf("ActiveVersion() = %q, want 1.0.0", active)
	}
	if err := store.Activate(context.Background(), "demo-skill", "1.1.0"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "channels") {
		t.Fatalf("legacy channels survived state rewrite: %s", data)
	}
}

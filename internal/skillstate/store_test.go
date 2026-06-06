package skillstate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreSupportsMultipleVersionsAndAtomicActivation(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1.0.0", "1.1.0"} {
		path, pathErr := store.InstalledPath("demo-skill", version)
		if pathErr != nil {
			t.Fatal(pathErr)
		}
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Activate(context.Background(), "demo-skill", "1.0.0", ChannelStable); err != nil {
		t.Fatal(err)
	}
	if err := store.Activate(context.Background(), "demo-skill", "1.1.0", ChannelCanary); err != nil {
		t.Fatal(err)
	}
	active, err := store.ActiveVersion("demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	if active != "1.1.0" {
		t.Fatalf("active version = %q, want 1.1.0", active)
	}
	previous, err := store.PreviousVersion("demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	if previous != "1.0.0" {
		t.Fatalf("previous version = %q, want 1.0.0", previous)
	}
	resolved, err := store.Resolve("demo-skill", "", ChannelStable)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(resolved) != "1.0.0" {
		t.Fatalf("stable resolved to %q", resolved)
	}
	versions, err := store.ListVersions("demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 {
		t.Fatalf("versions = %#v", versions)
	}
}

func TestStoreRejectsRemovingActiveVersion(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path, _ := store.InstalledPath("demo", "1.0.0")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.Activate(context.Background(), "demo", "1.0.0", ChannelStable); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveVersion("demo", "1.0.0"); err == nil {
		t.Fatal("RemoveVersion accepted the active version")
	}
}

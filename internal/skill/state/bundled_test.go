package state

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestBundledSkillsReplaceAndRead(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	empty, err := store.BundledSkills()
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("BundledSkills() = %#v, want empty", empty)
	}

	if err := store.ReplaceBundledSkills(context.Background(), []string{"skill-installation", "skill-authoring", "skill-installation"}); err != nil {
		t.Fatal(err)
	}
	got, err := store.BundledSkills()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"skill-authoring", "skill-installation"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BundledSkills() = %#v, want %#v", got, want)
	}

	bundled, err := store.IsBundled("skill-authoring")
	if err != nil {
		t.Fatal(err)
	}
	if !bundled {
		t.Fatal("skill-authoring should be bundled")
	}
	bundled, err = store.IsBundled("user-skill")
	if err != nil {
		t.Fatal(err)
	}
	if bundled {
		t.Fatal("user-skill should not be bundled")
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(store.Root(), bundledSkillsFile))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("bundled skill list permissions = %o, want private", info.Mode().Perm())
		}
	}
}

func TestReplaceBundledSkillsRejectsInvalidNameWithoutChangingList(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceBundledSkills(context.Background(), []string{"skill-authoring"}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceBundledSkills(context.Background(), []string{"../escape"}); err == nil {
		t.Fatal("ReplaceBundledSkills() accepted invalid name")
	}
	got, err := store.BundledSkills()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"skill-authoring"}) {
		t.Fatalf("BundledSkills() = %#v after failed replace", got)
	}
}

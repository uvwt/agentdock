package skillbundle

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/skills"
	"github.com/uvwt/agentdock/internal/skillstate"
)

func TestBootstrapInstallsActivatesAndRecordsBundledSkills(t *testing.T) {
	state, manager := newTestManager(t)
	bundle := t.TempDir()
	manifest := Manifest{Skills: []ManifestSkill{
		writeBundledSkill(t, bundle, "skill-authoring", "1.0.0"),
		writeBundledSkill(t, bundle, "skill-installation", "1.1.0"),
	}}
	writeManifest(t, bundle, manifest)

	result, err := Bootstrap(context.Background(), state, manager, bundle)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Skills) != 2 {
		t.Fatalf("Bootstrap() skills = %#v", result.Skills)
	}
	for _, entry := range manifest.Skills {
		active, err := state.ActiveVersion(entry.Name)
		if err != nil {
			t.Fatal(err)
		}
		if active != entry.Version {
			t.Fatalf("%s active version = %q, want %q", entry.Name, active, entry.Version)
		}
	}
	bundled, err := state.BundledSkills()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bundled, []string{"skill-authoring", "skill-installation"}) {
		t.Fatalf("BundledSkills() = %#v", bundled)
	}

	// 重复执行同一 Bundle 应保持幂等，不创建重复状态或报同版本冲突。
	if _, err := Bootstrap(context.Background(), state, manager, bundle); err != nil {
		t.Fatalf("second Bootstrap() failed: %v", err)
	}
}

func TestBootstrapValidatesWholeBundleBeforeInstalling(t *testing.T) {
	state, manager := newTestManager(t)
	bundle := t.TempDir()
	first := writeBundledSkill(t, bundle, "first-skill", "1.0.0")
	second := writeBundledSkill(t, bundle, "second-skill", "1.0.0")
	second.Digest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	writeManifest(t, bundle, Manifest{Skills: []ManifestSkill{first, second}})

	if _, err := Bootstrap(context.Background(), state, manager, bundle); err == nil {
		t.Fatal("Bootstrap() succeeded with invalid digest")
	}
	versions, err := state.ListVersions(first.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 0 {
		t.Fatalf("first skill was installed before full validation: %#v", versions)
	}
}

func TestBootstrapRestoresStateWhenActivationFails(t *testing.T) {
	state, manager := newTestManager(t)
	bundle := t.TempDir()
	first := writeBundledSkill(t, bundle, "first-skill", "1.0.0")
	second := writeBundledSkill(t, bundle, "second-skill", "1.0.0")
	writeManifest(t, bundle, Manifest{Skills: []ManifestSkill{first, second}})

	lockPath := filepath.Join(state.Root(), "locks", second.Name+".lock")
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = os.Remove(lockPath)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := Bootstrap(ctx, state, manager, bundle); err == nil {
		t.Fatal("Bootstrap() succeeded despite blocked activation")
	}
	for _, name := range []string{first.Name, second.Name} {
		active, err := state.ActiveVersion(name)
		if err != nil {
			t.Fatal(err)
		}
		if active != "" {
			t.Fatalf("%s active version after rollback = %q", name, active)
		}
		versions, err := state.ListVersions(name)
		if err != nil {
			t.Fatal(err)
		}
		if len(versions) != 0 {
			t.Fatalf("%s versions after rollback = %#v", name, versions)
		}
	}
	bundled, err := state.BundledSkills()
	if err != nil {
		t.Fatal(err)
	}
	if len(bundled) != 0 {
		t.Fatalf("BundledSkills() after rollback = %#v", bundled)
	}
	if _, err := os.Stat(filepath.Join(state.Root(), "bundled-skills.json")); !os.IsNotExist(err) {
		t.Fatalf("failed bootstrap created bundled list: %v", err)
	}
}

func newTestManager(t *testing.T) (*skillstate.Store, *skills.Manager) {
	t.Helper()
	state, err := skillstate.New(filepath.Join(t.TempDir(), "skill-store"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := skills.New(state)
	if err != nil {
		t.Fatal(err)
	}
	return state, manager
}

func writeBundledSkill(t *testing.T, bundle, name, version string) ManifestSkill {
	t.Helper()
	packageDir := filepath.Join(bundle, name)
	if err := os.MkdirAll(packageDir, 0o700); err != nil {
		t.Fatal(err)
	}
	document := "---\nname: " + name + "\ndescription: Test bundled Skill.\nversion: " + version + "\n---\n\n# Test\n"
	if err := os.WriteFile(filepath.Join(packageDir, "SKILL.md"), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	digest, err := skills.DigestDirectory(packageDir)
	if err != nil {
		t.Fatal(err)
	}
	return ManifestSkill{Name: name, Version: version, Path: name, Digest: digest}
}

func writeManifest(t *testing.T, bundle string, manifest Manifest) {
	t.Helper()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, ManifestFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

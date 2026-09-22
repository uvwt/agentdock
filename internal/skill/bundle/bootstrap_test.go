package bundle

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	skills "github.com/uvwt/agentdock/internal/skill"
	skillstate "github.com/uvwt/agentdock/internal/skill/state"
)

func TestBootstrapInstallsCurrentBundledSkillsIdempotently(t *testing.T) {
	state, manager := newTestManager(t)
	bundle := t.TempDir()
	first := writeBundledSkill(t, bundle, "skill-authoring", "Authoring")
	second := writeBundledSkill(t, bundle, "skill-installation", "Installation")
	writeManifest(t, bundle, Manifest{Skills: []ManifestSkill{first, second}})

	result, err := Bootstrap(context.Background(), state, manager, bundle)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Skills) != 2 || !result.Skills[0].Changed || !result.Skills[1].Changed {
		t.Fatalf("first bootstrap result = %#v", result)
	}
	for _, item := range []ManifestSkill{first, second} {
		root, err := state.Resolve(item.Name)
		if err != nil {
			t.Fatal(err)
		}
		if root != filepath.Join(state.Root(), item.Name) {
			t.Fatalf("bundled Skill root = %q", root)
		}
	}
	secondResult, err := Bootstrap(context.Background(), state, manager, bundle)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range secondResult.Skills {
		if item.Changed {
			t.Fatalf("repeat bootstrap was not idempotent: %#v", secondResult)
		}
	}
}

func TestBootstrapReplacesCurrentContentWithoutVersionHistory(t *testing.T) {
	state, manager := newTestManager(t)
	local := t.TempDir()
	writeBundledSkillWithBody(t, local, "skill-authoring", "Local modification")
	if _, err := manager.Install(context.Background(), skills.InstallRequest{Source: filepath.Join(local, "skill-authoring")}); err != nil {
		t.Fatal(err)
	}

	bundle := t.TempDir()
	official := writeBundledSkillWithBody(t, bundle, "skill-authoring", "Official content")
	writeManifest(t, bundle, Manifest{Skills: []ManifestSkill{official}})
	if _, err := Bootstrap(context.Background(), state, manager, bundle); err != nil {
		t.Fatal(err)
	}
	root, err := state.Resolve("skill-authoring")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Official content") || strings.Contains(string(data), "Local modification") {
		t.Fatalf("bundled Skill did not become current content: %s", data)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() && (entry.Name() == "1.0.0" || entry.Name() == "2.0.0") {
			t.Fatalf("bundle created version history directory %q", entry.Name())
		}
	}
}

func TestBootstrapValidatesWholeBundleBeforeInstalling(t *testing.T) {
	state, manager := newTestManager(t)
	bundle := t.TempDir()
	first := writeBundledSkill(t, bundle, "first-skill", "First")
	second := writeBundledSkill(t, bundle, "second-skill", "Second")
	second.Digest = strings.Repeat("0", 64)
	writeManifest(t, bundle, Manifest{Skills: []ManifestSkill{first, second}})

	if _, err := Bootstrap(context.Background(), state, manager, bundle); err == nil {
		t.Fatal("Bootstrap() succeeded with invalid digest")
	}
	if installed, err := state.IsInstalled(first.Name); err != nil || installed {
		t.Fatalf("first Skill installed before whole-bundle validation: installed=%v err=%v", installed, err)
	}
}

func TestBootstrapRestoresPreviousContentWhenLaterInstallIsCanceled(t *testing.T) {
	state, manager := newTestManager(t)
	local := t.TempDir()
	writeBundledSkillWithBody(t, local, "first-skill", "Local content")
	if _, err := manager.Install(context.Background(), skills.InstallRequest{Source: filepath.Join(local, "first-skill")}); err != nil {
		t.Fatal(err)
	}

	bundle := t.TempDir()
	first := writeBundledSkillWithBody(t, bundle, "first-skill", "Official replacement")
	second := writeBundledSkill(t, bundle, "second-skill", "Second")
	writeManifest(t, bundle, Manifest{Skills: []ManifestSkill{first, second}})

	blockSecond, err := state.AcquireRead(context.Background(), second.Name)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, bootstrapErr := Bootstrap(ctx, state, manager, bundle)
		done <- bootstrapErr
	}()

	waitForSkillBody(t, state, first.Name, "Official replacement")
	cancel()

	select {
	case err := <-done:
		if err == nil || !errors.Is(err, context.Canceled) {
			blockSecond()
			t.Fatalf("Bootstrap() error = %v, want context cancellation", err)
		}
	case <-time.After(5 * time.Second):
		blockSecond()
		t.Fatal("Bootstrap() did not finish after cancellation")
	}
	blockSecond()
	waitForSkillBody(t, state, first.Name, "Local content")
	if installed, err := state.IsInstalled(second.Name); err != nil || installed {
		t.Fatalf("second Skill survived failed transaction: installed=%v err=%v", installed, err)
	}
}

func waitForSkillBody(t *testing.T, state *skillstate.Store, name, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		root, err := state.Resolve(name)
		if err == nil {
			if data, readErr := os.ReadFile(filepath.Join(root, "SKILL.md")); readErr == nil && strings.Contains(string(data), want) {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s content %q", name, want)
}

func newTestManager(t *testing.T) (*skillstate.Store, *skills.Manager) {
	t.Helper()
	state, err := skillstate.New(filepath.Join(t.TempDir(), "skills"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := skills.New(state)
	if err != nil {
		t.Fatal(err)
	}
	return state, manager
}

func writeBundledSkill(t *testing.T, bundle, name, body string) ManifestSkill {
	t.Helper()
	return writeBundledSkillWithBody(t, bundle, name, body)
}

func writeBundledSkillWithBody(t *testing.T, bundle, name, body string) ManifestSkill {
	t.Helper()
	packageDir := filepath.Join(bundle, name)
	if err := os.MkdirAll(packageDir, 0o700); err != nil {
		t.Fatal(err)
	}
	document := "---\nname: " + name + "\ndescription: Test bundled Skill.\n---\n\n# " + body + "\n"
	if err := os.WriteFile(filepath.Join(packageDir, "SKILL.md"), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	digest, err := skills.DigestDirectory(packageDir)
	if err != nil {
		t.Fatal(err)
	}
	return ManifestSkill{Name: name, Path: name, Digest: digest}
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

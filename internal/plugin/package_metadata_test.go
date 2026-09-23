package plugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInstalledPluginIgnoresMacOSMetadataNoise(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	writeJSONFile(t, filepath.Join(source, "plugin.json"), map[string]any{
		"$schema": pluginSchemaURI,
		"name":    "metadata-noise",
		"version": "1.0.0",
	})
	skillRoot := filepath.Join(source, "skills", "demo")
	if err := os.MkdirAll(skillRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("---\nname: demo\ndescription: Metadata noise fixture.\n---\n\n# Demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	review := manager.Validate(source)
	if !review.Valid {
		t.Fatalf("initial review invalid: %#v", review)
	}
	result, err := manager.InstallReviewedSource(context.Background(), source, true, review.ReviewToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.FinalizeActivation(result.Name); err != nil {
		t.Fatal(err)
	}

	installed, err := manager.Inspect("metadata-noise")
	if err != nil {
		t.Fatal(err)
	}
	beforeDigest := installed.PackageDigest
	beforeSkillDigest := installed.Components.Skills[0].ContentDigest

	for path, content := range map[string]string{
		".DS_Store":                  "finder metadata",
		"._plugin.json":              "appledouble",
		"skills/.DS_Store":           "skills finder metadata",
		"skills/demo/.DS_Store":      "skill finder metadata",
		"skills/demo/._SKILL.md":     "skill appledouble",
		"__MACOSX/._plugin.json":     "zip metadata",
		"skills/__MACOSX/._SKILL.md": "nested zip metadata",
	} {
		full := filepath.Join(installed.Root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	after, err := manager.Inspect("metadata-noise")
	if err != nil {
		t.Fatalf("inspect after Finder metadata: %v", err)
	}
	if after.PackageDigest != beforeDigest {
		t.Fatalf("package digest drifted after metadata: before=%s after=%s", beforeDigest, after.PackageDigest)
	}
	if len(after.Components.Skills) != 1 || after.Components.Skills[0].Name != "demo" {
		t.Fatalf("metadata became a Skill component: %#v", after.Components.Skills)
	}
	if after.Components.Skills[0].ContentDigest != beforeSkillDigest {
		t.Fatalf("Skill digest drifted after metadata: before=%s after=%s", beforeSkillDigest, after.Components.Skills[0].ContentDigest)
	}

	acquired, release, err := manager.Acquire(context.Background(), "metadata-noise")
	if err != nil {
		t.Fatalf("acquire after Finder metadata: %v", err)
	}
	defer release()
	if acquired.PackageDigest != beforeDigest {
		t.Fatalf("acquired package digest drifted: %s", acquired.PackageDigest)
	}
}

func TestSnapshotPluginTreeDropsMacOSMetadata(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "plugin.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	for path := range map[string]struct{}{
		".DS_Store":              {},
		"._plugin.json":          {},
		"nested/.DS_Store":       {},
		"__MACOSX/._plugin.json": {},
	} {
		full := filepath.Join(source, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("noise"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	destination := filepath.Join(t.TempDir(), "snapshot")
	if err := snapshotPluginTree(source, destination, maxPluginExtractedBytes, maxPluginArchiveFiles); err != nil {
		t.Fatal(err)
	}
	if !regularFileExists(filepath.Join(destination, "plugin.json")) {
		t.Fatal("semantic package file was not copied")
	}
	for _, path := range []string{".DS_Store", "._plugin.json", "nested/.DS_Store", "__MACOSX"} {
		if _, err := os.Lstat(filepath.Join(destination, filepath.FromSlash(path))); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("snapshot retained macOS metadata %s: %v", path, err)
		}
	}
}

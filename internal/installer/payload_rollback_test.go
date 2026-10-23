package installer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestUnixPayloadRollbackRestoresExistingGeneration(t *testing.T) {
	root := t.TempDir()
	installRoot := filepath.Join(root, "install")
	generation := filepath.Join(installRoot, "versions", "v1.2.3")
	originalFiles := map[string]string{
		"agentdock":                 "previous binary",
		"core-skills/manifest.json": "previous manifest",
		"core-skills/previous.txt":  "previous package",
	}
	for name, content := range originalFiles {
		path := filepath.Join(generation, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	payload := filepath.Join(root, "candidate")
	if err := os.WriteFile(payload, []byte("candidate binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(root, "bundle")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"manifest.json", "candidate.txt"} {
		if err := os.WriteFile(filepath.Join(bundle, name), []byte("candidate package"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	journal := newJournal(filepath.Join(root, "runtime"), "existing-generation")
	staged, err := stageUnixPayload(Request{
		InstallRoot: installRoot, Version: "v1.2.3", BinaryPath: payload, SkillBundle: bundle,
	}, journal)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(staged.Binary); err != nil || string(data) != "candidate binary" {
		t.Fatalf("staged binary = %q, error = %v", data, err)
	}
	if err := journal.Restore(context.Background(), Request{}); err != nil {
		t.Fatal(err)
	}
	for name, want := range originalFiles {
		data, err := os.ReadFile(filepath.Join(generation, filepath.FromSlash(name)))
		if err != nil || string(data) != want {
			t.Errorf("restored %s = %q, error = %v; want %q", name, data, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(generation, "core-skills", "candidate.txt")); !os.IsNotExist(err) {
		t.Fatalf("candidate-only package survived rollback: %v", err)
	}
}

func TestUnixPayloadRollbackRemovesNewGenerationAfterStageFailure(t *testing.T) {
	root := t.TempDir()
	installRoot := filepath.Join(root, "install")
	payload := filepath.Join(root, "candidate")
	if err := os.WriteFile(payload, []byte("candidate binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	journal := newJournal(filepath.Join(root, "runtime"), "new-generation")
	_, err := stageUnixPayload(Request{
		InstallRoot: installRoot, Version: "v1.2.3", BinaryPath: payload,
		SkillBundle: filepath.Join(root, "missing-bundle"),
	}, journal)
	if err == nil {
		t.Fatal("staging with a missing bundle succeeded")
	}
	generation := filepath.Join(installRoot, "versions", "v1.2.3")
	if _, err := os.Stat(filepath.Join(generation, "agentdock")); err != nil {
		t.Fatalf("staging did not create the candidate binary before failing: %v", err)
	}
	if err := journal.Restore(context.Background(), Request{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(generation); !os.IsNotExist(err) {
		t.Fatalf("failed candidate generation survived rollback: %v", err)
	}
}

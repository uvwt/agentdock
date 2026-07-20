package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/skillstate"
)

func TestValidateAndInstallDocumentSkill(t *testing.T) {
	state, err := skillstate.New(filepath.Join(t.TempDir(), "skills"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(state)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "demo-skill")
	if err := os.MkdirAll(filepath.Join(source, "references"), 0o700); err != nil {
		t.Fatal(err)
	}
	doc := `---
name: demo-skill
description: Teach the model a demo workflow.
version: 1.0.0
---

# Demo Skill

Read references and use existing tools.
`
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "references", "guide.md"), []byte("# Guide\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	validated, err := manager.Validate(context.Background(), ValidateRequest{Source: source})
	if err != nil {
		t.Fatal(err)
	}
	if !validated.Valid || validated.Document.Name != "demo-skill" {
		t.Fatalf("unexpected validation: %#v", validated)
	}
	installed, err := manager.Install(context.Background(), InstallRequest{Source: source, Activate: true})
	if err != nil {
		t.Fatal(err)
	}
	if installed.Skill != "demo-skill" || installed.Version != "1.0.0" || !installed.Activated {
		t.Fatalf("unexpected install: %#v", installed)
	}
	if _, err := os.Stat(filepath.Join(installed.Path, "references", "guide.md")); err != nil {
		t.Fatalf("reference not installed: %v", err)
	}
	metadata, err := os.ReadFile(filepath.Join(installed.Path, ".agentdock-install.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(metadata), `"source"`) {
		t.Fatalf("install metadata still persists source: %s", metadata)
	}
}

func TestValidateRejectsLegacyManifest(t *testing.T) {
	state, err := skillstate.New(filepath.Join(t.TempDir(), "skills"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(state)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "legacy-skill")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("---\nname: legacy-skill\ndescription: Legacy.\nversion: 1.0.0\n---\n\n# Legacy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "agentdock.yaml"), []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := manager.Validate(context.Background(), ValidateRequest{Source: source})
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid || len(result.Issues) == 0 || result.Issues[0].Stage != "package.legacy_manifest" {
		t.Fatalf("legacy package should be rejected: %#v", result)
	}
}

func TestValidateRejectsPrivateEnvFile(t *testing.T) {
	state, err := skillstate.New(filepath.Join(t.TempDir(), "skills"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(state)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "private-skill")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("---\nname: private-skill\ndescription: Private package.\nversion: 1.0.0\n---\n\n# Private\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".env"), []byte("SECRET=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := manager.Validate(context.Background(), ValidateRequest{Source: source})
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid || len(result.Issues) == 0 || result.Issues[0].Stage != "package.secret_file" {
		t.Fatalf("private env file should be rejected: %#v", result)
	}
}

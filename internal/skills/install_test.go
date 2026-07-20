package skills

import (
	"archive/zip"
	"context"
	"io"
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

func TestInstallTreatsDirectoryAndZipAsSameContent(t *testing.T) {
	state, err := skillstate.New(filepath.Join(t.TempDir(), "skills"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(state)
	if err != nil {
		t.Fatal(err)
	}

	source := filepath.Join(t.TempDir(), "demo-skill")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	document := "---\nname: demo-skill\ndescription: Demo Skill.\nversion: 1.0.0\n---\n\n# Demo\n"
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte(document), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "run.py"), []byte("#!/usr/bin/env python3\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	first, err := manager.Install(context.Background(), InstallRequest{Source: source, Activate: true})
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "demo-skill.zip")
	writeSkillArchive(t, source, archive)
	second, err := manager.Install(context.Background(), InstallRequest{Source: archive, Activate: true})
	if err != nil {
		t.Fatalf("same Skill content from ZIP should be idempotent: %v", err)
	}
	if second.Path != first.Path || !second.Activated {
		t.Fatalf("unexpected second install: %#v", second)
	}
}

func writeSkillArchive(t *testing.T, source, archivePath string) {
	t.Helper()
	archive, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(archive)
	walkErr := filepath.Walk(source, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source || info.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		header.Method = zip.Deflate
		header.SetMode(info.Mode())
		entry, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(entry, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if walkErr != nil {
		_ = writer.Close()
		_ = archive.Close()
		t.Fatal(walkErr)
	}
	if err := writer.Close(); err != nil {
		_ = archive.Close()
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
}

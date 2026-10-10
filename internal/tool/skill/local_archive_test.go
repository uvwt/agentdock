package skill

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	skills "github.com/uvwt/agentdock/internal/skill"
	skillstate "github.com/uvwt/agentdock/internal/skill/state"
)

func TestInstallLocalArchiveRejectsDigestMismatchThenInstalls(t *testing.T) {
	home := t.TempDir()
	state, err := skillstate.New(filepath.Join(home, "skills"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := skills.New(state)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{manager: manager, state: state}
	archive := filepath.Join(t.TempDir(), "demo-skill.zip")
	writeSkillArchive(t, archive)

	if _, err := service.InstallLocalArchive(context.Background(), archive, "sha256:0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Fatal("digest mismatch was installed")
	}
	digest, err := skills.DigestFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := service.InstallLocalArchive(context.Background(), archive, digest)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Skill != "demo-skill" || !installed.Changed {
		t.Fatalf("install = %#v", installed)
	}
	again, err := service.InstallLocalArchive(context.Background(), archive, digest)
	if err != nil {
		t.Fatal(err)
	}
	if again.Changed {
		t.Fatal("same Skill archive installed a second time")
	}
	root, err := service.ManagedPackageRoot("demo-skill")
	if err != nil || !strings.HasSuffix(root, string(filepath.Separator)+"demo-skill") {
		t.Fatalf("managed root = %q %v", root, err)
	}
}

func writeSkillArchive(t *testing.T, archive string) {
	t.Helper()
	output, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(output)
	header := &zip.FileHeader{Name: "SKILL.md", Method: zip.Deflate}
	header.SetMode(0o600)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("---\nname: demo-skill\ndescription: Local archive demo.\n---\n\n# Demo\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}

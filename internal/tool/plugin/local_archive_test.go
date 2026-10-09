package plugin

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	pluginruntime "github.com/uvwt/agentdock/internal/plugin"
)

func TestInstallReviewedArchiveRejectsSymlinkAndWrongReviewToken(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentdock")
	manager, err := pluginruntime.NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{manager: manager}

	link := filepath.Join(t.TempDir(), "linked.zip")
	if err := os.Symlink(filepath.Join(t.TempDir(), "missing.zip"), link); err != nil {
		t.Fatal(err)
	}
	if _, err := service.InstallReviewedArchive(context.Background(), link, true, "review-v1:sha256:nope"); err == nil {
		t.Fatal("symlink archive was accepted")
	}

	source := filepath.Join(t.TempDir(), "plugin")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	writePluginServiceJSON(t, filepath.Join(source, "plugin.json"), map[string]any{
		"name": "demo-plugin", "version": "1.0.0", "description": "Demo",
	})
	if err := os.MkdirAll(filepath.Join(source, "skills", "demo-skill"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "skills", "demo-skill", "SKILL.md"), []byte("---\nname: demo-skill\ndescription: Demo plugin skill.\n---\n\n# Demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "demo-plugin.zip")
	writePluginZip(t, archive, source)
	if _, err := service.InstallReviewedArchive(context.Background(), archive, true, "review-v1:sha256:deadbeef"); err == nil {
		t.Fatal("wrong review token installed a plugin")
	}
	if _, err := manager.Inspect("demo-plugin"); err == nil {
		t.Fatal("plugin was installed without a matching review token")
	}
}

func writePluginZip(t *testing.T, archive, root string) {
	t.Helper()
	output, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(output)
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || path == root || entry.IsDir() {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		header := &zip.FileHeader{Name: filepath.ToSlash(relative), Method: zip.Deflate}
		header.SetMode(0o600)
		target, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(target, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}

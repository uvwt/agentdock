package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractCoreSkillBundleFromWindowsArchive(t *testing.T) {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for name, content := range map[string]string{
		"agentdock.exe":                                      "binary",
		coreSkillBundlePrefix + "manifest.json":              `{"skills":[]}`,
		coreSkillBundlePrefix + "packages/example-skill.zip": "package",
	} {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	bundlePath, err := extractCoreSkillBundle(archive.Bytes(), "windows", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"manifest.json", "packages/example-skill.zip"} {
		if _, err := os.Stat(filepath.Join(bundlePath, filepath.FromSlash(relative))); err != nil {
			t.Fatalf("extracted Bundle missing %s: %v", relative, err)
		}
	}
}

func TestExtractCoreSkillBundleRequiresManifest(t *testing.T) {
	archive := makeTarGzWithoutBundle(t, "bin/agentdock", []byte("binary"))
	if _, err := extractCoreSkillBundle(archive, "linux", t.TempDir()); err == nil {
		t.Fatal("extractCoreSkillBundle() accepted archive without manifest")
	}
}

func makeTarGzWithoutBundle(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDigestDirectoryStable(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("demo"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := DigestDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := DigestDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first == "" {
		t.Fatalf("unstable digest: %q %q", first, second)
	}
}

func TestDigestPackageContentIgnoresMacOSMetadata(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("demo"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := DigestPackageContent(root)
	if err != nil {
		t.Fatal(err)
	}

	for path, content := range map[string]string{
		".DS_Store":                  "finder metadata",
		"._SKILL.md":                 "appledouble",
		"nested/.DS_Store":           "nested finder metadata",
		"__MACOSX/._SKILL.md":        "zip appledouble",
		"__MACOSX/nested/._resource": "zip metadata",
	} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	after, err := DigestPackageContent(root)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("package digest changed after macOS metadata: before=%s after=%s", before, after)
	}
}

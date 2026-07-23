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

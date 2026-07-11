package skillruntime

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveStagedDeletesPartialPackageAndPreservesCause(t *testing.T) {
	staged := filepath.Join(t.TempDir(), "skill.tmp")
	if err := os.MkdirAll(staged, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, "partial"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("copy failed")
	if err := removeStaged(staged, cause); !errors.Is(err, cause) {
		t.Fatalf("removeStaged() error = %v, want original cause", err)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("staged path still exists or stat failed unexpectedly: %v", err)
	}
}

package securepath

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEnsurePrivateSecuresFileAndDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(file, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsurePrivate(root); err != nil {
		t.Fatalf("secure directory: %v", err)
	}
	if err := EnsurePrivate(file); err != nil {
		t.Fatalf("secure file: %v", err)
	}
	if runtime.GOOS != "windows" {
		rootInfo, _ := os.Stat(root)
		fileInfo, _ := os.Stat(file)
		if got := rootInfo.Mode().Perm(); got != 0o700 {
			t.Fatalf("directory mode = %o, want 700", got)
		}
		if got := fileInfo.Mode().Perm(); got != 0o600 {
			t.Fatalf("file mode = %o, want 600", got)
		}
	}
}

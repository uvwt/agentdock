//go:build unix

package agentinstructions

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestInstructionFIFOIsRejectedWithoutOpening(t *testing.T) {
	root := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(root, Filename), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot := loadGuidance(t, Options{DefaultDir: root, Workdir: root})
	if snapshot.Files[0].Reason != "not_regular_file" || snapshot.Files[0].Content != "" {
		t.Fatalf("FIFO accepted: %#v", snapshot.Files[0])
	}
}

func TestInstructionPermissionDeniedReturnsNoBody(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read files without DAC read permission")
	}
	root := t.TempDir()
	path := writeGuidance(t, root, "unreadable marker")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	snapshot := loadGuidance(t, Options{DefaultDir: root, Workdir: root})
	if snapshot.Files[0].Status != "error" || snapshot.Files[0].Reason != "permission_denied" || snapshot.Files[0].Content != "" {
		t.Fatalf("unreadable file accepted: %#v", snapshot.Files[0])
	}
}

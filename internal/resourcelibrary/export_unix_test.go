//go:build unix

package resourcelibrary

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestExportDirectory_拒绝FIFO(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "SKILL.md"), "---\nname: demo-skill\ndescription: Export demo.\n---\n\n# Demo\n")
	fifoDir := filepath.Join(root, "scripts")
	if err := os.MkdirAll(fifoDir, 0o700); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(fifoDir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportDirectory(root, KindSkill, filepath.Join(t.TempDir(), "fifo.zip")); err == nil {
		t.Fatal("export accepted a FIFO")
	}
}

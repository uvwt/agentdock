//go:build windows

package selfupdate

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestMoveFileReplaceRetriesTransientSharingViolation(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.exe")
	targetPath := filepath.Join(root, "target.exe")
	if err := os.WriteFile(sourcePath, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	target, err := windows.UTF16PtrFromString(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(
		target,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}

	released := make(chan struct{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = windows.CloseHandle(handle)
		close(released)
	}()

	if err := moveFileReplace(sourcePath, targetPath); err != nil {
		_ = windows.CloseHandle(handle)
		t.Fatalf("moveFileReplace() did not recover from a transient sharing violation: %v", err)
	}
	<-released

	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("target content = %q, want %q", data, "new")
	}
}

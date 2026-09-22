//go:build !windows

package app

import (
	"os"
	"testing"
)

func createWorkspaceDirectoryLinkForTest(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
}

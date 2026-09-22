//go:build windows

package app

import (
	"os/exec"
	"strings"
	"testing"
)

func createWorkspaceDirectoryLinkForTest(t *testing.T, target, link string) {
	t.Helper()
	output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Fatalf("create directory junction: %v (%s)", err, strings.TrimSpace(string(output)))
	}
}

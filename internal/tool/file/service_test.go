package file

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/uvwt/agentdock/internal/workspace"
)

func newCodeToolsRuntime(t *testing.T) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	service := New(ws, func(string) (string, string, error) {
		return "", "", os.ErrNotExist
	}, func(string, map[string]any) ([]string, error) {
		return os.Environ(), nil
	})
	return service, root
}

func sameExistingTestPath(left, right string) bool {
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	if leftErr == nil && rightErr == nil {
		return os.SameFile(leftInfo, rightInfo)
	}
	leftAbs, _ := filepath.Abs(left)
	rightAbs, _ := filepath.Abs(right)
	return filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}

func sameTestPath(left, right string) bool {
	if sameExistingTestPath(left, right) {
		return true
	}
	if filepath.Base(left) != filepath.Base(right) {
		return false
	}
	return sameExistingTestPath(filepath.Dir(left), filepath.Dir(right))
}

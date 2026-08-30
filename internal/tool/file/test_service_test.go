package file

import (
	"testing"

	"github.com/uvwt/agentdock/internal/workspace"
)

func newFileTestService(t *testing.T) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	return New(ws, nil, nil), root
}

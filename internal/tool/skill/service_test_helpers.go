package skill

import (
	"path/filepath"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/envstore"
	"github.com/uvwt/agentdock/internal/workspace"
)

func newSkillTestService(t *testing.T) (*Service, string) {
	t.Helper()
	return newSkillTestServiceAtRoot(t, t.TempDir())
}

func newSkillTestServiceAtRoot(t *testing.T, root string) (*Service, string) {
	t.Helper()
	cfg := config.Config{AgentDockDefaultDir: root, AgentDockHome: filepath.Join(root, ".agentdock")}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	envs, err := envstore.New(cfg.AgentDockHome)
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(cfg, ws, envs)
	if err != nil {
		t.Fatal(err)
	}
	return service, root
}

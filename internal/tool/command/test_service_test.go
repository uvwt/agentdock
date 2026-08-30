package command

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/envstore"
	"github.com/uvwt/agentdock/internal/workspace"
)

func newCommandTestService(t *testing.T) (*Service, *config.Config) {
	t.Helper()
	root := t.TempDir()
	cfg := &config.Config{
		AgentDockDefaultDir: root,
		AgentDockHome:       filepath.Join(root, ".agentdock"),
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(cfg.AgentDockDefaultDir)
	if err != nil {
		t.Fatal(err)
	}
	envs, err := envstore.New(cfg.AgentDockHome)
	if err != nil {
		t.Fatal(err)
	}
	commandCtx, cancel := context.WithCancel(context.Background())
	service := New(
		func() config.Config { return *cfg },
		ws,
		envs,
		nil,
		func() (context.Context, error) { return commandCtx, nil },
	)
	t.Cleanup(func() {
		cancel()
		_ = service.Close()
	})
	return service, cfg
}

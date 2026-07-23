package media

import (
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/workspace"
)

type CommandEnv func(extra map[string]string) ([]string, error)

type Service struct {
	cfg        config.Config
	ws         *workspace.Workspace
	commandEnv CommandEnv
}

func New(cfg config.Config, ws *workspace.Workspace, commandEnv CommandEnv) *Service {
	return &Service{cfg: cfg, ws: ws, commandEnv: commandEnv}
}

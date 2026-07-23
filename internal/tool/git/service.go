package git

import "github.com/uvwt/agentdock/internal/workspace"

type CommandEnv func(skillName string, extra map[string]any) ([]string, error)

type Service struct {
	ws         *workspace.Workspace
	commandEnv CommandEnv
}

func New(ws *workspace.Workspace, commandEnv CommandEnv) *Service {
	return &Service{ws: ws, commandEnv: commandEnv}
}

func ReadActions() []string {
	return append([]string(nil), readActions...)
}

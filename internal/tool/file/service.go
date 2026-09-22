package file

import (
	"context"

	"github.com/uvwt/agentdock/internal/workspace"
)

type SkillResourceResolver func(ctx context.Context, raw string) (absolutePath, displayPath string, release func(), err error)
type CommandEnv func(skillName string, extra map[string]string) ([]string, error)

type Service struct {
	ws                   *workspace.Workspace
	resolveSkillResource SkillResourceResolver
	commandEnv           CommandEnv
}

func New(ws *workspace.Workspace, resolveSkillResource SkillResourceResolver, commandEnv CommandEnv) *Service {
	return &Service{ws: ws, resolveSkillResource: resolveSkillResource, commandEnv: commandEnv}
}

const (
	maxTextFileReadBytes = 32 << 20
	MaxTextOutputBytes   = 4 << 20
	maxTextOutputBytes   = MaxTextOutputBytes
)

package file

import "github.com/uvwt/agentdock/internal/workspace"

type SkillResourceResolver func(raw string) (absolutePath, displayPath string, err error)
type CommandEnv func(skillName string, extra map[string]any) ([]string, error)

type Service struct {
	ws                   *workspace.Workspace
	resolveSkillResource SkillResourceResolver
	commandEnv           CommandEnv
}

func New(ws *workspace.Workspace, resolveSkillResource SkillResourceResolver, commandEnv CommandEnv) *Service {
	return &Service{ws: ws, resolveSkillResource: resolveSkillResource, commandEnv: commandEnv}
}

const (
	MaxTextFileReadBytes = 32 << 20
	MaxTextOutputBytes   = 4 << 20
)

const (
	maxTextFileReadBytes = MaxTextFileReadBytes
	maxTextOutputBytes   = MaxTextOutputBytes
)

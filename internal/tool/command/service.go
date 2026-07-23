package command

import (
	"context"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/envstore"
	"github.com/uvwt/agentdock/internal/tool/command/session"
	"github.com/uvwt/agentdock/internal/workspace"
)

type ConfigProvider func() config.Config
type SkillResolver func(skill string) (string, error)
type CommandContext func() (context.Context, error)
type Diagnostic func(output string) map[string]any

type Service struct {
	config         ConfigProvider
	ws             *workspace.Workspace
	envs           *envstore.Store
	sessions       *session.Store
	resolveSkill   SkillResolver
	commandContext CommandContext
	diagnose       Diagnostic
}

func New(configProvider ConfigProvider, ws *workspace.Workspace, envs *envstore.Store, sessions *session.Store, resolveSkill SkillResolver, commandContext CommandContext, diagnose Diagnostic) *Service {
	return &Service{
		config: configProvider, ws: ws, envs: envs, sessions: sessions,
		resolveSkill: resolveSkill, commandContext: commandContext, diagnose: diagnose,
	}
}

func (s *Service) Store() *session.Store { return s.sessions }

type InvocationPreview struct {
	Workdir string
	Env     []string
}

func (s *Service) PreparePreview(args map[string]any, command string) (InvocationPreview, error) {
	invocation, err := s.prepareCommandInvocation(args, command)
	if err != nil {
		return InvocationPreview{}, err
	}
	return InvocationPreview{Workdir: invocation.workdir, Env: append([]string(nil), invocation.env...)}, nil
}

func (s *Service) CommandEnv(skillName string, extra map[string]any) ([]string, error) {
	return s.commandEnv(skillName, extra)
}

func (s *Service) InternalCommandEnv(extra map[string]string) ([]string, error) {
	return s.internalCommandEnv(extra)
}

const (
	MaxOutputBytes        = maxCommandOutputBytes
	MaxConcurrentSessions = maxConcurrentCommandSessions
	MaxRetainedSessions   = maxRetainedCommandSessions
)

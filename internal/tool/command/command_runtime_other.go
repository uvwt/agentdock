//go:build !windows

package command

import (
	"context"
	"strings"
)

func (svc *Service) prepareCommandInvocation(ctx context.Context, request ExecRequest) (commandInvocation, error) {
	if runtimeName := strings.TrimSpace(request.Runtime); runtimeName != "" {
		return commandInvocation{}, toolError("INVALID_ARGUMENT", "runtime is only supported by AgentDock on Windows", "validation")
	}
	if distribution := strings.TrimSpace(request.WSLDistribution); distribution != "" {
		return commandInvocation{}, toolError("INVALID_ARGUMENT", "wsl_distribution is only supported by AgentDock on Windows", "validation")
	}
	return svc.newHostCommandInvocation(ctx, request)
}

func AddRuntimeProperties(_ map[string]any) {}

func WorkdirDescription() string {
	return "Host working directory. Relative paths resolve from ~/AgentDock."
}

func Description() string {
	return "Run a bounded command. Bind an exact Skill with skill_ref to use its resolved root; managed Skills receive isolated environment plus private SKILL_DATA_DIR. Explicit workdir and non-reserved env values override defaults."
}

//go:build !windows

package command

import "strings"

func (svc *Service) prepareCommandInvocation(request ExecRequest) (commandInvocation, error) {
	if runtimeName := strings.TrimSpace(request.Runtime); runtimeName != "" {
		return commandInvocation{}, toolError("INVALID_ARGUMENT", "runtime is only supported by AgentDock on Windows", "validation")
	}
	if distribution := strings.TrimSpace(request.WSLDistribution); distribution != "" {
		return commandInvocation{}, toolError("INVALID_ARGUMENT", "wsl_distribution is only supported by AgentDock on Windows", "validation")
	}
	return svc.newHostCommandInvocation(request)
}

func AddRuntimeProperties(_ map[string]any) {}

func WorkdirDescription() string {
	return "Host working directory. Relative paths resolve from ~/AgentDock."
}

func Description() string {
	return "Run a bounded command. Bind an active Skill with skill to use its installed root and isolated environment for this command; explicit workdir and env values override those defaults."
}

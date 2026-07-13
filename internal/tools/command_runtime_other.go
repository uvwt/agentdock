//go:build !windows

package tools

import "strings"

func (r *Runtime) prepareCommandInvocation(args map[string]any, command string) (commandInvocation, error) {
	if runtimeName := strings.TrimSpace(stringArg(args, "runtime", "")); runtimeName != "" {
		return commandInvocation{}, toolError("INVALID_ARGUMENT", "runtime is only supported by AgentDock on Windows", "validation")
	}
	if distribution := strings.TrimSpace(stringArg(args, "wsl_distribution", "")); distribution != "" {
		return commandInvocation{}, toolError("INVALID_ARGUMENT", "wsl_distribution is only supported by AgentDock on Windows", "validation")
	}
	return r.newHostCommandInvocation(args, command)
}

func addExecCommandRuntimeProperties(_ map[string]any) {}

func execCommandWorkdirDescription() string {
	return "Host working directory. Relative paths resolve from ~/AgentDock."
}

func execCommandDescription() string {
	return "Run a bounded command. Bind an active Skill with skill to use its installed root and isolated environment for this command; explicit workdir and env values override those defaults."
}

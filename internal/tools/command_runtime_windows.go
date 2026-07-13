//go:build windows

package tools

import (
	"os/exec"
	"strings"

	"github.com/uvwt/agentdock/internal/session"
)

func (r *Runtime) prepareCommandInvocation(args map[string]any, command string) (commandInvocation, error) {
	runtimeName := strings.ToLower(strings.TrimSpace(stringArg(args, "runtime", "windows")))
	if runtimeName == "" {
		runtimeName = "windows"
	}
	distribution := strings.TrimSpace(stringArg(args, "wsl_distribution", ""))

	switch runtimeName {
	case "windows":
		if distribution != "" {
			return commandInvocation{}, toolError("INVALID_ARGUMENT", "wsl_distribution is only valid when runtime is wsl", "validation")
		}
		invocation, err := r.newHostCommandInvocation(args, command)
		if err != nil {
			return commandInvocation{}, err
		}
		invocation.execution = session.ExecutionContext{
			Runtime: "windows",
			Workdir: invocation.workdir,
		}
		return invocation, nil

	case "wsl":
		wslPath, err := exec.LookPath("wsl.exe")
		if err != nil {
			return commandInvocation{}, toolErrorDetails(
				"WSL_NOT_AVAILABLE",
				"wsl.exe was not found on this Windows host",
				"runtime",
				map[string]any{"reason": err.Error()},
			)
		}
		workdir, err := r.resolveWSLWorkdir(args)
		if err != nil {
			return commandInvocation{}, err
		}
		linuxEnv, err := r.commandEnvOverrides(strings.TrimSpace(stringArg(args, "skill_env", "")), mapArg(args, "env"))
		if err != nil {
			return commandInvocation{}, err
		}
		hostEnv, err := r.commandEnv("", nil)
		if err != nil {
			return commandInvocation{}, err
		}
		wslArgs := buildWSLCommandArgs(distribution, workdir, command)
		return commandInvocation{
			build: newWSLCommandFactory(wslPath, wslArgs, buildWSLProcessEnv(hostEnv, linuxEnv), r.ws.DefaultCWD()),
			execution: session.ExecutionContext{
				Runtime:      "wsl",
				Distribution: distribution,
				Workdir:      workdir,
			},
		}, nil

	default:
		return commandInvocation{}, toolErrorDetails(
			"INVALID_ARGUMENT",
			"runtime must be windows or wsl",
			"validation",
			map[string]any{"runtime": runtimeName},
		)
	}
}

func (r *Runtime) resolveWSLWorkdir(args map[string]any) (string, error) {
	raw := strings.TrimSpace(stringArg(args, "workdir", ""))
	if raw == "" {
		return "~", nil
	}
	if strings.ContainsRune(raw, 0) {
		return "", toolError("INVALID_ARGUMENT", "workdir contains an invalid byte", "validation")
	}
	if raw == "~" || strings.HasPrefix(raw, "~/") || strings.HasPrefix(raw, "/") {
		return raw, nil
	}
	if converted, ok := windowsPathToWSL(raw); ok {
		return converted, nil
	}
	if strings.HasPrefix(raw, `\\`) {
		return "", toolError("INVALID_ARGUMENT", "WSL workdir does not accept UNC paths; use a Linux path such as /home/user/project", "validation")
	}

	resolved, err := r.ws.ResolveExisting(raw)
	if err != nil {
		return "", err
	}
	converted, ok := windowsPathToWSL(resolved.Abs)
	if !ok {
		return "", toolErrorDetails(
			"INVALID_ARGUMENT",
			"relative WSL workdir could not be mapped from the Windows host path",
			"validation",
			map[string]any{"workdir": raw, "resolved_path": resolved.Abs},
		)
	}
	return converted, nil
}

func execCommandWorkdirDescription() string {
	return "Working directory. runtime=windows uses a Host path. runtime=wsl accepts a WSL POSIX path such as /home/user/project or /mnt/d/Project and converts Windows drive paths such as D:\\Project."
}

func addExecCommandRuntimeProperties(props map[string]any) {
	props["runtime"] = map[string]any{
		"type":        "string",
		"description": "Command runtime on Windows. Defaults to windows; use wsl to run through wsl.exe.",
		"enum":        []string{"windows", "wsl"},
	}
	props["wsl_distribution"] = map[string]any{
		"type":        "string",
		"description": "Optional WSL distribution name used only when runtime=wsl. Omit it to use the system default distribution.",
	}
}

func execCommandDescription() string {
	return "Run a bounded command on Windows or WSL. runtime defaults to windows. When WSL is required, use runtime=wsl instead of putting wsl.exe in cmd; existing TTY and session lifecycle are reused. Skill environment values are loaded before explicit env overrides."
}

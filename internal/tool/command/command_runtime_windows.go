//go:build windows

package command

import (
	"context"
	"os/exec"
	"strings"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/tool/command/session"
)

func (svc *Service) prepareCommandInvocation(ctx context.Context, request ExecRequest) (commandInvocation, error) {
	runtimeName := strings.ToLower(strings.TrimSpace(request.Runtime))
	if runtimeName == "" {
		runtimeName = "windows"
	}
	distribution := strings.TrimSpace(request.WSLDistribution)

	switch runtimeName {
	case "windows":
		if distribution != "" {
			return commandInvocation{}, toolError("INVALID_ARGUMENT", "wsl_distribution is only valid when runtime is wsl", "validation")
		}
		invocation, err := svc.newHostCommandInvocation(ctx, request)
		if err != nil {
			return commandInvocation{}, err
		}
		invocation.execution = session.ExecutionContext{Runtime: "windows", Workdir: invocation.workdir}
		return invocation, nil

	case "wsl":
		lease, err := svc.acquireSkill(ctx, request.SkillRef)
		if err != nil {
			return commandInvocation{}, err
		}
		releaseOnError := true
		defer func() {
			if releaseOnError && lease.Release != nil {
				lease.Release()
			}
		}()

		wslPath, err := exec.LookPath("wsl.exe")
		if err != nil {
			return commandInvocation{}, toolErrorDetails("WSL_NOT_AVAILABLE", "wsl.exe was not found on this Windows host", "runtime", map[string]any{"reason": err.Error()})
		}
		workdir, err := svc.resolveWSLWorkdir(request.Workdir, lease.Root)
		if err != nil {
			return commandInvocation{}, err
		}
		linuxEnv, err := svc.commandEnvOverrides(lease.EnvName, request.Env)
		if err != nil {
			return commandInvocation{}, err
		}
		expectedDataDir := ""
		if lease.EnvName != "" {
			expectedDataDir, err = config.SkillDataDir(svc.config(), lease.EnvName)
			if err != nil {
				return commandInvocation{}, toolErrorDetails("SKILL_DATA_DIR_INVALID", "resolve managed Skill data directory", "runtime", map[string]any{
					"skill": lease.EnvName, "reason": err.Error(),
				})
			}
			wslDataDir, ok := windowsPathToWSL(expectedDataDir)
			if !ok {
				return commandInvocation{}, toolErrorDetails("SKILL_DATA_DIR_INVALID", "managed Skill data directory could not be mapped into WSL", "validation", map[string]any{
					"skill": lease.EnvName, "path": expectedDataDir,
				})
			}
			linuxEnv[config.SkillDataDirEnvKey] = wslDataDir
		}
		hostEnv, err := svc.commandEnv("", nil)
		if err != nil {
			return commandInvocation{}, err
		}
		if expectedDataDir != "" {
			if _, err := svc.ensureManagedSkillDataDir(lease.EnvName); err != nil {
				return commandInvocation{}, err
			}
		}
		wslArgs := buildWSLCommandArgs(distribution, workdir, request.Cmd)
		releaseOnError = false
		return commandInvocation{
			build:        newWSLCommandFactory(wslPath, wslArgs, buildWSLProcessEnv(hostEnv, linuxEnv), svc.ws.DefaultCWD()),
			execution:    session.ExecutionContext{Runtime: "wsl", Distribution: distribution, Workdir: workdir},
			skillRelease: lease.Release,
		}, nil

	default:
		return commandInvocation{}, toolErrorDetails("INVALID_ARGUMENT", "runtime must be windows or wsl", "validation", map[string]any{"runtime": runtimeName})
	}
}

func (svc *Service) resolveWSLWorkdir(requested, hostSkillDir string) (string, error) {
	skillDir := ""
	if hostSkillDir != "" {
		converted, ok := windowsPathToWSL(hostSkillDir)
		if !ok {
			return "", toolErrorDetails("SKILL_CONTEXT_INVALID", "managed Skill directory could not be mapped into WSL", "validation", map[string]any{"path": hostSkillDir})
		}
		skillDir = converted
	}
	raw := strings.TrimSpace(requested)
	if raw == "" && skillDir != "" {
		return skillDir, nil
	}
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
	resolved, err := svc.ws.ResolveExisting(raw)
	if err != nil {
		return "", err
	}
	converted, ok := windowsPathToWSL(resolved.Abs)
	if !ok {
		return "", toolErrorDetails("INVALID_ARGUMENT", "relative WSL workdir could not be mapped from the Windows host path", "validation", map[string]any{"workdir": raw, "resolved_path": resolved.Abs})
	}
	return converted, nil
}

func WorkdirDescription() string {
	return "Working directory. runtime=windows uses a Host path. runtime=wsl accepts a WSL POSIX path such as /home/user/project or /mnt/d/Project and converts Windows drive paths such as D:\\Project."
}

func AddRuntimeProperties(props map[string]any) {
	props["runtime"] = map[string]any{"type": "string", "description": "Command runtime on Windows. Defaults to windows; use wsl to run through wsl.exe.", "enum": []string{"windows", "wsl"}}
	props["wsl_distribution"] = map[string]any{"type": "string", "description": "Optional WSL distribution name used only when runtime=wsl. Omit it to use the system default distribution."}
}

func Description() string {
	return "Run a bounded command on Windows or WSL. Bind an exact Skill with skill_ref to use its resolved root; managed Skills receive isolated environment plus private SKILL_DATA_DIR (Host path on Windows, Linux path in WSL). Explicit workdir and non-reserved env values override defaults. runtime defaults to windows."
}

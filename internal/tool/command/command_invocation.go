package command

import (
	"context"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/envstore"
	"github.com/uvwt/agentdock/internal/tool/command/session"
	"github.com/uvwt/agentdock/internal/workspace"
)

type commandInvocation struct {
	command      string
	workdir      string
	env          []string
	build        session.CommandFactory
	execution    session.ExecutionContext
	skillRelease func()
}

func (invocation commandInvocation) start(ctx context.Context, timeout time.Duration, tty bool, prepare session.PrepareFunc) (*session.Session, session.PreparationStatus, error) {
	if invocation.build != nil {
		return session.StartCommandWithTTY(ctx, invocation.build, timeout, tty, prepare)
	}
	return session.StartWithTTY(ctx, invocation.command, invocation.workdir, invocation.env, timeout, tty, prepare)
}

func (svc *Service) acquireSkill(ctx context.Context, skillRef string) (SkillLease, error) {
	skillRef = strings.TrimSpace(skillRef)
	if skillRef == "" {
		return SkillLease{}, nil
	}
	if svc.resolveSkill == nil {
		return SkillLease{}, toolError("SKILL_CONTEXT_INVALID", "Skill resolver is unavailable", "runtime")
	}
	lease, err := svc.resolveSkill(ctx, skillRef)
	if err != nil {
		return SkillLease{}, err
	}
	if lease.Release == nil {
		lease.Release = func() {}
	}
	if strings.TrimSpace(lease.Name) == "" || strings.TrimSpace(lease.Root) == "" {
		lease.Release()
		return SkillLease{}, toolErrorDetails("SKILL_CONTEXT_INVALID", "resolved Skill context is incomplete", "runtime", map[string]any{"skill_ref": skillRef})
	}
	return lease, nil
}

func (svc *Service) newHostCommandInvocation(ctx context.Context, request ExecRequest) (commandInvocation, error) {
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

	workdir, err := svc.resolveHostCommandWorkdir(request.Workdir, lease.Root)
	if err != nil {
		return commandInvocation{}, err
	}
	info, err := os.Stat(workdir)
	if err != nil {
		return commandInvocation{}, err
	}
	if !info.IsDir() {
		return commandInvocation{}, toolError("NOT_A_DIRECTORY", "workdir is not a directory", "validation")
	}
	runtimeEnv := map[string]string(nil)
	expectedDataDir := ""
	if lease.EnvName != "" {
		expectedDataDir, err = config.SkillDataDir(svc.config(), lease.EnvName)
		if err != nil {
			return commandInvocation{}, toolErrorDetails("SKILL_DATA_DIR_INVALID", "resolve managed Skill data directory", "runtime", map[string]any{
				"skill": lease.EnvName, "reason": err.Error(),
			})
		}
		runtimeEnv = map[string]string{config.SkillDataDirEnvKey: expectedDataDir}
	}
	commandEnv, err := svc.commandEnvWithRuntime(lease.EnvName, request.Env, runtimeEnv)
	if err != nil {
		return commandInvocation{}, err
	}
	if expectedDataDir != "" {
		if _, err := svc.ensureManagedSkillDataDir(lease.EnvName); err != nil {
			return commandInvocation{}, err
		}
	}
	releaseOnError = false
	return commandInvocation{
		command: request.Cmd, workdir: workdir, env: commandEnv, skillRelease: lease.Release,
	}, nil
}

func (svc *Service) resolveHostCommandWorkdir(requested, skillDir string) (string, error) {
	if raw := strings.TrimSpace(requested); raw != "" {
		resolved, err := svc.ws.ResolveExisting(raw)
		if err != nil {
			return "", err
		}
		return resolved.Abs, nil
	}
	if skillDir != "" {
		return skillDir, nil
	}
	resolved, err := svc.ws.ResolveExisting(".")
	if err != nil {
		return "", err
	}
	return resolved.Abs, nil
}

func (svc *Service) commandEnvOverrides(skillName string, extra map[string]string) (map[string]string, error) {
	overrides := map[string]string{}
	if skillName != "" {
		values, err := svc.envs.Load(envstore.Scope{Kind: envstore.ScopeSkill, Name: skillName})
		if err != nil {
			return nil, toolErrorDetails("SKILL_ENV_INVALID", "load Skill environment", "validation", map[string]any{"skill": skillName, "reason": err.Error()})
		}
		for key, value := range values {
			if config.IsReservedSkillEnvironmentKey(key) {
				return nil, toolErrorDetails("SKILL_ENV_INVALID", reservedSkillEnvironmentError(key).Error(), "validation", map[string]any{"skill": skillName, "key": key})
			}
			setPlatformCommandEnv(overrides, key, value)
		}
	}
	for key, value := range extra {
		if err := envstore.ValidateKey(key); err != nil {
			return nil, toolErrorDetails("INVALID_ENV_NAME", err.Error(), "validation", map[string]any{"key": key})
		}
		if config.IsReservedSkillEnvironmentKey(key) {
			return nil, toolErrorDetails("INVALID_ENV_NAME", reservedSkillEnvironmentError(key).Error(), "validation", map[string]any{"key": key})
		}
		setPlatformCommandEnv(overrides, key, value)
	}
	return overrides, nil
}

func buildWSLCommandArgs(distribution, workdir, command string) []string {
	args := make([]string, 0, 8)
	if distribution != "" {
		args = append(args, "--distribution", distribution)
	}
	if workdir != "" {
		args = append(args, "--cd", workdir)
	}
	return append(args, "--exec", "bash", "-lc", command)
}

func buildWSLProcessEnv(base []string, forwarded map[string]string) []string {
	values := make(map[string]string, len(base)+len(forwarded)+1)
	names := make(map[string]string, len(base)+len(forwarded)+1)
	for _, entry := range base {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		normalized := strings.ToUpper(key)
		names[normalized] = key
		values[normalized] = value
	}

	wslEnvItems := make([]string, 0, len(forwarded))
	forwardedNames := map[string]bool{}
	existingWSLEnv := values["WSLENV"]
	for key, value := range forwarded {
		if strings.EqualFold(key, "WSLENV") {
			existingWSLEnv = value
			break
		}
	}
	if existingWSLEnv != "" {
		for _, item := range strings.Split(existingWSLEnv, ":") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			wslEnvItems = append(wslEnvItems, item)
			name := strings.SplitN(item, "/", 2)[0]
			forwardedNames[strings.ToUpper(name)] = true
		}
	}

	keys := make([]string, 0, len(forwarded))
	for key := range forwarded {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		normalized := strings.ToUpper(key)
		if normalized == "WSLENV" {
			continue
		}
		names[normalized] = key
		values[normalized] = forwarded[key]
		if !forwardedNames[normalized] {
			wslEnvItems = append(wslEnvItems, key)
			forwardedNames[normalized] = true
		}
	}
	if len(wslEnvItems) > 0 {
		names["WSLENV"] = "WSLENV"
		values["WSLENV"] = strings.Join(wslEnvItems, ":")
	} else {
		delete(names, "WSLENV")
		delete(values, "WSLENV")
	}

	normalizedKeys := make([]string, 0, len(values))
	for key := range values {
		normalizedKeys = append(normalizedKeys, key)
	}
	sort.Strings(normalizedKeys)
	result := make([]string, 0, len(normalizedKeys))
	for _, normalized := range normalizedKeys {
		result = append(result, names[normalized]+"="+values[normalized])
	}
	return result
}

func windowsPathToWSL(raw string) (string, bool) {
	return workspace.WindowsPathToWSL(raw)
}

func newWSLCommandFactory(executable string, args, hostEnv []string, hostWorkdir string) session.CommandFactory {
	commandArgs := append([]string(nil), args...)
	commandEnv := append([]string(nil), hostEnv...)
	return func(ctx context.Context) *exec.Cmd {
		cmd := exec.CommandContext(ctx, executable, commandArgs...)
		cmd.Dir = hostWorkdir
		cmd.Env = commandEnv
		return cmd
	}
}

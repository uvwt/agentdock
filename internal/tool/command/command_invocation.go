package command

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/envstore"
	"github.com/uvwt/agentdock/internal/session"
	"github.com/uvwt/agentdock/internal/workspace"
)

type commandInvocation struct {
	command   string
	workdir   string
	env       []string
	build     session.CommandFactory
	execution session.ExecutionContext
}

type commandSkillContext struct {
	skill    string
	envSkill string
}

func (invocation commandInvocation) start(ctx context.Context, timeout time.Duration, tty bool, prepare session.PrepareFunc) (*session.Session, map[string]any, error) {
	if invocation.build != nil {
		return session.StartCommandWithTTY(ctx, invocation.build, timeout, tty, prepare)
	}
	return session.StartWithTTY(ctx, invocation.command, invocation.workdir, invocation.env, timeout, tty, prepare)
}

func (svc *Service) newHostCommandInvocation(args map[string]any, command string) (commandInvocation, error) {
	skillContext, err := parseCommandSkillContext(args)
	if err != nil {
		return commandInvocation{}, err
	}
	workdir, err := svc.resolveHostCommandWorkdir(args, skillContext.skill)
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
	commandEnv, err := svc.commandEnv(skillContext.envSkill, mapArg(args, "env"))
	if err != nil {
		return commandInvocation{}, err
	}
	return commandInvocation{command: command, workdir: workdir, env: commandEnv}, nil
}

func parseCommandSkillContext(args map[string]any) (commandSkillContext, error) {
	skill := strings.TrimSpace(stringArg(args, "skill", ""))
	envSkill := strings.TrimSpace(stringArg(args, "skill_env", ""))
	if skill != "" && envSkill != "" && skill != envSkill {
		return commandSkillContext{}, toolErrorDetails(
			"INVALID_ARGUMENT",
			"skill and skill_env must reference the same Skill when both are provided",
			"validation",
			map[string]any{"skill": skill, "skill_env": envSkill},
		)
	}
	if envSkill == "" {
		envSkill = skill
	}
	return commandSkillContext{skill: skill, envSkill: envSkill}, nil
}

func (svc *Service) resolveHostCommandWorkdir(args map[string]any, skill string) (string, error) {
	skillDir := ""
	if skill != "" {
		var err error
		skillDir, err = svc.resolveSkillCommandDir(skill)
		if err != nil {
			return "", err
		}
	}
	if raw := strings.TrimSpace(stringArg(args, "workdir", "")); raw != "" {
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

func (svc *Service) resolveSkillCommandDir(skill string) (string, error) {
	path, err := svc.resolveSkill(skill)
	if err != nil {
		return "", toolErrorDetails(
			"SKILL_CONTEXT_INVALID",
			"resolve active Skill directory: "+err.Error(),
			"validation",
			map[string]any{"skill": skill, "reason": err.Error()},
		)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", toolErrorDetails(
			"SKILL_CONTEXT_INVALID",
			"inspect active Skill directory: "+err.Error(),
			"validation",
			map[string]any{"skill": skill, "reason": err.Error()},
		)
	}
	if !info.IsDir() {
		return "", toolErrorDetails(
			"SKILL_CONTEXT_INVALID",
			"active Skill path is not a directory",
			"validation",
			map[string]any{"skill": skill, "path": path},
		)
	}
	return path, nil
}

func (svc *Service) commandEnvOverrides(skillName string, extra map[string]any) (map[string]string, error) {
	overrides := map[string]string{}
	if skillName != "" {
		values, err := svc.envs.Load(envstore.Scope{Kind: envstore.ScopeSkill, Name: skillName})
		if err != nil {
			return nil, toolErrorDetails("SKILL_ENV_INVALID", "load Skill environment", "validation", map[string]any{"skill": skillName, "reason": err.Error()})
		}
		for key, value := range values {
			overrides[key] = value
		}
	}
	for key, value := range extra {
		if err := envstore.ValidateKey(key); err != nil {
			return nil, toolErrorDetails("INVALID_ENV_NAME", err.Error(), "validation", map[string]any{"key": key})
		}
		overrides[key] = fmt.Sprint(value)
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

// buildWSLProcessEnv 避免把环境变量值暴露在 Windows 命令行中。
// WSLENV 只携带变量名，wsl.exe 从自身进程环境读取对应值并传入 Linux。
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

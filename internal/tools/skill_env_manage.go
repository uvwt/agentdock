package tools

import (
	"context"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/skillruntime"
	"github.com/uvwt/agentdock/internal/skillstate"
)

func (r *Runtime) skillEnvManage(ctx context.Context, args map[string]any) (Result, error) {
	action := strings.ToLower(strings.TrimSpace(stringArg(args, "action", "")))
	switch action {
	case "list":
		items, err := r.skills.env.List()
		if err != nil {
			return nil, toolErrorDetails("ENV_REGISTRY_FAILED", err.Error(), "runtime", nil)
		}
		return Result{"ok": true, "action": "list", "skills": items, "count": len(items)}, nil
	case "inspect":
		skill, err := requiredSkillArg(args)
		if err != nil {
			return nil, err
		}
		items, err := r.skills.env.Inspect(skill)
		if err != nil {
			return nil, toolErrorDetails("ENV_REGISTRY_FAILED", err.Error(), "runtime", nil)
		}
		return Result{"ok": true, "action": "inspect", "skill": skill, "vars": items, "count": len(items)}, nil
	case "set":
		skill, err := requiredSkillArg(args)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSpace(stringArg(args, "name", ""))
		value := stringArg(args, "value", "")
		kind := strings.TrimSpace(stringArg(args, "kind", "secret"))
		entry, err := r.skills.env.Set(skill, name, kind, value)
		if err != nil {
			return nil, toolErrorDetails("ENV_REGISTRY_FAILED", err.Error(), "runtime", nil)
		}
		return Result{"ok": true, "action": "set", "skill": skill, "var": entry}, nil
	case "delete":
		skill, err := requiredSkillArg(args)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSpace(stringArg(args, "name", ""))
		deleted, err := r.skills.env.Delete(skill, name)
		if err != nil {
			return nil, toolErrorDetails("ENV_REGISTRY_FAILED", err.Error(), "runtime", nil)
		}
		return Result{"ok": true, "action": "delete", "skill": skill, "name": name, "deleted": deleted}, nil
	case "verify":
		return r.envVerify(ctx, args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported skill_env_manage action", "validation", map[string]any{
			"action":  action,
			"allowed": []string{"list", "inspect", "set", "delete", "verify"},
		})
	}
}

func (r *Runtime) envVerify(ctx context.Context, args map[string]any) (Result, error) {
	skill, err := requiredSkillArg(args)
	if err != nil {
		return nil, err
	}
	operation := strings.TrimSpace(stringArg(args, "operation", ""))
	if operation == "" {
		operation = "status"
	}
	request, err := skillRunRequest(skill, operation, args)
	if err != nil {
		return nil, err
	}
	run, err := r.skills.runtime.Run(ctx, request)
	ok := err == nil && run.OK
	message := "ok"
	if err != nil {
		message = err.Error()
	} else if !run.OK {
		message = run.Error
	}
	if recordErr := r.skills.env.RecordVerification(skill, ok, message); recordErr != nil {
		return nil, toolErrorCause(
			"ENV_REGISTRY_FAILED",
			"record skill verification: "+recordErr.Error(),
			"runtime",
			map[string]any{"skill": skill, "operation": operation, "run_ok": ok},
			recordErr,
		)
	}
	if err != nil {
		return Result{"ok": false, "action": "verify", "skill": skill, "result": run, "message": message}, nil
	}
	return Result{"ok": ok, "action": "verify", "skill": skill, "result": run, "message": message}, nil
}

func skillRunRequest(skill, operation string, args map[string]any) (skillruntime.RunRequest, error) {
	input, err := skillRunInput(args)
	if err != nil {
		return skillruntime.RunRequest{}, err
	}
	return skillruntime.RunRequest{
		Skill:     skill,
		Version:   strings.TrimSpace(stringArg(args, "version", "")),
		Channel:   skillstate.Channel(strings.TrimSpace(stringArg(args, "channel", ""))),
		Operation: operation,
		Binding:   strings.TrimSpace(stringArg(args, "binding", "")),
		Timeout:   time.Duration(intArg(args, "timeout_ms", 0)) * time.Millisecond,
		MaxOutput: intArg(args, "max_output_bytes", 0),
		Input:     input,
	}, nil
}

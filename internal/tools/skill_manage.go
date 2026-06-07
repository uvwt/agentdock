package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/skillruntime"
	"github.com/uvwt/agentdock/internal/skillstate"
)

type skillManager struct {
	runtime  *skillruntime.Runtime
	state    *skillstate.Store
	bindings *skillruntime.BindingStore
}

func newSkillManager(cfg config.Config) (*skillManager, error) {
	stateDir, err := config.ResolveNexusStateDir(cfg)
	if err != nil {
		return nil, err
	}
	state, err := skillstate.New(filepath.Join(stateDir, "skills"))
	if err != nil {
		return nil, err
	}
	bindings, err := skillruntime.NewBindingStore(filepath.Join(stateDir, "bindings"))
	if err != nil {
		return nil, err
	}
	runtime, err := skillruntime.New(state, bindings)
	if err != nil {
		return nil, err
	}
	return &skillManager{runtime: runtime, state: state, bindings: bindings}, nil
}

func (r *Runtime) skillManage(ctx context.Context, args map[string]any) (Result, error) {
	action := strings.ToLower(strings.TrimSpace(stringArg(args, "action", "")))
	switch action {
	case "list":
		return r.skillList()
	case "inspect":
		return r.skillInspect(args)
	case "install":
		return r.skillInstall(ctx, args)
	case "run":
		return r.skillRun(ctx, args)
	case "rollback":
		return r.skillRollback(ctx, args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported skill_manage action", "validation", map[string]any{
			"action":  action,
			"allowed": []string{"list", "inspect", "install", "run", "rollback"},
		})
	}
}

func (r *Runtime) skillList() (Result, error) {
	names, err := r.skills.state.ListSkills()
	if err != nil {
		return nil, skillToolError(err)
	}
	items := make([]map[string]any, 0, len(names))
	for _, name := range names {
		versions, err := r.skills.state.ListVersions(name)
		if err != nil {
			return nil, skillToolError(err)
		}
		selection, err := r.skills.state.Snapshot(name)
		if err != nil {
			return nil, skillToolError(err)
		}
		items = append(items, map[string]any{
			"skill":          name,
			"versions":       versions,
			"active_version": selection.ActiveVersion,
			"channels":       selection.Channels,
			"updated_at":     selection.UpdatedAt,
		})
	}
	return Result{"ok": true, "action": "list", "count": len(items), "skills": items}, nil
}

func (r *Runtime) skillInspect(args map[string]any) (Result, error) {
	skill, err := requiredSkillArg(args)
	if err != nil {
		return nil, err
	}
	versions, err := r.skills.state.ListVersions(skill)
	if err != nil {
		return nil, skillToolError(err)
	}
	selection, err := r.skills.state.Snapshot(skill)
	if err != nil {
		return nil, skillToolError(err)
	}
	selected := strings.TrimSpace(stringArg(args, "version", ""))
	if selected == "" {
		channel := skillstate.Channel(strings.TrimSpace(stringArg(args, "channel", "")))
		if channel != "" {
			selected = selection.Channels[channel]
		}
	}
	if selected == "" {
		selected = selection.ActiveVersion
	}
	bindingConfigured := false
	if _, statErr := os.Stat(r.skills.bindings.Path(skill)); statErr == nil {
		bindingConfigured = true
	}
	result := Result{
		"ok":                 true,
		"action":             "inspect",
		"skill":              skill,
		"versions":           versions,
		"selection":          selection,
		"binding_configured": bindingConfigured,
	}
	if selected == "" {
		return result, nil
	}
	installed, err := r.skills.state.IsInstalled(skill, selected)
	if err != nil {
		return nil, skillToolError(err)
	}
	if !installed {
		return nil, toolErrorDetails("SKILL_VERSION_NOT_INSTALLED", "requested skill version is not installed", "validation", map[string]any{"skill": skill, "version": selected})
	}
	packageDir, err := r.skills.state.InstalledPath(skill, selected)
	if err != nil {
		return nil, skillToolError(err)
	}
	manifest, err := skillruntime.LoadManifest(packageDir)
	if err != nil {
		return nil, skillToolError(err)
	}
	result["version"] = selected
	result["manifest"] = manifest
	return result, nil
}

func (r *Runtime) skillInstall(ctx context.Context, args map[string]any) (Result, error) {
	source := strings.TrimSpace(stringArg(args, "source", ""))
	if source == "" {
		return nil, toolErrorDetails("VALIDATION_ERROR", "source is required for skill install", "validation", map[string]any{"field": "source"})
	}
	resolved, err := r.resolveSkillSource(source)
	if err != nil {
		return nil, err
	}
	result, err := r.skills.runtime.Install(ctx, skillruntime.InstallRequest{
		Source:       resolved,
		DigestSHA256: strings.TrimSpace(stringArg(args, "digest", "")),
		Activate:     boolArg(args, "activate", true),
		Channel:      skillstate.Channel(strings.TrimSpace(stringArg(args, "channel", string(skillstate.ChannelStable)))),
		MaxBytes:     int64(intArg(args, "max_bytes", 0)),
	})
	if err != nil {
		return nil, skillToolError(err)
	}
	return Result{"ok": true, "action": "install", "result": result}, nil
}

func (r *Runtime) skillRun(ctx context.Context, args map[string]any) (Result, error) {
	skill, err := requiredSkillArg(args)
	if err != nil {
		return nil, err
	}
	operation := strings.TrimSpace(stringArg(args, "operation", ""))
	if operation == "" {
		return nil, toolErrorDetails("VALIDATION_ERROR", "operation is required for skill run", "validation", map[string]any{"field": "operation"})
	}
	input := json.RawMessage(`{}`)
	if inputJSON := strings.TrimSpace(stringArg(args, "input_json", "")); inputJSON != "" {
		if !json.Valid([]byte(inputJSON)) {
			return nil, toolErrorDetails("VALIDATION_ERROR", "input_json must contain valid JSON", "validation", map[string]any{"field": "input_json"})
		}
		input = json.RawMessage(inputJSON)
	} else if value, ok := args["input"]; ok {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, toolErrorDetails("VALIDATION_ERROR", "input cannot be encoded as JSON", "validation", map[string]any{"field": "input", "reason": err.Error()})
		}
		input = encoded
	}
	result, err := r.skills.runtime.Run(ctx, skillruntime.RunRequest{
		RunID:     strings.TrimSpace(stringArg(args, "run_id", "")),
		Skill:     skill,
		Version:   strings.TrimSpace(stringArg(args, "version", "")),
		Channel:   skillstate.Channel(strings.TrimSpace(stringArg(args, "channel", ""))),
		Operation: operation,
		Input:     input,
		Binding:   strings.TrimSpace(stringArg(args, "binding", "")),
		Timeout:   time.Duration(intArg(args, "timeout_ms", 0)) * time.Millisecond,
		MaxOutput: intArg(args, "max_output_bytes", 0),
	})
	if err != nil {
		return nil, skillToolError(err)
	}
	return Result{"ok": true, "action": "run", "result": result}, nil
}

func (r *Runtime) skillRollback(ctx context.Context, args map[string]any) (Result, error) {
	skill, err := requiredSkillArg(args)
	if err != nil {
		return nil, err
	}
	result, err := r.skills.runtime.Rollback(ctx, skill, skillstate.Channel(strings.TrimSpace(stringArg(args, "channel", string(skillstate.ChannelStable)))), nil)
	if err != nil {
		return nil, skillToolError(err)
	}
	return Result{"ok": true, "action": "rollback", "result": result}, nil
}

func (r *Runtime) resolveSkillSource(source string) (string, error) {
	if parsed, err := url.Parse(source); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		return source, nil
	}
	p, err := r.ws.ResolveExisting(source)
	if err != nil {
		return "", toolErrorDetails("SKILL_SOURCE_INVALID", "skill source cannot be resolved", "validation", map[string]any{"source": source, "reason": err.Error()})
	}
	return p.Abs, nil
}

func requiredSkillArg(args map[string]any) (string, error) {
	skill := strings.TrimSpace(stringArg(args, "skill", ""))
	if skill == "" {
		return "", toolErrorDetails("VALIDATION_ERROR", "skill is required", "validation", map[string]any{"field": "skill"})
	}
	return skill, nil
}

func skillToolError(err error) error {
	var runtimeErr *skillruntime.Error
	if errors.As(err, &runtimeErr) {
		return toolErrorDetails(runtimeErr.Code, runtimeErr.Error(), "runtime", map[string]any{"stage": runtimeErr.Stage})
	}
	return toolErrorDetails("SKILL_OPERATION_FAILED", err.Error(), "runtime", nil)
}

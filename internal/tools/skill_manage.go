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

	"github.com/uvwt/agentdock/internal/compatenv"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/envregistry"
	"github.com/uvwt/agentdock/internal/skillruntime"
	"github.com/uvwt/agentdock/internal/skillstate"
)

// skillRuntimeManager 集中保存 Skill Runtime 的运行态依赖。
// 它不是通用 Manager，只负责 Skill 包、channel、binding 和 Env 注入这条链路。
type skillRuntimeManager struct {
	runtime  *skillruntime.Runtime
	state    *skillstate.Store
	bindings *skillruntime.BindingStore
	env      *envregistry.Store
}

func newSkillRuntimeManager(cfg config.Config) (*skillRuntimeManager, error) {
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
	skills := &skillRuntimeManager{state: state, bindings: bindings}
	envStore, err := envregistry.New(filepath.Join(stateDir, "env"), skills.envDefinitions)
	if err != nil {
		return nil, err
	}
	runtime, err := skillruntime.New(state, bindings)
	if err != nil {
		return nil, err
	}
	skills.runtime = runtime
	skills.env = envStore
	runtime.EnvProvider = skillEnvProvider{store: envStore}
	return skills, nil
}

type skillEnvProvider struct{ store *envregistry.Store }

func (p skillEnvProvider) EnvForSkill(skill string, definitions []skillruntime.EnvDefinition) (map[string]string, []string, error) {
	converted := make([]envregistry.Definition, 0, len(definitions))
	for _, def := range definitions {
		converted = append(converted, envregistry.Definition{
			Skill:  def.Skill,
			Name:   def.Name,
			Kind:   def.Kind,
			Source: def.Source,
		})
	}
	return p.store.EnvForSkill(skill, converted)
}

func (r *Runtime) skillManage(ctx context.Context, args map[string]any) (Result, error) {
	action := strings.ToLower(strings.TrimSpace(stringArg(args, "action", "")))
	switch action {
	case "list":
		return r.skillList()
	case "inspect":
		return r.skillInspect(args)
	case "validate":
		return r.skillValidate(ctx, args)
	case "install":
		return r.skillInstall(ctx, args)
	case "run":
		return r.skillRun(ctx, args)
	case "rollback":
		return r.skillRollback(ctx, args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported skill_manage action", "validation", map[string]any{
			"action":  action,
			"allowed": []string{"list", "inspect", "validate", "install", "run", "rollback"},
		})
	}
}

func (m *skillRuntimeManager) envDefinitions() []envregistry.Definition {
	names, err := m.state.ListSkills()
	if err != nil {
		return compatEnvDefinitions()
	}
	defsByKey := map[string]envregistry.Definition{}
	for _, name := range names {
		selection, err := m.state.Snapshot(name)
		if err != nil || selection.ActiveVersion == "" {
			continue
		}
		packageDir, err := m.state.InstalledPath(name, selection.ActiveVersion)
		if err != nil {
			continue
		}
		manifest, err := skillruntime.LoadManifest(packageDir)
		if err != nil {
			continue
		}
		for _, def := range skillruntime.EnvDefinitionsForManifest(manifest) {
			key := def.Skill + "\x00" + def.Name
			defsByKey[key] = envregistry.Definition{Skill: def.Skill, Name: def.Name, Kind: def.Kind, Source: def.Source}
		}
	}
	for _, def := range compatEnvDefinitions() {
		key := def.Skill + "\x00" + def.Name
		if _, ok := defsByKey[key]; !ok {
			defsByKey[key] = def
		}
	}
	defs := make([]envregistry.Definition, 0, len(defsByKey))
	for _, def := range defsByKey {
		defs = append(defs, def)
	}
	return defs
}

func compatEnvDefinitions() []envregistry.Definition {
	defs := compatenv.All()
	items := make([]envregistry.Definition, 0, len(defs))
	for _, def := range defs {
		items = append(items, envregistry.Definition{Skill: def.Skill, Name: def.Name, Kind: def.Kind, Source: def.Source})
	}
	return items
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

func (r *Runtime) skillValidate(ctx context.Context, args map[string]any) (Result, error) {
	source := strings.TrimSpace(stringArg(args, "source", ""))
	if source == "" {
		return nil, toolErrorDetails("VALIDATION_ERROR", "source is required for skill validate", "validation", map[string]any{"field": "source"})
	}
	resolved, err := r.resolveSkillSource(source)
	if err != nil {
		return nil, err
	}
	result, err := r.skills.runtime.Validate(ctx, skillruntime.ValidateRequest{
		Source:         resolved,
		DigestSHA256:   strings.TrimSpace(stringArg(args, "digest", "")),
		MaxBytes:       int64(intArg(args, "max_bytes", 0)),
		ConfirmedNoEnv: boolArg(args, "confirmed_no_env", false),
	})
	if err != nil {
		return nil, skillToolError(err)
	}
	response := Result{
		"ok":                      true,
		"action":                  "validate",
		"valid":                   result.Valid,
		"source":                  result.Source,
		"digest":                  result.Digest,
		"env":                     result.Env,
		"commands":                result.Commands,
		"issues":                  result.Issues,
		"requires_no_env_confirm": result.RequiresNoEnvConfirm,
	}
	if result.Manifest.Metadata.Name != "" {
		response["manifest"] = result.Manifest
	}
	return response, nil
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
		Source:         resolved,
		DigestSHA256:   strings.TrimSpace(stringArg(args, "digest", "")),
		Activate:       boolArg(args, "activate", true),
		Channel:        skillstate.Channel(strings.TrimSpace(stringArg(args, "channel", string(skillstate.ChannelStable)))),
		MaxBytes:       int64(intArg(args, "max_bytes", 0)),
		ConfirmedNoEnv: boolArg(args, "confirmed_no_env", false),
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
	input, err := skillRunInput(args)
	if err != nil {
		return nil, err
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

func skillRunInput(args map[string]any) (json.RawMessage, error) {
	if inputJSON := strings.TrimSpace(stringArg(args, "input_json", "")); inputJSON != "" {
		if !json.Valid([]byte(inputJSON)) {
			return nil, toolErrorDetails("VALIDATION_ERROR", "input_json must contain valid JSON", "validation", map[string]any{"field": "input_json"})
		}
		return json.RawMessage(inputJSON), nil
	}
	if value, ok := args["input"]; ok {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, toolErrorDetails("VALIDATION_ERROR", "input cannot be encoded as JSON", "validation", map[string]any{"field": "input", "reason": err.Error()})
		}
		return encoded, nil
	}
	return json.RawMessage(`{}`), nil
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

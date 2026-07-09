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

type skillToolInput struct {
	Action         string
	Skill          string
	Version        string
	Channel        skillstate.Channel
	Source         string
	Digest         string
	MaxBytes       int64
	ConfirmedNoEnv bool
	Activate       bool
	Operation      string
	RunID          string
	Binding        string
	Timeout        time.Duration
	MaxOutput      int
	InputJSON      string
	InputValue     any
	HasInputValue  bool
}

func parseSkillToolInput(args map[string]any) skillToolInput {
	inputValue, hasInputValue := args["input"]
	return skillToolInput{
		Action:         strings.ToLower(strings.TrimSpace(stringArg(args, "action", ""))),
		Skill:          strings.TrimSpace(stringArg(args, "skill", "")),
		Version:        strings.TrimSpace(stringArg(args, "version", "")),
		Channel:        skillstate.Channel(strings.TrimSpace(stringArg(args, "channel", ""))),
		Source:         strings.TrimSpace(stringArg(args, "source", "")),
		Digest:         strings.TrimSpace(stringArg(args, "digest", "")),
		MaxBytes:       int64(intArg(args, "max_bytes", 0)),
		ConfirmedNoEnv: boolArg(args, "confirmed_no_env", false),
		Activate:       boolArg(args, "activate", true),
		Operation:      strings.TrimSpace(stringArg(args, "operation", "")),
		RunID:          strings.TrimSpace(stringArg(args, "run_id", "")),
		Binding:        strings.TrimSpace(stringArg(args, "binding", "")),
		Timeout:        time.Duration(intArg(args, "timeout_ms", 0)) * time.Millisecond,
		MaxOutput:      intArg(args, "max_output_bytes", 0),
		InputJSON:      strings.TrimSpace(stringArg(args, "input_json", "")),
		InputValue:     inputValue,
		HasInputValue:  hasInputValue,
	}
}

func (input skillToolInput) channelOr(fallback skillstate.Channel) skillstate.Channel {
	if input.Channel == "" {
		return fallback
	}
	return input.Channel
}

func (input skillToolInput) requiredSkill() (string, error) {
	if input.Skill == "" {
		return "", toolErrorDetails("VALIDATION_ERROR", "skill is required", "validation", map[string]any{"field": "skill"})
	}
	return input.Skill, nil
}

func (input skillToolInput) runInput() (json.RawMessage, error) {
	if input.InputJSON != "" {
		if !json.Valid([]byte(input.InputJSON)) {
			return nil, toolErrorDetails("VALIDATION_ERROR", "input_json must contain valid JSON", "validation", map[string]any{"field": "input_json"})
		}
		return json.RawMessage(input.InputJSON), nil
	}
	if input.HasInputValue {
		encoded, err := json.Marshal(input.InputValue)
		if err != nil {
			return nil, toolErrorDetails("VALIDATION_ERROR", "input cannot be encoded as JSON", "validation", map[string]any{"field": "input", "reason": err.Error()})
		}
		return encoded, nil
	}
	return json.RawMessage(`{}`), nil
}

func newSkillRuntimeManager(cfg config.Config) (*skillRuntimeManager, error) {
	stateDir, err := config.NexusStateDir(cfg)
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

func (r *Runtime) skillRead(_ context.Context, args map[string]any) (Result, error) {
	input := parseSkillToolInput(args)
	switch input.Action {
	case "list":
		return r.skillList()
	case "inspect":
		return r.skillInspectInput(input)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported skill_read action", "validation", map[string]any{
			"action":  input.Action,
			"allowed": []string{"list", "inspect"},
		})
	}
}

func (r *Runtime) skillPackage(ctx context.Context, args map[string]any) (Result, error) {
	input := parseSkillToolInput(args)
	switch input.Action {
	case "validate":
		return r.skillValidate(ctx, input)
	case "install":
		return r.skillInstall(ctx, input)
	case "rollback":
		return r.skillRollback(ctx, input)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported skill_package action", "validation", map[string]any{
			"action":  input.Action,
			"allowed": []string{"validate", "install", "rollback"},
		})
	}
}

func (m *skillRuntimeManager) envDefinitions() []envregistry.Definition {
	names, err := m.state.ListSkills()
	if err != nil {
		return nil
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
	defs := make([]envregistry.Definition, 0, len(defsByKey))
	for _, def := range defsByKey {
		defs = append(defs, def)
	}
	return defs
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
	return r.skillInspectInput(parseSkillToolInput(args))
}

func (r *Runtime) skillInspectInput(input skillToolInput) (Result, error) {
	skill, err := input.requiredSkill()
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
	selected := input.Version
	if selected == "" && input.Channel != "" {
		selected = selection.Channels[input.Channel]
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

func (r *Runtime) skillValidate(ctx context.Context, input skillToolInput) (Result, error) {
	if input.Source == "" {
		return nil, toolErrorDetails("VALIDATION_ERROR", "source is required for skill validate", "validation", map[string]any{"field": "source"})
	}
	resolved, err := r.resolveSkillSource(input.Source)
	if err != nil {
		return nil, err
	}
	result, err := r.skills.runtime.Validate(ctx, skillruntime.ValidateRequest{
		Source:         resolved,
		DigestSHA256:   input.Digest,
		MaxBytes:       input.MaxBytes,
		ConfirmedNoEnv: input.ConfirmedNoEnv,
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

func (r *Runtime) skillInstall(ctx context.Context, input skillToolInput) (Result, error) {
	if input.Source == "" {
		return nil, toolErrorDetails("VALIDATION_ERROR", "source is required for skill install", "validation", map[string]any{"field": "source"})
	}
	resolved, err := r.resolveSkillSource(input.Source)
	if err != nil {
		return nil, err
	}
	result, err := r.skills.runtime.Install(ctx, skillruntime.InstallRequest{
		Source:         resolved,
		DigestSHA256:   input.Digest,
		Activate:       input.Activate,
		Channel:        input.channelOr(skillstate.ChannelStable),
		MaxBytes:       input.MaxBytes,
		ConfirmedNoEnv: input.ConfirmedNoEnv,
	})
	if err != nil {
		return nil, skillToolError(err)
	}
	return Result{"ok": true, "action": "install", "result": result}, nil
}

func (r *Runtime) skillRun(ctx context.Context, args map[string]any) (Result, error) {
	input := parseSkillToolInput(args)
	if input.Action != "" && input.Action != "run" {
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported skill_run action", "validation", map[string]any{
			"action":  input.Action,
			"allowed": []string{"run"},
		})
	}
	skill, err := input.requiredSkill()
	if err != nil {
		return nil, err
	}
	if input.Operation == "" {
		return nil, toolErrorDetails("VALIDATION_ERROR", "operation is required for skill run", "validation", map[string]any{"field": "operation"})
	}
	rawInput, err := input.runInput()
	if err != nil {
		return nil, err
	}
	result, err := r.skills.runtime.Run(ctx, skillruntime.RunRequest{
		RunID:     input.RunID,
		Skill:     skill,
		Version:   input.Version,
		Channel:   input.Channel,
		Operation: input.Operation,
		Input:     rawInput,
		Binding:   input.Binding,
		Timeout:   input.Timeout,
		MaxOutput: input.MaxOutput,
	})
	if err != nil {
		return nil, skillToolError(err)
	}
	return Result{"ok": true, "action": "run", "result": result}, nil
}

func (r *Runtime) skillRollback(ctx context.Context, input skillToolInput) (Result, error) {
	skill, err := input.requiredSkill()
	if err != nil {
		return nil, err
	}
	result, err := r.skills.runtime.Rollback(ctx, skill, input.channelOr(skillstate.ChannelStable), nil)
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

func skillRunInput(args map[string]any) (json.RawMessage, error) {
	return parseSkillToolInput(args).runInput()
}

func requiredSkillArg(args map[string]any) (string, error) {
	return parseSkillToolInput(args).requiredSkill()
}

func skillToolError(err error) error {
	var runtimeErr *skillruntime.Error
	if errors.As(err, &runtimeErr) {
		return toolErrorDetails(runtimeErr.Code, runtimeErr.Error(), "runtime", map[string]any{"stage": runtimeErr.Stage})
	}
	return toolErrorDetails("SKILL_OPERATION_FAILED", err.Error(), "runtime", nil)
}

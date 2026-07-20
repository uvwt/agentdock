package tools

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/envstore"
	"github.com/uvwt/agentdock/internal/skills"
	"github.com/uvwt/agentdock/internal/skillstate"
)

type skillManager struct {
	manager *skills.Manager
	state   *skillstate.Store
}

type skillToolInput struct {
	Action   string
	Skill    string
	Version  string
	Source   string
	Digest   string
	MaxBytes int64
	Activate bool
}

func parseSkillToolInput(args map[string]any) skillToolInput {
	return skillToolInput{
		Action:   strings.ToLower(strings.TrimSpace(stringArg(args, "action", ""))),
		Skill:    strings.TrimSpace(stringArg(args, "skill", "")),
		Version:  strings.TrimSpace(stringArg(args, "version", "")),
		Source:   strings.TrimSpace(stringArg(args, "source", "")),
		Digest:   strings.TrimSpace(stringArg(args, "digest", "")),
		MaxBytes: int64(intArg(args, "max_bytes", 0)),
		Activate: boolArg(args, "activate", true),
	}
}

func (input skillToolInput) requiredSkill() (string, error) {
	if input.Skill == "" {
		return "", toolErrorDetails("VALIDATION_ERROR", "skill is required", "validation", map[string]any{"field": "skill"})
	}
	return input.Skill, nil
}

func newSkillManager(cfg config.Config) (*skillManager, error) {
	stateDir, err := config.SkillStateDir(cfg)
	if err != nil {
		return nil, err
	}
	state, err := skillstate.New(stateDir)
	if err != nil {
		return nil, err
	}
	manager, err := skills.New(state)
	if err != nil {
		return nil, err
	}
	return &skillManager{manager: manager, state: state}, nil
}

func (r *Runtime) skillPackage(ctx context.Context, args map[string]any) (Result, error) {
	input := parseSkillToolInput(args)
	switch input.Action {
	case "validate":
		return r.skillValidate(ctx, input)
	case "install":
		return r.skillInstall(ctx, input)
	case "activate":
		return r.skillActivate(ctx, input)
	case "rollback":
		return r.skillRollback(ctx, input)
	case "env_set", "env_unset", "env_list":
		skill, err := input.requiredSkill()
		if err != nil {
			return nil, err
		}
		return r.scopedEnvAction(envstore.ScopeSkill, skill, input.Action, args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported skill_package action", "validation", map[string]any{
			"action":  input.Action,
			"allowed": []string{"validate", "install", "activate", "rollback", "env_set", "env_unset", "env_list"},
		})
	}
}

func (r *Runtime) skillList() (Result, error) {
	names, err := r.skills.state.ListSkills()
	if err != nil {
		return nil, skillToolError(err)
	}
	bundledNames, err := r.skills.state.BundledSkills()
	if err != nil {
		return nil, skillToolError(err)
	}
	bundled := make(map[string]struct{}, len(bundledNames))
	for _, name := range bundledNames {
		bundled[name] = struct{}{}
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
		_, isBundled := bundled[name]
		items = append(items, map[string]any{
			"skill":          name,
			"versions":       versions,
			"active_version": selection.ActiveVersion,
			"bundled":        isBundled,
			"updated_at":     selection.UpdatedAt,
		})
	}
	return Result{"action": "list", "count": len(items), "skills": items}, nil
}

func (r *Runtime) skillInspect(args map[string]any) (Result, error) {
	input := parseSkillToolInput(args)
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
	bundled, err := r.skills.state.IsBundled(skill)
	if err != nil {
		return nil, skillToolError(err)
	}
	selected := input.Version
	if selected == "" {
		selected = selection.ActiveVersion
	}
	result := Result{
		"action":    "inspect",
		"skill":     skill,
		"versions":  versions,
		"selection": selection,
		"bundled":   bundled,
	}
	if selected == "" {
		return result, nil
	}
	packageDir, err := r.skills.state.InstalledPath(skill, selected)
	if err != nil {
		return nil, skillToolError(err)
	}
	doc, err := skills.LoadSkillDocument(packageDir)
	if err != nil {
		return nil, skillToolError(err)
	}
	result["version"] = selected
	result["document"] = doc
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
	result, err := r.skills.manager.Validate(ctx, skills.ValidateRequest{
		Source:       resolved,
		DigestSHA256: input.Digest,
		MaxBytes:     input.MaxBytes,
	})
	if err != nil {
		return nil, skillToolError(err)
	}
	response := Result{
		"action":   "validate",
		"valid":    result.Valid,
		"source":   result.Source,
		"digest":   result.Digest,
		"document": result.Document,
		"issues":   result.Issues,
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
	result, err := r.skills.manager.Install(ctx, skills.InstallRequest{
		Source:       resolved,
		DigestSHA256: input.Digest,
		Activate:     input.Activate,
		MaxBytes:     input.MaxBytes,
	})
	if err != nil {
		return nil, skillToolError(err)
	}
	return Result{"action": "install", "result": result}, nil
}

func (r *Runtime) skillActivate(ctx context.Context, input skillToolInput) (Result, error) {
	skill, err := input.requiredSkill()
	if err != nil {
		return nil, err
	}
	if input.Version == "" {
		return nil, toolErrorDetails("VALIDATION_ERROR", "version is required for skill activate", "validation", map[string]any{"field": "version"})
	}
	result, err := r.skills.manager.Activate(ctx, skill, input.Version)
	if err != nil {
		return nil, skillToolError(err)
	}
	return Result{"action": "activate", "result": result}, nil
}

func (r *Runtime) skillRollback(ctx context.Context, input skillToolInput) (Result, error) {
	skill, err := input.requiredSkill()
	if err != nil {
		return nil, err
	}
	result, err := r.skills.manager.Rollback(ctx, skill)
	if err != nil {
		return nil, skillToolError(err)
	}
	return Result{"action": "rollback", "result": result}, nil
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

func skillToolError(err error) error {
	var runtimeErr *skills.Error
	if errors.As(err, &runtimeErr) {
		return toolErrorDetails(runtimeErr.Code, runtimeErr.Error(), "runtime", map[string]any{"stage": runtimeErr.Stage})
	}
	return toolErrorDetails("SKILL_PACKAGE_FAILED", err.Error(), "runtime", nil)
}

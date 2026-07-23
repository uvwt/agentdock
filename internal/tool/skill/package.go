package skill

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/uvwt/agentdock/internal/envstore"
	skills "github.com/uvwt/agentdock/internal/skill"
	skillstate "github.com/uvwt/agentdock/internal/skill/state"
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

func (s *Service) Package(ctx context.Context, args map[string]any) (Result, error) {
	input := parseSkillToolInput(args)
	switch input.Action {
	case "validate":
		return s.skillValidate(ctx, input)
	case "install":
		return s.skillInstall(ctx, input)
	case "activate":
		return s.skillActivate(ctx, input)
	case "rollback":
		return s.skillRollback(ctx, input)
	case "env_set", "env_unset", "env_list":
		skill, err := input.requiredSkill()
		if err != nil {
			return nil, err
		}
		return s.scopedEnvAction(envstore.ScopeSkill, skill, input.Action, args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported skill_package action", "validation", map[string]any{
			"action":  input.Action,
			"allowed": []string{"validate", "install", "activate", "rollback", "env_set", "env_unset", "env_list"},
		})
	}
}

func (s *Service) List() (Result, error) {
	names, err := s.state.ListSkills()
	if err != nil {
		return nil, skillToolError(err)
	}
	bundledNames, err := s.state.BundledSkills()
	if err != nil {
		return nil, skillToolError(err)
	}
	bundled := make(map[string]struct{}, len(bundledNames))
	for _, name := range bundledNames {
		bundled[name] = struct{}{}
	}
	items := make([]map[string]any, 0, len(names))
	for _, name := range names {
		versions, err := s.state.ListVersions(name)
		if err != nil {
			return nil, skillToolError(err)
		}
		selection, err := s.state.Snapshot(name)
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

func (s *Service) Inspect(args map[string]any) (Result, error) {
	input := parseSkillToolInput(args)
	skill, err := input.requiredSkill()
	if err != nil {
		return nil, err
	}
	versions, err := s.state.ListVersions(skill)
	if err != nil {
		return nil, skillToolError(err)
	}
	selection, err := s.state.Snapshot(skill)
	if err != nil {
		return nil, skillToolError(err)
	}
	bundled, err := s.state.IsBundled(skill)
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
	packageDir, err := s.state.InstalledPath(skill, selected)
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

func (s *Service) skillValidate(ctx context.Context, input skillToolInput) (Result, error) {
	if input.Source == "" {
		return nil, toolErrorDetails("VALIDATION_ERROR", "source is required for skill validate", "validation", map[string]any{"field": "source"})
	}
	resolved, err := s.resolveSkillSource(input.Source)
	if err != nil {
		return nil, err
	}
	result, err := s.manager.Validate(ctx, skills.ValidateRequest{
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

func (s *Service) skillInstall(ctx context.Context, input skillToolInput) (Result, error) {
	if input.Source == "" {
		return nil, toolErrorDetails("VALIDATION_ERROR", "source is required for skill install", "validation", map[string]any{"field": "source"})
	}
	resolved, err := s.resolveSkillSource(input.Source)
	if err != nil {
		return nil, err
	}
	result, err := s.manager.Install(ctx, skills.InstallRequest{
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

func (s *Service) skillActivate(ctx context.Context, input skillToolInput) (Result, error) {
	skill, err := input.requiredSkill()
	if err != nil {
		return nil, err
	}
	if input.Version == "" {
		return nil, toolErrorDetails("VALIDATION_ERROR", "version is required for skill activate", "validation", map[string]any{"field": "version"})
	}
	result, err := s.manager.Activate(ctx, skill, input.Version)
	if err != nil {
		return nil, skillToolError(err)
	}
	return Result{"action": "activate", "result": result}, nil
}

func (s *Service) skillRollback(ctx context.Context, input skillToolInput) (Result, error) {
	skill, err := input.requiredSkill()
	if err != nil {
		return nil, err
	}
	result, err := s.manager.Rollback(ctx, skill)
	if err != nil {
		return nil, skillToolError(err)
	}
	return Result{"action": "rollback", "result": result}, nil
}

func (s *Service) resolveSkillSource(source string) (string, error) {
	if parsed, err := url.Parse(source); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		return source, nil
	}
	p, err := s.ws.ResolveExisting(source)
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

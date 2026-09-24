package skill

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/uvwt/agentdock/internal/envstore"
	skills "github.com/uvwt/agentdock/internal/skill"
)

func (s *Service) Manage(ctx context.Context, request ManageRequest) (Result, error) {
	action := strings.ToLower(strings.TrimSpace(request.Action))
	switch action {
	case "install":
		return s.install(ctx, request)
	case "remove":
		return s.remove(ctx, request)
	case "env_set", "env_unset", "env_list":
		skillRef := strings.TrimSpace(request.SkillRef)
		if skillRef == "" {
			return nil, toolErrorDetails("VALIDATION_ERROR", "skill_ref is required for environment management", "validation", map[string]any{"field": "skill_ref"})
		}
		return s.scopedEnvAction(ctx, skillRef, action, request)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported skill_manage action", "validation", map[string]any{
			"action": action, "allowed": []string{"install", "remove", "env_set", "env_unset", "env_list"},
		})
	}
}

func (s *Service) install(ctx context.Context, request ManageRequest) (Result, error) {
	source := strings.TrimSpace(request.Source)
	if source == "" {
		return nil, toolErrorDetails("VALIDATION_ERROR", "source is required for skill install", "validation", map[string]any{"field": "source"})
	}
	resolved, err := s.resolveSkillSource(source)
	if err != nil {
		return nil, err
	}
	result, err := s.manager.Install(ctx, skills.InstallRequest{
		Source: resolved, DigestSHA256: strings.TrimSpace(request.Digest),
	})
	if err != nil {
		return nil, skillToolError(err)
	}
	return Result{"action": "install", "skill": result.Skill, "content_digest": result.ContentDigest, "changed": result.Changed}, nil
}

func (s *Service) remove(ctx context.Context, request ManageRequest) (Result, error) {
	skill := strings.TrimSpace(request.Skill)
	if skill == "" {
		return nil, toolErrorDetails("VALIDATION_ERROR", "skill is required for remove", "validation", map[string]any{"field": "skill"})
	}
	var (
		result skills.RemoveResult
		err    error
	)
	if request.Purge {
		result, err = s.manager.RemoveWithPurge(ctx, skill, func() error {
			return s.purgeManagedSkillState(skill)
		})
	} else {
		result, err = s.manager.Remove(ctx, skill)
	}
	if err != nil {
		return nil, skillToolError(err)
	}
	return Result{
		"action": "remove", "skill": skill, "removed": result.Removed,
		"purged": request.Purge,
	}, nil
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
	return toolErrorDetails("SKILL_MANAGE_FAILED", err.Error(), "runtime", nil)
}

var _ = envstore.ScopeSkill

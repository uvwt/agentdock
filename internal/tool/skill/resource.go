package skill

import (
	"context"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func (s *Service) ResolveResource(ctx context.Context, raw string) (string, string, func(), error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "skill" {
		return "", "", nil, toolErrorDetails("INVALID_SKILL_URI", "invalid Skill resource URI", "validation", map[string]any{"path": raw})
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Port() != "" {
		return "", "", nil, toolErrorDetails("INVALID_SKILL_URI", "Skill resource URI cannot contain credentials, query, fragment, or port", "validation", map[string]any{"path": raw})
	}
	resourcePath, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil {
		return "", "", nil, toolErrorDetails("INVALID_SKILL_URI", "Skill URI path is not valid URL encoding", "validation", map[string]any{"path": raw})
	}
	parts := splitSkillURIPath(resourcePath)

	var skillRef string
	var relativeParts []string
	switch parsed.Host {
	case managedSourceType:
		if len(parts) < 2 {
			return "", "", nil, invalidSkillResourceURI(raw)
		}
		skillRef = ManagedSkillRef(parts[0])
		relativeParts = parts[1:]
	case sharedSourceType:
		if len(parts) < 2 {
			return "", "", nil, invalidSkillResourceURI(raw)
		}
		skillRef = SharedSkillRef(parts[0])
		relativeParts = parts[1:]
	case workspaceSourceType:
		if len(parts) < 3 {
			return "", "", nil, invalidSkillResourceURI(raw)
		}
		skillRef = "skill://workspace/" + parts[0] + "/" + parts[1]
		relativeParts = parts[2:]
	default:
		return "", "", nil, invalidSkillResourceURI(raw)
	}

	cleaned := path.Clean(strings.Join(relativeParts, "/"))
	if cleaned == "." || cleaned == "" || strings.HasPrefix(cleaned, "/") ||
		strings.HasPrefix(cleaned, "../") || cleaned == ".." || strings.Contains(cleaned, `\`) {
		return "", "", nil, toolErrorDetails("INVALID_SKILL_URI", "Skill resource path must stay inside the resolved Skill package", "validation", map[string]any{"path": raw})
	}

	resolved, release, err := s.Acquire(ctx, skillRef)
	if err != nil {
		return "", "", nil, err
	}
	keep := false
	defer func() {
		if !keep {
			release()
		}
	}()

	realRoot, err := filepath.EvalSymlinks(resolved.Root)
	if err != nil {
		return "", "", nil, toolErrorCause("SKILL_PATH_INVALID", "cannot resolve Skill package path", "runtime", map[string]any{"skill_ref": resolved.SkillRef}, err)
	}
	candidate := filepath.Join(realRoot, filepath.FromSlash(cleaned))
	realCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", nil, toolErrorCause("SKILL_RESOURCE_NOT_FOUND", "Skill resource does not exist", "not_found", map[string]any{"path": raw}, err)
		}
		return "", "", nil, toolErrorCause("SKILL_PATH_INVALID", "cannot resolve Skill resource path", "runtime", map[string]any{"path": raw}, err)
	}
	rel, err := filepath.Rel(realRoot, realCandidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", "", nil, toolErrorDetails("SKILL_PATH_ESCAPE", "Skill resource path escapes the resolved Skill package", "validation", map[string]any{"path": raw})
	}
	keep = true
	return realCandidate, resolved.SkillRef + "/" + path.Clean(cleaned), release, nil
}

func invalidSkillResourceURI(raw string) error {
	return toolErrorDetails("INVALID_SKILL_URI", "Skill resource URI does not match a supported AgentDock Skill reference", "validation", map[string]any{"path": raw})
}

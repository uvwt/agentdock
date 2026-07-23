package skill

import (
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func (s *Service) ResolveResource(raw string) (string, string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "skill" {
		return "", "", toolErrorDetails("INVALID_SKILL_URI", "invalid skill URI", "validation", map[string]any{"path": raw})
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Host == "" || strings.Contains(parsed.Host, ":") {
		return "", "", toolErrorDetails("INVALID_SKILL_URI", "skill URI must use skill://<name>/<path> without credentials, query, fragment, or port", "validation", map[string]any{"path": raw})
	}

	resourcePath, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil {
		return "", "", toolErrorDetails("INVALID_SKILL_URI", "skill URI path is not valid URL encoding", "validation", map[string]any{"path": raw})
	}
	resourcePath = strings.TrimPrefix(resourcePath, "/")
	cleaned := path.Clean(resourcePath)
	if cleaned == "." || cleaned == "" || strings.HasPrefix(cleaned, "/") || strings.HasPrefix(cleaned, "../") || cleaned == ".." || strings.Contains(cleaned, `\`) {
		return "", "", toolErrorDetails("INVALID_SKILL_URI", "skill resource path must stay inside the active Skill package", "validation", map[string]any{"path": raw})
	}

	packageDir, err := s.state.Resolve(parsed.Host, "")
	if err != nil {
		return "", "", toolErrorCause("SKILL_NOT_AVAILABLE", "skill has no active installed version", "validation", map[string]any{"skill": parsed.Host}, err)
	}
	realRoot, err := filepath.EvalSymlinks(packageDir)
	if err != nil {
		return "", "", toolErrorCause("SKILL_PATH_INVALID", "cannot resolve active Skill package path", "runtime", map[string]any{"skill": parsed.Host}, err)
	}
	candidate := filepath.Join(realRoot, filepath.FromSlash(cleaned))
	realCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", toolErrorCause("SKILL_RESOURCE_NOT_FOUND", "skill resource does not exist", "validation", map[string]any{"path": raw}, err)
		}
		return "", "", toolErrorCause("SKILL_PATH_INVALID", "cannot resolve Skill resource path", "runtime", map[string]any{"path": raw}, err)
	}
	rel, err := filepath.Rel(realRoot, realCandidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", "", toolErrorDetails("SKILL_PATH_ESCAPE", "skill resource path escapes the active Skill package", "validation", map[string]any{"path": raw})
	}

	display := "skill://" + parsed.Host + "/" + path.Clean(cleaned)
	return realCandidate, display, nil
}

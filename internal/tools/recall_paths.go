package tools

import (
	"path"
	"strings"
)

type recallPathMapping struct {
	Public  string
	Backend string
}

var recallPathMappings = []recallPathMapping{
	{Public: "recall/managed/cards", Backend: "cards"},
	{Public: "recall/managed/notes", Backend: "notes"},
	{Public: "recall/docs/projects", Backend: "projects"},
	{Public: "recall/docs/ops", Backend: "ops"},
	{Public: "recall/docs/devices", Backend: "devices"},
	{Public: "recall/docs/inbox", Backend: "inbox"},
}

func recallBackendPath(value string) string {
	return recallRewritePath(value, true)
}

func recallPublicPath(value string) string {
	return recallRewritePath(value, false)
}

func recallRewritePath(value string, toBackend bool) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "/"))
	if value == "" {
		return value
	}
	cleaned := path.Clean(value)
	if cleaned == "." || strings.HasPrefix(cleaned, "../") {
		return value
	}
	for _, mapping := range recallPathMappings {
		from, to := mapping.Backend, mapping.Public
		if toBackend {
			from, to = mapping.Public, mapping.Backend
		}
		if cleaned == from {
			return to
		}
		if strings.HasPrefix(cleaned, from+"/") {
			return to + strings.TrimPrefix(cleaned, from)
		}
	}
	return cleaned
}

func recallMapResponsePaths(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if recallPathLikeKey(key) {
				switch s := item.(type) {
				case string:
					typed[key] = recallPublicPath(s)
				case []any:
					for i, raw := range s {
						if v, ok := raw.(string); ok {
							s[i] = recallPublicPath(v)
						} else {
							s[i] = recallMapResponsePaths(raw)
						}
					}
					typed[key] = s
				default:
					typed[key] = recallMapResponsePaths(item)
				}
				continue
			}
			typed[key] = recallMapResponsePaths(item)
		}
		return typed
	case []any:
		for i, item := range typed {
			typed[i] = recallMapResponsePaths(item)
		}
		return typed
	default:
		return value
	}
}

func recallPathLikeKey(key string) bool {
	switch key {
	case "path", "prefix", "index_path", "target_path", "candidate_paths":
		return true
	default:
		return false
	}
}

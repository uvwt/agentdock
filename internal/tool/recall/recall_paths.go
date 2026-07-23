package recall

import (
	"path"
	"strings"
)

func BackendPath(value string) string {
	return recallCleanPath(value)
}

func recallPublicPath(value string) string {
	return recallCleanPath(value)
}

func recallCleanPath(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "/"))
	if value == "" {
		return value
	}
	cleaned := path.Clean(value)
	if cleaned == "." || strings.HasPrefix(cleaned, "../") {
		return value
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

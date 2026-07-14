package tools

import (
	"strings"

	"github.com/uvwt/agentdock/internal/envstore"
)

func (r *Runtime) scopedEnvAction(kind envstore.ScopeKind, name, action string, args map[string]any) (Result, error) {
	scope := envstore.Scope{Kind: kind, Name: strings.TrimSpace(name)}
	switch action {
	case "env_set":
		key := strings.TrimSpace(stringArg(args, "key", ""))
		value, exists := args["value"]
		if key == "" || !exists {
			return nil, toolErrorDetails("VALIDATION_ERROR", "key and value are required for env_set", "validation", map[string]any{"scope": scope.Name})
		}
		text := stringArg(map[string]any{"value": value}, "value", "")
		if err := r.envs.Set(scope, key, text); err != nil {
			return nil, scopedEnvToolError(scope, err)
		}
		return Result{"action": action, "name": scope.Name, "key": key, "configured": text != ""}, nil
	case "env_unset":
		key := strings.TrimSpace(stringArg(args, "key", ""))
		if key == "" {
			return nil, toolErrorDetails("VALIDATION_ERROR", "key is required for env_unset", "validation", map[string]any{"scope": scope.Name})
		}
		removed, err := r.envs.Unset(scope, key)
		if err != nil {
			return nil, scopedEnvToolError(scope, err)
		}
		return Result{"action": action, "name": scope.Name, "key": key, "removed": removed}, nil
	case "env_list":
		items, err := r.envs.List(scope)
		if err != nil {
			return nil, scopedEnvToolError(scope, err)
		}
		return Result{"action": action, "name": scope.Name, "items": items, "count": len(items)}, nil
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported environment action", "validation", map[string]any{"action": action})
	}
}

func scopedEnvToolError(scope envstore.Scope, err error) error {
	return toolErrorDetails(
		"ENV_STORE_ERROR",
		"manage scoped environment",
		"validation",
		map[string]any{"kind": scope.Kind, "name": scope.Name, "reason": err.Error()},
	)
}

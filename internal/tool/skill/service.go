package skill

import (
	"strings"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/envstore"
	skills "github.com/uvwt/agentdock/internal/skill"
	skillstate "github.com/uvwt/agentdock/internal/skill/state"
	"github.com/uvwt/agentdock/internal/workspace"
)

const runtimeAPISource = "agentdock-api"

type Service struct {
	manager *skills.Manager
	state   *skillstate.Store
	ws      *workspace.Workspace
	envs    *envstore.Store
}

func New(cfg config.Config, ws *workspace.Workspace, envs *envstore.Store) (*Service, error) {
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
	return &Service{manager: manager, state: state, ws: ws, envs: envs}, nil
}

func (s *Service) ResolveActive(skill string) (string, error) {
	path, err := s.state.Resolve(strings.TrimSpace(skill), "")
	if err != nil {
		return "", toolErrorDetails("SKILL_CONTEXT_INVALID", "resolve active Skill directory: "+err.Error(), "validation", map[string]any{"skill": skill, "reason": err.Error()})
	}
	return path, nil
}

func (s *Service) scopedEnvAction(kind envstore.ScopeKind, name, action string, request PackageRequest) (Result, error) {
	scope := envstore.Scope{Kind: kind, Name: strings.TrimSpace(name)}
	switch action {
	case "env_set":
		key := strings.TrimSpace(request.Key)
		if key == "" || request.Value == nil {
			return nil, toolErrorDetails("VALIDATION_ERROR", "key and value are required for env_set", "validation", map[string]any{"scope": scope.Name})
		}
		text := *request.Value
		if err := s.envs.Set(scope, key, text); err != nil {
			return nil, skillEnvError(scope, err)
		}
		return Result{"action": action, "name": scope.Name, "key": key, "configured": text != ""}, nil
	case "env_unset":
		key := strings.TrimSpace(request.Key)
		if key == "" {
			return nil, toolErrorDetails("VALIDATION_ERROR", "key is required for env_unset", "validation", map[string]any{"scope": scope.Name})
		}
		removed, err := s.envs.Unset(scope, key)
		if err != nil {
			return nil, skillEnvError(scope, err)
		}
		return Result{"action": action, "name": scope.Name, "key": key, "removed": removed}, nil
	case "env_list":
		items, err := s.envs.List(scope)
		if err != nil {
			return nil, skillEnvError(scope, err)
		}
		return Result{"action": action, "name": scope.Name, "items": items, "count": len(items)}, nil
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported environment action", "validation", map[string]any{"action": action})
	}
}

func skillEnvError(scope envstore.Scope, err error) error {
	return toolErrorDetails("ENV_STORE_ERROR", "manage scoped environment", "validation", map[string]any{"kind": scope.Kind, "name": scope.Name, "reason": err.Error()})
}

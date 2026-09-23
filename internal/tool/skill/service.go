package skill

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/envstore"
	plugins "github.com/uvwt/agentdock/internal/plugin"
	skills "github.com/uvwt/agentdock/internal/skill"
	skillstate "github.com/uvwt/agentdock/internal/skill/state"
	"github.com/uvwt/agentdock/internal/workspace"
)

const (
	runtimeAPISource    = "agentdock-api"
	managedSourceType   = "managed"
	sharedSourceType    = "shared"
	workspaceSourceType = "workspace"
	pluginSourceType    = "plugin"
	sharedSourceID      = "global"
)

type ResolvedSkill struct {
	Name          string
	SkillRef      string
	SourceType    string
	SourceID      string
	PluginName    string
	Root          string
	ContentDigest string
}

type Service struct {
	cfg               config.Config
	manager           *skills.Manager
	state             *skillstate.Store
	ws                *workspace.Workspace
	envs              *envstore.Store
	plugins           *plugins.Manager
	workspaceMu       sync.RWMutex
	workspaceByID     map[string]string
	workspaceIDByRoot map[string]string
	workspaceIssued   map[string]map[string]struct{}
}

func New(cfg config.Config, ws *workspace.Workspace, envs *envstore.Store, pluginManagers ...*plugins.Manager) (*Service, error) {
	state, err := skillstate.New(config.SkillDir(cfg))
	if err != nil {
		return nil, err
	}
	manager, err := skills.New(state)
	if err != nil {
		return nil, err
	}
	if _, err := skills.MigrateLegacyLayout(context.Background(), cfg.AgentDockHome, manager); err != nil {
		return nil, fmt.Errorf("migrate legacy Skill layout: %w", err)
	}
	var pluginManager *plugins.Manager
	if len(pluginManagers) > 0 {
		pluginManager = pluginManagers[0]
	}
	return &Service{
		cfg: cfg, manager: manager, state: state, ws: ws, envs: envs, plugins: pluginManager,
		workspaceByID: make(map[string]string), workspaceIDByRoot: make(map[string]string),
		workspaceIssued: make(map[string]map[string]struct{}),
	}, nil
}

func ManagedSkillRef(name string) string {
	return "skill://managed/" + name
}

func SharedSkillRef(name string) string {
	return "skill://shared/" + name
}

func PluginSkillRef(pluginName, name string) string {
	return "skill://plugin/" + pluginName + "/" + name
}

func (s *Service) WorkspaceSkillRef(workspaceRoot, name string) (skillRef, sourceID string, err error) {
	if !validSkillRefName(name) {
		return "", "", fmt.Errorf("invalid workspace Skill name %q", name)
	}
	root, err := filepath.Abs(strings.TrimSpace(workspaceRoot))
	if err != nil {
		return "", "", err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", err
	}
	s.workspaceMu.Lock()
	defer s.workspaceMu.Unlock()
	if existing := s.workspaceIDByRoot[realRoot]; existing != "" {
		sourceID = existing
	} else {
		raw := make([]byte, 18)
		if _, err := rand.Read(raw); err != nil {
			return "", "", fmt.Errorf("create workspace Skill source id: %w", err)
		}
		sourceID = base64.RawURLEncoding.EncodeToString(raw)
		s.workspaceIDByRoot[realRoot] = sourceID
		s.workspaceByID[sourceID] = realRoot
	}
	issued := s.workspaceIssued[sourceID]
	if issued == nil {
		issued = make(map[string]struct{})
		s.workspaceIssued[sourceID] = issued
	}
	issued[name] = struct{}{}
	return "skill://workspace/" + sourceID + "/" + name, sourceID, nil
}

func (s *Service) Acquire(ctx context.Context, skillRef string) (ResolvedSkill, func(), error) {
	ref, err := parseSkillRef(skillRef)
	if err != nil {
		return ResolvedSkill{}, nil, err
	}
	switch ref.SourceType {
	case managedSourceType:
		return s.acquireManaged(ctx, ref)
	case sharedSourceType:
		return s.acquireShared(ctx, ref)
	case workspaceSourceType:
		return s.acquireWorkspace(ctx, ref)
	case pluginSourceType:
		return s.acquirePlugin(ctx, ref)
	default:
		return ResolvedSkill{}, nil, toolErrorDetails("INVALID_SKILL_REF", "unsupported Skill source type", "validation", map[string]any{"skill_ref": skillRef})
	}
}

type parsedSkillRef struct {
	SourceType string
	SourceID   string
	Name       string
}

func parseSkillRef(raw string) (parsedSkillRef, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return parsedSkillRef{}, toolErrorDetails("INVALID_SKILL_REF", "skill_ref is required", "validation", nil)
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "skill" {
		return parsedSkillRef{}, toolErrorDetails("INVALID_SKILL_REF", "skill_ref must be a skill:// reference returned by AgentDock", "validation", map[string]any{"skill_ref": raw})
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Port() != "" {
		return parsedSkillRef{}, toolErrorDetails("INVALID_SKILL_REF", "skill_ref cannot contain credentials, query, fragment, or port", "validation", map[string]any{"skill_ref": raw})
	}
	resourcePath, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil {
		return parsedSkillRef{}, toolErrorDetails("INVALID_SKILL_REF", "skill_ref has invalid URL encoding", "validation", map[string]any{"skill_ref": raw})
	}
	parts := splitSkillURIPath(resourcePath)
	switch parsed.Host {
	case managedSourceType:
		if len(parts) != 1 || !validSkillRefName(parts[0]) {
			return parsedSkillRef{}, invalidSkillRef(raw)
		}
		return parsedSkillRef{SourceType: managedSourceType, SourceID: parts[0], Name: parts[0]}, nil
	case sharedSourceType:
		if len(parts) != 1 || !validSkillRefName(parts[0]) {
			return parsedSkillRef{}, invalidSkillRef(raw)
		}
		return parsedSkillRef{SourceType: sharedSourceType, SourceID: sharedSourceID, Name: parts[0]}, nil
	case workspaceSourceType:
		if len(parts) != 2 || parts[0] == "" || !validSkillRefName(parts[1]) {
			return parsedSkillRef{}, invalidSkillRef(raw)
		}
		sourceBytes, decodeErr := base64.RawURLEncoding.DecodeString(parts[0])
		if decodeErr != nil || len(sourceBytes) != 18 || base64.RawURLEncoding.EncodeToString(sourceBytes) != parts[0] {
			return parsedSkillRef{}, invalidSkillRef(raw)
		}
		return parsedSkillRef{SourceType: workspaceSourceType, SourceID: parts[0], Name: parts[1]}, nil
	case pluginSourceType:
		if len(parts) != 2 || plugins.ValidateName(parts[0]) != nil || !validSkillRefName(parts[1]) {
			return parsedSkillRef{}, invalidSkillRef(raw)
		}
		return parsedSkillRef{SourceType: pluginSourceType, SourceID: parts[0], Name: parts[1]}, nil
	default:
		return parsedSkillRef{}, invalidSkillRef(raw)
	}
}

func invalidSkillRef(raw string) error {
	return toolErrorDetails("INVALID_SKILL_REF", "skill_ref has an invalid or unsupported shape", "validation", map[string]any{"skill_ref": raw})
}

func splitSkillURIPath(value string) []string {
	value = strings.TrimPrefix(value, "/")
	if value == "" {
		return nil
	}
	return strings.Split(value, "/")
}

func validSkillRefName(name string) bool {
	return name != "" && name == path.Base(name) && !strings.ContainsAny(name, `/\\?#`) && name != "." && name != ".."
}

func (s *Service) acquireManaged(ctx context.Context, ref parsedSkillRef) (ResolvedSkill, func(), error) {
	release, err := s.state.AcquireRead(ctx, ref.Name)
	if err != nil {
		return ResolvedSkill{}, nil, toolErrorDetails("SKILL_CONTEXT_INVALID", "acquire managed Skill read lock: "+err.Error(), "runtime", map[string]any{"skill_ref": ManagedSkillRef(ref.Name)})
	}
	ok := false
	defer func() {
		if !ok {
			release()
		}
	}()

	root, err := s.state.Resolve(ref.Name)
	if err != nil {
		return ResolvedSkill{}, nil, toolErrorDetails("SKILL_NOT_AVAILABLE", "managed Skill is not installed", "not_found", map[string]any{"skill_ref": ManagedSkillRef(ref.Name)})
	}
	if err := verifyResolvedSkillDocument(root, ref.Name); err != nil {
		return ResolvedSkill{}, nil, err
	}
	ok = true
	return ResolvedSkill{
		Name: ref.Name, SkillRef: ManagedSkillRef(ref.Name), SourceType: managedSourceType,
		SourceID: ref.Name, Root: root,
	}, release, nil
}

func (s *Service) acquireShared(ctx context.Context, ref parsedSkillRef) (ResolvedSkill, func(), error) {
	if err := ctx.Err(); err != nil {
		return ResolvedSkill{}, nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ResolvedSkill{}, nil, toolErrorDetails("SKILL_CONTEXT_INVALID", "resolve shared Skill home: "+err.Error(), "runtime", map[string]any{"skill_ref": SharedSkillRef(ref.Name)})
	}
	root := filepath.Join(home, ".agents", "skills", ref.Name)
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return ResolvedSkill{}, nil, toolErrorDetails("SKILL_NOT_AVAILABLE", "shared Skill is not available", "not_found", map[string]any{"skill_ref": SharedSkillRef(ref.Name)})
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return ResolvedSkill{}, nil, toolErrorDetails("SKILL_CONTEXT_INVALID", "resolve shared Skill root: "+err.Error(), "runtime", map[string]any{"skill_ref": SharedSkillRef(ref.Name)})
	}
	if err := verifyResolvedSkillDocument(realRoot, ref.Name); err != nil {
		return ResolvedSkill{}, nil, err
	}
	return ResolvedSkill{
		Name: ref.Name, SkillRef: SharedSkillRef(ref.Name), SourceType: sharedSourceType,
		SourceID: sharedSourceID, Root: realRoot,
	}, func() {}, nil
}

func (s *Service) acquireWorkspace(ctx context.Context, ref parsedSkillRef) (ResolvedSkill, func(), error) {
	if err := ctx.Err(); err != nil {
		return ResolvedSkill{}, nil, err
	}
	s.workspaceMu.RLock()
	workspaceRoot, issued := s.workspaceByID[ref.SourceID]
	_, nameIssued := s.workspaceIssued[ref.SourceID][ref.Name]
	s.workspaceMu.RUnlock()
	if !issued || !nameIssued {
		return ResolvedSkill{}, nil, toolErrorDetails("INVALID_SKILL_REF", "workspace skill_ref was not issued by this AgentDock runtime", "validation", map[string]any{"source_id": ref.SourceID})
	}
	realWorkspaceRoot, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		return ResolvedSkill{}, nil, toolErrorDetails("SKILL_CONTEXT_INVALID", "workspace source is unavailable", "not_found", map[string]any{"skill_ref": "skill://workspace/" + ref.SourceID + "/" + ref.Name})
	}
	packageRoot := filepath.Join(realWorkspaceRoot, ".agents", "skills", ref.Name)
	info, err := os.Lstat(packageRoot)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ResolvedSkill{}, nil, toolErrorDetails("SKILL_NOT_AVAILABLE", "workspace Skill is not available as a regular directory", "not_found", map[string]any{"skill_ref": "skill://workspace/" + ref.SourceID + "/" + ref.Name})
	}
	realPackageRoot, err := filepath.EvalSymlinks(packageRoot)
	if err != nil {
		return ResolvedSkill{}, nil, toolErrorDetails("SKILL_CONTEXT_INVALID", "resolve workspace Skill root: "+err.Error(), "runtime", map[string]any{"skill_ref": "skill://workspace/" + ref.SourceID + "/" + ref.Name})
	}
	rel, err := filepath.Rel(realWorkspaceRoot, realPackageRoot)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return ResolvedSkill{}, nil, toolErrorDetails("SKILL_PATH_ESCAPE", "workspace Skill resolves outside its workspace", "validation", map[string]any{"skill_ref": "skill://workspace/" + ref.SourceID + "/" + ref.Name})
	}
	if err := verifyResolvedSkillDocument(realPackageRoot, ref.Name); err != nil {
		return ResolvedSkill{}, nil, err
	}
	skillRef := "skill://workspace/" + ref.SourceID + "/" + ref.Name
	return ResolvedSkill{
		Name: ref.Name, SkillRef: skillRef, SourceType: workspaceSourceType,
		SourceID: ref.SourceID, Root: realPackageRoot,
	}, func() {}, nil
}

func (s *Service) acquirePlugin(ctx context.Context, ref parsedSkillRef) (ResolvedSkill, func(), error) {
	if s.plugins == nil {
		return ResolvedSkill{}, nil, toolErrorDetails("SKILL_NOT_AVAILABLE", "Plugin Skill runtime is unavailable", "not_found", map[string]any{
			"skill_ref": PluginSkillRef(ref.SourceID, ref.Name),
		})
	}
	installed, release, err := s.plugins.Acquire(ctx, ref.SourceID)
	if err != nil {
		return ResolvedSkill{}, nil, toolErrorDetails("SKILL_NOT_AVAILABLE", "Plugin Skill source is unavailable", "not_found", map[string]any{
			"skill_ref": PluginSkillRef(ref.SourceID, ref.Name), "reason": err.Error(),
		})
	}
	ok := false
	defer func() {
		if !ok {
			release()
		}
	}()

	var component *plugins.SkillComponent
	for index := range installed.Components.Skills {
		candidate := &installed.Components.Skills[index]
		if candidate.Name == ref.Name {
			component = candidate
			break
		}
	}
	if component == nil {
		return ResolvedSkill{}, nil, toolErrorDetails("SKILL_NOT_AVAILABLE", "Plugin does not provide the requested Skill", "not_found", map[string]any{
			"skill_ref": PluginSkillRef(ref.SourceID, ref.Name),
		})
	}
	root := filepath.Join(installed.Root, filepath.FromSlash(component.RelativePath))
	relative, relErr := filepath.Rel(installed.Root, root)
	if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return ResolvedSkill{}, nil, toolErrorDetails("SKILL_PATH_ESCAPE", "Plugin Skill path escapes the installed Plugin package", "validation", map[string]any{
			"skill_ref": PluginSkillRef(ref.SourceID, ref.Name),
		})
	}
	info, statErr := os.Lstat(root)
	if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ResolvedSkill{}, nil, toolErrorDetails("SKILL_NOT_AVAILABLE", "Plugin Skill is not available as a regular directory", "not_found", map[string]any{
			"skill_ref": PluginSkillRef(ref.SourceID, ref.Name),
		})
	}
	if err := verifyResolvedSkillDocument(root, ref.Name); err != nil {
		return ResolvedSkill{}, nil, err
	}
	ok = true
	skillRef := PluginSkillRef(ref.SourceID, ref.Name)
	return ResolvedSkill{
		Name: ref.Name, SkillRef: skillRef, SourceType: pluginSourceType,
		SourceID: ref.SourceID, PluginName: ref.SourceID, Root: root,
		ContentDigest: component.ContentDigest,
	}, release, nil
}

func verifyResolvedSkillDocument(root, expectedName string) error {
	doc, err := skills.LoadSkillDocument(root)
	if err != nil {
		return skillToolError(err)
	}
	if doc.Name != expectedName {
		return toolErrorDetails("SKILL_CONTEXT_INVALID", "Skill document name does not match its source identity", "runtime", map[string]any{"skill": expectedName})
	}
	return nil
}

func (s *Service) scopedEnvAction(name, action string, request ManageRequest) (Result, error) {
	if _, err := s.state.Resolve(strings.TrimSpace(name)); err != nil {
		return nil, toolErrorDetails("SKILL_NOT_AVAILABLE", "managed Skill is not installed", "not_found", map[string]any{"skill": name})
	}
	scope := envstore.Scope{Kind: envstore.ScopeSkill, Name: strings.TrimSpace(name)}
	switch action {
	case "env_set":
		key := strings.TrimSpace(request.Key)
		if key == "" || request.Value == nil {
			return nil, toolErrorDetails("VALIDATION_ERROR", "key and value are required for env_set", "validation", map[string]any{"scope": scope.Name})
		}
		if config.IsReservedSkillEnvironmentKey(key) {
			return nil, toolErrorDetails("VALIDATION_ERROR", "environment variable is reserved by the Skill runtime", "validation", map[string]any{"scope": scope.Name, "key": key})
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

func (s *Service) purgeManagedSkillState(skill string) error {
	scope := envstore.Scope{Kind: envstore.ScopeSkill, Name: skill}
	envPath, err := s.envs.Path(scope)
	if err != nil {
		return err
	}
	var purgeErrs []error
	if err := os.Remove(envPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		purgeErrs = append(purgeErrs, fmt.Errorf("remove Skill environment: %w", err))
	}
	dataPath, err := config.SkillDataDir(s.cfg, skill)
	if err != nil {
		purgeErrs = append(purgeErrs, fmt.Errorf("resolve Skill data: %w", err))
	} else if err := removeManagedSkillData(s.cfg.AgentDockHome, dataPath); err != nil {
		purgeErrs = append(purgeErrs, fmt.Errorf("remove Skill data: %w", err))
	}
	return errors.Join(purgeErrs...)
}

func removeManagedSkillData(agentDockHome, dataPath string) error {
	root, err := os.OpenRoot(agentDockHome)
	if err != nil {
		return err
	}
	defer root.Close()

	relative, err := filepath.Rel(agentDockHome, dataPath)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("Skill data path escapes AgentDockHome")
	}
	for _, path := range []string{"data", filepath.Join("data", "skills"), relative} {
		info, statErr := root.Lstat(path)
		if errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("Skill data path contains a symlink or non-directory component: %s", path)
		}
	}
	return root.RemoveAll(relative)
}

func skillEnvError(scope envstore.Scope, err error) error {
	return toolErrorDetails("ENV_STORE_ERROR", "manage scoped environment", "validation", map[string]any{"kind": scope.Kind, "name": scope.Name, "reason": err.Error()})
}

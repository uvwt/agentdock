package plugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/envstore"
	mcpclient "github.com/uvwt/agentdock/internal/mcp/client"
	pluginruntime "github.com/uvwt/agentdock/internal/plugin"
	toolcore "github.com/uvwt/agentdock/internal/tool/core"
	"github.com/uvwt/agentdock/internal/workspace"
)

type Result = toolcore.Result

type Service struct {
	cfg        config.Config
	manager    *pluginruntime.Manager
	mcpClients *mcpclient.Manager
	envs       *envstore.Store
	ws         *workspace.Workspace

	mcpLeaseMu sync.Mutex
	mcpLeases  map[string]func()
}

func New(cfg config.Config, manager *pluginruntime.Manager, mcpClients *mcpclient.Manager, envs *envstore.Store, ws *workspace.Workspace) *Service {
	return &Service{
		cfg: cfg, manager: manager, mcpClients: mcpClients, envs: envs, ws: ws,
		mcpLeases: make(map[string]func()),
	}
}

func (s *Service) ReconcileMCP() error {
	return s.reconcileMCP("", "", nil)
}

func (s *Service) reconcileMCPExcluding(pluginName string) error {
	return s.reconcileMCP(pluginName, "", nil)
}

func (s *Service) reconcileMCPActivation(pluginName string) error {
	return s.reconcileMCP(pluginName, pluginName, nil)
}

func (s *Service) reconcileMCPState(state pluginruntime.State) error {
	if !state.Enabled || len(state.Components.MCP) == 0 {
		return s.reconcileMCP(state.Name, "", nil)
	}
	root, err := s.manager.Store().PackagePath(state.Name, state.Version)
	if err != nil {
		return err
	}
	override := &pluginruntime.Installed{State: state, Root: root}
	return s.reconcileMCP(state.Name, "", override)
}

func (s *Service) reconcileMCP(excludedPlugin, activationPlugin string, override *pluginruntime.Installed) error {
	configs, leases, err := s.ownedMCPConfigs(excludedPlugin, activationPlugin, override)
	if err != nil {
		return err
	}
	if err := s.mcpClients.SetOwnedServers(configs); err != nil {
		releasePluginLeases(leases)
		return err
	}
	s.mcpLeaseMu.Lock()
	previous := s.mcpLeases
	s.mcpLeases = leases
	s.mcpLeaseMu.Unlock()
	releasePluginLeases(previous)
	return nil
}

// ReleaseMCPLeases releases package references after the MCP client manager has
// closed the corresponding child processes/connections.
func (s *Service) ReleaseMCPLeases() {
	if s == nil {
		return
	}
	s.mcpLeaseMu.Lock()
	leases := s.mcpLeases
	s.mcpLeases = make(map[string]func())
	s.mcpLeaseMu.Unlock()
	releasePluginLeases(leases)
}

func releasePluginLeases(leases map[string]func()) {
	for _, release := range leases {
		if release != nil {
			release()
		}
	}
}

func (s *Service) Manage(ctx context.Context, request ManageRequest) (Result, error) {
	action := strings.ToLower(strings.TrimSpace(request.Action))
	switch action {
	case "list":
		items, err := s.manager.List()
		if err != nil {
			return nil, pluginToolError(err)
		}
		plugins := make([]map[string]any, 0, len(items))
		for _, item := range items {
			plugins = append(plugins, map[string]any{
				"name": item.Name, "version": item.Version, "enabled": item.Enabled,
				"package_digest": item.PackageDigest, "source": item.Source,
				"skill_count": len(item.Components.Skills), "mcp_count": len(item.Components.MCP),
			})
		}
		return Result{"action": action, "plugins": plugins, "count": len(plugins)}, nil

	case "inspect":
		name := strings.TrimSpace(request.Name)
		if name == "" {
			return nil, validationError("name is required for inspect", "name")
		}
		installed, err := s.manager.Inspect(name)
		if err != nil {
			return nil, pluginToolError(err)
		}
		review := s.manager.Validate(installed.Root)
		review.Source = installed.Source
		review.Compatibility = installed.Compatibility
		if !review.Valid || review.PackageDigest != installed.PackageDigest {
			return nil, toolcore.NewErrorDetails(
				"PLUGIN_PACKAGE_DRIFT",
				"installed Plugin package no longer matches persisted state",
				"runtime",
				map[string]any{"plugin_name": installed.Name},
			)
		}
		return Result{
			"action": action, "name": installed.Name, "version": installed.Version,
			"enabled": installed.Enabled, "package_digest": installed.PackageDigest,
			"plugin": map[string]any{
				"name": installed.Name, "version": installed.Version, "description": review.Description,
				"source": installed.Source, "enabled": installed.Enabled, "installed_at": installed.InstalledAt,
				"package_digest": installed.PackageDigest, "skills": review.Skills,
				"mcp": review.MCP, "executables": review.Executables,
				"warnings": review.Warnings, "unsupported": review.Unsupported,
				"compatibility": review.Compatibility,
			},
		}, nil

	case "validate":
		sourceRequest, err := s.sourceRequest(request)
		if err != nil {
			return nil, err
		}
		review := s.manager.ValidateSource(ctx, sourceRequest)
		return Result{
			"action": action, "review": review,
			"package_digest": review.PackageDigest, "review_token": review.ReviewToken,
		}, nil

	case "catalog":
		sourceRequest, err := s.sourceRequest(request)
		if err != nil {
			return nil, err
		}
		sourceRequest.CatalogItem = ""
		catalog, err := s.manager.LoadCatalog(ctx, sourceRequest)
		if err != nil {
			return nil, pluginToolError(err)
		}
		return Result{"action": action, "catalog": catalog, "count": len(catalog.Entries)}, nil

	case "install":
		sourceRequest, err := s.sourceRequest(request)
		if err != nil {
			return nil, err
		}
		enabled := true
		if request.Enabled != nil {
			enabled = *request.Enabled
		}
		result, err := s.manager.InstallReviewedSource(ctx, sourceRequest, enabled, request.ReviewToken)
		if err != nil {
			return nil, pluginToolError(err)
		}
		if !result.Changed {
			if err := s.ReconcileMCP(); err != nil {
				return nil, toolcore.NewErrorCause(
					"PLUGIN_RUNTIME_ACTIVATION_FAILED",
					"Plugin runtime reconciliation failed",
					"runtime",
					map[string]any{"plugin_name": result.Name},
					err,
				)
			}
			return changeResult(result), nil
		}
		if err := s.reconcileMCPActivation(result.Name); err != nil {
			abortErr := s.manager.AbortActivation(ctx, result.Name)
			reconcileErr := s.ReconcileMCP()
			return nil, toolcore.NewErrorCause(
				"PLUGIN_RUNTIME_ACTIVATION_FAILED",
				"Plugin install candidate could not activate its runtime; installation was aborted",
				"runtime",
				map[string]any{"plugin_name": result.Name},
				errors.Join(err, abortErr, reconcileErr),
			)
		}
		if err := s.manager.FinalizeActivation(result.Name); err != nil {
			deactivateErr := s.reconcileMCPExcluding(result.Name)
			abortErr := s.manager.AbortActivation(ctx, result.Name)
			reconcileErr := s.ReconcileMCP()
			return nil, toolcore.NewErrorCause(
				"PLUGIN_INSTALL_FINALIZE_FAILED",
				"Plugin runtime activated but durable install finalization failed; installation was aborted",
				"runtime",
				map[string]any{"plugin_name": result.Name, "version": result.Version},
				errors.Join(err, deactivateErr, abortErr, reconcileErr),
			)
		}
		return changeResult(result), nil

	case "update":
		sourceRequest, err := s.sourceRequest(request)
		if err != nil {
			return nil, err
		}
		var previous pluginruntime.State
		deactivated := false
		var deactivationErr error
		result, err := s.manager.UpdateReviewedSource(ctx, sourceRequest, request.ConfirmedSourceChange, request.ReviewToken, func(state pluginruntime.State) error {
			previous = state
			if !state.Enabled {
				return nil
			}
			if reconcileErr := s.reconcileMCPExcluding(state.Name); reconcileErr != nil {
				deactivationErr = toolcore.NewErrorCause(
					"PLUGIN_RUNTIME_DEACTIVATION_FAILED",
					"could not stop the current Plugin MCP runtime before update",
					"runtime",
					map[string]any{"plugin_name": state.Name, "previous_version": state.Version},
					reconcileErr,
				)
				return deactivationErr
			}
			deactivated = true
			return nil
		})
		if err != nil {
			if deactivationErr != nil {
				return nil, deactivationErr
			}
			if deactivated {
				reconcileErr := s.ReconcileMCP()
				if reconcileErr != nil {
					return nil, toolcore.NewErrorCause(
						"PLUGIN_UPDATE_FAILED",
						"Plugin update failed and the previous MCP runtime could not be fully restored",
						"runtime",
						map[string]any{"plugin_name": previous.Name, "previous_version": previous.Version},
						errors.Join(err, reconcileErr),
					)
				}
			}
			return nil, pluginToolError(err)
		}
		if err := s.reconcileMCPActivation(result.Name); err != nil {
			restoreErr := s.manager.AbortActivation(ctx, result.Name)
			reconcileErr := s.ReconcileMCP()
			return nil, toolcore.NewErrorCause(
				"PLUGIN_RUNTIME_ACTIVATION_FAILED",
				"Plugin update could not activate its runtime; the previous Plugin state was restored",
				"runtime",
				map[string]any{"plugin_name": previous.Name, "previous_version": previous.Version},
				errors.Join(err, restoreErr, reconcileErr),
			)
		}
		if err := s.manager.FinalizeActivation(result.Name); err != nil {
			deactivateErr := s.reconcileMCPExcluding(result.Name)
			restoreErr := s.manager.AbortActivation(ctx, result.Name)
			reconcileErr := s.ReconcileMCP()
			return nil, toolcore.NewErrorCause(
				"PLUGIN_UPDATE_FINALIZE_FAILED",
				"Plugin runtime activated but durable update finalization failed; the candidate was rolled back",
				"runtime",
				map[string]any{"plugin_name": result.Name, "version": result.Version},
				errors.Join(err, deactivateErr, restoreErr, reconcileErr),
			)
		}
		return changeResult(result), nil

	case "enable", "disable":
		name := strings.TrimSpace(request.Name)
		if name == "" {
			return nil, validationError("name is required for enable/disable", "name")
		}
		result, err := s.manager.SetEnabledWithLifecycle(ctx, name, action == "enable", func(target pluginruntime.State) error {
			return s.reconcileMCPState(target)
		})
		if err != nil {
			return nil, pluginToolError(err)
		}
		return changeResult(result), nil

	case "remove":
		name := strings.TrimSpace(request.Name)
		if name == "" {
			return nil, validationError("name is required for remove", "name")
		}
		policy := strings.ToLower(strings.TrimSpace(request.DataPolicy))
		if policy != "keep" && policy != "purge" {
			return nil, validationError("data_policy must be keep or purge for remove", "data_policy")
		}
		result, err := s.manager.RemoveWithLifecycle(ctx, name, policy, pluginruntime.RemoveLifecycle{
			BeforeDelete: func(state pluginruntime.State) error {
				if err := s.reconcileMCPExcluding(name); err != nil {
					return fmt.Errorf("deactivate Plugin runtime before remove: %w", err)
				}
				return nil
			},
			Restore: func(state pluginruntime.State) error {
				return s.reconcileMCPState(state)
			},
			Purge: func(ownership pluginruntime.PurgeOwnership) error {
				return s.purgePluginOwnedState(ownership)
			},
		})
		if err != nil {
			return nil, pluginToolError(err)
		}
		return changeResult(result), nil

	default:
		return nil, toolcore.NewErrorDetails(
			"INVALID_ACTION",
			"unsupported plugin_manage action",
			"validation",
			map[string]any{"action": action, "allowed": []string{"list", "inspect", "validate", "install", "update", "enable", "disable", "remove", "catalog"}},
		)
	}
}

func (s *Service) sourceRequest(request ManageRequest) (pluginruntime.SourceRequest, error) {
	source := strings.TrimSpace(request.Source)
	if source == "" {
		return pluginruntime.SourceRequest{}, validationError("source is required", "source")
	}
	sourceType := strings.ToLower(strings.TrimSpace(request.SourceType))
	if sourceType == "" {
		sourceType = "auto"
	}
	if strings.TrimSpace(request.CatalogItem) != "" && sourceType == "auto" {
		sourceType = "catalog"
	}
	if (sourceType == "local" || sourceType == "catalog" || sourceType == "git" || sourceType == "archive" || sourceType == "auto") &&
		!strings.Contains(source, "://") && !pluginruntime.IsSCPLikeGitSource(source) {
		resolved, err := s.ws.ResolveExisting(source)
		if err != nil {
			if sourceType == "git" {
				return pluginruntime.SourceRequest{}, toolcore.NewErrorCause("PLUGIN_SOURCE_INVALID", "local Git source cannot be resolved", "validation", map[string]any{"source": source}, err)
			}
			if sourceType == "local" || sourceType == "catalog" || sourceType == "archive" || sourceType == "auto" {
				return pluginruntime.SourceRequest{}, toolcore.NewErrorCause("PLUGIN_SOURCE_INVALID", "Plugin source cannot be resolved", "validation", map[string]any{"source": source}, err)
			}
		} else {
			source = resolved.Abs
			if sourceType == "auto" {
				info, statErr := os.Lstat(source)
				if statErr != nil {
					return pluginruntime.SourceRequest{}, toolcore.NewErrorCause("PLUGIN_SOURCE_INVALID", "Plugin source cannot be inspected", "validation", map[string]any{"source": source}, statErr)
				}
				switch {
				case info.IsDir() && info.Mode()&os.ModeSymlink == 0:
					sourceType = "local"
				case info.Mode().IsRegular() && strings.EqualFold(filepath.Ext(source), ".zip"):
					sourceType = "archive"
				default:
					return pluginruntime.SourceRequest{}, validationError("auto source must be a Plugin directory, Git URL, or ZIP archive", "source")
				}
			}
		}
	}
	return pluginruntime.SourceRequest{
		Type: sourceType, Ref: source,
		GitRef: strings.TrimSpace(request.GitRef), GitCommit: strings.TrimSpace(request.GitCommit),
		Subdir: strings.TrimSpace(request.Subdir), SHA256: strings.TrimSpace(request.SHA256),
		Adapter: strings.TrimSpace(request.SourceAdapter), Version: strings.TrimSpace(request.SourceVersion),
		Catalog: strings.TrimSpace(request.Catalog), CatalogItem: strings.TrimSpace(request.CatalogItem),
	}, nil
}

func (s *Service) ownedMCPConfigs(excludedPlugin, activationPlugin string, override *pluginruntime.Installed) ([]mcpclient.ServerConfig, map[string]func(), error) {
	installed, err := s.manager.List()
	if err != nil {
		return nil, nil, err
	}
	configs := make([]mcpclient.ServerConfig, 0)
	leases := make(map[string]func())
	fail := func(err error) ([]mcpclient.ServerConfig, map[string]func(), error) {
		releasePluginLeases(leases)
		return nil, nil, err
	}
	add := func(item pluginruntime.Installed, release func()) error {
		pluginConfigs, err := s.mcpConfigsForInstalled(item)
		if err != nil {
			release()
			return err
		}
		leases[item.Name] = release
		configs = append(configs, pluginConfigs...)
		return nil
	}

	for _, listed := range installed {
		if listed.Name == excludedPlugin || !listed.Enabled || len(listed.Components.MCP) == 0 {
			continue
		}
		leaseCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		item, release, err := s.manager.Acquire(leaseCtx, listed.Name)
		cancel()
		if err != nil {
			return fail(fmt.Errorf("acquire Plugin %s for MCP runtime: %w", listed.Name, err))
		}
		if err := add(item, release); err != nil {
			return fail(err)
		}
	}

	if activationPlugin != "" {
		item, release, err := s.manager.AcquireActivationCandidate(activationPlugin)
		if err != nil {
			return fail(fmt.Errorf("acquire Plugin %s activation candidate: %w", activationPlugin, err))
		}
		if !item.Enabled || len(item.Components.MCP) == 0 {
			release()
		} else if err := add(item, release); err != nil {
			return fail(err)
		}
	}
	if override != nil && override.Enabled && len(override.Components.MCP) > 0 {
		release, err := s.manager.Store().AcquireVersionRead(override.Name, override.Version)
		if err != nil {
			return fail(fmt.Errorf("acquire Plugin %s state override: %w", override.Name, err))
		}
		if err := add(*override, release); err != nil {
			return fail(err)
		}
	}
	return configs, leases, nil
}

func (s *Service) mcpConfigsForInstalled(item pluginruntime.Installed) ([]mcpclient.ServerConfig, error) {
	dataDir, err := s.manager.EnsureDataDir(item.Name)
	if err != nil {
		return nil, fmt.Errorf("prepare Plugin data directory for %s: %w", item.Name, err)
	}
	pkg, err := pluginruntime.LoadPackage(item.Root)
	if err != nil {
		return nil, fmt.Errorf("load installed Plugin %s MCP config: %w", item.Name, err)
	}
	if pkg.Manifest.Name != item.Name || pkg.Manifest.Version != item.Version || pkg.PackageDigest != item.PackageDigest {
		return nil, fmt.Errorf("installed Plugin %s package drifted from persisted state", item.Name)
	}

	configs := make([]mcpclient.ServerConfig, 0, len(pkg.Components.MCP))
	for _, component := range pkg.Components.MCP {
		command := component.Command
		if strings.HasPrefix(filepath.ToSlash(command), "./") {
			command = filepath.Join(item.Root, filepath.FromSlash(strings.TrimPrefix(filepath.ToSlash(command), "./")))
		}
		args := make([]string, len(component.Args))
		for index, value := range component.Args {
			args[index] = expandPortablePluginValue(value, item.Root, dataDir)
		}
		staticEnv := make(map[string]string, len(component.Environment))
		for key, value := range component.Environment {
			staticEnv[key] = expandPortablePluginValue(value, item.Root, dataDir)
		}
		cwd := ""
		if component.Transport == mcpclient.TransportStdio {
			cwd = item.Root
			if component.CWD != "" {
				cwd = expandPortablePluginValue(component.CWD, item.Root, dataDir)
			}
			if err := validateActivatedPluginCWD(cwd, component.CWD, item.Root, dataDir); err != nil {
				return nil, fmt.Errorf("activate Plugin MCP %s/%s cwd: %w", item.Name, component.Name, err)
			}
		}
		configs = append(configs, mcpclient.ServerConfig{
			Name: component.RuntimeName, DisplayName: component.Name,
			Description: component.Description, Transport: component.Transport,
			URL: component.URL, Command: command, Args: args, Cwd: cwd,
			StaticEnv: staticEnv, StaticHeaders: cloneMap(component.Headers),
			HeaderEnv: cloneMap(component.HeaderEnv), EnvBindings: cloneMap(component.EnvBindings),
			StorageKey: component.StorageKey, SourceType: "plugin", PluginName: item.Name,
			PluginRoot: item.Root, PluginDataDir: dataDir, Enabled: true, TimeoutMS: component.TimeoutMS,
		})
	}
	return configs, nil
}

func expandPortablePluginValue(value, pluginRoot, pluginData string) string {
	value = strings.ReplaceAll(value, "${PLUGIN_ROOT}", pluginRoot)
	value = strings.ReplaceAll(value, "${PLUGIN_DATA}", pluginData)
	return value
}

func validateActivatedPluginCWD(cwd, declared, pluginRoot, pluginData string) error {
	base := pluginRoot
	if strings.HasPrefix(declared, "${PLUGIN_DATA}") {
		base = pluginData
	}
	relative, err := filepath.Rel(base, filepath.Clean(cwd))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return errors.New("expanded cwd escapes its portable Plugin root")
	}
	return nil
}

func pluginStorageKeys(components []pluginruntime.MCPComponent) []string {
	keys := make([]string, 0, len(components))
	for _, component := range components {
		if key := strings.TrimSpace(component.StorageKey); key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

func (s *Service) purgeMCPEnvironment(storageKeys []string) error {
	var result error
	for _, storageKey := range storageKeys {
		scope := envstore.Scope{Kind: envstore.ScopeMCP, Name: strings.TrimSpace(storageKey)}
		path, err := s.envs.Path(scope)
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			result = errors.Join(result, fmt.Errorf("Plugin-owned MCP environment is not a regular file: %s", path))
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
		}
	}
	return result
}

func (s *Service) purgePluginOwnedState(ownership pluginruntime.PurgeOwnership) error {
	var result error
	result = errors.Join(result, s.purgeMCPEnvironment(ownership.MCPStorageKeys))
	result = errors.Join(result, s.envs.RemovePluginSkillScopes(ownership.Name))
	dataRoot, err := config.PluginSkillDataRoot(s.cfg, ownership.Name)
	if err != nil {
		result = errors.Join(result, err)
	} else {
		result = errors.Join(result, removePluginSkillData(s.cfg.AgentDockHome, dataRoot))
	}
	return result
}

func removePluginSkillData(agentDockHome, dataRoot string) error {
	root, err := os.OpenRoot(agentDockHome)
	if err != nil {
		return err
	}
	defer root.Close()

	relative, err := filepath.Rel(agentDockHome, dataRoot)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return errors.New("Plugin Skill data root escapes AgentDockHome")
	}
	for _, item := range []string{
		"data",
		filepath.Join("data", "skills"),
		filepath.Join("data", "skills", ".plugin"),
		relative,
	} {
		info, statErr := root.Lstat(item)
		if errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("Plugin Skill data path contains a symlink or non-directory component: %s", item)
		}
	}
	return root.RemoveAll(relative)
}

func changeResult(result pluginruntime.ChangeResult) Result {
	out := Result{
		"action": result.Action, "name": result.Name, "version": result.Version,
		"enabled": result.Enabled, "changed": result.Changed, "package_digest": result.PackageDigest,
	}
	if result.DataPolicy != "" {
		out["data_policy"] = result.DataPolicy
	}
	if result.PreviousVersion != "" {
		out["previous_version"] = result.PreviousVersion
	}
	return out
}

func pluginToolError(err error) error {
	var pluginErr *pluginruntime.Error
	if errors.As(err, &pluginErr) {
		category := "runtime"
		if strings.Contains(pluginErr.Code, "INVALID") || strings.Contains(pluginErr.Code, "NOT_FOUND") ||
			strings.Contains(pluginErr.Code, "REQUIRED") || strings.Contains(pluginErr.Code, "CONFLICT") ||
			strings.Contains(pluginErr.Code, "UNSUPPORTED") || strings.Contains(pluginErr.Code, "CONFIRMATION") {
			category = "validation"
		}
		return toolcore.NewErrorCause(pluginErr.Code, pluginErr.Error(), category, map[string]any{"stage": pluginErr.Stage}, err)
	}
	return toolcore.NewErrorCause("PLUGIN_MANAGE_FAILED", err.Error(), "runtime", nil, err)
}

func validationError(message, field string) error {
	return toolcore.NewErrorDetails("VALIDATION_ERROR", message, "validation", map[string]any{"field": field})
}

func cloneMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

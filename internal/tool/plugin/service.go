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
	return s.reconcileMCPExcluding("")
}

func (s *Service) reconcileMCPExcluding(pluginName string) error {
	configs, leases, err := s.ownedMCPConfigs(pluginName)
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
			},
		}, nil

	case "validate":
		source, err := s.resolveLocalSource(request.Source)
		if err != nil {
			return nil, err
		}
		review := s.manager.Validate(source)
		return Result{"action": action, "review": review}, nil

	case "install":
		if !request.Confirmed {
			return nil, toolcore.NewErrorDetails(
				"PLUGIN_CONFIRMATION_REQUIRED",
				"validate the Plugin security review, then repeat install with confirmed=true",
				"validation",
				map[string]any{"next_action": "plugin_manage validate"},
			)
		}
		source, err := s.resolveLocalSource(request.Source)
		if err != nil {
			return nil, err
		}
		enabled := true
		if request.Enabled != nil {
			enabled = *request.Enabled
		}
		result, err := s.manager.Install(ctx, source, enabled)
		if err != nil {
			return nil, pluginToolError(err)
		}
		if err := s.ReconcileMCP(); err != nil {
			var rollbackErr error
			if result.Changed {
				_, rollbackErr = s.manager.Remove(ctx, result.Name, "keep")
				_ = s.ReconcileMCP()
			}
			return nil, toolcore.NewErrorCause(
				"PLUGIN_RUNTIME_ACTIVATION_FAILED",
				"Plugin package installed but runtime activation failed; installation was rolled back",
				"runtime",
				map[string]any{"plugin_name": result.Name},
				errors.Join(err, rollbackErr),
			)
		}
		return changeResult(result), nil

	case "update":
		if !request.Confirmed {
			return nil, toolcore.NewErrorDetails(
				"PLUGIN_CONFIRMATION_REQUIRED",
				"validate the Plugin security review, then repeat update with confirmed=true",
				"validation",
				map[string]any{"next_action": "plugin_manage validate"},
			)
		}
		source, err := s.resolveLocalSource(request.Source)
		if err != nil {
			return nil, err
		}
		review := s.manager.Validate(source)
		if !review.Valid || review.Name == "" {
			return nil, toolcore.NewErrorDetails(
				"PLUGIN_VALIDATION_FAILED",
				"Plugin candidate failed validation; current Plugin remains unchanged",
				"validation",
				map[string]any{"issues": review.Issues, "unsupported": review.Unsupported},
			)
		}
		previous, err := s.manager.Inspect(review.Name)
		if err != nil {
			return nil, pluginToolError(err)
		}
		if previous.Enabled {
			if err := s.reconcileMCPExcluding(previous.Name); err != nil {
				return nil, toolcore.NewErrorCause(
					"PLUGIN_RUNTIME_DEACTIVATION_FAILED",
					"could not stop the current Plugin MCP runtime before update",
					"runtime",
					map[string]any{"plugin_name": previous.Name, "previous_version": previous.Version},
					err,
				)
			}
		}
		result, err := s.manager.Update(ctx, source, request.ConfirmedSourceChange)
		if err != nil {
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
			return nil, pluginToolError(err)
		}
		if err := s.ReconcileMCP(); err != nil {
			restoreErr := s.manager.RestoreState(ctx, previous.State)
			reconcileErr := s.ReconcileMCP()
			return nil, toolcore.NewErrorCause(
				"PLUGIN_RUNTIME_ACTIVATION_FAILED",
				"Plugin update could not activate its runtime; the previous Plugin state was restored",
				"runtime",
				map[string]any{"plugin_name": previous.Name, "previous_version": previous.Version},
				errors.Join(err, restoreErr, reconcileErr),
			)
		}
		s.manager.FinalizeUpdate(result.Name)
		return changeResult(result), nil

	case "enable", "disable":
		name := strings.TrimSpace(request.Name)
		if name == "" {
			return nil, validationError("name is required for enable/disable", "name")
		}
		previous, err := s.manager.Inspect(name)
		if err != nil {
			return nil, pluginToolError(err)
		}
		if action == "disable" && previous.Enabled {
			if err := s.reconcileMCPExcluding(name); err != nil {
				return nil, toolcore.NewErrorCause(
					"PLUGIN_RUNTIME_DEACTIVATION_FAILED",
					"could not stop the Plugin MCP runtime before disabling it",
					"runtime",
					map[string]any{"plugin_name": name},
					err,
				)
			}
		}
		result, err := s.manager.SetEnabled(ctx, name, action == "enable")
		if err != nil {
			_ = s.ReconcileMCP()
			return nil, pluginToolError(err)
		}
		if err := s.ReconcileMCP(); err != nil {
			if action == "enable" {
				_ = s.reconcileMCPExcluding(name)
			}
			_, restoreErr := s.manager.SetEnabled(ctx, name, previous.Enabled)
			reconcileErr := s.ReconcileMCP()
			return nil, toolcore.NewErrorCause(
				"PLUGIN_RUNTIME_ACTIVATION_FAILED",
				"Plugin runtime state change failed; the previous enabled state was restored",
				"runtime",
				map[string]any{"plugin_name": name},
				errors.Join(err, restoreErr, reconcileErr),
			)
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
		previous, err := s.manager.Inspect(name)
		if err != nil {
			return nil, pluginToolError(err)
		}

		// 先从新请求的运行时索引中撤下 Plugin，并关闭 owned MCP，再删除包。
		// 如果删除失败，可以恢复原 enabled 状态而不会丢失 package。
		if previous.Enabled {
			if err := s.reconcileMCPExcluding(name); err != nil {
				return nil, toolcore.NewErrorCause("PLUGIN_RUNTIME_DEACTIVATION_FAILED", "disable Plugin runtime before remove", "runtime", map[string]any{"plugin_name": name}, err)
			}
			if _, err := s.manager.SetEnabled(ctx, name, false); err != nil {
				_ = s.ReconcileMCP()
				return nil, pluginToolError(err)
			}
		}
		result, err := s.manager.Remove(ctx, name, policy)
		if err != nil {
			if previous.Enabled {
				_, _ = s.manager.SetEnabled(ctx, name, true)
				_ = s.ReconcileMCP()
			}
			return nil, pluginToolError(err)
		}
		if err := s.ReconcileMCP(); err != nil {
			return nil, toolcore.NewErrorCause("PLUGIN_RUNTIME_RECONCILE_FAILED", "Plugin was removed but MCP runtime reconciliation failed", "runtime", map[string]any{"plugin_name": name}, err)
		}
		if policy == "purge" {
			keys := previous.MCPStorageKeys
			if len(keys) == 0 {
				keys = pluginStorageKeys(previous.Components.MCP)
			}
			if err := s.purgeMCPEnvironment(keys); err != nil {
				return nil, toolcore.NewErrorCause("PLUGIN_PURGE_FAILED", "Plugin was removed but Plugin-owned MCP environment could not be fully purged", "runtime", map[string]any{"plugin_name": name}, err)
			}
		}
		return changeResult(result), nil

	default:
		return nil, toolcore.NewErrorDetails(
			"INVALID_ACTION",
			"unsupported plugin_manage action",
			"validation",
			map[string]any{"action": action, "allowed": []string{"list", "inspect", "validate", "install", "update", "enable", "disable", "remove"}},
		)
	}
}

func (s *Service) resolveLocalSource(source string) (string, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", validationError("source is required", "source")
	}
	resolved, err := s.ws.ResolveExisting(source)
	if err != nil {
		return "", toolcore.NewErrorCause("PLUGIN_SOURCE_INVALID", "Plugin source cannot be resolved", "validation", map[string]any{"source": source}, err)
	}
	return resolved.Abs, nil
}

func (s *Service) ownedMCPConfigs(excludedPlugin string) ([]mcpclient.ServerConfig, map[string]func(), error) {
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
		leases[item.Name] = release
		if len(item.Components.MCP) == 0 {
			continue
		}
		dataDir, err := s.manager.EnsureDataDir(item.Name)
		if err != nil {
			return fail(fmt.Errorf("prepare Plugin data directory for %s: %w", item.Name, err))
		}
		pkg, err := pluginruntime.LoadPackage(item.Root)
		if err != nil {
			return fail(fmt.Errorf("load installed Plugin %s MCP config: %w", item.Name, err))
		}
		if pkg.Manifest.Name != item.Name || pkg.Manifest.Version != item.Version || pkg.PackageDigest != item.PackageDigest {
			return fail(fmt.Errorf("installed Plugin %s package drifted from persisted state", item.Name))
		}
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
					return fail(fmt.Errorf("activate Plugin MCP %s/%s cwd: %w", item.Name, component.Name, err))
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
	}
	return configs, leases, nil
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

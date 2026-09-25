package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/envstore"
	"github.com/uvwt/agentdock/internal/mcp/oauthclient"
)

type Manager struct {
	registryMu sync.Mutex
	closed     atomic.Bool
	mu         sync.RWMutex
	store      *store
	envs       *envstore.Store
	oauth      *oauthclient.Manager
	servers    map[string]ServerConfig
	owned      map[string]ServerConfig
	states     map[string]*serverState
}

type serverState struct {
	mu            sync.Mutex
	client        protocolClient
	tools         map[string]Tool
	lastError     string
	lastErrorCode string
	oauthStatus   string
	refreshedAt   time.Time
}

func NewManager(agentDockHome string, provided ...*envstore.Store) (*Manager, error) {
	registry := newStore(agentDockHome)
	servers, err := registry.load()
	if err != nil {
		return nil, err
	}
	envs := (*envstore.Store)(nil)
	if len(provided) > 0 {
		envs = provided[0]
	}
	if envs == nil {
		envs, err = envstore.New(agentDockHome)
		if err != nil {
			return nil, err
		}
	}
	oauthManager, err := oauthclient.New(agentDockHome)
	if err != nil {
		return nil, fmt.Errorf("initialize MCP OAuth client: %w", err)
	}
	servers = standaloneServerConfigs(servers)
	states := make(map[string]*serverState, len(servers))
	for name := range servers {
		states[name] = &serverState{}
	}
	return &Manager{
		store: registry, envs: envs, oauth: oauthManager, servers: servers,
		owned: make(map[string]ServerConfig), states: states,
	}, nil
}

func standaloneServerConfigs(input map[string]ServerConfig) map[string]ServerConfig {
	out := make(map[string]ServerConfig, len(input))
	for name, raw := range input {
		cfg := normalizeServerConfig(raw)
		cfg.SourceType = "standalone"
		cfg.PluginName = ""
		cfg.DisplayName = cfg.Name
		cfg.StorageKey = cfg.Name
		cfg.PluginDataDir = ""
		cfg.EnvBindings = nil
		out[name] = cfg
	}
	return out
}

// ValidateOwnedServers checks a complete Plugin-owned MCP overlay without
// mutating the currently running registry.
func (m *Manager) ValidateOwnedServers(configs []ServerConfig) error {
	m.registryMu.Lock()
	defer m.registryMu.Unlock()
	if err := m.ensureOpenLocked(); err != nil {
		return err
	}
	standalone, err := m.store.load()
	if err != nil {
		return newError("MCP_REGISTRY_READ_FAILED", "read dynamic MCP registry", true, nil, err)
	}
	_, _, err = buildOwnedRegistry(standalone, configs)
	return err
}

// SetOwnedServers replaces the in-memory Plugin-owned MCP overlay. Owned
// servers reuse the normal MCP runtime but are never persisted to servers.json.
func (m *Manager) SetOwnedServers(configs []ServerConfig) error {
	m.registryMu.Lock()
	defer m.registryMu.Unlock()
	if err := m.ensureOpenLocked(); err != nil {
		return err
	}
	standalone, err := m.store.load()
	if err != nil {
		return newError("MCP_REGISTRY_READ_FAILED", "read dynamic MCP registry", true, nil, err)
	}
	owned, merged, err := buildOwnedRegistry(standalone, configs)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.owned = owned
	staleStates := m.replaceRegistryLocked(merged)
	m.mu.Unlock()
	return closeServerStates(staleStates)
}

func buildOwnedRegistry(standalone map[string]ServerConfig, configs []ServerConfig) (map[string]ServerConfig, map[string]ServerConfig, error) {
	standalone = standaloneServerConfigs(standalone)
	owned := make(map[string]ServerConfig, len(configs))
	for _, raw := range configs {
		cfg := normalizeServerConfig(raw)
		if cfg.SourceType != "plugin" || cfg.PluginName == "" {
			return nil, nil, newError("MCP_CONFIG_INVALID", "owned MCP server requires Plugin provenance", false, map[string]any{"server": cfg.Name}, nil)
		}
		if cfg.StorageKey == "" {
			return nil, nil, newError("MCP_CONFIG_INVALID", "owned MCP server requires a stable storage key", false, map[string]any{"server": cfg.Name}, nil)
		}
		if cfg.PluginRuntimeRoot == "" || cfg.PluginDataDir == "" {
			return nil, nil, newError("MCP_CONFIG_INVALID", "owned MCP server requires Plugin runtime root and data directory", false, map[string]any{"server": cfg.Name}, nil)
		}
		if err := validateServerConfig(cfg); err != nil {
			return nil, nil, newError("MCP_CONFIG_INVALID", err.Error(), false, map[string]any{"server": cfg.Name}, err)
		}
		if _, exists := standalone[cfg.Name]; exists {
			return nil, nil, newError("MCP_SERVER_COLLISION", "Plugin MCP runtime name conflicts with a standalone MCP server", false, map[string]any{"server": cfg.Name, "plugin_name": cfg.PluginName}, nil)
		}
		if _, exists := owned[cfg.Name]; exists {
			return nil, nil, newError("MCP_SERVER_COLLISION", "duplicate Plugin MCP runtime name", false, map[string]any{"server": cfg.Name, "plugin_name": cfg.PluginName}, nil)
		}
		owned[cfg.Name] = cfg
	}

	merged := make(map[string]ServerConfig, len(standalone)+len(owned))
	for name, cfg := range standalone {
		merged[name] = cfg
	}
	for name, cfg := range owned {
		merged[name] = cfg
	}
	return owned, merged, nil
}

func (m *Manager) mergeOwned(standalone map[string]ServerConfig) (map[string]ServerConfig, error) {
	merged := standaloneServerConfigs(standalone)
	m.mu.RLock()
	defer m.mu.RUnlock()
	for name, cfg := range m.owned {
		if _, exists := merged[name]; exists {
			return nil, newError("MCP_SERVER_COLLISION", "Plugin MCP runtime name conflicts with a standalone MCP server", false, map[string]any{"server": name, "plugin_name": cfg.PluginName}, nil)
		}
		merged[name] = cfg
	}
	return merged, nil
}

func (m *Manager) Add(cfg ServerConfig) (ServerSummary, error) {
	cfg = normalizeServerConfig(cfg)
	cfg.SourceType = "standalone"
	cfg.DisplayName = cfg.Name
	cfg.StorageKey = cfg.Name
	if err := validateServerConfig(cfg); err != nil {
		return ServerSummary{}, newError("MCP_CONFIG_INVALID", err.Error(), false, map[string]any{"server": cfg.Name}, err)
	}
	m.registryMu.Lock()
	defer m.registryMu.Unlock()
	if err := m.ensureOpenLocked(); err != nil {
		return ServerSummary{}, err
	}
	m.mu.RLock()
	_, ownedCollision := m.owned[cfg.Name]
	m.mu.RUnlock()
	if ownedCollision {
		return ServerSummary{}, newError("MCP_SERVER_COLLISION", "standalone MCP name conflicts with a Plugin-owned MCP server", false, map[string]any{"server": cfg.Name}, nil)
	}
	servers, err := m.store.update(func(servers map[string]ServerConfig) error {
		if _, exists := servers[cfg.Name]; exists {
			return newError("MCP_SERVER_EXISTS", "dynamic MCP server already exists", false, map[string]any{"server": cfg.Name}, nil)
		}
		servers[cfg.Name] = cfg
		return nil
	})
	if err != nil {
		var mcpErr *Error
		if errors.As(err, &mcpErr) {
			return ServerSummary{}, err
		}
		return ServerSummary{}, newError("MCP_REGISTRY_WRITE_FAILED", "persist dynamic MCP server", false, map[string]any{"server": cfg.Name}, err)
	}
	servers, err = m.mergeOwned(servers)
	if err != nil {
		return ServerSummary{}, err
	}

	m.mu.Lock()
	staleStates := m.replaceRegistryLocked(servers)
	state := m.states[cfg.Name]
	m.mu.Unlock()
	closeServerStates(staleStates)
	return summaryFor(cfg, state), nil
}

func (m *Manager) Remove(name string) error {
	name = strings.TrimSpace(name)
	m.registryMu.Lock()
	defer m.registryMu.Unlock()
	if err := m.ensureOpenLocked(); err != nil {
		return err
	}
	m.mu.RLock()
	ownedCfg, owned := m.owned[name]
	m.mu.RUnlock()
	if owned {
		return newError("MCP_OWNED_BY_PLUGIN", "Plugin-owned MCP lifecycle is managed by plugin_manage", false, map[string]any{"server": name, "plugin_name": ownedCfg.PluginName}, nil)
	}
	servers, err := m.store.update(func(servers map[string]ServerConfig) error {
		if _, exists := servers[name]; !exists {
			return newError("MCP_SERVER_NOT_FOUND", "dynamic MCP server not found", false, map[string]any{"server": name}, nil)
		}
		delete(servers, name)
		return nil
	})
	if err != nil {
		var mcpErr *Error
		if errors.As(err, &mcpErr) {
			return err
		}
		return newError("MCP_REGISTRY_WRITE_FAILED", "remove dynamic MCP server", false, map[string]any{"server": name}, err)
	}
	servers, err = m.mergeOwned(servers)
	if err != nil {
		return err
	}

	m.mu.Lock()
	staleStates := m.replaceRegistryLocked(servers)
	m.mu.Unlock()
	// Registry 删除成功后再清理 OAuth grant，避免持久化注册失败时留下“服务器还在、
	// 授权却先丢了”的不可逆半状态。DCR client 是跨 MCP 共享状态，不在这里删除。
	return errors.Join(closeServerStates(staleStates), m.oauth.RemoveGrant(name))
}

func (m *Manager) SetEnabled(name string, enabled bool) (ServerSummary, error) {
	name = strings.TrimSpace(name)
	m.registryMu.Lock()
	defer m.registryMu.Unlock()
	if err := m.ensureOpenLocked(); err != nil {
		return ServerSummary{}, err
	}
	m.mu.RLock()
	ownedCfg, owned := m.owned[name]
	m.mu.RUnlock()
	if owned {
		return ServerSummary{}, newError("MCP_OWNED_BY_PLUGIN", "Plugin-owned MCP lifecycle is managed by plugin_manage", false, map[string]any{"server": name, "plugin_name": ownedCfg.PluginName}, nil)
	}
	var selected ServerConfig
	servers, err := m.store.update(func(servers map[string]ServerConfig) error {
		cfg, exists := servers[name]
		if !exists {
			return newError("MCP_SERVER_NOT_FOUND", "dynamic MCP server not found", false, map[string]any{"server": name}, nil)
		}
		cfg.Enabled = enabled
		servers[name] = cfg
		selected = cfg
		return nil
	})
	if err != nil {
		var mcpErr *Error
		if errors.As(err, &mcpErr) {
			return ServerSummary{}, err
		}
		return ServerSummary{}, newError("MCP_REGISTRY_WRITE_FAILED", "persist dynamic MCP server state", false, map[string]any{"server": name}, err)
	}
	servers, err = m.mergeOwned(servers)
	if err != nil {
		return ServerSummary{}, err
	}

	m.mu.Lock()
	staleStates := m.replaceRegistryLocked(servers)
	state := m.states[name]
	m.mu.Unlock()
	if err := closeServerStates(staleStates); err != nil {
		return ServerSummary{}, err
	}
	if !enabled {
		if err := closeState(state); err != nil {
			return ServerSummary{}, err
		}
	}
	return summaryFor(selected, state), nil
}

func (m *Manager) replaceRegistryLocked(servers map[string]ServerConfig) []*serverState {
	states := make(map[string]*serverState, len(servers))
	stale := make([]*serverState, 0)
	for name, cfg := range servers {
		if previous, exists := m.servers[name]; exists && reflect.DeepEqual(previous, cfg) {
			states[name] = m.states[name]
			continue
		}
		if previousState := m.states[name]; previousState != nil {
			stale = append(stale, previousState)
		}
		states[name] = &serverState{}
	}
	for name, state := range m.states {
		if _, exists := servers[name]; !exists && state != nil {
			stale = append(stale, state)
		}
	}
	m.servers = servers
	m.states = states
	return stale
}

func closeServerStates(states []*serverState) error {
	var result error
	for _, state := range states {
		result = errors.Join(result, closeState(state))
	}
	return result
}

func (m *Manager) syncRegistry() error {
	m.registryMu.Lock()
	defer m.registryMu.Unlock()
	if err := m.ensureOpenLocked(); err != nil {
		return err
	}
	servers, err := m.store.load()
	if err != nil {
		return newError("MCP_REGISTRY_READ_FAILED", "read dynamic MCP registry", true, nil, err)
	}
	servers, err = m.mergeOwned(servers)
	if err != nil {
		return err
	}
	m.mu.Lock()
	staleStates := m.replaceRegistryLocked(servers)
	m.mu.Unlock()
	return closeServerStates(staleStates)
}

func (m *Manager) List() []ServerSummary {
	if err := m.syncRegistry(); err != nil {
		slog.Warn("refresh dynamic MCP registry before list failed", "error", err)
	}
	m.mu.RLock()
	names := make([]string, 0, len(m.servers))
	for name := range m.servers {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]ServerSummary, 0, len(names))
	for _, name := range names {
		items = append(items, summaryFor(m.servers[name], m.states[name]))
	}
	m.mu.RUnlock()
	return items
}

func (m *Manager) EnabledIndex() []ServerSummary {
	all := m.List()
	items := make([]ServerSummary, 0, len(all))
	for _, item := range all {
		if item.Enabled {
			items = append(items, item)
		}
	}
	return items
}

func (m *Manager) Inspect(name string) (ServerConfig, ServerSummary, error) {
	if err := m.syncRegistry(); err != nil {
		return ServerConfig{}, ServerSummary{}, err
	}
	m.mu.RLock()
	cfg, exists := m.servers[strings.TrimSpace(name)]
	state := m.states[strings.TrimSpace(name)]
	m.mu.RUnlock()
	if !exists {
		return ServerConfig{}, ServerSummary{}, newError("MCP_SERVER_NOT_FOUND", "dynamic MCP server not found", false, map[string]any{"server": name}, nil)
	}
	return cfg, summaryFor(cfg, state), nil
}

func (m *Manager) SetOAuthCallback(option oauthclient.CallbackOption) error {
	return m.oauth.SetCallback(option)
}

func (m *Manager) RemoveOAuthCallback(id string) {
	m.oauth.RemoveCallback(id)
}

func (m *Manager) Authorize(ctx context.Context, name, callbackID string) (oauthclient.BeginResult, error) {
	cfg, _, err := m.Inspect(name)
	if err != nil {
		return oauthclient.BeginResult{}, err
	}
	if !cfg.Enabled {
		return oauthclient.BeginResult{}, newError("MCP_SERVER_DISABLED", "dynamic MCP server is disabled", false, map[string]any{"server": cfg.Name}, nil)
	}
	if cfg.Transport != TransportStreamableHTTP {
		return oauthclient.BeginResult{}, newError("MCP_AUTH_UNSUPPORTED", "OAuth authorization is only available for streamable HTTP MCP servers", false, map[string]any{"server": cfg.Name}, nil)
	}
	storageKey := cfg.StorageKey
	if storageKey == "" {
		storageKey = cfg.Name
	}
	result, done, err := m.oauth.Begin(ctx, cfg.Name, storageKey, cfg.URL, callbackID)
	if err != nil {
		return oauthclient.BeginResult{}, oauthFlowError(cfg.Name, err)
	}
	if done == nil || result.AuthorizationURL == "" {
		return result, nil
	}
	m.mu.RLock()
	state := m.states[cfg.Name]
	m.mu.RUnlock()
	if state != nil {
		state.mu.Lock()
		state.oauthStatus = oauthclient.StatusAuthorizing
		state.lastError = ""
		state.lastErrorCode = ""
		state.mu.Unlock()
	}
	go m.watchAuthorization(cfg.Name, done)
	return result, nil
}

func (m *Manager) DeliverOAuthCallback(result oauthclient.CallbackResult) error {
	if err := m.oauth.DeliverCallback(result); err != nil {
		return oauthFlowError("", err)
	}
	return nil
}

func (m *Manager) ClearAuthorization(name string) error {
	cfg, _, err := m.Inspect(name)
	if err != nil {
		return err
	}
	storageKey := cfg.StorageKey
	if storageKey == "" {
		storageKey = cfg.Name
	}
	if err := m.oauth.Clear(storageKey); err != nil {
		return newError("MCP_AUTH_CLEAR_FAILED", "clear MCP OAuth authorization", false, map[string]any{"server": cfg.Name}, err)
	}
	m.mu.RLock()
	state := m.states[cfg.Name]
	m.mu.RUnlock()
	return closeState(state)
}

func (m *Manager) RemoveOAuthGrant(storageKey string) error {
	if err := m.oauth.RemoveGrant(strings.TrimSpace(storageKey)); err != nil {
		return newError("MCP_AUTH_CLEAR_FAILED", "remove MCP OAuth authorization", false, map[string]any{"storage_key": strings.TrimSpace(storageKey)}, err)
	}
	return nil
}

func (m *Manager) watchAuthorization(name string, done <-chan error) {
	err, ok := <-done
	if !ok {
		return
	}
	m.mu.RLock()
	state := m.states[name]
	m.mu.RUnlock()
	if state == nil {
		return
	}
	if err != nil {
		var flowErr *oauthclient.FlowError
		if errors.As(err, &flowErr) && flowErr.Code == "MCP_AUTH_CANCELLED" {
			// auth_clear 会负责关闭 session 并把状态恢复为 idle；这里不能在它之后
			// 又异步写回一个“取消失败”，否则状态会产生竞态回弹。
			return
		}
		state.mu.Lock()
		state.oauthStatus = ""
		converted := oauthFlowError(name, err)
		recordStateError(state, converted)
		if errors.As(err, &flowErr) {
			// Flow 失败后 grant 仍不可用。无论是用户拒绝、超时、token exchange、issuer
			// 校验还是落盘失败，都让模型/UI 保留“可重新授权”的下一步；具体原因继续
			// 通过 last_error_code 暴露。auth_clear 的 CANCELLED 已在上方单独吞掉。
			state.oauthStatus = oauthclient.StatusAuthRequired
		}
		state.mu.Unlock()
		return
	}

	state.mu.Lock()
	state.lastError = ""
	state.lastErrorCode = ""
	// 保持 authorizing 直到 MCP 真正重新初始化完成，避免 callback 收到后 UI 短暂
	// 回到 idle。refreshStateLocked 成功后会切到 authorized/ready。
	state.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, _, refreshErr := m.Refresh(ctx, name); refreshErr != nil {
		state.mu.Lock()
		if state.oauthStatus == oauthclient.StatusAuthorizing {
			state.oauthStatus = ""
		}
		state.mu.Unlock()
		slog.Warn("refresh MCP after OAuth authorization failed", "server", name, "error", refreshErr)
	}
}

func oauthFlowError(server string, err error) error {
	var flowErr *oauthclient.FlowError
	if errors.As(err, &flowErr) {
		details := map[string]any{}
		if strings.TrimSpace(server) != "" {
			details["server"] = strings.TrimSpace(server)
		}
		return newError(flowErr.Code, flowErr.Message, false, details, err)
	}
	return newError("MCP_AUTH_FAILED", "MCP OAuth authorization failed", false, map[string]any{"server": strings.TrimSpace(server)}, err)
}

func (m *Manager) Refresh(ctx context.Context, name string) (ServerSummary, []ToolSummary, error) {
	if err := m.syncRegistry(); err != nil {
		return ServerSummary{}, nil, err
	}
	cfg, state, unlockState, err := m.lockServer(strings.TrimSpace(name))
	if err != nil {
		return ServerSummary{}, nil, err
	}
	defer unlockState()
	if !cfg.Enabled {
		return ServerSummary{}, nil, newError("MCP_SERVER_DISABLED", "dynamic MCP server is disabled", false, map[string]any{"server": cfg.Name}, nil)
	}
	runtimeCfg, err := m.runtimeConfig(cfg)
	if err != nil {
		recordStateError(state, err)
		return ServerSummary{}, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutMS)*time.Millisecond)
	defer cancel()
	tools, err := m.refreshStateLocked(ctx, runtimeCfg, state)
	summary := summaryForLocked(cfg, state)
	if err != nil {
		return summary, nil, err
	}
	return summary, summarizeTools(cfg, tools), nil
}

func (m *Manager) Search(ctx context.Context, query, server string, limit int) ([]ToolSummary, error) {
	if err := m.syncRegistry(); err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	server = strings.TrimSpace(server)
	if query == "" {
		return nil, newError("MCP_QUERY_REQUIRED", "MCP tool search query is required", false, nil, nil)
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	configs, err := m.searchServers(server)
	if err != nil {
		return nil, err
	}
	type scoredTool struct {
		score int
		item  ToolSummary
	}
	matches := make([]scoredTool, 0)
	var firstErr error
	for _, cfg := range configs {
		tools, ensureErr := m.ensureTools(ctx, cfg.Name)
		if ensureErr != nil {
			if server != "" {
				return nil, ensureErr
			}
			if firstErr == nil {
				firstErr = ensureErr
			}
			continue
		}
		for _, tool := range tools {
			score := toolMatchScore(query, tool)
			if score == 0 {
				continue
			}
			matches = append(matches, scoredTool{score: score, item: toolSummary(cfg, tool)})
		}
	}
	if len(matches) == 0 && firstErr != nil {
		return nil, firstErr
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].item.QualifiedName < matches[j].item.QualifiedName
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	items := make([]ToolSummary, 0, len(matches))
	for _, match := range matches {
		items = append(items, match.item)
	}
	return items, nil
}

func (m *Manager) InspectTool(ctx context.Context, qualifiedName string) (string, Tool, error) {
	if err := m.syncRegistry(); err != nil {
		return "", Tool{}, err
	}
	server, name, err := splitQualifiedToolName(qualifiedName)
	if err != nil {
		return "", Tool{}, newError("MCP_TOOL_NAME_INVALID", err.Error(), false, map[string]any{"tool": qualifiedName}, err)
	}
	tools, err := m.ensureTools(ctx, server)
	if err != nil {
		return "", Tool{}, err
	}
	tool, exists := tools[name]
	if !exists {
		return "", Tool{}, newError("MCP_TOOL_NOT_FOUND", "MCP tool not found", false, map[string]any{"tool": qualifiedName}, nil)
	}
	return server, tool, nil
}

func (m *Manager) Call(ctx context.Context, qualifiedName string, arguments map[string]any) (map[string]any, error) {
	if err := m.syncRegistry(); err != nil {
		return nil, err
	}
	server, name, err := splitQualifiedToolName(qualifiedName)
	if err != nil {
		return nil, newError("MCP_TOOL_NAME_INVALID", err.Error(), false, map[string]any{"tool": qualifiedName}, err)
	}
	cfg, state, unlockState, err := m.lockServer(server)
	if err != nil {
		return nil, err
	}
	defer unlockState()
	if !cfg.Enabled {
		return nil, newError("MCP_SERVER_DISABLED", "dynamic MCP server is disabled", false, map[string]any{"server": server}, nil)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutMS)*time.Millisecond)
	defer cancel()
	if state.client == nil || len(state.tools) == 0 {
		runtimeCfg, err := m.runtimeConfig(cfg)
		if err != nil {
			recordStateError(state, err)
			return nil, err
		}
		if _, err := m.refreshStateLocked(ctx, runtimeCfg, state); err != nil {
			return nil, err
		}
	}
	tool, exists := state.tools[name]
	if !exists {
		return nil, newError("MCP_TOOL_NOT_FOUND", "MCP tool not found", false, map[string]any{"tool": qualifiedName}, nil)
	}
	if err := validateToolArguments(tool, arguments); err != nil {
		return nil, err
	}
	result, err := state.client.callTool(ctx, name, arguments)
	if err != nil {
		// 工具调用失败是请求级结果，不代表 MCP server 的连接或发现状态失效。
		// server 的 lastError 只记录 refresh / initialize / tools/list 生命周期故障。
		return nil, err
	}
	return result, nil
}

func (m *Manager) Close() error {
	m.registryMu.Lock()
	defer m.registryMu.Unlock()
	if m.closed.Swap(true) {
		return nil
	}
	m.mu.RLock()
	states := make([]*serverState, 0, len(m.states))
	for _, state := range m.states {
		states = append(states, state)
	}
	m.mu.RUnlock()
	var result error
	for _, state := range states {
		result = errors.Join(result, closeState(state))
	}
	return result
}

func (m *Manager) lockServer(name string) (ServerConfig, *serverState, func(), error) {
	if m.closed.Load() {
		return ServerConfig{}, nil, nil, newError("MCP_MANAGER_CLOSED", "dynamic MCP manager is closed", false, nil, nil)
	}
	m.mu.RLock()
	cfg, exists := m.servers[name]
	state := m.states[name]
	if !exists {
		m.mu.RUnlock()
		return ServerConfig{}, nil, nil, newError("MCP_SERVER_NOT_FOUND", "dynamic MCP server not found", false, map[string]any{"server": name}, nil)
	}
	state.mu.Lock()
	m.mu.RUnlock()
	if m.closed.Load() {
		state.mu.Unlock()
		return ServerConfig{}, nil, nil, newError("MCP_MANAGER_CLOSED", "dynamic MCP manager is closed", false, nil, nil)
	}
	return cfg, state, state.mu.Unlock, nil
}

func (m *Manager) ensureOpenLocked() error {
	if !m.closed.Load() {
		return nil
	}
	return newError("MCP_MANAGER_CLOSED", "dynamic MCP manager is closed", false, nil, nil)
}

func (m *Manager) runtimeConfig(cfg ServerConfig) (ServerConfig, error) {
	storageKey := cfg.StorageKey
	if storageKey == "" {
		storageKey = cfg.Name
	}
	values, err := m.envs.Load(envstore.Scope{Kind: envstore.ScopeMCP, Name: storageKey})
	if err != nil {
		return ServerConfig{}, newError(
			"MCP_ENV_READ_FAILED",
			"read dynamic MCP environment",
			false,
			map[string]any{"server": cfg.Name},
			err,
		)
	}
	if cfg.SourceType != "plugin" {
		cfg.RuntimeEnv = values
		return cfg, nil
	}

	for key := range values {
		if config.IsReservedPluginEnvironmentKey(key) {
			return ServerConfig{}, newError(
				"MCP_ENV_READ_FAILED",
				"Plugin MCP environment contains a runtime-reserved variable",
				false,
				map[string]any{"server": cfg.Name, "key": key},
				nil,
			)
		}
	}
	// env/mcp/<storage-key>.env 是用户运行时覆盖层；portable package 中的
	// env 是公开默认值，由 stdioEnvironment 在它之前注入。P3 adapter 的
	// credential bindings 也从这里取值，但永远不会落入 Plugin state。
	runtimeValues := make(map[string]string, len(values)+len(cfg.EnvBindings)+len(cfg.HeaderEnv)+3)
	// HeaderEnv/EnvBindings 只描述映射；是否缺失即失败由 RequiredEnv 单独决定。
	requiredEnv := make(map[string]struct{}, len(cfg.RequiredEnv))
	for _, envName := range cfg.RequiredEnv {
		requiredEnv[envName] = struct{}{}
	}
	for key, value := range values {
		runtimeValues[key] = value
	}
	for header, envName := range cfg.HeaderEnv {
		value, ok := values[envName]
		if !ok || value == "" {
			if _, required := requiredEnv[envName]; required {
				return ServerConfig{}, newError(
					"MCP_CREDENTIAL_REQUIRED",
					"required Plugin MCP environment variable is missing",
					false,
					map[string]any{"server": cfg.Name, "header": header, "env": envName},
					nil,
				)
			}
			// ${ENV:-} 保留字段本身，只把缺失/空值展开成空字符串。
			runtimeValues[envName] = ""
			continue
		}
		runtimeValues[envName] = value
	}
	for childName, envName := range cfg.EnvBindings {
		value, ok := values[envName]
		if !ok || value == "" {
			if _, required := requiredEnv[envName]; required {
				return ServerConfig{}, newError(
					"MCP_CREDENTIAL_REQUIRED",
					"required Plugin MCP environment variable is missing",
					false,
					map[string]any{"server": cfg.Name, "env": envName},
					nil,
				)
			}
			// stdio env 与 HTTP header 使用同一套 optional binding 语义。
			runtimeValues[childName] = ""
			continue
		}
		runtimeValues[childName] = value
	}
	runtimeValues[config.PluginDataDirEnvKey] = cfg.PluginDataDir
	cfg.RuntimeEnv = runtimeValues
	return cfg, nil
}

func (m *Manager) searchServers(name string) ([]ServerConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if name != "" {
		cfg, exists := m.servers[name]
		if !exists {
			return nil, newError("MCP_SERVER_NOT_FOUND", "dynamic MCP server not found", false, map[string]any{"server": name}, nil)
		}
		if !cfg.Enabled {
			return nil, newError("MCP_SERVER_DISABLED", "dynamic MCP server is disabled", false, map[string]any{"server": name}, nil)
		}
		return []ServerConfig{cfg}, nil
	}
	configs := make([]ServerConfig, 0, len(m.servers))
	for _, cfg := range m.servers {
		if cfg.Enabled {
			configs = append(configs, cfg)
		}
	}
	sort.Slice(configs, func(i, j int) bool { return configs[i].Name < configs[j].Name })
	return configs, nil
}

func (m *Manager) ensureTools(ctx context.Context, name string) (map[string]Tool, error) {
	cfg, state, unlockState, err := m.lockServer(name)
	if err != nil {
		return nil, err
	}
	defer unlockState()
	if !cfg.Enabled {
		return nil, newError("MCP_SERVER_DISABLED", "dynamic MCP server is disabled", false, map[string]any{"server": name}, nil)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutMS)*time.Millisecond)
	defer cancel()
	if state.client == nil || len(state.tools) == 0 {
		runtimeCfg, err := m.runtimeConfig(cfg)
		if err != nil {
			recordStateError(state, err)
			return nil, err
		}
		return m.refreshStateLocked(ctx, runtimeCfg, state)
	}
	return cloneTools(state.tools), nil
}

func (m *Manager) refreshStateLocked(ctx context.Context, cfg ServerConfig, state *serverState) (map[string]Tool, error) {
	if state.client != nil {
		_ = state.client.close()
	}
	state.client = nil
	state.tools = nil
	client, err := m.newProtocolClient(cfg)
	if err != nil {
		recordStateError(state, err)
		return nil, err
	}
	if err := client.initialize(ctx); err != nil {
		_ = client.close()
		recordStateError(state, err)
		return nil, err
	}
	listed, err := client.listTools(ctx)
	if err != nil {
		_ = client.close()
		recordStateError(state, err)
		return nil, err
	}
	tools := make(map[string]Tool, len(listed))
	for _, tool := range listed {
		tool.Name = strings.TrimSpace(tool.Name)
		if tool.Name == "" {
			_ = client.close()
			err := newError("MCP_INVALID_RESPONSE", "MCP tools/list returned an empty tool name", false, map[string]any{"server": cfg.Name}, nil)
			recordStateError(state, err)
			return nil, err
		}
		if _, duplicate := tools[tool.Name]; duplicate {
			_ = client.close()
			err := newError("MCP_INVALID_RESPONSE", "MCP tools/list returned duplicate tool names", false, map[string]any{"server": cfg.Name, "tool": tool.Name}, nil)
			recordStateError(state, err)
			return nil, err
		}
		if tool.InputSchema == nil {
			tool.InputSchema = map[string]any{"type": "object", "additionalProperties": true}
		}
		validator, err := compileToolInputSchema(tool.InputSchema)
		if err != nil {
			_ = client.close()
			schemaErr := newError(
				"MCP_SCHEMA_INVALID",
				"MCP tools/list returned an invalid input schema",
				false,
				map[string]any{"server": cfg.Name, "tool": tool.Name, "reason": err.Error()},
				err,
			)
			recordStateError(state, schemaErr)
			return nil, schemaErr
		}
		tool.inputValidator = validator
		tools[tool.Name] = tool
	}
	state.client = client
	state.tools = tools
	state.lastError = ""
	state.lastErrorCode = ""
	state.oauthStatus = ""
	state.refreshedAt = time.Now().UTC()
	return cloneTools(tools), nil
}

func (m *Manager) newProtocolClient(cfg ServerConfig) (protocolClient, error) {
	switch cfg.Transport {
	case TransportStreamableHTTP:
		storageKey := cfg.StorageKey
		if storageKey == "" {
			storageKey = cfg.Name
		}
		return newStreamableHTTPClient(cfg, m.oauth.Handler(cfg.Name, storageKey, cfg.URL)), nil
	case TransportStdio:
		return newStdioClient(cfg), nil
	default:
		return nil, newError("MCP_TRANSPORT_UNSUPPORTED", fmt.Sprintf("unsupported MCP transport %q", cfg.Transport), false, map[string]any{"server": cfg.Name}, nil)
	}
}

func closeState(state *serverState) error {
	if state == nil {
		return nil
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	var err error
	if state.client != nil {
		err = state.client.close()
	}
	state.client = nil
	state.tools = nil
	state.lastError = ""
	state.lastErrorCode = ""
	state.oauthStatus = ""
	state.refreshedAt = time.Time{}
	return err
}

func summaryFor(cfg ServerConfig, state *serverState) ServerSummary {
	state.mu.Lock()
	defer state.mu.Unlock()
	return summaryForLocked(cfg, state)
}

func summaryForLocked(cfg ServerConfig, state *serverState) ServerSummary {
	status := "idle"
	if !cfg.Enabled {
		status = "disabled"
	} else if state.oauthStatus == oauthclient.StatusAuthorizing {
		status = "authorizing"
	} else if state.oauthStatus == oauthclient.StatusAuthRequired {
		status = "auth_required"
	} else if state.lastError != "" {
		status = "error"
	} else if state.client != nil {
		status = "ready"
	}
	sourceType := cfg.SourceType
	if sourceType == "" {
		sourceType = "standalone"
	}
	displayName := cfg.DisplayName
	if displayName == "" {
		displayName = cfg.Name
	}
	item := ServerSummary{
		Name:          cfg.Name,
		DisplayName:   displayName,
		Description:   cfg.Description,
		Transport:     cfg.Transport,
		SourceType:    sourceType,
		PluginName:    cfg.PluginName,
		Enabled:       cfg.Enabled,
		Status:        status,
		ToolCount:     len(state.tools),
		LastError:     state.lastError,
		LastErrorCode: state.lastErrorCode,
	}
	if !state.refreshedAt.IsZero() {
		item.RefreshedAt = state.refreshedAt.Format(time.RFC3339Nano)
	}
	return item
}

func recordStateError(state *serverState, err error) {
	state.lastError = err.Error()
	state.lastErrorCode = "MCP_ERROR"
	// 每次失败重新判断 OAuth 状态。上一次 401 留下的 auth_required 不能掩盖
	// 后续真实的网络、协议或 schema 错误。
	state.oauthStatus = ""
	var mcpErr *Error
	if errors.As(err, &mcpErr) {
		state.lastErrorCode = mcpErr.Code
		if mcpErr.Code == "MCP_AUTH_REQUIRED" {
			var authRequired *oauthclient.AuthRequiredError
			if errors.As(err, &authRequired) {
				state.oauthStatus = oauthclient.StatusAuthRequired
			}
		}
	}
}

func summarizeTools(cfg ServerConfig, tools map[string]Tool) []ToolSummary {
	items := make([]ToolSummary, 0, len(tools))
	for _, tool := range tools {
		items = append(items, toolSummary(cfg, tool))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].QualifiedName < items[j].QualifiedName })
	return items
}

func toolSummary(cfg ServerConfig, tool Tool) ToolSummary {
	sourceType := cfg.SourceType
	if sourceType == "" {
		sourceType = "standalone"
	}
	return ToolSummary{
		Name:          tool.Name,
		QualifiedName: qualifiedToolName(cfg.Name, tool.Name),
		Title:         tool.Title,
		Description:   tool.Description,
		Server:        cfg.Name,
		SourceType:    sourceType,
		PluginName:    cfg.PluginName,
	}
}

func cloneTools(input map[string]Tool) map[string]Tool {
	out := make(map[string]Tool, len(input))
	for name, tool := range input {
		out[name] = tool
	}
	return out
}

func toolMatchScore(query string, tool Tool) int {
	if query == "*" {
		return 1
	}
	name := strings.ToLower(tool.Name)
	title := strings.ToLower(tool.Title)
	description := strings.ToLower(tool.Description)
	score := 0
	if name == query {
		score += 100
	} else if strings.Contains(name, query) {
		score += 60
	}
	if strings.Contains(title, query) {
		score += 30
	}
	if strings.Contains(description, query) {
		score += 20
	}
	for _, token := range strings.Fields(query) {
		if strings.Contains(name, token) {
			score += 10
		}
		if strings.Contains(title, token) || strings.Contains(description, token) {
			score += 5
		}
	}
	return score
}

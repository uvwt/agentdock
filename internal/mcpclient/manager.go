package mcpclient

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	mu      sync.RWMutex
	store   *store
	servers map[string]ServerConfig
	states  map[string]*serverState
}

type serverState struct {
	mu          sync.Mutex
	client      protocolClient
	tools       map[string]Tool
	lastError   string
	refreshedAt time.Time
}

func NewManager(agentDockHome string) (*Manager, error) {
	registry := newStore(agentDockHome)
	servers, err := registry.load()
	if err != nil {
		return nil, err
	}
	states := make(map[string]*serverState, len(servers))
	for name := range servers {
		states[name] = &serverState{}
	}
	return &Manager{store: registry, servers: servers, states: states}, nil
}

func (m *Manager) Add(cfg ServerConfig) (ServerSummary, error) {
	cfg = normalizeServerConfig(cfg)
	if err := validateServerConfig(cfg); err != nil {
		return ServerSummary{}, newError("MCP_CONFIG_INVALID", err.Error(), false, map[string]any{"server": cfg.Name}, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.servers[cfg.Name]; exists {
		return ServerSummary{}, newError("MCP_SERVER_EXISTS", "dynamic MCP server already exists", false, map[string]any{"server": cfg.Name}, nil)
	}
	m.servers[cfg.Name] = cfg
	m.states[cfg.Name] = &serverState{}
	if err := m.store.save(m.servers); err != nil {
		delete(m.servers, cfg.Name)
		delete(m.states, cfg.Name)
		return ServerSummary{}, newError("MCP_REGISTRY_WRITE_FAILED", "persist dynamic MCP server", false, map[string]any{"server": cfg.Name}, err)
	}
	return summaryFor(cfg, m.states[cfg.Name]), nil
}

func (m *Manager) Remove(name string) error {
	name = strings.TrimSpace(name)
	m.mu.Lock()
	cfg, exists := m.servers[name]
	if !exists {
		m.mu.Unlock()
		return newError("MCP_SERVER_NOT_FOUND", "dynamic MCP server not found", false, map[string]any{"server": name}, nil)
	}
	state := m.states[name]
	delete(m.servers, name)
	delete(m.states, name)
	if err := m.store.save(m.servers); err != nil {
		m.servers[name] = cfg
		m.states[name] = state
		m.mu.Unlock()
		return newError("MCP_REGISTRY_WRITE_FAILED", "remove dynamic MCP server", false, map[string]any{"server": name}, err)
	}
	m.mu.Unlock()
	return closeState(state)
}

func (m *Manager) SetEnabled(name string, enabled bool) (ServerSummary, error) {
	name = strings.TrimSpace(name)
	m.mu.Lock()
	cfg, exists := m.servers[name]
	if !exists {
		m.mu.Unlock()
		return ServerSummary{}, newError("MCP_SERVER_NOT_FOUND", "dynamic MCP server not found", false, map[string]any{"server": name}, nil)
	}
	previous := cfg
	cfg.Enabled = enabled
	m.servers[name] = cfg
	if err := m.store.save(m.servers); err != nil {
		m.servers[name] = previous
		m.mu.Unlock()
		return ServerSummary{}, newError("MCP_REGISTRY_WRITE_FAILED", "persist dynamic MCP server state", false, map[string]any{"server": name}, err)
	}
	state := m.states[name]
	m.mu.Unlock()
	if !enabled {
		if err := closeState(state); err != nil {
			return ServerSummary{}, err
		}
	}
	return summaryFor(cfg, state), nil
}

func (m *Manager) List() []ServerSummary {
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
	m.mu.RLock()
	cfg, exists := m.servers[strings.TrimSpace(name)]
	state := m.states[strings.TrimSpace(name)]
	m.mu.RUnlock()
	if !exists {
		return ServerConfig{}, ServerSummary{}, newError("MCP_SERVER_NOT_FOUND", "dynamic MCP server not found", false, map[string]any{"server": name}, nil)
	}
	return cfg, summaryFor(cfg, state), nil
}

func (m *Manager) Refresh(ctx context.Context, name string) (ServerSummary, []ToolSummary, error) {
	cfg, state, err := m.server(strings.TrimSpace(name))
	if err != nil {
		return ServerSummary{}, nil, err
	}
	if !cfg.Enabled {
		return ServerSummary{}, nil, newError("MCP_SERVER_DISABLED", "dynamic MCP server is disabled", false, map[string]any{"server": cfg.Name}, nil)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutMS)*time.Millisecond)
	defer cancel()
	state.mu.Lock()
	tools, err := refreshStateLocked(ctx, cfg, state)
	summary := summaryForLocked(cfg, state)
	state.mu.Unlock()
	if err != nil {
		return summary, nil, err
	}
	return summary, summarizeTools(cfg.Name, tools), nil
}

func (m *Manager) Search(ctx context.Context, query, server string, limit int) ([]ToolSummary, error) {
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
			matches = append(matches, scoredTool{score: score, item: toolSummary(cfg.Name, tool)})
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
	server, name, err := splitQualifiedToolName(qualifiedName)
	if err != nil {
		return nil, newError("MCP_TOOL_NAME_INVALID", err.Error(), false, map[string]any{"tool": qualifiedName}, err)
	}
	cfg, state, err := m.server(server)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, newError("MCP_SERVER_DISABLED", "dynamic MCP server is disabled", false, map[string]any{"server": server}, nil)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutMS)*time.Millisecond)
	defer cancel()
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.client == nil || len(state.tools) == 0 {
		if _, err := refreshStateLocked(ctx, cfg, state); err != nil {
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
		state.lastError = err.Error()
		return nil, err
	}
	state.lastError = ""
	return result, nil
}

func (m *Manager) Close() error {
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

func (m *Manager) server(name string) (ServerConfig, *serverState, error) {
	m.mu.RLock()
	cfg, exists := m.servers[name]
	state := m.states[name]
	m.mu.RUnlock()
	if !exists {
		return ServerConfig{}, nil, newError("MCP_SERVER_NOT_FOUND", "dynamic MCP server not found", false, map[string]any{"server": name}, nil)
	}
	return cfg, state, nil
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
	cfg, state, err := m.server(name)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, newError("MCP_SERVER_DISABLED", "dynamic MCP server is disabled", false, map[string]any{"server": name}, nil)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutMS)*time.Millisecond)
	defer cancel()
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.client == nil || len(state.tools) == 0 {
		return refreshStateLocked(ctx, cfg, state)
	}
	return cloneTools(state.tools), nil
}

func refreshStateLocked(ctx context.Context, cfg ServerConfig, state *serverState) (map[string]Tool, error) {
	if state.client != nil {
		_ = state.client.close()
	}
	state.client = nil
	state.tools = nil
	client, err := newProtocolClient(cfg)
	if err != nil {
		state.lastError = err.Error()
		return nil, err
	}
	if err := client.initialize(ctx); err != nil {
		_ = client.close()
		state.lastError = err.Error()
		return nil, err
	}
	listed, err := client.listTools(ctx)
	if err != nil {
		_ = client.close()
		state.lastError = err.Error()
		return nil, err
	}
	tools := make(map[string]Tool, len(listed))
	for _, tool := range listed {
		tool.Name = strings.TrimSpace(tool.Name)
		if tool.Name == "" {
			_ = client.close()
			state.lastError = "MCP tools/list returned an empty tool name"
			return nil, newError("MCP_INVALID_RESPONSE", state.lastError, false, map[string]any{"server": cfg.Name}, nil)
		}
		if _, duplicate := tools[tool.Name]; duplicate {
			_ = client.close()
			state.lastError = "MCP tools/list returned duplicate tool names"
			return nil, newError("MCP_INVALID_RESPONSE", state.lastError, false, map[string]any{"server": cfg.Name, "tool": tool.Name}, nil)
		}
		if tool.InputSchema == nil {
			tool.InputSchema = map[string]any{"type": "object", "additionalProperties": true}
		}
		if err := validateToolInputSchema(tool.InputSchema); err != nil {
			_ = client.close()
			state.lastError = "MCP tools/list returned an invalid input schema"
			return nil, newError(
				"MCP_SCHEMA_INVALID",
				state.lastError,
				false,
				map[string]any{"server": cfg.Name, "tool": tool.Name, "reason": err.Error()},
				err,
			)
		}
		tools[tool.Name] = tool
	}
	state.client = client
	state.tools = tools
	state.lastError = ""
	state.refreshedAt = time.Now().UTC()
	return cloneTools(tools), nil
}

func newProtocolClient(cfg ServerConfig) (protocolClient, error) {
	switch cfg.Transport {
	case TransportStreamableHTTP:
		return newStreamableHTTPClient(cfg), nil
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
	} else if state.lastError != "" {
		status = "error"
	} else if state.client != nil {
		status = "ready"
	}
	item := ServerSummary{
		Name:        cfg.Name,
		Description: cfg.Description,
		Transport:   cfg.Transport,
		Enabled:     cfg.Enabled,
		Status:      status,
		ToolCount:   len(state.tools),
		LastError:   state.lastError,
	}
	if !state.refreshedAt.IsZero() {
		item.RefreshedAt = state.refreshedAt.Format(time.RFC3339Nano)
	}
	return item
}

func summarizeTools(server string, tools map[string]Tool) []ToolSummary {
	items := make([]ToolSummary, 0, len(tools))
	for _, tool := range tools {
		items = append(items, toolSummary(server, tool))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].QualifiedName < items[j].QualifiedName })
	return items
}

func toolSummary(server string, tool Tool) ToolSummary {
	return ToolSummary{
		Name:          tool.Name,
		QualifiedName: qualifiedToolName(server, tool.Name),
		Title:         tool.Title,
		Description:   tool.Description,
		Server:        server,
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

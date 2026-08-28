package acp

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type SessionResult struct {
	Session       SessionRecord `json:"session"`
	Modes         any           `json:"modes,omitempty"`
	ConfigOptions any           `json:"config_options,omitempty"`
	Agent         AgentInfo     `json:"agent"`
}

type sessionLifecycleResponse struct {
	SessionID     string `json:"sessionId,omitempty"`
	Modes         any    `json:"modes,omitempty"`
	ConfigOptions any    `json:"configOptions,omitempty"`
}

func (m *Manager) Authenticate(ctx context.Context, methodID string) error {
	methodID = strings.TrimSpace(methodID)
	if methodID == "" {
		return newError("ACP_AUTH_METHOD_INVALID", "ACP authentication method id is required", false, nil, nil)
	}
	process, err := m.ensureProcess(ctx)
	if err != nil {
		return err
	}
	advertised := false
	for _, rawMethod := range process.initialize.AuthMethods {
		method, ok := rawMethod.(map[string]any)
		if !ok {
			continue
		}
		id, _ := method["id"].(string)
		if id == methodID {
			advertised = true
			break
		}
	}
	if !advertised {
		return newError("ACP_AUTH_METHOD_INVALID", "ACP authentication method was not advertised by the agent", false, map[string]any{"auth_method_id": methodID}, nil)
	}
	if err := process.connection.Request(ctx, "authenticate", map[string]any{"methodId": methodID}, nil); err != nil {
		return process.wrapError("authenticate ACP agent", err)
	}
	return nil
}

func (m *Manager) NewSession(ctx context.Context, cwd string, additionalDirectories []string) (SessionResult, error) {
	resolved, err := m.resolveCWD(cwd)
	if err != nil {
		return SessionResult{}, err
	}
	additional, err := m.resolveAdditionalDirectories(additionalDirectories, resolved)
	if err != nil {
		return SessionResult{}, err
	}
	process, err := m.ensureProcess(ctx)
	if err != nil {
		return SessionResult{}, err
	}
	if len(additional) > 0 && !process.supportsSessionCapability("additionalDirectories") {
		return SessionResult{}, capabilityError("sessionCapabilities.additionalDirectories")
	}
	params := sessionCreationParams(resolved, additional)
	var response sessionLifecycleResponse
	if err := process.connection.Request(ctx, "session/new", params, &response); err != nil {
		return SessionResult{}, process.wrapError("create ACP session", err)
	}
	if strings.TrimSpace(response.SessionID) == "" {
		return SessionResult{}, newError("ACP_INVALID_RESPONSE", "ACP session/new omitted sessionId", false, map[string]any{"agent": m.opts.Agent.Name}, nil)
	}
	record, err := m.persistNewSession(response, resolved, additional)
	if err != nil {
		return SessionResult{}, err
	}
	return SessionResult{Session: record, Modes: response.Modes, ConfigOptions: response.ConfigOptions, Agent: process.initialize.AgentInfo}, nil
}

func (m *Manager) LoadSession(ctx context.Context, id string) (SessionResult, error) {
	record, err := m.sessionForActivation(id)
	if err != nil {
		return SessionResult{}, err
	}
	endOperation, err := m.beginSessionOperation(id)
	if err != nil {
		return SessionResult{}, err
	}
	defer endOperation()
	process, err := m.ensureProcess(ctx)
	if err != nil {
		return SessionResult{}, err
	}
	m.mu.RLock()
	response, alreadyLoaded := m.loaded[id]
	m.mu.RUnlock()
	if !alreadyLoaded {
		if !process.supportsLoadSession() {
			return SessionResult{}, capabilityError("loadSession")
		}
		if len(record.AdditionalDirectories) > 0 && !process.supportsSessionCapability("additionalDirectories") {
			return SessionResult{}, capabilityError("sessionCapabilities.additionalDirectories")
		}
		params := sessionActivationParams(record)
		if err := process.connection.Request(ctx, "session/load", params, &response); err != nil {
			wrapped := process.wrapError("load ACP session", err)
			if isCodexNoRolloutError(process.initialize.AgentInfo, wrapped) {
				return SessionResult{}, newError("ACP_SESSION_NOT_PERSISTED", "ACP session has no persisted remote turn to load", false, map[string]any{"session_id": id}, wrapped)
			}
			return SessionResult{}, wrapped
		}
		if err := m.restoreSessionMode(ctx, process, record, &response); err != nil {
			return SessionResult{}, err
		}
	}
	record, err = m.markSessionReady(record, response)
	if err != nil {
		return SessionResult{}, err
	}
	return SessionResult{Session: record, Modes: response.Modes, ConfigOptions: response.ConfigOptions, Agent: process.initialize.AgentInfo}, nil
}

func (m *Manager) ResumeSession(ctx context.Context, id string) (SessionResult, error) {
	record, err := m.sessionForActivation(id)
	if err != nil {
		return SessionResult{}, err
	}
	endOperation, err := m.beginSessionOperation(id)
	if err != nil {
		return SessionResult{}, err
	}
	defer endOperation()
	process, err := m.ensureProcess(ctx)
	if err != nil {
		return SessionResult{}, err
	}
	m.mu.RLock()
	loadedState, alreadyLoaded := m.loaded[id]
	m.mu.RUnlock()
	if alreadyLoaded {
		record, err = m.markSessionReady(record, loadedState)
		if err != nil {
			return SessionResult{}, err
		}
		return SessionResult{Session: record, Modes: loadedState.Modes, ConfigOptions: loadedState.ConfigOptions, Agent: process.initialize.AgentInfo}, nil
	}
	if !process.supportsSessionCapability("resume") {
		return SessionResult{}, capabilityError("sessionCapabilities.resume")
	}
	if len(record.AdditionalDirectories) > 0 && !process.supportsSessionCapability("additionalDirectories") {
		return SessionResult{}, capabilityError("sessionCapabilities.additionalDirectories")
	}
	var response sessionLifecycleResponse
	if err := process.connection.Request(ctx, "session/resume", sessionActivationParams(record), &response); err != nil {
		wrapped := process.wrapError("resume ACP session", err)
		if isCodexNoRolloutError(process.initialize.AgentInfo, wrapped) {
			return SessionResult{}, newError("ACP_SESSION_NOT_PERSISTED", "ACP session has no persisted remote turn to resume", false, map[string]any{"session_id": id}, wrapped)
		}
		return SessionResult{}, wrapped
	}
	if err := m.restoreSessionMode(ctx, process, record, &response); err != nil {
		return SessionResult{}, err
	}
	record, err = m.markSessionReady(record, response)
	if err != nil {
		return SessionResult{}, err
	}
	return SessionResult{Session: record, Modes: response.Modes, ConfigOptions: response.ConfigOptions, Agent: process.initialize.AgentInfo}, nil
}

func (m *Manager) ForkSession(ctx context.Context, id, cwd string, additionalDirectories []string) (SessionResult, error) {
	source, err := m.sessionForActivation(id)
	if err != nil {
		return SessionResult{}, err
	}
	endOperation, err := m.beginSessionOperation(id)
	if err != nil {
		return SessionResult{}, err
	}
	defer endOperation()
	if strings.TrimSpace(cwd) == "" {
		cwd = source.CWD
	}
	resolved, err := m.resolveCWD(cwd)
	if err != nil {
		return SessionResult{}, err
	}
	if additionalDirectories == nil {
		additionalDirectories = source.AdditionalDirectories
	}
	additional, err := m.resolveAdditionalDirectories(additionalDirectories, resolved)
	if err != nil {
		return SessionResult{}, err
	}
	process, err := m.ensureProcess(ctx)
	if err != nil {
		return SessionResult{}, err
	}
	if !process.supportsSessionCapability("fork") {
		return SessionResult{}, capabilityError("sessionCapabilities.fork")
	}
	if len(additional) > 0 && !process.supportsSessionCapability("additionalDirectories") {
		return SessionResult{}, capabilityError("sessionCapabilities.additionalDirectories")
	}
	params := map[string]any{
		"sessionId":  source.RemoteSessionID,
		"cwd":        resolved,
		"mcpServers": []any{},
	}
	if len(additional) > 0 {
		params["additionalDirectories"] = additional
	}
	var response sessionLifecycleResponse
	if err := process.connection.Request(ctx, "session/fork", params, &response); err != nil {
		return SessionResult{}, process.wrapError("fork ACP session", err)
	}
	if strings.TrimSpace(response.SessionID) == "" {
		return SessionResult{}, newError("ACP_INVALID_RESPONSE", "ACP session/fork omitted sessionId", false, map[string]any{"session_id": id}, nil)
	}
	record, err := m.persistNewSession(response, resolved, additional)
	if err != nil {
		return SessionResult{}, err
	}
	return SessionResult{Session: record, Modes: response.Modes, ConfigOptions: response.ConfigOptions, Agent: process.initialize.AgentInfo}, nil
}

func (m *Manager) SetSessionMode(ctx context.Context, id, modeID string) error {
	modeID = strings.TrimSpace(modeID)
	if modeID == "" {
		return newError("ACP_MODE_INVALID", "ACP session mode id is required", false, nil, nil)
	}
	loaded, err := m.LoadSession(ctx, id)
	if err != nil {
		return err
	}
	record := loaded.Session
	endOperation, err := m.beginSessionOperation(id)
	if err != nil {
		return err
	}
	defer endOperation()
	process, err := m.ensureProcess(ctx)
	if err != nil {
		return err
	}
	if err := process.connection.Request(ctx, "session/set_mode", map[string]any{"sessionId": record.RemoteSessionID, "modeId": modeID}, nil); err != nil {
		return process.wrapError("set ACP session mode", err)
	}
	m.mu.Lock()
	current := m.sessions[id]
	current.ModeID = modeID
	current.UpdatedAt = time.Now().UTC()
	if err := m.store.Save(current); err != nil {
		m.mu.Unlock()
		return err
	}
	m.sessions[id] = current
	if state, ok := m.loaded[id]; ok {
		applyModeToLifecycleState(&state, modeID)
		m.loaded[id] = state
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) SetSessionConfigOption(ctx context.Context, id, configID string, value any) (any, error) {
	configID = strings.TrimSpace(configID)
	if configID == "" {
		return nil, newError("ACP_CONFIG_OPTION_INVALID", "ACP session config id is required", false, nil, nil)
	}
	loaded, err := m.LoadSession(ctx, id)
	if err != nil {
		return nil, err
	}
	record := loaded.Session
	endOperation, err := m.beginSessionOperation(id)
	if err != nil {
		return nil, err
	}
	defer endOperation()
	params := map[string]any{"sessionId": record.RemoteSessionID, "configId": configID}
	switch typed := value.(type) {
	case bool:
		params["type"] = "boolean"
		params["value"] = typed
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil, newError("ACP_CONFIG_OPTION_INVALID", "ACP session config value must not be empty", false, nil, nil)
		}
		params["value"] = typed
	default:
		return nil, newError("ACP_CONFIG_OPTION_INVALID", "ACP session config value must be a string or boolean", false, map[string]any{"value_type": typeName(value)}, nil)
	}
	process, err := m.ensureProcess(ctx)
	if err != nil {
		return nil, err
	}
	var response struct {
		ConfigOptions any `json:"configOptions"`
	}
	if err := process.connection.Request(ctx, "session/set_config_option", params, &response); err != nil {
		return nil, process.wrapError("set ACP session config option", err)
	}
	// set_config_option 成功后必须返回更新后的配置选项；否则无法确认远端实际状态，
	// 也不能把缺失值继续包装成符合 MCP array schema 的成功结果。
	if response.ConfigOptions == nil {
		return nil, newError("ACP_INVALID_RESPONSE", "ACP session/set_config_option omitted configOptions", false, map[string]any{"session_id": id, "config_id": configID}, nil)
	}
	m.mu.Lock()
	if state, loaded := m.loaded[id]; loaded {
		state.ConfigOptions = response.ConfigOptions
		m.loaded[id] = state
	}
	m.mu.Unlock()
	return response.ConfigOptions, nil
}

func (m *Manager) ListSessions() ([]SessionRecord, error) {
	return m.store.List()
}

func (m *Manager) InspectSession(id string) (SessionRecord, error) {
	return m.session(id)
}

func (m *Manager) CloseSession(ctx context.Context, id string) (SessionRecord, error) {
	record, err := m.session(id)
	if err != nil {
		return SessionRecord{}, err
	}
	if record.Status == SessionClosed {
		return record, nil
	}
	previousTerminal, hadPreviousTerminal, err := m.beginTerminalTransition(ctx, id, SessionClosed)
	if err != nil {
		return SessionRecord{}, err
	}
	succeeded := false
	defer func() {
		if !succeeded {
			m.rollbackTerminalTransition(id, SessionClosed, previousTerminal, hadPreviousTerminal)
		}
	}()
	_ = m.CancelPrompt(ctx, id, "")
	process, err := m.ensureProcess(ctx)
	if err != nil {
		return SessionRecord{}, err
	}
	if process.supportsSessionCapability("close") {
		if err := process.connection.Request(ctx, "session/close", map[string]any{"sessionId": record.RemoteSessionID}, nil); err != nil {
			return SessionRecord{}, process.wrapError("close ACP session", err)
		}
	}
	now := time.Now().UTC()
	record.Status = SessionClosed
	record.UpdatedAt = now
	record.ClosedAt = &now
	record.LastStopReason = "closed"
	if err := m.store.Save(record); err != nil {
		return SessionRecord{}, err
	}
	m.mu.Lock()
	m.sessions[id] = record
	delete(m.loaded, id)
	m.mu.Unlock()
	succeeded = true
	return record, nil
}

func (m *Manager) DeleteSession(ctx context.Context, id string) error {
	record, err := m.session(id)
	if err != nil {
		return err
	}
	previousTerminal, hadPreviousTerminal, err := m.beginTerminalTransition(ctx, id, sessionDeleted)
	if err != nil {
		return err
	}
	succeeded := false
	defer func() {
		if !succeeded {
			m.rollbackTerminalTransition(id, sessionDeleted, previousTerminal, hadPreviousTerminal)
		}
	}()
	_ = m.CancelPrompt(ctx, id, "")
	process, err := m.ensureProcess(ctx)
	if err != nil {
		return err
	}
	if process.supportsSessionCapability("delete") {
		if err := process.connection.Request(ctx, "session/delete", map[string]any{"sessionId": record.RemoteSessionID}, nil); err != nil {
			wrapped := process.wrapError("delete ACP session", err)
			if !isCodexNoRolloutError(process.initialize.AgentInfo, wrapped) {
				return wrapped
			}
		}
	}
	if err := m.store.Delete(id); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.sessions, id)
	delete(m.remoteToLocal, record.RemoteSessionID)
	delete(m.loaded, id)
	m.mu.Unlock()
	succeeded = true
	return nil
}

func (m *Manager) persistNewSession(state sessionLifecycleResponse, cwd string, additional []string) (SessionRecord, error) {
	id, err := newID("acps")
	if err != nil {
		return SessionRecord{}, err
	}
	now := time.Now().UTC()
	record := SessionRecord{
		SchemaVersion: sessionSchemaVersion, ID: id, Agent: m.opts.Agent.Name,
		RemoteSessionID: state.SessionID, CWD: cwd, AdditionalDirectories: append([]string(nil), additional...),
		Status: SessionReady, CreatedAt: now, UpdatedAt: now,
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return SessionRecord{}, newError("ACP_MANAGER_CLOSED", "ACP manager is closed", false, nil, nil)
	}
	if existing := m.remoteToLocal[state.SessionID]; existing != "" {
		m.mu.Unlock()
		return SessionRecord{}, newError("ACP_INVALID_RESPONSE", "ACP agent reused an existing remote session id", false, map[string]any{"remote_session_id": state.SessionID, "existing_session_id": existing}, nil)
	}
	// The bounded private state write is intentionally serialized with the map
	// commit so no caller can observe an unpersisted remote-to-local mapping.
	if err := m.store.Save(record); err != nil {
		m.mu.Unlock()
		return SessionRecord{}, err
	}
	m.sessions[id] = record
	m.remoteToLocal[record.RemoteSessionID] = id
	m.loaded[id] = state
	m.mu.Unlock()
	return record, nil
}

func (m *Manager) sessionForActivation(id string) (SessionRecord, error) {
	record, err := m.session(id)
	if err != nil {
		return SessionRecord{}, err
	}
	m.mu.RLock()
	terminal, transitioning := m.terminalSessions[id]
	m.mu.RUnlock()
	if transitioning {
		if terminal == sessionDeleted {
			return SessionRecord{}, newError("ACP_SESSION_NOT_FOUND", "ACP session was deleted", false, map[string]any{"session_id": id}, nil)
		}
		return SessionRecord{}, newError("ACP_SESSION_CLOSED", "ACP session is closing or closed", false, map[string]any{"session_id": id}, nil)
	}
	if record.Status == SessionClosed {
		return SessionRecord{}, newError("ACP_SESSION_CLOSED", "ACP session is closed", false, map[string]any{"session_id": id}, nil)
	}
	if _, err := m.resolveCWD(record.CWD); err != nil {
		return SessionRecord{}, err
	}
	additional, err := m.resolveAdditionalDirectories(record.AdditionalDirectories, record.CWD)
	if err != nil {
		return SessionRecord{}, err
	}
	record.AdditionalDirectories = additional
	return record, nil
}

func currentModeID(modes any) string {
	modeState, _ := modes.(map[string]any)
	modeID, _ := modeState["currentModeId"].(string)
	return strings.TrimSpace(modeID)
}

func applyModeToLifecycleState(state *sessionLifecycleResponse, modeID string) {
	if state == nil || strings.TrimSpace(modeID) == "" {
		return
	}
	if modes, ok := state.Modes.(map[string]any); ok {
		updated := make(map[string]any, len(modes))
		for key, value := range modes {
			updated[key] = value
		}
		updated["currentModeId"] = modeID
		state.Modes = updated
	}
	if options, ok := state.ConfigOptions.([]any); ok {
		for _, raw := range options {
			if option, ok := raw.(map[string]any); ok && option["id"] == "mode" {
				option["currentValue"] = modeID
			}
		}
	}
	if options, ok := state.ConfigOptions.([]map[string]any); ok {
		for _, option := range options {
			if option["id"] == "mode" {
				option["currentValue"] = modeID
			}
		}
	}
}

func (m *Manager) restoreSessionMode(ctx context.Context, process *agentProcess, record SessionRecord, state *sessionLifecycleResponse) error {
	modeID := strings.TrimSpace(record.ModeID)
	if modeID == "" || currentModeID(state.Modes) == modeID {
		return nil
	}
	if err := process.connection.Request(ctx, "session/set_mode", map[string]any{"sessionId": record.RemoteSessionID, "modeId": modeID}, nil); err != nil {
		return process.wrapError("restore ACP session mode", err)
	}
	applyModeToLifecycleState(state, modeID)
	return nil
}

func (m *Manager) markSessionReady(record SessionRecord, state sessionLifecycleResponse) (SessionRecord, error) {
	record.Status = SessionReady
	record.LastStopReason = ""
	record.UpdatedAt = time.Now().UTC()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return SessionRecord{}, newError("ACP_MANAGER_CLOSED", "ACP manager is closed", false, nil, nil)
	}
	if terminal, exists := m.terminalSessions[record.ID]; exists {
		m.mu.Unlock()
		if terminal == sessionDeleted {
			return SessionRecord{}, newError("ACP_SESSION_NOT_FOUND", "ACP session was deleted", false, map[string]any{"session_id": record.ID}, nil)
		}
		return SessionRecord{}, newError("ACP_SESSION_CLOSED", "ACP session is closing or closed", false, map[string]any{"session_id": record.ID}, nil)
	}
	// Keep the bounded private state write and loaded-state cache update atomic
	// from the manager's perspective.
	if err := m.store.Save(record); err != nil {
		m.mu.Unlock()
		return SessionRecord{}, err
	}
	m.sessions[record.ID] = record
	m.remoteToLocal[record.RemoteSessionID] = record.ID
	m.loaded[record.ID] = state
	m.mu.Unlock()
	return record, nil
}

func sessionCreationParams(cwd string, additional []string) map[string]any {
	params := map[string]any{"cwd": cwd, "mcpServers": []any{}}
	if len(additional) > 0 {
		params["additionalDirectories"] = additional
	}
	return params
}

func sessionActivationParams(record SessionRecord) map[string]any {
	params := sessionCreationParams(record.CWD, record.AdditionalDirectories)
	params["sessionId"] = record.RemoteSessionID
	return params
}

func (m *Manager) rebindRemoteSession(record SessionRecord, state sessionLifecycleResponse) (SessionRecord, error) {
	remoteID := strings.TrimSpace(state.SessionID)
	if remoteID == "" {
		return SessionRecord{}, newError("ACP_INVALID_RESPONSE", "ACP session/new omitted sessionId during recovery", false, map[string]any{"session_id": record.ID}, nil)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return SessionRecord{}, newError("ACP_MANAGER_CLOSED", "ACP manager is closed", false, nil, nil)
	}
	current, exists := m.sessions[record.ID]
	if !exists {
		return SessionRecord{}, newError("ACP_SESSION_NOT_FOUND", "ACP session was not found", false, map[string]any{"session_id": record.ID}, nil)
	}
	if current.RemoteSessionID != record.RemoteSessionID {
		return SessionRecord{}, newError("ACP_SESSION_STATE_INVALID", "ACP session remote id changed during recovery", false, map[string]any{"session_id": record.ID}, nil)
	}
	if existing := m.remoteToLocal[remoteID]; existing != "" && existing != record.ID {
		return SessionRecord{}, newError("ACP_INVALID_RESPONSE", "ACP agent reused an existing remote session id", false, map[string]any{"remote_session_id": remoteID, "existing_session_id": existing}, nil)
	}

	current.RemoteSessionID = remoteID
	current.UpdatedAt = time.Now().UTC()
	if err := m.store.Save(current); err != nil {
		return SessionRecord{}, err
	}
	delete(m.remoteToLocal, record.RemoteSessionID)
	m.remoteToLocal[remoteID] = record.ID
	m.sessions[record.ID] = current
	delete(m.loaded, record.ID)
	return current, nil
}

func typeName(value any) string {
	if value == nil {
		return "null"
	}
	return fmt.Sprintf("%T", value)
}

func (m *Manager) beginTerminalTransition(ctx context.Context, id string, target SessionStatus) (SessionStatus, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", false, newError("ACP_SESSION_TRANSITION_CANCELLED", "ACP session terminal transition was cancelled", true, map[string]any{"session_id": id}, err)
	}
	m.mu.Lock()
	stopWake := context.AfterFunc(ctx, func() {
		m.mu.Lock()
		m.operationsCond.Broadcast()
		m.mu.Unlock()
	})
	defer func() {
		stopWake()
		m.mu.Unlock()
	}()
	if m.closed {
		return "", false, newError("ACP_MANAGER_CLOSED", "ACP manager is closed", false, nil, nil)
	}
	previous, exists := m.terminalSessions[id]
	if exists {
		switch {
		case previous == sessionDeleted:
			return previous, true, newError("ACP_SESSION_NOT_FOUND", "ACP session was deleted", false, map[string]any{"session_id": id}, nil)
		case previous == SessionClosed && target == sessionDeleted:
			if m.sessions[id].Status != SessionClosed {
				return previous, true, newError("ACP_SESSION_TRANSITION", "ACP session close is still in progress", true, map[string]any{"session_id": id, "target": previous}, nil)
			}
			m.terminalSessions[id] = target
		default:
			return previous, true, newError("ACP_SESSION_TRANSITION", "ACP session already has a terminal transition in progress", true, map[string]any{"session_id": id, "target": previous}, nil)
		}
	} else {
		m.terminalSessions[id] = target
	}
	for m.sessionOperations[id] > 0 {
		if err := ctx.Err(); err != nil {
			m.rollbackTerminalTransitionLocked(id, target, previous, exists)
			return previous, exists, newError("ACP_SESSION_TRANSITION_CANCELLED", "ACP session terminal transition was cancelled", true, map[string]any{"session_id": id}, err)
		}
		if m.closed {
			m.rollbackTerminalTransitionLocked(id, target, previous, exists)
			return previous, exists, newError("ACP_MANAGER_CLOSED", "ACP manager is closed", false, nil, nil)
		}
		m.operationsCond.Wait()
	}
	return previous, exists, nil
}

func (m *Manager) rollbackTerminalTransition(id string, target, previous SessionStatus, hadPrevious bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rollbackTerminalTransitionLocked(id, target, previous, hadPrevious)
}

func (m *Manager) rollbackTerminalTransitionLocked(id string, target, previous SessionStatus, hadPrevious bool) {
	if m.terminalSessions[id] != target {
		return
	}
	if hadPrevious {
		m.terminalSessions[id] = previous
		return
	}
	delete(m.terminalSessions, id)
}

func (m *Manager) beginSessionOperation(id string) (func(), error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, newError("ACP_MANAGER_CLOSED", "ACP manager is closed", false, nil, nil)
	}
	if terminal, exists := m.terminalSessions[id]; exists {
		m.mu.Unlock()
		if terminal == sessionDeleted {
			return nil, newError("ACP_SESSION_NOT_FOUND", "ACP session was deleted", false, map[string]any{"session_id": id}, nil)
		}
		return nil, newError("ACP_SESSION_CLOSED", "ACP session is closing or closed", false, map[string]any{"session_id": id}, nil)
	}
	m.sessionOperations[id]++
	m.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			m.sessionOperations[id]--
			if m.sessionOperations[id] <= 0 {
				delete(m.sessionOperations, id)
			}
			m.operationsCond.Broadcast()
			m.mu.Unlock()
		})
	}, nil
}

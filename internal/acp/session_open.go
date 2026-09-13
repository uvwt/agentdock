package acp

import (
	"context"
	"strings"
)

// EnsureSessionActive 只负责让已 managed 的会话可继续执行，不要求调用方理解
// ACP 的 load/resume 差异。优先使用 resume 避免无意义的历史回放；仅当 Adapter
// 没有 resume 时才退回标准 session/load。
func (m *Manager) EnsureSessionActive(ctx context.Context, id string) (SessionResult, error) {
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
	state, alreadyLoaded := m.loaded[id]
	m.mu.RUnlock()
	if alreadyLoaded {
		record, err = m.markSessionReady(record, state)
		if err != nil {
			return SessionResult{}, err
		}
		return SessionResult{Session: record, Modes: state.Modes, ConfigOptions: state.ConfigOptions, Agent: process.initialize.AgentInfo}, nil
	}
	if len(record.AdditionalDirectories) > 0 && !process.supportsSessionCapability("additionalDirectories") {
		return SessionResult{}, capabilityError("sessionCapabilities.additionalDirectories")
	}

	params := sessionActivationParams(record)
	switch {
	case process.supportsSessionCapability("resume"):
		if err := process.connection.Request(ctx, "session/resume", params, &state); err != nil {
			wrapped := process.wrapError("resume ACP session", err)
			if isCodexNoRolloutError(process.initialize.AgentInfo, wrapped) {
				return SessionResult{}, newError("ACP_SESSION_NOT_PERSISTED", "ACP session has no persisted remote turn to resume", false, map[string]any{"session_id": id}, wrapped)
			}
			return SessionResult{}, wrapped
		}
	case process.supportsLoadSession():
		if err := process.connection.Request(ctx, "session/load", params, &state); err != nil {
			wrapped := process.wrapError("load ACP session", err)
			if isCodexNoRolloutError(process.initialize.AgentInfo, wrapped) {
				return SessionResult{}, newError("ACP_SESSION_NOT_PERSISTED", "ACP session has no persisted remote turn to load", false, map[string]any{"session_id": id}, wrapped)
			}
			return SessionResult{}, wrapped
		}
	default:
		return SessionResult{}, capabilityError("sessionCapabilities.resume or loadSession")
	}
	if err := m.restoreSessionMode(ctx, process, record, &state); err != nil {
		return SessionResult{}, err
	}
	record, err = m.markSessionReady(record, state)
	if err != nil {
		return SessionResult{}, err
	}
	return SessionResult{Session: record, Modes: state.Modes, ConfigOptions: state.ConfigOptions, Agent: process.initialize.AgentInfo}, nil
}

// OpenSession 接受 AgentDock session id 或 Adapter 原生 remote id。
// 对 remote id，AgentDock 只建立轻量映射，不复制 transcript，再自动选择 resume/load 激活。
func (m *Manager) OpenSession(ctx context.Context, sessionID, remoteSessionID string) (SessionResult, bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	remoteSessionID = strings.TrimSpace(remoteSessionID)
	if sessionID != "" && remoteSessionID != "" {
		return SessionResult{}, false, newError("ACP_SESSION_TARGET_INVALID", "provide either session_id or remote_session_id, not both", false, nil, nil)
	}
	if sessionID == "" && remoteSessionID == "" {
		return SessionResult{}, false, newError("ACP_SESSION_TARGET_REQUIRED", "session_id or remote_session_id is required", false, nil, nil)
	}
	attached := false
	if sessionID == "" {
		if existing, ok := m.ManagedSessionForRemote(remoteSessionID); ok {
			sessionID = existing.ID
		} else {
			remote, err := m.FindRemoteSession(ctx, remoteSessionID)
			if err != nil {
				return SessionResult{}, false, err
			}
			record, err := m.AttachRemoteSession(remote)
			if err != nil {
				return SessionResult{}, false, err
			}
			sessionID = record.ID
			attached = true
		}
	}
	result, err := m.EnsureSessionActive(ctx, sessionID)
	return result, attached, err
}

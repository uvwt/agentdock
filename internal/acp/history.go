package acp

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
)

const (
	maxHistoryReplayEvents = 4096
	maxHistoryReplayBytes  = 8 << 20
	maxHistoryUpdateBytes  = 1 << 20
)

// HistoryEvent 是一次 session/load 期间由 Adapter 回放的原生会话事件。
// AgentDock 只在当前调用内暂存这些事件，不把 transcript 写入本地 SessionRecord。
type HistoryEvent struct {
	Source              string          `json:"source"`
	Type                string          `json:"type"`
	Update              json.RawMessage `json:"update"`
	UpdateTruncated     bool            `json:"update_truncated,omitempty"`
	OriginalUpdateBytes int             `json:"original_update_bytes,omitempty"`
}

type HistoryReplay struct {
	Events    []HistoryEvent `json:"events"`
	Truncated bool           `json:"truncated"`
}

type historyCollector struct {
	mu        sync.Mutex
	events    []HistoryEvent
	bytes     int
	truncated bool
}

func (c *historyCollector) append(eventType string, update json.RawMessage) {
	if c == nil || eventType == "agent_thought_chunk" {
		return
	}
	copyUpdate := append(json.RawMessage(nil), update...)
	updateTruncated := false
	originalBytes := 0
	if len(copyUpdate) > maxHistoryUpdateBytes {
		copyUpdate, updateTruncated, originalBytes = boundedEventUpdate(update)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.events) >= maxHistoryReplayEvents || c.bytes+len(copyUpdate) > maxHistoryReplayBytes {
		c.truncated = true
		return
	}
	c.events = append(c.events, HistoryEvent{
		Source: "acp", Type: eventType, Update: copyUpdate,
		UpdateTruncated: updateTruncated, OriginalUpdateBytes: originalBytes,
	})
	if updateTruncated {
		c.truncated = true
	}
	c.bytes += len(copyUpdate)
}

func (c *historyCollector) snapshot() HistoryReplay {
	if c == nil {
		return HistoryReplay{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	events := make([]HistoryEvent, len(c.events))
	copy(events, c.events)
	return HistoryReplay{Events: events, Truncated: c.truncated}
}

func (m *Manager) beginHistoryReplay(remoteSessionID string) (*historyCollector, func(), error) {
	remoteSessionID = strings.TrimSpace(remoteSessionID)
	if remoteSessionID == "" {
		return nil, nil, newError("ACP_SESSION_ID_INVALID", "ACP remote session id is required", false, nil, nil)
	}
	collector := &historyCollector{}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, nil, newError("ACP_MANAGER_CLOSED", "ACP manager is closed", false, nil, nil)
	}
	if localID := m.remoteToLocal[remoteSessionID]; localID != "" && m.activeRunBySession[localID] != "" {
		m.mu.Unlock()
		return nil, nil, newError("ACP_SESSION_BUSY", "ACP session history cannot be loaded while the managed session has an active prompt", true, map[string]any{"remote_session_id": remoteSessionID, "session_id": localID}, nil)
	}
	if m.historyCollectors[remoteSessionID] != nil {
		m.mu.Unlock()
		return nil, nil, newError("ACP_SESSION_BUSY", "ACP session history is already being loaded", true, map[string]any{"remote_session_id": remoteSessionID}, nil)
	}
	m.historyCollectors[remoteSessionID] = collector
	m.mu.Unlock()

	return collector, func() {
		m.mu.Lock()
		if m.historyCollectors[remoteSessionID] == collector {
			delete(m.historyCollectors, remoteSessionID)
		}
		m.mu.Unlock()
	}, nil
}

// ReadRemoteHistory 显式调用标准 session/load，并只收集该调用期间 Adapter 回放的
// session/update。它不会创建 acps_* 映射，也不会把历史写入 AgentDock 持久化状态。
func (m *Manager) ReadRemoteHistory(ctx context.Context, remoteSessionID, cwd string, additionalDirectories []string) (HistoryReplay, sessionLifecycleResponse, error) {
	remoteSessionID = strings.TrimSpace(remoteSessionID)
	if remoteSessionID == "" {
		return HistoryReplay{}, sessionLifecycleResponse{}, newError("ACP_SESSION_ID_INVALID", "ACP remote session id is required", false, nil, nil)
	}
	resolved, err := m.resolveCWD(cwd)
	if err != nil {
		return HistoryReplay{}, sessionLifecycleResponse{}, err
	}
	additional, err := m.resolveAdditionalDirectories(additionalDirectories, resolved)
	if err != nil {
		return HistoryReplay{}, sessionLifecycleResponse{}, err
	}
	process, err := m.ensureProcess(ctx)
	if err != nil {
		return HistoryReplay{}, sessionLifecycleResponse{}, err
	}
	if !process.supportsLoadSession() {
		return HistoryReplay{}, sessionLifecycleResponse{}, capabilityError("loadSession")
	}
	if len(additional) > 0 && !process.supportsSessionCapability("additionalDirectories") {
		return HistoryReplay{}, sessionLifecycleResponse{}, capabilityError("sessionCapabilities.additionalDirectories")
	}
	collector, finish, err := m.beginHistoryReplay(remoteSessionID)
	if err != nil {
		return HistoryReplay{}, sessionLifecycleResponse{}, err
	}
	defer finish()

	params := sessionCreationParams(resolved, additional)
	params["sessionId"] = remoteSessionID
	var response sessionLifecycleResponse
	if err := process.connection.Request(ctx, "session/load", params, &response); err != nil {
		return HistoryReplay{}, sessionLifecycleResponse{}, process.wrapError("load ACP session history", err)
	}
	return collector.snapshot(), response, nil
}

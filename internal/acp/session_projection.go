package acp

import (
	"encoding/json"
	"log/slog"
	"time"
)

// SessionProjection is bounded, process-local Adapter state derived from standard
// session/update notifications. It is not a transcript and is never treated as a
// second source of truth for conversation history.
type SessionProjection struct {
	ModeID            string         `json:"mode_id,omitempty"`
	ConfigOptions     any            `json:"config_options,omitempty"`
	AvailableCommands any            `json:"available_commands,omitempty"`
	SessionInfo       map[string]any `json:"session_info,omitempty"`
	Usage             map[string]any `json:"usage,omitempty"`
}

func (m *Manager) SessionProjection(sessionID string) (SessionProjection, error) {
	if _, err := m.session(sessionID); err != nil {
		return SessionProjection{}, err
	}
	m.mu.RLock()
	projection := m.projections[sessionID]
	m.mu.RUnlock()
	projection.SessionInfo = cloneMap(projection.SessionInfo)
	projection.Usage = cloneMap(projection.Usage)
	return projection, nil
}

func (m *Manager) applySessionProjection(localID, eventType string, update json.RawMessage) {
	if localID == "" {
		return
	}
	var payload map[string]any
	if json.Unmarshal(update, &payload) != nil {
		return
	}

	m.mu.Lock()
	if m.projections == nil {
		m.projections = make(map[string]SessionProjection)
	}
	projection := m.projections[localID]
	record, recordExists := m.sessions[localID]
	saveRecord := false
	switch eventType {
	case "current_mode_update":
		if modeID, _ := payload["currentModeId"].(string); modeID != "" {
			projection.ModeID = modeID
			if recordExists && record.ModeID != modeID {
				record.ModeID = modeID
				record.UpdatedAt = time.Now().UTC()
				m.sessions[localID] = record
				saveRecord = true
			}
			if state, ok := m.loaded[localID]; ok {
				applyModeToLifecycleState(&state, modeID)
				m.loaded[localID] = state
			}
		}
	case "config_option_update":
		if options, exists := payload["configOptions"]; exists {
			projection.ConfigOptions = options
			if state, ok := m.loaded[localID]; ok {
				state.ConfigOptions = options
				m.loaded[localID] = state
			}
		}
	case "available_commands_update":
		projection.AvailableCommands = payload["availableCommands"]
	case "session_info_update":
		delete(payload, "sessionUpdate")
		projection.SessionInfo = cloneMap(payload)
	case "usage_update":
		delete(payload, "sessionUpdate")
		projection.Usage = cloneMap(payload)
	}
	m.projections[localID] = projection
	m.mu.Unlock()

	if saveRecord {
		if err := m.store.Save(record); err != nil {
			slog.Warn("persist ACP session projection failed", "session_id", localID, "error", err)
		}
	}
}

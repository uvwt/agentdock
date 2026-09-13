package acp

import "encoding/json"

func (m *Manager) handleNotification(method string, params json.RawMessage) {
	if method != "session/update" {
		return
	}
	var notification struct {
		SessionID string          `json:"sessionId"`
		Update    json.RawMessage `json:"update"`
	}
	if err := json.Unmarshal(params, &notification); err != nil || notification.SessionID == "" {
		return
	}

	eventType := "session_update"
	var updateType struct {
		SessionUpdate string `json:"sessionUpdate"`
	}
	if json.Unmarshal(notification.Update, &updateType) == nil && updateType.SessionUpdate != "" {
		eventType = updateType.SessionUpdate
	}
	// ACP 的 thought chunk 属于模型私有推理，不应进入历史回放结果或 Run 事件环。
	if eventType == "agent_thought_chunk" {
		return
	}

	m.mu.RLock()
	collector := m.historyCollectors[notification.SessionID]
	localID := m.remoteToLocal[notification.SessionID]
	runID := m.activeRunBySession[localID]
	run := m.runs[runID]
	m.mu.RUnlock()

	// 标准 session/update 同时驱动轻量 runtime projection；它与 transcript 分离，
	// 因此即使没有 active Run，mode/config/commands/session info 也不会被静默丢弃。
	m.applySessionProjection(localID, eventType, notification.Update)

	// session/load 的历史 replay 不依赖 active prompt。过去这里只路由给 Run，
	// 导致标准 ACP load 回放在没有正在执行的 prompt 时被静默丢弃。
	if collector != nil {
		collector.append(eventType, notification.Update)
	}
	if run != nil {
		run.appendSessionUpdate(Event{Source: "acp", Type: eventType, Update: append(json.RawMessage(nil), notification.Update...)})
	}
}

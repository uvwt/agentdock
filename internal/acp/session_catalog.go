package acp

import (
	"context"
	"strings"
	"time"
)

const maxRemoteSessionLookupPages = 128

// RemoteSession 描述 Adapter 通过标准 session/list 暴露的原生会话。
// SessionID 只有在该 remote session 已经被 AgentDock 管理时才会出现。
type RemoteSession struct {
	RemoteSessionID       string         `json:"remote_session_id"`
	CWD                   string         `json:"cwd"`
	AdditionalDirectories []string       `json:"additional_directories,omitempty"`
	Title                 string         `json:"title,omitempty"`
	UpdatedAt             string         `json:"updated_at,omitempty"`
	Meta                  map[string]any `json:"_meta,omitempty"`
	Managed               bool           `json:"managed"`
	SessionID             string         `json:"session_id,omitempty"`
}

type RemoteSessionPage struct {
	Sessions   []RemoteSession `json:"sessions"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

type remoteSessionWire struct {
	SessionID             string         `json:"sessionId"`
	CWD                   string         `json:"cwd"`
	AdditionalDirectories []string       `json:"additionalDirectories,omitempty"`
	Title                 string         `json:"title,omitempty"`
	UpdatedAt             string         `json:"updatedAt,omitempty"`
	Meta                  map[string]any `json:"_meta,omitempty"`
}

type remoteSessionListResponse struct {
	Sessions   []remoteSessionWire `json:"sessions"`
	NextCursor string              `json:"nextCursor,omitempty"`
}

func (m *Manager) ListManagedSessions() ([]SessionRecord, error) {
	return m.store.List()
}

// ListSessions 保留为内部兼容别名。工具层的 list 已经不再把它误认为 ACP session/list。
func (m *Manager) ListSessions() ([]SessionRecord, error) {
	return m.ListManagedSessions()
}

func (m *Manager) ListRemoteSessions(ctx context.Context, cwd, cursor string) (RemoteSessionPage, error) {
	process, err := m.ensureProcess(ctx)
	if err != nil {
		return RemoteSessionPage{}, err
	}
	if !process.supportsSessionCapability("list") {
		return RemoteSessionPage{}, capabilityError("sessionCapabilities.list")
	}
	params := map[string]any{}
	if strings.TrimSpace(cwd) != "" {
		resolved, err := m.resolveCWD(cwd)
		if err != nil {
			return RemoteSessionPage{}, err
		}
		params["cwd"] = resolved
	}
	if cursor = strings.TrimSpace(cursor); cursor != "" {
		params["cursor"] = cursor
	}
	var response remoteSessionListResponse
	if err := process.connection.Request(ctx, "session/list", params, &response); err != nil {
		return RemoteSessionPage{}, process.wrapError("list ACP sessions", err)
	}

	page := RemoteSessionPage{Sessions: make([]RemoteSession, 0, len(response.Sessions)), NextCursor: response.NextCursor}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, remote := range response.Sessions {
		remoteID := strings.TrimSpace(remote.SessionID)
		if remoteID == "" {
			continue
		}
		localID := m.remoteToLocal[remoteID]
		page.Sessions = append(page.Sessions, RemoteSession{
			RemoteSessionID:       remoteID,
			CWD:                   remote.CWD,
			AdditionalDirectories: append([]string(nil), remote.AdditionalDirectories...),
			Title:                 remote.Title,
			UpdatedAt:             remote.UpdatedAt,
			Meta:                  cloneMap(remote.Meta),
			Managed:               localID != "",
			SessionID:             localID,
		})
	}
	return page, nil
}

func (m *Manager) FindRemoteSession(ctx context.Context, remoteSessionID string) (RemoteSession, error) {
	remoteSessionID = strings.TrimSpace(remoteSessionID)
	if remoteSessionID == "" {
		return RemoteSession{}, newError("ACP_SESSION_ID_INVALID", "ACP remote session id is required", false, nil, nil)
	}
	cursor := ""
	seen := map[string]struct{}{}
	for pageNumber := 0; pageNumber < maxRemoteSessionLookupPages; pageNumber++ {
		page, err := m.ListRemoteSessions(ctx, "", cursor)
		if err != nil {
			return RemoteSession{}, err
		}
		for _, remote := range page.Sessions {
			if remote.RemoteSessionID == remoteSessionID {
				return remote, nil
			}
		}
		next := strings.TrimSpace(page.NextCursor)
		if next == "" {
			break
		}
		if _, repeated := seen[next]; repeated {
			return RemoteSession{}, newError("ACP_INVALID_RESPONSE", "ACP session/list repeated a pagination cursor", false, map[string]any{"cursor": next}, nil)
		}
		seen[next] = struct{}{}
		cursor = next
	}
	return RemoteSession{}, newError("ACP_REMOTE_SESSION_NOT_FOUND", "ACP remote session was not found", false, map[string]any{"remote_session_id": remoteSessionID}, nil)
}

func (m *Manager) ManagedSessionForRemote(remoteSessionID string) (SessionRecord, bool) {
	remoteSessionID = strings.TrimSpace(remoteSessionID)
	m.mu.RLock()
	localID := m.remoteToLocal[remoteSessionID]
	record, ok := m.sessions[localID]
	m.mu.RUnlock()
	return record, ok
}

func (m *Manager) AttachRemoteSession(remote RemoteSession) (SessionRecord, error) {
	remoteID := strings.TrimSpace(remote.RemoteSessionID)
	if remoteID == "" {
		return SessionRecord{}, newError("ACP_SESSION_ID_INVALID", "ACP remote session id is required", false, nil, nil)
	}
	if existing, ok := m.ManagedSessionForRemote(remoteID); ok {
		return existing, nil
	}
	resolved, err := m.resolveCWD(remote.CWD)
	if err != nil {
		return SessionRecord{}, err
	}
	additional, err := m.resolveAdditionalDirectories(remote.AdditionalDirectories, resolved)
	if err != nil {
		return SessionRecord{}, err
	}
	id, err := newID("acps")
	if err != nil {
		return SessionRecord{}, err
	}
	now := time.Now().UTC()
	record := SessionRecord{
		SchemaVersion:         sessionSchemaVersion,
		ID:                    id,
		Agent:                 m.opts.Agent.Name,
		RemoteSessionID:       remoteID,
		CWD:                   resolved,
		AdditionalDirectories: additional,
		Status:                SessionClosed,
		CreatedAt:             now,
		UpdatedAt:             now,
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return SessionRecord{}, newError("ACP_MANAGER_CLOSED", "ACP manager is closed", false, nil, nil)
	}
	if existingID := m.remoteToLocal[remoteID]; existingID != "" {
		return m.sessions[existingID], nil
	}
	if err := m.store.Save(record); err != nil {
		return SessionRecord{}, err
	}
	m.sessions[id] = record
	m.remoteToLocal[remoteID] = id
	return record, nil
}

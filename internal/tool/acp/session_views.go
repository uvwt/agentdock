package acp

import (
	"time"

	acpruntime "github.com/uvwt/agentdock/internal/acp"
)

type sessionListItem struct {
	ID                    string                   `json:"id"`
	Source                string                   `json:"source"`
	SessionID             string                   `json:"session_id,omitempty"`
	RemoteSessionID       string                   `json:"remote_session_id"`
	Managed               bool                     `json:"managed"`
	RemoteListed          bool                     `json:"remote_listed"`
	Agent                 string                   `json:"agent,omitempty"`
	Status                acpruntime.SessionStatus `json:"status,omitempty"`
	CWD                   string                   `json:"cwd,omitempty"`
	AdditionalDirectories []string                 `json:"additional_directories,omitempty"`
	Title                 string                   `json:"title,omitempty"`
	UpdatedAt             string                   `json:"updated_at,omitempty"`
}

func mergeSessionList(profileID string, managed []acpruntime.SessionRecord, remote []acpruntime.RemoteSession) []sessionListItem {
	items := make([]sessionListItem, 0, len(managed)+len(remote))
	byRemote := make(map[string]int, len(managed))
	for _, session := range managed {
		item := sessionListItem{
			ID: session.ID, Source: "managed", SessionID: session.ID,
			RemoteSessionID: session.RemoteSessionID, Managed: true,
			Agent: session.Agent, Status: session.Status, CWD: session.CWD,
			AdditionalDirectories: append([]string(nil), session.AdditionalDirectories...),
		}
		if !session.UpdatedAt.IsZero() {
			item.UpdatedAt = session.UpdatedAt.UTC().Format(time.RFC3339Nano)
		}
		byRemote[session.RemoteSessionID] = len(items)
		items = append(items, item)
	}
	for _, session := range remote {
		if index, exists := byRemote[session.RemoteSessionID]; exists {
			items[index].RemoteListed = true
			if items[index].Title == "" {
				items[index].Title = session.Title
			}
			continue
		}
		items = append(items, sessionListItem{
			ID: session.RemoteSessionID, Source: "remote",
			RemoteSessionID: session.RemoteSessionID, Managed: false, RemoteListed: true,
			Agent: profileID, CWD: session.CWD,
			AdditionalDirectories: append([]string(nil), session.AdditionalDirectories...),
			Title:                 session.Title, UpdatedAt: session.UpdatedAt,
		})
	}
	return items
}

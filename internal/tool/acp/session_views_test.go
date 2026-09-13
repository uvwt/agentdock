package acp

import (
	"testing"
	"time"

	acpruntime "github.com/uvwt/agentdock/internal/acp"
)

func TestMergeSessionListDeduplicatesManagedRemoteIdentity(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	managed := []acpruntime.SessionRecord{
		{
			ID: "acps_1", Agent: "zcode", RemoteSessionID: "sess_1",
			CWD: "/workspace", Status: acpruntime.SessionReady, UpdatedAt: now,
		},
	}
	remote := []acpruntime.RemoteSession{
		{RemoteSessionID: "sess_1", CWD: "/workspace", Title: "Managed native", Managed: true, SessionID: "acps_1"},
		{RemoteSessionID: "sess_2", CWD: "/workspace", Title: "Native only"},
	}

	items := mergeSessionList("zcode", managed, remote)
	if len(items) != 2 {
		t.Fatalf("merged session rows = %d, want 2: %#v", len(items), items)
	}
	first := items[0]
	if first.ID != "acps_1" || first.Source != "managed" || !first.Managed || !first.RemoteListed || first.SessionID != "acps_1" || first.RemoteSessionID != "sess_1" || first.Title != "Managed native" {
		t.Fatalf("managed row = %#v", first)
	}
	second := items[1]
	if second.ID != "sess_2" || second.Source != "remote" || second.Managed || !second.RemoteListed || second.SessionID != "" || second.RemoteSessionID != "sess_2" || second.Agent != "zcode" {
		t.Fatalf("remote row = %#v", second)
	}
}

func TestMergeSessionListKeepsManagedRowsWhenRemoteListingUnavailable(t *testing.T) {
	managed := []acpruntime.SessionRecord{{
		ID: "acps_1", Agent: "claude", RemoteSessionID: "sess_1", CWD: "/workspace", Status: acpruntime.SessionClosed,
	}}
	items := mergeSessionList("claude", managed, nil)
	if len(items) != 1 || items[0].ID != "acps_1" || items[0].Source != "managed" || !items[0].Managed || items[0].RemoteListed {
		t.Fatalf("managed-only list = %#v", items)
	}
}

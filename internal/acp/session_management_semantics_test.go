package acp

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestRemoteSessionListPaginationOpenAndReopen(t *testing.T) {
	workspace := t.TempDir()
	manager, err := newTestManagerWithPromptMode(t.TempDir(), workspace, "helper-acp", "session_list_pagination")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.Close() }()

	first, err := manager.ListRemoteSessions(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Sessions) != 1 || first.Sessions[0].RemoteSessionID != "native-1" || first.NextCursor != "page-2" {
		t.Fatalf("first remote page = %#v", first)
	}
	second, err := manager.ListRemoteSessions(context.Background(), "", first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Sessions) != 1 || second.Sessions[0].RemoteSessionID != "native-2" || second.NextCursor != "" {
		t.Fatalf("second remote page = %#v", second)
	}

	opened, attached, err := manager.OpenSession(context.Background(), "", "native-2")
	if err != nil {
		t.Fatal(err)
	}
	if !attached || opened.Session.ID == "" || opened.Session.RemoteSessionID != "native-2" || opened.Session.Status != SessionReady {
		t.Fatalf("opened native session = %#v attached=%v", opened.Session, attached)
	}
	localID := opened.Session.ID

	second, err = manager.ListRemoteSessions(context.Background(), "", "page-2")
	if err != nil {
		t.Fatal(err)
	}
	if !second.Sessions[0].Managed || second.Sessions[0].SessionID != localID {
		t.Fatalf("managed remote session = %#v", second.Sessions[0])
	}

	reopenedRemote, attachedAgain, err := manager.OpenSession(context.Background(), "", "native-2")
	if err != nil {
		t.Fatal(err)
	}
	if attachedAgain || reopenedRemote.Session.ID != localID {
		t.Fatalf("second remote open created another mapping: %#v attached=%v", reopenedRemote.Session, attachedAgain)
	}

	closed, err := manager.CloseSession(context.Background(), localID)
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != SessionClosed || closed.ClosedAt == nil {
		t.Fatalf("closed session = %#v", closed)
	}

	reopenedLocal, attachedLocal, err := manager.OpenSession(context.Background(), localID, "")
	if err != nil {
		t.Fatal(err)
	}
	if attachedLocal || reopenedLocal.Session.ID != localID || reopenedLocal.Session.Status != SessionReady || reopenedLocal.Session.ClosedAt != nil {
		t.Fatalf("reopened managed session = %#v attached=%v", reopenedLocal.Session, attachedLocal)
	}
}

func TestRemoteSessionIdentityIsIsolatedByProfileManager(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	workspace := t.TempDir()
	zcode, err := NewManager(Options{Home: home, DefaultCWD: workspace, Agent: AgentSpec{Name: "zcode", Command: executable}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zcode.Close() }()
	agy, err := NewManager(Options{Home: home, DefaultCWD: workspace, Agent: AgentSpec{Name: "agy", Command: executable}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = agy.Close() }()

	remote := RemoteSession{RemoteSessionID: "shared-native-id", CWD: workspace}
	zcodeRecord, err := zcode.AttachRemoteSession(remote)
	if err != nil {
		t.Fatal(err)
	}
	agyRecord, err := agy.AttachRemoteSession(remote)
	if err != nil {
		t.Fatal(err)
	}
	if zcodeRecord.ID == agyRecord.ID || zcodeRecord.Agent != "zcode" || agyRecord.Agent != "agy" {
		t.Fatalf("cross-profile mappings collided: zcode=%#v agy=%#v", zcodeRecord, agyRecord)
	}
	if got, ok := zcode.ManagedSessionForRemote(remote.RemoteSessionID); !ok || got.ID != zcodeRecord.ID {
		t.Fatalf("zcode remote mapping = %#v ok=%v", got, ok)
	}
	if got, ok := agy.ManagedSessionForRemote(remote.RemoteSessionID); !ok || got.ID != agyRecord.ID {
		t.Fatalf("agy remote mapping = %#v ok=%v", got, ok)
	}
}

func TestSessionProjectionUpdatesWithoutActiveRun(t *testing.T) {
	workspace := t.TempDir()
	manager, err := newTestManager(t.TempDir(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.Close() }()

	created, err := manager.NewSession(context.Background(), workspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	remoteID := created.Session.RemoteSessionID

	modeParams, _ := json.Marshal(map[string]any{
		"sessionId": remoteID,
		"update":    map[string]any{"sessionUpdate": "current_mode_update", "currentModeId": "review"},
	})
	manager.handleNotification("session/update", modeParams)

	configParams, _ := json.Marshal(map[string]any{
		"sessionId": remoteID,
		"update": map[string]any{
			"sessionUpdate": "config_option_update",
			"configOptions": []map[string]any{{"id": "safe", "type": "boolean", "currentValue": false}},
		},
	})
	manager.handleNotification("session/update", configParams)

	record, err := manager.InspectSession(created.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.ModeID != "review" {
		t.Fatalf("persisted mode id = %q", record.ModeID)
	}
	projection, err := manager.SessionProjection(created.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if projection.ModeID != "review" || projection.ConfigOptions == nil {
		t.Fatalf("session projection = %#v", projection)
	}
}

func TestPromptContentCapabilitiesAndRequiredFields(t *testing.T) {
	process := &agentProcess{initialize: InitializeResult{AgentCapabilities: map[string]any{
		"promptCapabilities": map[string]any{"image": true, "audio": false, "embeddedContext": true},
	}}}

	valid := []ContentBlock{
		TextBlock("hello"),
		{"type": "resource_link", "name": "guide", "uri": "file:///tmp/guide.md"},
		{"type": "image", "data": "AA==", "mimeType": "image/png"},
		{"type": "resource", "resource": map[string]any{"uri": "file:///tmp/context.txt", "text": "context"}},
	}
	if _, err := validatePromptBlocks(process, valid); err != nil {
		t.Fatalf("valid content blocks rejected: %v", err)
	}

	if _, err := validatePromptBlocks(process, []ContentBlock{{"type": "audio", "data": "AA==", "mimeType": "audio/wav"}}); errorCode(err) != "ACP_CAPABILITY_UNSUPPORTED" {
		t.Fatalf("audio capability error = %#v", err)
	}
	if _, err := validatePromptBlocks(process, []ContentBlock{{"type": "image", "mimeType": "image/png"}}); errorCode(err) != "ACP_PROMPT_INVALID" {
		t.Fatalf("missing image data error = %#v", err)
	}
	if _, err := validatePromptBlocks(process, []ContentBlock{{"type": "resource_link", "uri": "file:///tmp/a"}}); errorCode(err) != "ACP_PROMPT_INVALID" {
		t.Fatalf("missing resource name error = %#v", err)
	}
}

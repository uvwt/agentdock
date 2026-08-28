package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestManagerPromptEventsPermissionAndPersistence(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	manager, err := newTestManager(home, workspace)
	if err != nil {
		t.Fatal(err)
	}

	created, err := manager.NewSession(context.Background(), workspace, nil)
	if err != nil {
		_ = manager.Close()
		t.Fatal(err)
	}
	if created.Session.ID == "" || created.Session.RemoteSessionID != "remote-1" {
		_ = manager.Close()
		t.Fatalf("unexpected session: %#v", created)
	}

	started, err := manager.StartPrompt(context.Background(), created.Session.ID, "exercise permission")
	if err != nil {
		_ = manager.Close()
		t.Fatal(err)
	}
	if started.Status != RunRunning {
		_ = manager.Close()
		t.Fatalf("start status = %s", started.Status)
	}

	var allEvents []Event
	var permission Interaction
	after := uint64(0)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		events, err := manager.PromptEvents(context.Background(), started.RunID, after, 100, 250*time.Millisecond)
		if err != nil {
			_ = manager.Close()
			t.Fatal(err)
		}
		allEvents = append(allEvents, events.Events...)
		if len(events.Events) > 0 {
			after = events.Events[len(events.Events)-1].Seq
		}
		interactions := manager.ListInteractions(created.Session.ID, true)
		if len(interactions) > 0 {
			permission = interactions[0]
			break
		}
	}
	if permission.ID == "" {
		_ = manager.Close()
		t.Fatalf("permission interaction was not emitted; events=%#v", allEvents)
	}
	if len(permission.Options) != 1 || permission.Options[0].OptionID != "allow-once" {
		_ = manager.Close()
		t.Fatalf("policy did not filter always option: %#v", permission.Options)
	}
	runningMessages, err := manager.SessionMessages(created.Session.ID)
	if err != nil {
		_ = manager.Close()
		t.Fatal(err)
	}
	if len(runningMessages) != 1 || runningMessages[0].Role != "user" || runningMessages[0].Content != "exercise permission" {
		_ = manager.Close()
		t.Fatalf("running conversation messages = %#v", runningMessages)
	}
	if _, err := manager.RespondInteraction(permission.ID, "allow-always", false); err == nil {
		_ = manager.Close()
		t.Fatal("always option was accepted")
	}
	if _, err := manager.RespondInteraction(permission.ID, "allow-once", false); err != nil {
		_ = manager.Close()
		t.Fatal(err)
	}

	completed := false
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		events, err := manager.PromptEvents(context.Background(), started.RunID, after, 100, 250*time.Millisecond)
		if err != nil {
			_ = manager.Close()
			t.Fatal(err)
		}
		allEvents = append(allEvents, events.Events...)
		if len(events.Events) > 0 {
			after = events.Events[len(events.Events)-1].Seq
		}
		if events.Status == RunCompleted {
			completed = true
			break
		}
	}
	if !completed {
		_ = manager.Close()
		t.Fatalf("prompt did not complete; events=%#v", allEvents)
	}
	assertEventTypes(t, allEvents, "agent_message_chunk", "permission_request", "completed")
	messages, err := manager.SessionMessages(created.Session.ID)
	if err != nil {
		_ = manager.Close()
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].Role != "user" || messages[0].Content != "exercise permission" || messages[1].Role != "assistant" || messages[1].Content != "working" {
		_ = manager.Close()
		t.Fatalf("conversation messages = %#v", messages)
	}

	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := newTestManager(home, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reloaded.Close() }()
	sessions, err := reloaded.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != created.Session.ID {
		t.Fatalf("persisted sessions = %#v", sessions)
	}
	loaded, err := reloaded.LoadSession(context.Background(), created.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Session.Status != SessionReady {
		t.Fatalf("loaded status = %s", loaded.Session.Status)
	}
	reloadedMessages, err := reloaded.SessionMessages(created.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloadedMessages) != 0 {
		t.Fatalf("conversation unexpectedly persisted = %#v", reloadedMessages)
	}
}

func TestSetSessionConfigOptionRejectsMissingConfigOptions(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	manager, err := newTestManagerWithPromptMode(home, workspace, "helper-acp", "omit_config_options")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.Close() }()

	created, err := manager.NewSession(context.Background(), workspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.SetSessionConfigOption(context.Background(), created.Session.ID, "safe", false)
	var acpErr *Error
	if !errors.As(err, &acpErr) || acpErr.Code != "ACP_INVALID_RESPONSE" {
		t.Fatalf("missing configOptions error = %#v, want ACP_INVALID_RESPONSE", err)
	}
}

func TestManagerSessionLifecycleCapabilities(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	additional := filepath.Join(workspace, "secondary")
	if err := os.MkdirAll(additional, 0o700); err != nil {
		t.Fatal(err)
	}
	manager, err := newTestManager(home, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.Close() }()

	if err := manager.Authenticate(context.Background(), "missing-auth"); err == nil {
		t.Fatal("unadvertised authentication method was accepted")
	}
	if err := manager.Authenticate(context.Background(), "test-auth"); err != nil {
		t.Fatal(err)
	}
	created, err := manager.NewSession(context.Background(), workspace, []string{additional})
	if err != nil {
		t.Fatal(err)
	}
	expectedAdditional := canonicalTestPath(t, additional)
	if len(created.Session.AdditionalDirectories) != 1 || created.Session.AdditionalDirectories[0] != expectedAdditional {
		t.Fatalf("additional directories = %#v", created.Session.AdditionalDirectories)
	}
	loadedOnce, err := manager.LoadSession(context.Background(), created.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	loadedTwice, err := manager.LoadSession(context.Background(), created.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sessionModeID(t, loadedOnce.Modes) != "code" || sessionModeID(t, loadedTwice.Modes) != "code" {
		t.Fatalf("repeated load lost mode state: first=%#v second=%#v", loadedOnce.Modes, loadedTwice.Modes)
	}
	if sessionConfigValue(t, loadedOnce.ConfigOptions, "safe") != true || sessionConfigValue(t, loadedTwice.ConfigOptions, "safe") != true {
		t.Fatalf("repeated load lost config state: first=%#v second=%#v", loadedOnce.ConfigOptions, loadedTwice.ConfigOptions)
	}
	if err := manager.SetSessionMode(context.Background(), created.Session.ID, "review"); err != nil {
		t.Fatal(err)
	}
	options, err := manager.SetSessionConfigOption(context.Background(), created.Session.ID, "safe", false)
	if err != nil {
		t.Fatal(err)
	}
	if options == nil {
		t.Fatal("set_config_option omitted config options")
	}
	cached, err := manager.LoadSession(context.Background(), created.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sessionModeID(t, cached.Modes) != "review" || sessionConfigValue(t, cached.ConfigOptions, "safe") != false {
		t.Fatalf("cached lifecycle state was stale: modes=%#v config=%#v", cached.Modes, cached.ConfigOptions)
	}
	steering, err := manager.Steer(context.Background(), created.Session.ID, "adjust")
	if err != nil {
		t.Fatal(err)
	}
	if steering["outcome"] != "injected" {
		t.Fatalf("steering outcome = %#v", steering)
	}
	forked, err := manager.ForkSession(context.Background(), created.Session.ID, "", []string{})
	if err != nil {
		t.Fatal(err)
	}
	if forked.Session.RemoteSessionID == created.Session.RemoteSessionID || len(forked.Session.AdditionalDirectories) != 0 {
		t.Fatalf("forked session = %#v", forked.Session)
	}
	resumed, err := manager.ResumeSession(context.Background(), created.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Session.Status != SessionReady {
		t.Fatalf("resumed status = %s", resumed.Session.Status)
	}
	closed, err := manager.CloseSession(context.Background(), forked.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != SessionClosed {
		t.Fatalf("closed status = %s", closed.Status)
	}
	if err := manager.DeleteSession(context.Background(), forked.Session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.InspectSession(forked.Session.ID); err == nil {
		t.Fatal("deleted session remained inspectable")
	}
}

func TestCapabilityInspectionUsesInitializeContract(t *testing.T) {
	process := &agentProcess{initialize: InitializeResult{
		AgentCapabilities: map[string]any{
			"loadSession":         true,
			"sessionCapabilities": map[string]any{"resume": map[string]any{}},
		},
		Meta: map[string]any{"steering": map[string]any{"supported": true}},
	}}
	if !process.supportsLoadSession() || !process.supportsSessionCapability("resume") || process.supportsSessionCapability("fork") || !process.supportsSteering() {
		t.Fatalf("capability inspection failed: %#v", process.initialize)
	}
}

func TestManagerRecoveryMarksRunningSessionInterrupted(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	store, err := newSessionStore(home, "helper")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	record := SessionRecord{
		SchemaVersion: sessionSchemaVersion,
		ID:            "acps_recovery", Agent: "helper", RemoteSessionID: "remote-recovery", CWD: workspace,
		Status: SessionRunning, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.Save(record); err != nil {
		t.Fatal(err)
	}
	manager, err := newTestManager(home, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.Close() }()
	loaded, err := manager.InspectSession(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != SessionInterrupted || loaded.LastStopReason != "agentdock_restart" {
		t.Fatalf("recovered session = %#v", loaded)
	}
}

func TestManagerCloseMarksActivePromptInterrupted(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	manager, err := newTestManager(home, workspace)
	if err != nil {
		t.Fatal(err)
	}
	created, err := manager.NewSession(context.Background(), workspace, nil)
	if err != nil {
		_ = manager.Close()
		t.Fatal(err)
	}
	started, err := manager.StartPrompt(context.Background(), created.Session.ID, "block for shutdown")
	if err != nil {
		_ = manager.Close()
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(manager.ListInteractions(created.Session.ID, true)) > 0 {
			break
		}
		if _, err := manager.PromptEvents(context.Background(), started.RunID, 0, 10, 50*time.Millisecond); err != nil {
			_ = manager.Close()
			t.Fatal(err)
		}
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := newTestManager(home, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reloaded.Close() }()
	record, err := reloaded.InspectSession(created.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != SessionInterrupted || record.LastStopReason != "agentdock_shutdown" {
		t.Fatalf("shutdown record = %#v", record)
	}
}

func TestResolveCWDAllowsAccessibleDirectoriesAndCanonicalizesSymlinks(t *testing.T) {
	defaultCWD := t.TempDir()
	inside := filepath.Join(defaultCWD, "inside")
	if err := os.MkdirAll(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	manager, err := newTestManager(t.TempDir(), defaultCWD)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.Close() }()

	resolved, err := manager.resolveCWD("inside")
	expectedInside := canonicalTestPath(t, inside)
	if err != nil || resolved != expectedInside {
		t.Fatalf("relative cwd = %q, %v", resolved, err)
	}
	resolved, err = manager.resolveCWD(outside)
	if err != nil || resolved != canonicalTestPath(t, outside) {
		t.Fatalf("absolute cwd outside default directory = %q, %v", resolved, err)
	}

	alias := filepath.Join(defaultCWD, "inside-link")
	if err := os.Symlink(inside, alias); err != nil {
		t.Logf("symlink tests skipped on this host: %v", err)
		return
	}
	additional, err := manager.resolveAdditionalDirectories([]string{inside, alias}, defaultCWD)
	if err != nil {
		t.Fatal(err)
	}
	if len(additional) != 1 || additional[0] != expectedInside {
		t.Fatalf("same-file additional directories were not deduplicated: %#v", additional)
	}

	outsideLink := filepath.Join(defaultCWD, "outside-link")
	if err := os.Symlink(outside, outsideLink); err != nil {
		t.Logf("symlink test skipped on this host: %v", err)
		return
	}
	resolved, err = manager.resolveCWD(outsideLink)
	if err != nil || resolved != canonicalTestPath(t, outside) {
		t.Fatalf("symlink to accessible directory = %q, %v", resolved, err)
	}
}

func TestConnectionCancelsInboundRequest(t *testing.T) {
	agentReader, agentWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	clientReader, clientWriter, err := os.Pipe()
	if err != nil {
		_ = agentReader.Close()
		_ = agentWriter.Close()
		t.Fatal(err)
	}
	defer func() { _ = agentWriter.Close() }()
	defer func() { _ = clientReader.Close() }()

	started := make(chan struct{})
	cancelled := make(chan struct{})
	connection := NewConnection(agentReader, clientWriter, func(ctx context.Context, _ string, _ json.RawMessage) (any, *rpcError) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return cancelledPermissionOutcome(), nil
	}, nil)
	defer func() { _ = connection.Close() }()

	encoder := json.NewEncoder(agentWriter)
	if err := encoder.Encode(rpcMessage{JSONRPC: "2.0", ID: json.RawMessage("7"), Method: "session/request_permission", Params: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("inbound request did not start")
	}
	if err := encoder.Encode(rpcMessage{JSONRPC: "2.0", Method: "$/cancel_request", Params: json.RawMessage(`{"requestId":7}`)}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("inbound request was not cancelled")
	}
}

func TestCancelThenDeleteDoesNotRecreateSessionState(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	manager, err := newTestManager(home, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.Close() }()
	created, err := manager.NewSession(context.Background(), workspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := manager.NewSession(context.Background(), workspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	started, err := manager.StartPrompt(context.Background(), created.Session.ID, "wait for permission")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(manager.ListInteractions(created.Session.ID, true)) > 0 {
			break
		}
		_, _ = manager.PromptEvents(context.Background(), started.RunID, 0, 10, 50*time.Millisecond)
	}
	if err := manager.CancelPrompt(context.Background(), other.Session.ID, started.RunID); err == nil {
		t.Fatal("cancel accepted a run from another session")
	}
	if err := manager.CancelPrompt(context.Background(), created.Session.ID, started.RunID); err != nil {
		t.Fatal(err)
	}
	events, err := manager.PromptEvents(context.Background(), started.RunID, 0, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if events.Status != RunCancelled {
		t.Fatalf("cancelled run status = %s", events.Status)
	}

	manager.mu.Lock()
	if manager.process == nil {
		manager.mu.Unlock()
		t.Fatal("helper process is not initialized")
	}
	sessionCapabilities := manager.process.initialize.AgentCapabilities["sessionCapabilities"].(map[string]any)
	delete(sessionCapabilities, "delete")
	manager.mu.Unlock()
	if err := manager.DeleteSession(context.Background(), created.Session.ID); err != nil {
		t.Fatal(err)
	}
	deleteDeadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := manager.store.Get(created.Session.ID); err != nil {
			break
		}
		if time.Now().After(deleteDeadline) {
			t.Fatal("cancelled prompt recreated deleted session state")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRunEventRingReportsTruncation(t *testing.T) {
	run := newRun("acpr_ring", "acps_ring")
	for index := 0; index < maxEventCount+10; index++ {
		run.appendEvent(Event{Type: "chunk", Message: strconv.Itoa(index)})
	}
	page, err := run.eventsAfter(0, 200)
	if err != nil {
		t.Fatal(err)
	}
	if !page.Truncated || page.FirstSeq <= 1 || page.NextSeq != page.FirstSeq+199 || len(page.Events) != 200 ||
		!page.HasMore || page.LatestSeq != maxEventCount+10 || page.DroppedCount != 10 {
		t.Fatalf("event ring = %#v", page)
	}
	second, err := run.eventsAfter(page.NextSeq, 200)
	if err != nil {
		t.Fatal(err)
	}
	if second.Truncated || len(second.Events) != 200 || second.Events[0].Seq != page.NextSeq+1 ||
		second.NextSeq != second.Events[len(second.Events)-1].Seq || !second.HasMore {
		t.Fatalf("second event page = %#v", second)
	}
	if _, err := run.eventsAfter(page.LatestSeq+1, 10); errorCode(err) != "ACP_CURSOR_AHEAD" {
		t.Fatalf("cursor-ahead error = %v", err)
	}
}

func TestRunBoundsOversizedEventUpdate(t *testing.T) {
	run := newRun("acpr_large", "acps_large")
	update, err := json.Marshal(map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"content":       map[string]any{"type": "text", "text": strings.Repeat("x", maxEventUpdateBytes+1024)},
	})
	if err != nil {
		t.Fatal(err)
	}
	run.appendEvent(Event{Type: "agent_message_chunk", Update: update})
	page, err := run.eventsAfter(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 {
		t.Fatalf("events = %d", len(page.Events))
	}
	event := page.Events[0]
	if !event.UpdateTruncated || event.OriginalUpdateBytes != len(update) || len(event.Update) >= maxEventUpdateBytes || !json.Valid(event.Update) {
		t.Fatalf("bounded event = truncated %v original %d retained %d valid %v", event.UpdateTruncated, event.OriginalUpdateBytes, len(event.Update), json.Valid(event.Update))
	}
	if !strings.Contains(string(event.Update), "agent_message_chunk") {
		t.Fatalf("bounded update lost event type: %s", event.Update)
	}
}

func TestPromptEventWaitIgnoresStaleWakeup(t *testing.T) {
	run := newRun("acpr_wait", "acps_wait")
	run.appendEvent(Event{Type: "first"})
	manager := &Manager{closedCh: make(chan struct{})}
	go func() {
		time.Sleep(25 * time.Millisecond)
		run.appendEvent(Event{Type: "second"})
	}()
	if err := manager.waitForPromptEvents(context.Background(), run, 1, time.Second); err != nil {
		t.Fatal(err)
	}
	page, err := run.eventsAfter(1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].Type != "second" || page.NextSeq != 2 {
		t.Fatalf("wait page = %#v", page)
	}
}

func TestRedactEnvironmentValues(t *testing.T) {
	redacted := redactEnvironmentValues("failed with secret-token and abc", map[string]string{"TOKEN": "secret-token", "SHORT": "abc"})
	if strings.Contains(redacted, "secret-token") || !strings.Contains(redacted, "[REDACTED]") || !strings.Contains(redacted, "abc") {
		t.Fatalf("redacted text = %q", redacted)
	}
}

func TestConnectionRejectsUnencodablePayloadWithoutPanic(t *testing.T) {
	reader, writer, cleanup := pipeConnectionPair(t)
	defer cleanup()
	connection := NewConnection(reader, writer, nil, nil)
	defer func() { _ = connection.Close() }()
	err := connection.Notify("test", map[string]any{"value": math.NaN()})
	var acpErr *Error
	if !errors.As(err, &acpErr) || acpErr.Code != "ACP_PROTOCOL_ERROR" {
		t.Fatalf("unencodable payload error = %#v", err)
	}
}

func TestConnectionRejectsOversizedMessage(t *testing.T) {
	reader, writer, cleanup := pipeConnectionPair(t)
	defer cleanup()
	connection := NewConnection(reader, writer, nil, nil)
	defer func() { _ = connection.Close() }()
	err := connection.Notify("test", map[string]any{"value": strings.Repeat("x", maxRPCLineBytes)})
	var acpErr *Error
	if !errors.As(err, &acpErr) || acpErr.Code != "ACP_MESSAGE_TOO_LARGE" {
		t.Fatalf("oversized error = %#v", err)
	}
}

func newTestManager(home, workspace string) (*Manager, error) {
	return newTestManagerWithPromptMode(home, workspace, "helper-acp", "permission")
}

func newTestManagerWithPromptMode(home, workspace, agentInfoName, promptMode string) (*Manager, error) {
	return newTestManagerWithAgent(home, workspace, agentInfoName, "1.0.0", promptMode)
}

func newTestManagerWithAgent(home, workspace, agentInfoName, agentVersion, promptMode string) (*Manager, error) {
	return NewManager(Options{
		Home:       home,
		DefaultCWD: workspace,
		Agent: AgentSpec{
			Name: "helper", Command: os.Args[0], Args: []string{"-test.run=TestACPHelperProcess"}, Environment: map[string]string{
				"GO_WANT_ACP_HELPER": "1", "GO_ACP_HELPER_AGENT_INFO_NAME": agentInfoName,
				"GO_ACP_HELPER_AGENT_VERSION": agentVersion, "GO_ACP_HELPER_PROMPT_MODE": promptMode,
			},
		},
		MaxConcurrentRuns: 2, InteractionTimeout: 3 * time.Second,
	})
}

func assertEventTypes(t *testing.T, events []Event, expected ...string) {
	t.Helper()
	seen := map[string]bool{}
	for _, event := range events {
		seen[event.Type] = true
	}
	for _, eventType := range expected {
		if !seen[eventType] {
			t.Fatalf("event type %q missing from %#v", eventType, events)
		}
	}
}

func canonicalTestPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		t.Fatalf("resolve test path %s: %v", path, err)
	}
	return resolved
}

func sessionModeID(t *testing.T, value any) string {
	t.Helper()
	modes, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("modes type = %T", value)
	}
	modeID, _ := modes["currentModeId"].(string)
	return modeID
}

func sessionConfigValue(t *testing.T, value any, id string) any {
	t.Helper()
	if options, ok := value.([]any); ok {
		for _, raw := range options {
			option, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if optionID, _ := option["id"].(string); optionID == id {
				return option["currentValue"]
			}
		}
	}
	if options, ok := value.([]map[string]any); ok {
		for _, option := range options {
			if optionID, _ := option["id"].(string); optionID == id {
				return option["currentValue"]
			}
		}
	}
	t.Fatalf("config option %q not found in %#v", id, value)
	return nil
}

func pipeConnectionPair(t *testing.T) (*os.File, *os.File, func()) {
	t.Helper()
	reader, peerWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	peerReader, writer, err := os.Pipe()
	if err != nil {
		_ = reader.Close()
		_ = peerWriter.Close()
		t.Fatal(err)
	}
	cleanup := func() {
		// Close peer ends first so blocked Windows pipe I/O observes EOF before
		// either local handle is closed.
		_ = peerWriter.Close()
		_ = peerReader.Close()
		_ = writer.Close()
		_ = reader.Close()
	}
	return reader, writer, cleanup
}

func TestACPHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_ACP_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64<<10), maxRPCLineBytes)
	encoder := json.NewEncoder(os.Stdout)
	remoteCount := 0
	agentInfoName := os.Getenv("GO_ACP_HELPER_AGENT_INFO_NAME")
	if agentInfoName == "" {
		agentInfoName = "helper-acp"
	}
	agentVersion := os.Getenv("GO_ACP_HELPER_AGENT_VERSION")
	if agentVersion == "" {
		agentVersion = "1.0.0"
	}
	promptMode := os.Getenv("GO_ACP_HELPER_PROMPT_MODE")
	promptCount := 0
	for scanner.Scan() {
		var message rpcMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			os.Exit(2)
		}
		switch message.Method {
		case "initialize":
			result := map[string]any{
				"protocolVersion": ProtocolVersion,
				"_meta":           map[string]any{"steering": map[string]any{"supported": true}},
			}
			if os.Getenv("GO_ACP_HELPER_OMIT_AGENT_CAPABILITIES") != "1" {
				result["agentCapabilities"] = map[string]any{
					"loadSession": true,
					"sessionCapabilities": map[string]any{
						"close": map[string]any{}, "delete": map[string]any{}, "resume": map[string]any{},
						"fork": map[string]any{}, "additionalDirectories": map[string]any{},
					},
				}
			}
			if os.Getenv("GO_ACP_HELPER_OMIT_AUTH_METHODS") != "1" {
				result["authMethods"] = []map[string]any{{"id": "test-auth", "name": "Test auth"}}
			}
			if os.Getenv("GO_ACP_HELPER_OMIT_AGENT_INFO") != "1" {
				result["agentInfo"] = map[string]any{"name": agentInfoName, "title": "Helper ACP", "version": agentVersion}
			}
			writeHelperResult(encoder, message.ID, result)
		case "authenticate":
			writeHelperResult(encoder, message.ID, map[string]any{})
		case "session/new":
			remoteCount++
			writeHelperResult(encoder, message.ID, map[string]any{
				"sessionId":     "remote-" + strconv.Itoa(remoteCount),
				"modes":         map[string]any{"currentModeId": "code", "availableModes": []any{}},
				"configOptions": []map[string]any{{"id": "safe", "name": "Safe", "type": "boolean", "currentValue": true}},
			})
		case "session/load", "session/resume":
			if promptMode == "codex_no_rollout" || promptMode == "codex_no_rollout_steer" {
				_ = encoder.Encode(rpcMessage{JSONRPC: "2.0", ID: message.ID, Error: &rpcError{Code: -32603, Message: "Internal error", Data: testMarshalRaw(map[string]any{"details": "no rollout found for thread id remote"})}})
			} else {
				writeHelperResult(encoder, message.ID, map[string]any{
					"modes":         map[string]any{"currentModeId": "code", "availableModes": []any{}},
					"configOptions": []map[string]any{{"id": "safe", "name": "Safe", "type": "boolean", "currentValue": true}},
				})
			}
		case "session/fork":
			remoteCount++
			writeHelperResult(encoder, message.ID, map[string]any{"sessionId": "remote-" + strconv.Itoa(remoteCount)})
		case "session/set_mode":
			writeHelperResult(encoder, message.ID, map[string]any{})
		case "session/set_config_option":
			if promptMode == "omit_config_options" {
				writeHelperResult(encoder, message.ID, map[string]any{})
			} else {
				writeHelperResult(encoder, message.ID, map[string]any{
					"configOptions": []map[string]any{{"id": "safe", "name": "Safe", "type": "boolean", "currentValue": false}},
				})
			}
		case "session/close":
			if promptMode == "steering_reset_failure" {
				_ = encoder.Encode(rpcMessage{JSONRPC: "2.0", ID: message.ID, Error: &rpcError{Code: -32603, Message: "reset failed"}})
			} else {
				writeHelperResult(encoder, message.ID, map[string]any{})
			}
		case "session/delete":
			if promptMode == "codex_no_rollout" {
				_ = encoder.Encode(rpcMessage{JSONRPC: "2.0", ID: message.ID, Error: &rpcError{Code: -32603, Message: "Internal error", Data: testMarshalRaw(map[string]any{"details": "no rollout found for thread id remote"})}})
			} else {
				writeHelperResult(encoder, message.ID, map[string]any{})
			}
		case "_session/steering":
			if promptMode == "steering_prompt_required" {
				writeHelperResult(encoder, message.ID, map[string]any{"outcome": "promptRequired", "reason": "noRunningTurn"})
			} else {
				writeHelperResult(encoder, message.ID, map[string]any{"outcome": "injected"})
			}
		case "session/prompt":
			promptCount++
			handleHelperPrompt(scanner, encoder, message, promptMode, promptCount)
		case "session/cancel", "$/cancel_request":
			// Notifications are accepted; prompt cancellation is exercised by manager context.
		default:
			if len(message.ID) > 0 {
				_ = encoder.Encode(rpcMessage{JSONRPC: "2.0", ID: message.ID, Error: &rpcError{Code: -32601, Message: "Method not found"}})
			}
		}
	}
	os.Exit(0)
}

func handleHelperPrompt(scanner *bufio.Scanner, encoder *json.Encoder, prompt rpcMessage, mode string, promptCount int) {
	var promptParams struct {
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(prompt.Params, &promptParams) != nil || promptParams.SessionID == "" {
		os.Exit(6)
	}
	if mode == "steering_prompt_required" {
		_ = encoder.Encode(rpcMessage{
			JSONRPC: "2.0", Method: "session/update",
			Params: testMarshalRaw(map[string]any{
				"sessionId": promptParams.SessionID,
				"update":    map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "STEERED"}},
			}),
		})
		writeHelperResult(encoder, prompt.ID, map[string]any{"stopReason": "end_turn"})
		return
	}
	if mode == "claude_steer_fallback" || mode == "steering_reset_failure" || mode == "codex_no_rollout_steer" {
		handleSteeringFallbackPrompt(scanner, encoder, prompt, promptParams.SessionID, promptCount)
		return
	}
	if mode == "codex_false_success" || mode == "codex_recovered" {
		handleCodexHelperPrompt(encoder, prompt, promptParams.SessionID, mode)
		return
	}
	_ = encoder.Encode(rpcMessage{
		JSONRPC: "2.0", Method: "session/update",
		Params: testMarshalRaw(map[string]any{
			"sessionId": promptParams.SessionID,
			"update":    map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "working"}},
		}),
	})
	permissionID := json.RawMessage("900")
	_ = encoder.Encode(rpcMessage{
		JSONRPC: "2.0", ID: permissionID, Method: "session/request_permission",
		Params: testMarshalRaw(map[string]any{
			"sessionId": promptParams.SessionID,
			"toolCall":  map[string]any{"toolCallId": "tool-1", "title": "write file", "kind": "edit"},
			"options": []map[string]any{
				{"optionId": "allow-once", "name": "Allow once", "kind": "allow_once"},
				{"optionId": "allow-always", "name": "Always allow", "kind": "allow_always"},
			},
		}),
	})
	for scanner.Scan() {
		var response rpcMessage
		if json.Unmarshal(scanner.Bytes(), &response) != nil {
			os.Exit(3)
		}
		if string(response.ID) != string(permissionID) {
			continue
		}
		var result struct {
			Outcome struct {
				Outcome  string `json:"outcome"`
				OptionID string `json:"optionId"`
			} `json:"outcome"`
		}
		if json.Unmarshal(response.Result, &result) != nil || result.Outcome.Outcome != "selected" || result.Outcome.OptionID != "allow-once" {
			os.Exit(4)
		}
		writeHelperResult(encoder, prompt.ID, map[string]any{"stopReason": "end_turn"})
		return
	}
	os.Exit(5)
}

func handleSteeringFallbackPrompt(scanner *bufio.Scanner, encoder *json.Encoder, prompt rpcMessage, sessionID string, promptCount int) {
	if promptCount == 1 {
		for scanner.Scan() {
			var message rpcMessage
			if json.Unmarshal(scanner.Bytes(), &message) != nil {
				os.Exit(6)
			}
			if message.Method == "session/cancel" || message.Method == "$/cancel_request" {
				return
			}
		}
		os.Exit(7)
	}
	_ = encoder.Encode(rpcMessage{
		JSONRPC: "2.0", Method: "session/update",
		Params: testMarshalRaw(map[string]any{
			"sessionId": sessionID,
			"update": map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]any{"type": "text", "text": "STEERED"},
			},
		}),
	})
	writeHelperResult(encoder, prompt.ID, map[string]any{"stopReason": "end_turn"})
}

func handleCodexHelperPrompt(encoder *json.Encoder, prompt rpcMessage, sessionID, mode string) {
	remoteMessage := "unexpected status 424 Failed Dependency: Service temporarily unavailable"
	_ = encoder.Encode(rpcMessage{
		JSONRPC: "2.0", Method: "session/update",
		Params: testMarshalRaw(map[string]any{
			"sessionId": sessionID,
			"update": map[string]any{
				"sessionUpdate": "session_info_update",
				"_meta": map[string]any{"codex": map[string]any{"error": map[string]any{
					"message": "Reconnecting... 1/5", "additionalDetails": remoteMessage, "willRetry": true,
				}}},
			},
		}),
	})
	text := remoteMessage
	if mode == "codex_recovered" {
		text = "recovered successfully"
	}
	_ = encoder.Encode(rpcMessage{
		JSONRPC: "2.0", Method: "session/update",
		Params: testMarshalRaw(map[string]any{
			"sessionId": sessionID,
			"update":    map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": text}},
		}),
	})
	writeHelperResult(encoder, prompt.ID, map[string]any{"stopReason": "end_turn"})
}

func writeHelperResult(encoder *json.Encoder, id json.RawMessage, result any) {
	_ = encoder.Encode(rpcMessage{JSONRPC: "2.0", ID: id, Result: testMarshalRaw(result)})
}

func testMarshalRaw(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}

func TestSessionModePersistsAndIsRestoredAfterManagerRestart(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	manager, err := newTestManagerWithAgent(home, workspace, "helper-acp", "1.0.0", "permission")
	if err != nil {
		t.Fatal(err)
	}
	created, err := manager.NewSession(context.Background(), workspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetSessionMode(context.Background(), created.Session.ID, "review"); err != nil {
		t.Fatal(err)
	}
	stored, err := manager.InspectSession(created.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ModeID != "review" {
		t.Fatalf("stored mode = %q, want review", stored.ModeID)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := newTestManagerWithAgent(home, workspace, "helper-acp", "1.0.0", "permission")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.Close() }()
	resumed, err := restarted.ResumeSession(context.Background(), created.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := sessionModeID(t, resumed.Modes); got != "review" {
		t.Fatalf("resumed mode = %q, want review", got)
	}
	if resumed.Session.ModeID != "review" {
		t.Fatalf("resumed stored mode = %q, want review", resumed.Session.ModeID)
	}
}

func TestApplyModeToLifecycleStateKeepsModeViewsConsistent(t *testing.T) {
	state := sessionLifecycleResponse{
		Modes: map[string]any{"currentModeId": "agent", "availableModes": []any{}},
		ConfigOptions: []any{
			map[string]any{"id": "mode", "currentValue": "agent"},
			map[string]any{"id": "model", "currentValue": "test"},
		},
	}
	applyModeToLifecycleState(&state, "read-only")
	if got := sessionModeID(t, state.Modes); got != "read-only" {
		t.Fatalf("mode view = %q", got)
	}
	options := state.ConfigOptions.([]any)
	if got := options[0].(map[string]any)["currentValue"]; got != "read-only" {
		t.Fatalf("mode config option = %#v", got)
	}
	if got := options[1].(map[string]any)["currentValue"]; got != "test" {
		t.Fatalf("unrelated config option changed = %#v", got)
	}
}

func TestCodexNoRolloutDeleteIsTreatedAsAlreadyDeleted(t *testing.T) {
	workspace := t.TempDir()
	manager, err := newTestManagerWithAgent(t.TempDir(), workspace, codexACPName, "1.1.9", "codex_no_rollout")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.Close() }()
	created, err := manager.NewSession(context.Background(), workspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.DeleteSession(context.Background(), created.Session.ID); err != nil {
		t.Fatalf("delete no-rollout session: %v", err)
	}
	if _, err := manager.InspectSession(created.Session.ID); errorCode(err) != "ACP_SESSION_NOT_FOUND" {
		t.Fatalf("deleted session inspect error = %#v", err)
	}
}

func TestCodexNoRolloutResumeAfterRestartReturnsExplicitError(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	manager, err := newTestManagerWithAgent(home, workspace, codexACPName, "1.1.9", "codex_no_rollout")
	if err != nil {
		t.Fatal(err)
	}
	created, err := manager.NewSession(context.Background(), workspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := newTestManagerWithAgent(home, workspace, codexACPName, "1.1.9", "codex_no_rollout")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.Close() }()
	if _, err := restarted.ResumeSession(context.Background(), created.Session.ID); errorCode(err) != "ACP_SESSION_NOT_PERSISTED" {
		t.Fatalf("resume no-rollout error = %#v", err)
	}
}

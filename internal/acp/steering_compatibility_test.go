package acp

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"
)

type gatedACPWriter struct {
	io.WriteCloser
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *gatedACPWriter) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.release
	return w.WriteCloser.Write(data)
}

func TestStartPromptWaitsForPromptDispatchBeforeSteering(t *testing.T) {
	workspace := t.TempDir()
	manager, err := newTestManagerWithAgent(t.TempDir(), workspace, claudeAgentACPName, "0.64.2", "claude_steer_fallback")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.Close() }()

	created, err := manager.NewSession(context.Background(), workspace, nil)
	if err != nil {
		t.Fatal(err)
	}

	manager.mu.RLock()
	connection := manager.process.connection
	manager.mu.RUnlock()
	connection.writeMu.Lock()
	gate := &gatedACPWriter{
		WriteCloser: connection.writer,
		started:     make(chan struct{}),
		release:     make(chan struct{}),
	}
	connection.writer = gate
	connection.writeMu.Unlock()

	type startResult struct {
		result PromptStartResult
		err    error
	}
	started := make(chan startResult, 1)
	go func() {
		result, startErr := manager.StartPrompt(context.Background(), created.Session.ID, "original")
		started <- startResult{result: result, err: startErr}
	}()

	select {
	case <-gate.started:
	case <-time.After(5 * time.Second):
		close(gate.release)
		t.Fatal("session/prompt write did not start")
	}
	select {
	case result := <-started:
		close(gate.release)
		t.Fatalf("StartPrompt returned before session/prompt was dispatched: result=%#v err=%v", result.result, result.err)
	default:
	}
	close(gate.release)

	var original PromptStartResult
	select {
	case result := <-started:
		if result.err != nil {
			t.Fatal(result.err)
		}
		original = result.result
	case <-time.After(5 * time.Second):
		t.Fatal("StartPrompt did not return after session/prompt dispatch")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	steering, err := manager.Steer(ctx, created.Session.ID, "STEERED")
	if err != nil {
		t.Fatal(err)
	}
	if steering["cancelledRunId"] != original.RunID {
		t.Fatalf("steering cancelled run = %#v, want %q", steering["cancelledRunId"], original.RunID)
	}
}

func TestClaudeSteeringCompatibilityVersionBoundary(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{name: "affected release", version: "0.64.2", want: true},
		{name: "affected older release", version: "v0.63.9", want: true},
		{name: "fixed boundary", version: "0.64.3", want: false},
		{name: "future release", version: "1.0.0", want: false},
		{name: "unknown version", version: "development", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := requiresHostSteeringFallback(AgentInfo{Name: claudeAgentACPName, Version: test.version})
			if got != test.want {
				t.Fatalf("fallback(%q) = %v, want %v", test.version, got, test.want)
			}
		})
	}
	if requiresHostSteeringFallback(AgentInfo{Name: "other", Version: "0.64.2"}) {
		t.Fatal("non-Claude adapter selected Claude compatibility fallback")
	}
}

func TestClaudeSteeringFallbackCancelsOldRunAndStartsObservableTurn(t *testing.T) {
	workspace := t.TempDir()
	manager, err := newTestManagerWithAgent(t.TempDir(), workspace, claudeAgentACPName, "0.64.2", "claude_steer_fallback")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.Close() }()

	created, err := manager.NewSession(context.Background(), workspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	original, err := manager.StartPrompt(context.Background(), created.Session.ID, "original")
	if err != nil {
		t.Fatal(err)
	}
	steering, err := manager.Steer(context.Background(), created.Session.ID, "STEERED")
	if err != nil {
		t.Fatal(err)
	}
	if steering["outcome"] != "startedNewTurn" || steering["reason"] != "claude_ede_compatibility" || steering["cancelledRunId"] != original.RunID {
		t.Fatalf("steering result = %#v", steering)
	}
	newRunID, _ := steering["runId"].(string)
	if newRunID == "" || newRunID == original.RunID {
		t.Fatalf("new steering run id = %q", newRunID)
	}
	cancelled := waitForSettledRun(t, manager, original.RunID)
	if cancelled.Status != RunCancelled || cancelled.Events[len(cancelled.Events)-1].Type != "cancelled" {
		t.Fatalf("original run = %#v", cancelled)
	}
	completed := waitForSettledRun(t, manager, newRunID)
	if completed.Status != RunCompleted || completed.StopReason != "end_turn" {
		t.Fatalf("fallback run = %#v", completed)
	}
	assertEventTypes(t, completed.Events, "agent_message_chunk", "completed")
}

func TestSteeringPromptRequiredStartsObservableRun(t *testing.T) {
	workspace := t.TempDir()
	manager, err := newTestManagerWithAgent(t.TempDir(), workspace, "generic-agent", "1.0.0", "steering_prompt_required")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.Close() }()

	created, err := manager.NewSession(context.Background(), workspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	steering, err := manager.Steer(context.Background(), created.Session.ID, "STEERED")
	if err != nil {
		t.Fatal(err)
	}
	if steering["outcome"] != "startedNewTurn" || steering["reason"] != "adapter_prompt_required" {
		t.Fatalf("steering result = %#v", steering)
	}
	runID, _ := steering["runId"].(string)
	completed := waitForSettledRun(t, manager, runID)
	if completed.Status != RunCompleted || completed.StopReason != "end_turn" {
		t.Fatalf("prompt-required run = %#v", completed)
	}
	assertEventTypes(t, completed.Events, "agent_message_chunk", "completed")
}

func TestClaudeSteeringResetFailureInterruptsSession(t *testing.T) {
	workspace := t.TempDir()
	manager, err := newTestManagerWithAgent(t.TempDir(), workspace, claudeAgentACPName, "0.64.2", "steering_reset_failure")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.Close() }()

	created, err := manager.NewSession(context.Background(), workspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	original, err := manager.StartPrompt(context.Background(), created.Session.ID, "original")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Steer(context.Background(), created.Session.ID, "replacement"); errorCode(err) != "ACP_REMOTE_ERROR" {
		t.Fatalf("steering reset error = %v", err)
	}
	cancelled := waitForSettledRun(t, manager, original.RunID)
	if cancelled.Status != RunCancelled {
		t.Fatalf("original run = %#v", cancelled)
	}
	record, err := manager.InspectSession(created.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != SessionInterrupted || record.LastStopReason != "steering_reset_failed" {
		t.Fatalf("session after reset failure = %#v", record)
	}
	manager.mu.RLock()
	_, loaded := manager.loaded[created.Session.ID]
	manager.mu.RUnlock()
	if loaded {
		t.Fatal("failed steering reset retained loaded-session cache")
	}
}

func TestCodexSteeringCompatibilityVersionBoundary(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{version: "1.1.9", want: true},
		{version: "1.1.8", want: true},
		{version: "1.2.0", want: false},
		{version: "2.0.0", want: false},
		{version: "development", want: false},
	}
	for _, test := range tests {
		t.Run(test.version, func(t *testing.T) {
			got := requiresHostSteeringFallback(AgentInfo{Name: codexACPName, Version: test.version})
			if got != test.want {
				t.Fatalf("fallback(%q) = %v, want %v", test.version, got, test.want)
			}
		})
	}
}

func TestCodexNoRolloutCompatibilityIsExact(t *testing.T) {
	matching := newError("ACP_REMOTE_ERROR", "Internal error", false, map[string]any{
		"rpc_code": -32603,
		"rpc_data": `{"details":"no rollout found for thread id test"}`,
	}, nil)
	if !isCodexNoRolloutError(AgentInfo{Name: codexACPName, Version: "1.1.9"}, matching) {
		t.Fatal("confirmed Codex 1.1.9 no-rollout error was not recognized")
	}
	if isCodexNoRolloutError(AgentInfo{Name: codexACPName, Version: "1.2.0"}, matching) {
		t.Fatal("future Codex release inherited the 1.1.9 compatibility rule")
	}
	other := newError("ACP_REMOTE_ERROR", "Internal error", false, map[string]any{"rpc_data": `{"details":"other"}`}, nil)
	if isCodexNoRolloutError(AgentInfo{Name: codexACPName, Version: "1.1.9"}, other) {
		t.Fatal("unrelated Codex remote error was swallowed")
	}
}

func TestCodexSteeringFallbackCancelsOldRunAndStartsObservableTurn(t *testing.T) {
	workspace := t.TempDir()
	manager, err := newTestManagerWithAgent(t.TempDir(), workspace, codexACPName, "1.1.9", "claude_steer_fallback")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.Close() }()

	created, err := manager.NewSession(context.Background(), workspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	original, err := manager.StartPrompt(context.Background(), created.Session.ID, "original")
	if err != nil {
		t.Fatal(err)
	}
	steering, err := manager.Steer(context.Background(), created.Session.ID, "STEERED")
	if err != nil {
		t.Fatal(err)
	}
	if steering["outcome"] != "startedNewTurn" || steering["reason"] != "codex_streaming_compatibility" || steering["cancelledRunId"] != original.RunID {
		t.Fatalf("steering result = %#v", steering)
	}
	newRunID, _ := steering["runId"].(string)
	cancelled := waitForSettledRun(t, manager, original.RunID)
	if cancelled.Status != RunCancelled {
		t.Fatalf("original run = %#v", cancelled)
	}
	completed := waitForSettledRun(t, manager, newRunID)
	if completed.Status != RunCompleted {
		t.Fatalf("fallback run = %#v", completed)
	}
}

func TestCodexFirstTurnSteeringFallbackRecreatesUnpersistedRemoteSession(t *testing.T) {
	workspace := t.TempDir()
	manager, err := newTestManagerWithAgent(t.TempDir(), workspace, codexACPName, "1.1.9", "codex_no_rollout_steer")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.Close() }()

	created, err := manager.NewSession(context.Background(), workspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetSessionMode(context.Background(), created.Session.ID, "review"); err != nil {
		t.Fatal(err)
	}
	original, err := manager.StartPrompt(context.Background(), created.Session.ID, "original")
	if err != nil {
		t.Fatal(err)
	}
	steering, err := manager.Steer(context.Background(), created.Session.ID, "STEERED")
	if err != nil {
		t.Fatal(err)
	}
	if steering["outcome"] != "startedNewTurn" || steering["reason"] != "codex_streaming_compatibility" {
		t.Fatalf("steering result = %#v", steering)
	}
	cancelled := waitForSettledRun(t, manager, original.RunID)
	if cancelled.Status != RunCancelled {
		t.Fatalf("original run = %#v", cancelled)
	}
	newRunID, _ := steering["runId"].(string)
	completed := waitForSettledRun(t, manager, newRunID)
	if completed.Status != RunCompleted {
		t.Fatalf("replacement run = %#v", completed)
	}

	recovered, err := manager.LoadSession(context.Background(), created.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Session.RemoteSessionID == created.Session.RemoteSessionID {
		t.Fatalf("unpersisted remote session was not replaced: %q", recovered.Session.RemoteSessionID)
	}
	if recovered.Session.ModeID != "review" || sessionModeID(t, recovered.Modes) != "review" {
		t.Fatalf("recovered mode = stored %q, response %#v", recovered.Session.ModeID, recovered.Modes)
	}
}

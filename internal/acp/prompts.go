package acp

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
)

const (
	maxRetainedRuns    = 256
	targetRetainedRuns = 192
)

type PromptStartResult struct {
	RunID       string    `json:"run_id"`
	SessionID   string    `json:"session_id"`
	Status      RunStatus `json:"status"`
	Disposition string    `json:"disposition"`
	StartedAt   time.Time `json:"started_at"`
}

type PromptEventsResult struct {
	RunID           string     `json:"run_id"`
	SessionID       string     `json:"session_id"`
	Status          RunStatus  `json:"status"`
	Events          []Event    `json:"events"`
	NextSeq         uint64     `json:"next_seq"`
	FirstSeq        uint64     `json:"first_seq"`
	LatestSeq       uint64     `json:"latest_seq"`
	DroppedCount    uint64     `json:"dropped_count"`
	HasMore         bool       `json:"has_more"`
	Truncated       bool       `json:"truncated"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	StopReason      string     `json:"stop_reason,omitempty"`
	ErrorCode       string     `json:"error_code,omitempty"`
	Message         string     `json:"message,omitempty"`
	CancelRequested bool       `json:"cancel_requested"`
}

func (m *Manager) StartPrompt(ctx context.Context, sessionID, text string) (PromptStartResult, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return PromptStartResult{}, newError("ACP_PROMPT_INVALID", "ACP prompt text is required", false, nil, nil)
	}
	return m.StartPromptBlocks(ctx, sessionID, []ContentBlock{TextBlock(text)})
}

// SubmitPrompt 是工具层唯一的 prompt 入口：空闲时启动新的 session/prompt；已有
// active Run 时仅在 Adapter 广告 steering 扩展且输入为纯文本时自动注入当前 Run。
func (m *Manager) SubmitPrompt(ctx context.Context, sessionID string, blocks []ContentBlock) (PromptStartResult, error) {
	m.mu.RLock()
	activeRunID := m.activeRunBySession[sessionID]
	activeRun := m.runs[activeRunID]
	m.mu.RUnlock()
	if activeRunID == "" || activeRun == nil || runStatus(activeRun) != RunRunning {
		return m.StartPromptBlocks(ctx, sessionID, blocks)
	}

	text, textOnly := promptText(blocks)
	if !textOnly || text == "" {
		return PromptStartResult{}, newError("ACP_SESSION_BUSY", "ACP session has an active prompt and steering only accepts text content", true, map[string]any{"session_id": sessionID, "run_id": activeRunID}, nil)
	}
	process, err := m.ensureProcess(ctx)
	if err != nil {
		return PromptStartResult{}, err
	}
	if !process.supportsSteering() {
		return PromptStartResult{}, newError("ACP_SESSION_BUSY", "ACP session already has an active prompt", true, map[string]any{"session_id": sessionID, "run_id": activeRunID}, nil)
	}
	steering, err := m.Steer(ctx, sessionID, text)
	if err != nil {
		return PromptStartResult{}, err
	}
	if runID, _ := steering["runId"].(string); runID != "" && runID != activeRunID {
		run, runErr := m.run(runID)
		if runErr != nil {
			return PromptStartResult{}, runErr
		}
		return PromptStartResult{RunID: run.ID, SessionID: sessionID, Status: runStatus(run), Disposition: "restarted", StartedAt: run.StartedAt}, nil
	}
	return PromptStartResult{RunID: activeRun.ID, SessionID: sessionID, Status: runStatus(activeRun), Disposition: "steered", StartedAt: activeRun.StartedAt}, nil
}

func (m *Manager) StartPromptBlocks(ctx context.Context, sessionID string, blocks []ContentBlock) (PromptStartResult, error) {
	process, err := m.ensureProcess(ctx)
	if err != nil {
		return PromptStartResult{}, err
	}
	blocks, err = validatePromptBlocks(process, blocks)
	if err != nil {
		return PromptStartResult{}, err
	}
	if _, err := m.EnsureSessionActive(ctx, sessionID); err != nil {
		return PromptStartResult{}, err
	}
	endOperation, err := m.beginSessionOperation(sessionID)
	if err != nil {
		return PromptStartResult{}, err
	}
	defer endOperation()
	select {
	case m.runSlots <- struct{}{}:
	default:
		return PromptStartResult{}, newError("ACP_BUSY", "ACP concurrent prompt limit reached", true, map[string]any{"limit": cap(m.runSlots)}, nil)
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		<-m.runSlots
		return PromptStartResult{}, newError("ACP_MANAGER_CLOSED", "ACP manager is closed", false, nil, nil)
	}
	if existing := m.activeRunBySession[sessionID]; existing != "" {
		m.mu.Unlock()
		<-m.runSlots
		return PromptStartResult{}, newError("ACP_SESSION_BUSY", "ACP session already has an active prompt", true, map[string]any{"session_id": sessionID, "run_id": existing}, nil)
	}
	runID, err := newID("acpr")
	if err != nil {
		m.mu.Unlock()
		<-m.runSlots
		return PromptStartResult{}, err
	}
	run := newRun(runID, sessionID)
	runCtx, cancel := context.WithCancel(context.Background())
	run.cancel = cancel
	m.pruneRunsLocked(time.Now().UTC())
	m.runs[runID] = run
	m.activeRunBySession[sessionID] = runID
	record, exists := m.sessions[sessionID]
	if !exists {
		delete(m.activeRunBySession, sessionID)
		delete(m.runs, runID)
		m.mu.Unlock()
		cancel()
		<-m.runSlots
		return PromptStartResult{}, newError("ACP_SESSION_NOT_FOUND", "ACP session was not found", false, map[string]any{"session_id": sessionID}, nil)
	}
	record.Status = SessionRunning
	record.UpdatedAt = time.Now().UTC()
	m.sessions[sessionID] = record
	m.mu.Unlock()

	if err := m.store.Save(record); err != nil {
		m.finishRun(run, RunFailed, "", err)
		return PromptStartResult{}, err
	}
	go m.runPrompt(runCtx, run, record, blocks)
	return PromptStartResult{RunID: run.ID, SessionID: sessionID, Status: RunRunning, Disposition: "started", StartedAt: run.StartedAt}, nil
}

func (m *Manager) PromptEvents(ctx context.Context, runID string, after uint64, limit int, wait time.Duration) (PromptEventsResult, error) {
	run, err := m.run(runID)
	if err != nil {
		return PromptEventsResult{}, err
	}
	result, err := run.promptEventsSnapshot(after, limit)
	if err != nil {
		return PromptEventsResult{}, err
	}
	if len(result.Events) == 0 && result.Status == RunRunning && wait > 0 {
		if err := m.waitForPromptEvents(ctx, run, after, wait); err != nil {
			return PromptEventsResult{}, err
		}
		result, err = run.promptEventsSnapshot(after, limit)
		if err != nil {
			return PromptEventsResult{}, err
		}
	}
	if result.Status != RunRunning {
		select {
		case <-run.finalized:
		case <-ctx.Done():
			return PromptEventsResult{}, newError("ACP_EVENTS_CANCELLED", "ACP event read was cancelled while the run finalized", true, map[string]any{"run_id": run.ID}, ctx.Err())
		}
		return run.promptEventsSnapshot(after, limit)
	}
	return result, nil
}

func (r *Run) promptEventsSnapshot(after uint64, limit int) (PromptEventsResult, error) {
	r.eventsMu.Lock()
	defer r.eventsMu.Unlock()
	page, err := r.eventsAfterLocked(after, limit)
	if err != nil {
		return PromptEventsResult{}, err
	}
	result := PromptEventsResult{
		RunID:           r.ID,
		SessionID:       r.SessionID,
		Status:          r.Status,
		Events:          page.Events,
		NextSeq:         page.NextSeq,
		FirstSeq:        page.FirstSeq,
		LatestSeq:       page.LatestSeq,
		DroppedCount:    page.DroppedCount,
		HasMore:         page.HasMore,
		Truncated:       page.Truncated,
		StartedAt:       r.StartedAt,
		EndedAt:         r.EndedAt,
		StopReason:      r.StopReason,
		CancelRequested: r.CancelRequested,
	}
	if r.Err != nil {
		result.ErrorCode = errorCode(r.Err)
		result.Message = r.Err.Error()
	}
	return result, nil
}

func (m *Manager) waitForPromptEvents(ctx context.Context, run *Run, after uint64, wait time.Duration) error {
	if wait > 25*time.Second {
		wait = 25 * time.Second
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		// Drain a stale wake-up before taking a fresh, single-lock snapshot. The
		// second check closes the race where an event arrives between drain and wait.
		select {
		case <-run.notify:
		default:
		}
		result, err := run.promptEventsSnapshot(after, 1)
		if err != nil {
			return err
		}
		if len(result.Events) > 0 || result.Status != RunRunning {
			return nil
		}
		select {
		case <-run.notify:
			continue
		case <-timer.C:
			return nil
		case <-ctx.Done():
			return newError("ACP_EVENTS_CANCELLED", "ACP event wait was cancelled", true, map[string]any{"run_id": run.ID}, ctx.Err())
		case <-m.closedCh:
			return newError("ACP_MANAGER_CLOSED", "ACP manager is closed", true, nil, nil)
		}
	}
}

func (m *Manager) CancelPrompt(_ context.Context, sessionID, runID string) error {
	m.mu.RLock()
	explicitRunID := runID != ""
	if runID == "" {
		runID = m.activeRunBySession[sessionID]
	}
	run := m.runs[runID]
	var record SessionRecord
	if run != nil {
		if sessionID != "" && sessionID != run.SessionID {
			m.mu.RUnlock()
			return newError("ACP_CANCEL_TARGET_MISMATCH", "ACP run does not belong to the supplied session", false, map[string]any{"session_id": sessionID, "run_id": runID, "run_session_id": run.SessionID}, nil)
		}
		sessionID = run.SessionID
		record = m.sessions[sessionID]
	}
	process := m.process
	m.mu.RUnlock()
	if run == nil {
		if explicitRunID {
			return newError("ACP_RUN_NOT_FOUND", "ACP prompt run was not found", false, map[string]any{"run_id": runID}, nil)
		}
		return nil
	}

	run.eventsMu.Lock()
	if run.Status != RunRunning {
		run.eventsMu.Unlock()
		if explicitRunID {
			return newError("ACP_RUN_SETTLED", "ACP prompt run is no longer running", false, map[string]any{"run_id": runID}, nil)
		}
		return nil
	}
	if run.CancelRequested {
		run.eventsMu.Unlock()
		return nil
	}
	run.eventsMu.Unlock()

	if process != nil && record.RemoteSessionID != "" {
		if err := process.connection.Notify("session/cancel", map[string]any{"sessionId": record.RemoteSessionID}); err != nil {
			return process.wrapError("cancel ACP prompt", err)
		}
	}

	// ACP 规定 cancel 后仍可能发送最后一批 session/update，并最终以
	// stopReason=cancelled 结束原 session/prompt。这里只标记 cancelling 意图，
	// 不取消 JSON-RPC request，也不提前从 activeRunBySession 移除 Run。
	run.eventsMu.Lock()
	if run.Status == RunRunning && !run.CancelRequested {
		run.CancelRequested = true
		run.appendEventLocked(Event{Type: "cancel_requested"})
	}
	run.eventsMu.Unlock()
	run.signalEvent()
	m.cancelPendingInteractions(sessionID)
	return nil
}

func (m *Manager) Steer(ctx context.Context, sessionID, text string) (map[string]any, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, newError("ACP_PROMPT_INVALID", "ACP steering text is required", false, nil, nil)
	}
	loaded, err := m.EnsureSessionActive(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	record := loaded.Session
	endOperation, err := m.beginSessionOperation(sessionID)
	if err != nil {
		return nil, err
	}
	defer endOperation()
	process, err := m.ensureProcess(ctx)
	if err != nil {
		return nil, err
	}
	if !process.supportsSteering() {
		return nil, capabilityError("_meta.steering.supported")
	}
	if requiresHostSteeringFallback(process.initialize.AgentInfo) {
		return m.startHostOwnedSteering(ctx, process, record, text, hostSteeringFallbackReason(process.initialize.AgentInfo))
	}
	var result map[string]any
	if err := process.connection.Request(ctx, "_session/steering", map[string]any{
		"sessionId": record.RemoteSessionID,
		"prompt":    []map[string]any{{"type": "text", "text": text}},
		"_meta":     map[string]any{"steering": map[string]any{"idleBehavior": "promptRequired"}},
	}, &result); err != nil {
		return nil, process.wrapError("steer ACP session", err)
	}
	if result["outcome"] == "promptRequired" {
		return m.startHostOwnedSteering(ctx, process, record, text, "adapter_prompt_required")
	}
	return result, nil
}

func (m *Manager) startHostOwnedSteering(ctx context.Context, process *agentProcess, record SessionRecord, text, reason string) (map[string]any, error) {
	sessionID := record.ID
	m.mu.RLock()
	cancelledRunID := m.activeRunBySession[sessionID]
	m.mu.RUnlock()
	if cancelledRunID != "" {
		if err := m.CancelPrompt(ctx, sessionID, cancelledRunID); err != nil && errorCode(err) != "ACP_RUN_SETTLED" {
			return nil, err
		}
		// Affected adapters can retain cancelled turn state in memory. A standard
		// close/load cycle resets that state while preserving the adapter-owned
		// transcript whenever the remote session already has persisted history.
		if !process.supportsLoadSession() || !process.supportsSessionCapability("close") {
			return nil, capabilityError("loadSession + sessionCapabilities.close")
		}
		if err := process.connection.Request(ctx, "session/close", map[string]any{"sessionId": record.RemoteSessionID}, nil); err != nil {
			m.abortRunLocally(sessionID, RunCancelled, "cancelled")
			m.markSessionInterrupted(record, "steering_reset_failed")
			return nil, process.wrapError("reset ACP session after steering cancellation", err)
		}
		m.settleRunAfterRemoteClose(sessionID)
		var loaded sessionLifecycleResponse
		if err := process.connection.Request(ctx, "session/load", sessionActivationParams(record), &loaded); err != nil {
			wrapped := process.wrapError("reload ACP session after steering cancellation", err)
			if !isCodexNoRolloutError(process.initialize.AgentInfo, wrapped) {
				m.markSessionInterrupted(record, "steering_reload_failed")
				return nil, wrapped
			}

			// codex-acp 1.1.9 cannot reload a cancelled first turn before any
			// rollout exists. The cancelled turn has no transcript to preserve, so
			// recreate only the remote thread while keeping the AgentDock session.
			if err := process.connection.Request(ctx, "session/new", sessionCreationParams(record.CWD, record.AdditionalDirectories), &loaded); err != nil {
				m.markSessionInterrupted(record, "steering_recreate_failed")
				return nil, process.wrapError("recreate ACP session after unpersisted steering cancellation", err)
			}
			updatedRecord, rebindErr := m.rebindRemoteSession(record, loaded)
			if rebindErr != nil {
				m.markSessionInterrupted(record, "steering_rebind_failed")
				return nil, rebindErr
			}
			record = updatedRecord
		}
		if err := m.restoreSessionMode(ctx, process, record, &loaded); err != nil {
			m.markSessionInterrupted(record, "steering_mode_restore_failed")
			return nil, err
		}
		if _, err := m.markSessionReady(record, loaded); err != nil {
			return nil, err
		}
	}
	started, err := m.StartPrompt(ctx, sessionID, text)
	if err != nil {
		return nil, err
	}
	result := map[string]any{
		"outcome": "startedNewTurn", "reason": reason,
		"runId": started.RunID, "sessionId": started.SessionID,
	}
	if cancelledRunID != "" {
		result["cancelledRunId"] = cancelledRunID
	}
	return result, nil
}

func (m *Manager) markSessionInterrupted(record SessionRecord, reason string) {
	now := time.Now().UTC()
	m.mu.Lock()
	current, exists := m.sessions[record.ID]
	_, terminal := m.terminalSessions[record.ID]
	if exists && !terminal {
		current.Status = SessionInterrupted
		current.LastStopReason = reason
		current.UpdatedAt = now
		m.sessions[record.ID] = current
		delete(m.loaded, record.ID)
		record = current
	}
	m.mu.Unlock()
	if exists && !terminal {
		if err := m.store.Save(record); err != nil {
			slog.Warn("persist interrupted ACP session failed", "session_id", record.ID, "reason", reason, "error", err)
		}
	}
}

func (m *Manager) runPrompt(ctx context.Context, run *Run, record SessionRecord, blocks []ContentBlock) {
	m.mu.RLock()
	process := m.process
	m.mu.RUnlock()
	if process == nil {
		m.finishRun(run, RunFailed, "", newError("ACP_CONNECTION_FAILED", "ACP process is not available", true, nil, nil))
		return
	}
	var response struct {
		StopReason string `json:"stopReason"`
	}
	err := process.connection.Request(ctx, "session/prompt", map[string]any{
		"sessionId": record.RemoteSessionID,
		"prompt":    blocks,
	}, &response)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			m.finishRun(run, RunCancelled, "cancelled", nil)
			return
		}
		status := RunFailed
		var acpErr *Error
		if errors.As(err, &acpErr) && acpErr.Code == "ACP_CONNECTION_CLOSED" {
			status = RunInterrupted
		}
		m.finishRun(run, status, "", process.wrapError("run ACP prompt", err))
		return
	}
	if process.initialize.AgentInfo.Name == "@agentclientprotocol/codex-acp" {
		if remoteMessage, falseSuccess := run.codexFalseSuccess(); falseSuccess {
			remoteErr := newError(
				"ACP_REMOTE_ERROR",
				"Codex turn failed after remote retries",
				true,
				map[string]any{"adapter": "codex-acp", "remote_message": remoteMessage},
				nil,
			)
			m.finishRun(run, RunFailed, "", process.wrapError("run ACP prompt", remoteErr))
			return
		}
	}
	if response.StopReason == "cancelled" {
		m.finishRun(run, RunCancelled, response.StopReason, nil)
		return
	}
	m.finishRun(run, RunCompleted, response.StopReason, nil)
}

func (m *Manager) finishRun(run *Run, status RunStatus, stopReason string, err error) {
	now := time.Now().UTC()
	run.eventsMu.Lock()
	if run.Status != RunRunning {
		run.eventsMu.Unlock()
		return
	}
	run.Status = status
	run.EndedAt = &now
	run.StopReason = stopReason
	run.Err = err
	terminalEvent := Event{}
	switch {
	case err != nil:
		terminalEvent = Event{Type: "error", ErrorCode: errorCode(err), Message: err.Error()}
	case status == RunCompleted:
		terminalEvent = Event{Type: "completed", StopReason: stopReason}
	case status == RunCancelled:
		terminalEvent = Event{Type: "cancelled", StopReason: stopReason}
	case status == RunInterrupted:
		terminalEvent = Event{Type: "interrupted", StopReason: stopReason}
	}
	if terminalEvent.Type != "" {
		run.appendEventLocked(terminalEvent)
	}
	run.eventsMu.Unlock()

	m.mu.Lock()
	delete(m.activeRunBySession, run.SessionID)
	_, terminalTransition := m.terminalSessions[run.SessionID]
	var record SessionRecord
	recordExists := false
	if !terminalTransition {
		record, recordExists = m.sessions[run.SessionID]
	}
	if !terminalTransition && recordExists {
		switch status {
		case RunInterrupted:
			record.Status = SessionInterrupted
		case RunFailed:
			record.Status = SessionError
		default:
			record.Status = SessionReady
		}
		record.LastStopReason = stopReason
		if err != nil && record.LastStopReason == "" {
			record.LastStopReason = errorCode(err)
		}
		record.UpdatedAt = now
		m.sessions[run.SessionID] = record
	}
	m.mu.Unlock()
	if !terminalTransition && recordExists {
		if saveErr := m.store.Save(record); saveErr != nil {
			slog.Warn("persist ACP session after prompt failed", "session_id", run.SessionID, "run_id", run.ID, "error", saveErr)
		}
	}
	run.finalizeOnce.Do(func() { close(run.finalized) })
	run.signalEvent()
	select {
	case <-m.runSlots:
	default:
	}
}

func runStatus(run *Run) RunStatus {
	run.eventsMu.Lock()
	defer run.eventsMu.Unlock()
	return run.Status
}

func (m *Manager) pruneRunsLocked(now time.Time) {
	if len(m.runs) <= maxRetainedRuns {
		return
	}
	for id, run := range m.runs {
		run.eventsMu.Lock()
		settled := run.Status != RunRunning
		endedAt := run.EndedAt
		run.eventsMu.Unlock()
		if settled && endedAt != nil && now.Sub(*endedAt) >= time.Hour {
			delete(m.runs, id)
		}
	}
	if len(m.runs) <= maxRetainedRuns {
		return
	}
	for id, run := range m.runs {
		run.eventsMu.Lock()
		settled := run.Status != RunRunning
		run.eventsMu.Unlock()
		if settled {
			delete(m.runs, id)
		}
		if len(m.runs) <= targetRetainedRuns {
			break
		}
	}
}

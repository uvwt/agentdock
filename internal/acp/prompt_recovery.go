package acp

// abortRunLocally is reserved for error-recovery paths where AgentDock can no longer
// trust the Adapter turn state (for example a close/reset request itself failed).
// It prevents a late prompt response from overwriting a stronger interrupted session state.
func (m *Manager) abortRunLocally(sessionID string, status RunStatus, stopReason string) {
	m.mu.RLock()
	run := m.runs[m.activeRunBySession[sessionID]]
	m.mu.RUnlock()
	if run == nil || runStatus(run) != RunRunning {
		return
	}
	m.finishRun(run, status, stopReason, nil)
	if run.cancel != nil {
		run.cancel()
	}
}

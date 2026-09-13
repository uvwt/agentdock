package acp

// settleRunAfterRemoteClose 只能在远端 session/close 或 session/delete 已确认后调用。
// 此时 ACP 已保证进行中工作被取消，可以安全结束本地 Run，并取消仍在等待响应的
// JSON-RPC request，避免占用 run slot 或阻塞后续同 session 的 prompt。
func (m *Manager) settleRunAfterRemoteClose(sessionID string) {
	m.mu.RLock()
	run := m.runs[m.activeRunBySession[sessionID]]
	m.mu.RUnlock()
	if run == nil || runStatus(run) != RunRunning {
		return
	}
	m.finishRun(run, RunCancelled, "cancelled", nil)
	if run.cancel != nil {
		run.cancel()
	}
}

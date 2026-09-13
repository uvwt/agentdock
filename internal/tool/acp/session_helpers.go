package acp

import (
	"strings"

	acpruntime "github.com/uvwt/agentdock/internal/acp"
)

func requireOneSessionTarget(sessionID, remoteSessionID string) error {
	hasLocal := strings.TrimSpace(sessionID) != ""
	hasRemote := strings.TrimSpace(remoteSessionID) != ""
	if hasLocal == hasRemote {
		return validationError(
			"ACP_SESSION_TARGET_INVALID",
			"provide exactly one of session_id or remote_session_id",
			nil,
		)
	}
	return nil
}

func appendHistoryResult(result Result, history acpruntime.HistoryReplay) {
	result["history_source"] = "adapter"
	result["history_events"] = history.Events
	result["history_truncated"] = history.Truncated
}

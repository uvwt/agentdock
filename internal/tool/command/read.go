package command

import "time"

func (svc *Service) readSession(request SessionObserveRequest) (Result, error) {
	maxBytes := commandOutputLimit(request.MaxOutputBytes)
	if maxBytes < 4 {
		return nil, toolError("INVALID_ARGUMENT", "read requires max_output_bytes >= 4", "validation")
	}
	svc.sessions.PruneCompletedBefore(time.Now().Add(-completedSessionRetention))
	s, ok := svc.sessions.Get(request.SessionID)
	if !ok {
		return nil, toolError("SESSION_NOT_FOUND", "session not found", "not_found")
	}
	page, err := s.Read(intValue(request.StdoutOffset, 0), intValue(request.StderrOffset, 0), maxBytes)
	if err != nil {
		return nil, toolError("INVALID_ARGUMENT", err.Error(), "validation")
	}
	result := snapshotResult(page.Snapshot)
	result["stdout_offset"], result["stderr_offset"] = page.StdoutOffset, page.StderrOffset
	result["stdout_next_offset"], result["stderr_next_offset"] = page.StdoutNextOffset, page.StderrNextOffset
	result["stdout_missed_bytes"], result["stderr_missed_bytes"] = page.StdoutMissedBytes, page.StderrMissedBytes
	if page.Completed {
		if err := s.WaitError(); err != nil {
			result["command_error"] = err.Error()
		}
	}
	return result, nil
}

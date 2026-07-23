package command

import (
	"fmt"
	"time"
)

func (s *Service) Close() error {
	if s == nil || s.sessions == nil {
		return nil
	}
	deadline := time.Now().Add(sessionKillWait)
	for s.sessions.ReservationCount() > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if remaining := s.sessions.ReservationCount(); remaining > 0 {
		return fmt.Errorf("wait for %d starting command session(s): timeout after %s", remaining, sessionKillWait)
	}
	if _, err := s.KillAll(nil); err != nil {
		return fmt.Errorf("stop command sessions: %w", err)
	}
	return nil
}

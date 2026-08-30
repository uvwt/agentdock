package command

import (
	"context"
	"errors"
	"fmt"
)

// BeginClose 先关闭新的 command reservation 入口。
// Runtime 会在取消共享 commandCtx 之前等待 startup 窗口排空，避免半启动进程被抢占。
func (s *Service) BeginClose() {
	if s == nil || s.sessions == nil {
		return
	}
	s.sessions.BeginClose()
}

func (s *Service) WaitForStarts() error {
	if s == nil || s.sessions == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), sessionKillWait)
	defer cancel()
	if s.sessions.WaitForStarts(ctx) {
		return nil
	}
	return fmt.Errorf(
		"wait for %d starting command session(s): timeout after %s",
		s.sessions.StartingCount(),
		sessionKillWait,
	)
}

func (s *Service) Close() error {
	if s == nil || s.sessions == nil {
		return nil
	}
	s.sessions.BeginClose()

	// Runtime 已经取消共享 commandCtx。先等 foreground/sync 调用释放 reservation，
	// 再统一清理已经进入 Store 的 async session，避免 session 在 KillAll 之后才入库。
	ctx, cancel := context.WithTimeout(context.Background(), sessionKillWait)
	reservationsDrained := s.sessions.WaitForReservations(ctx)
	cancel()

	var closeErrors []error
	if !reservationsDrained {
		closeErrors = append(closeErrors, fmt.Errorf(
			"wait for %d command reservation(s): timeout after %s",
			s.sessions.ReservationCount(),
			sessionKillWait,
		))
	}
	if _, err := s.killAll(nil); err != nil {
		closeErrors = append(closeErrors, fmt.Errorf("stop command sessions: %w", err))
	}
	return errors.Join(closeErrors...)
}

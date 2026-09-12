package desktopruntime

import (
	"fmt"
	"strings"
)

const (
	wtsActiveSession   = 0
	noConsoleSessionID = ^uint32(0)
)

type InteractiveSession struct {
	SessionID uint32
	State     int
	UserSID   string
}

// SelectInteractiveTaskSessionID 把 InteractiveToken 任务钉到确定的用户会话。
// 规则必须与 launch-windows-process.ps1 / 兼容垫片里的实现保持一致：
// UAC 提权会保留原 Session ID，优先用调用方已验证会话，避免 RDP 被重定向到 console。
func SelectInteractiveTaskSessionID(sessions []InteractiveSession, expectedUserSID string, currentSessionID int, currentUserSID string, consoleSessionID uint32) (int, error) {
	expected := strings.TrimSpace(expectedUserSID)
	if expected == "" {
		return 0, fmt.Errorf("scheduled-task user SID is required")
	}

	var currentMatch *InteractiveSession
	var matching []InteractiveSession
	for _, session := range sessions {
		if session.State != wtsActiveSession || strings.TrimSpace(session.UserSID) == "" {
			continue
		}
		if !strings.EqualFold(session.UserSID, expected) {
			continue
		}
		matching = append(matching, session)
		if int(session.SessionID) == currentSessionID {
			copySession := session
			currentMatch = &copySession
		}
	}

	if currentSessionID > 0 &&
		strings.EqualFold(strings.TrimSpace(currentUserSID), expected) &&
		currentMatch != nil {
		return currentSessionID, nil
	}
	if len(matching) == 0 {
		return 0, fmt.Errorf("No active interactive Windows session was found for scheduled-task user SID %s. InteractiveToken tasks cannot be started from Session 0 or a disconnected-only login.", expected)
	}
	if len(matching) == 1 {
		return int(matching[0].SessionID), nil
	}
	if consoleSessionID != noConsoleSessionID {
		for _, session := range matching {
			if session.SessionID == consoleSessionID {
				return int(consoleSessionID), nil
			}
		}
	}
	ids := make([]string, 0, len(matching))
	for _, session := range matching {
		ids = append(ids, fmt.Sprintf("%d", session.SessionID))
	}
	return 0, fmt.Errorf("Multiple active interactive Windows sessions match scheduled-task user SID %s (sessions: %s), and the caller/console session does not disambiguate them. Refusing to guess a target session.", expected, strings.Join(ids, ", "))
}

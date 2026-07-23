package command

import (
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/tool/command/session"
)

func (s *Service) Observe(args map[string]any) (Result, error) {
	action := strings.ToLower(stringArg(args, "action", "list"))
	switch action {
	case "list", "sessions":
		return s.listSessions()
	case "status", "get":
		return s.sessionStatus(args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported session_observe action", "validation", map[string]any{"action": stringArg(args, "action", ""), "allowed": []string{"list", "status"}})
	}
}

func (s *Service) Act(args map[string]any) (Result, error) {
	action := strings.ToLower(stringArg(args, "action", ""))
	switch action {
	case "write", "stdin", "send", "send_stdin":
		return s.writeStdin(args)
	case "kill", "stop":
		return s.killSession(args)
	case "kill_all", "stop_all", "clear":
		return s.KillAll(args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported session_act action", "validation", map[string]any{"action": stringArg(args, "action", ""), "allowed": []string{"write", "kill", "kill_all"}})
	}
}

func OutputLimit(args map[string]any) int {
	return commandOutputLimit(args)
}

func WaitForSessionsCompletion(sessions []*session.Session, timeout time.Duration) ([]*session.Session, []string) {
	return waitForSessionsCompletion(sessions, timeout)
}

func (s *Service) WriteStdin(args map[string]any) (Result, error)    { return s.writeStdin(args) }
func (s *Service) KillSession(args map[string]any) (Result, error)   { return s.killSession(args) }
func (s *Service) ListSessions() (Result, error)                     { return s.listSessions() }
func (s *Service) SessionStatus(args map[string]any) (Result, error) { return s.sessionStatus(args) }

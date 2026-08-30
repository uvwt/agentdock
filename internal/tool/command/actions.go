package command

import "strings"

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
		return s.killAll(args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported session_act action", "validation", map[string]any{"action": stringArg(args, "action", ""), "allowed": []string{"write", "kill", "kill_all"}})
	}
}

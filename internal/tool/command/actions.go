package command

import "strings"

func (s *Service) Observe(request SessionObserveRequest) (Result, error) {
	action := strings.ToLower(strings.TrimSpace(request.Action))
	if action == "" {
		action = "list"
	}
	if action != "read" && (request.StdoutOffset != nil || request.StderrOffset != nil) {
		return nil, toolError("INVALID_ARGUMENT", "output offsets require action=read", "validation")
	}
	switch action {
	case "list":
		return s.listSessions()
	case "status":
		return s.sessionStatus(request)
	case "read":
		return s.readSession(request)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported session_observe action", "validation", map[string]any{"action": request.Action, "allowed": []string{"list", "status", "read"}})
	}
}

func (s *Service) Act(request SessionActRequest) (Result, error) {
	action := strings.ToLower(strings.TrimSpace(request.Action))
	switch action {
	case "write":
		return s.writeStdin(request)
	case "kill":
		return s.killSession(request)
	case "kill_all":
		return s.killAll()
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported session_act action", "validation", map[string]any{"action": request.Action, "allowed": []string{"write", "kill", "kill_all"}})
	}
}

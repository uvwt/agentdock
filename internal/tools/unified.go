package tools

import (
	"context"
	"strings"
)

func (r *Runtime) sessionObserve(args map[string]any) (Result, error) {
	action := strings.ToLower(stringArg(args, "action", "list"))
	switch action {
	case "list", "sessions":
		return r.listSessions()
	case "status", "get":
		return r.sessionStatus(args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported session_observe action", "validation", map[string]any{"action": stringArg(args, "action", ""), "allowed": []string{"list", "status"}})
	}
}

func (r *Runtime) sessionAct(args map[string]any) (Result, error) {
	action := strings.ToLower(stringArg(args, "action", ""))
	switch action {
	case "write", "stdin", "send", "send_stdin":
		return r.writeStdin(args)
	case "kill", "stop":
		return r.killSession(args)
	case "kill_all", "stop_all", "clear":
		return r.killAllSessions(args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported session_act action", "validation", map[string]any{"action": stringArg(args, "action", ""), "allowed": []string{"write", "kill", "kill_all"}})
	}
}

func (r *Runtime) browserSession(ctx context.Context, args map[string]any) (Result, error) {
	action := strings.ToLower(stringArg(args, "action", "start"))
	switch action {
	case "start", "open", "new":
		return r.browserRunnerCall(ctx, "session_start", args)
	case "close", "stop":
		return r.browserRunnerCall(ctx, "session_close", args)
	case "cleanup", "cleanup_stale":
		return r.browserRunnerCall(ctx, "session_cleanup", args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported browser_session action", "validation", map[string]any{"action": stringArg(args, "action", ""), "allowed": []string{"start", "close", "cleanup_stale"}})
	}
}

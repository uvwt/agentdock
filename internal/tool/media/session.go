package media

import (
	"context"
	"strings"
)

func (s *Service) Session(ctx context.Context, args map[string]any) (Result, error) {
	action := strings.ToLower(stringArg(args, "action", "start"))
	switch action {
	case "start", "open", "new":
		return s.BrowserCall(ctx, "session_start", args)
	case "close", "stop":
		return s.BrowserCall(ctx, "session_close", args)
	case "cleanup", "cleanup_stale":
		return s.BrowserCall(ctx, "session_cleanup", args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported browser_session action", "validation", map[string]any{"action": stringArg(args, "action", ""), "allowed": []string{"start", "close", "cleanup_stale"}})
	}
}

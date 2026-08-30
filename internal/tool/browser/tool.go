package browser

import (
	"context"
	"fmt"
	"time"

	toolcore "github.com/uvwt/agentdock/internal/tool/core"
)

func (s *Service) HandleSession(ctx context.Context, args map[string]any) (toolcore.Result, error) {
	action, err := requiredString(args, "action")
	if err != nil {
		return browserFailure(err), nil
	}
	switch action {
	case "start":
		if err := validateBrowserKeys(args, "action", "url", "browser", "headless", "viewport", "profile_id", "cdp_url", "cookies", "local_storage", "reload_after_local_storage", "timeout_ms"); err != nil {
			return browserFailure(err), nil
		}
		req, err := parseBrowserStart(args)
		if err != nil {
			return browserFailure(err), nil
		}
		started, err := s.start(ctx, req)
		if err != nil {
			return browserFailure(err), nil
		}
		result := browserResultMap(started)
		result["browser_ok"] = true
		return result, nil
	case "close":
		if err := validateBrowserKeys(args, "action", "session_id", "timeout_ms"); err != nil {
			return browserFailure(err), nil
		}
		if _, err := durationArg(args, "timeout_ms", 30*time.Second, time.Millisecond, 5*time.Minute); err != nil {
			return browserFailure(err), nil
		}
		sessionID, err := requiredString(args, "session_id")
		if err != nil {
			return browserFailure(err), nil
		}
		closed, err := s.closeSession(CloseRequest{SessionID: sessionID})
		if err != nil {
			return browserFailure(err), nil
		}
		result := browserResultMap(closed)
		result["browser_ok"] = true
		return result, nil
	case "cleanup_stale":
		if err := validateBrowserKeys(args, "action", "max_age_ms", "timeout_ms"); err != nil {
			return browserFailure(err), nil
		}
		if _, err := durationArg(args, "timeout_ms", 30*time.Second, time.Millisecond, 5*time.Minute); err != nil {
			return browserFailure(err), nil
		}
		maxAge, err := durationArg(args, "max_age_ms", 6*time.Hour, time.Millisecond, 365*24*time.Hour)
		if err != nil {
			return browserFailure(err), nil
		}
		cleaned := s.cleanupStale(CleanupRequest{MaxAge: maxAge})
		result := browserResultMap(cleaned)
		result["browser_ok"] = true
		return result, nil
	default:
		return browserFailure(browserInvalid("unsupported browser_session action", map[string]any{"action": action})), nil
	}
}

func (s *Service) HandleAct(ctx context.Context, args map[string]any) (toolcore.Result, error) {
	if err := validateBrowserKeys(args, "session_id", "page_id", "actions", "full_page", "max_text_chars", "max_interactive_elements", "retention_seconds", "close_after", "timeout_ms"); err != nil {
		return browserFailure(err), nil
	}
	actions, err := parseBrowserActions(args["actions"])
	if err != nil {
		return browserFailure(err), nil
	}
	options, err := parseSnapshotOptions(args)
	if err != nil {
		return browserFailure(err), nil
	}

	snapshot, err := s.act(ctx, ActRequest{
		SessionID: options.sessionID, PageID: options.pageID, Actions: actions, FullPage: options.fullPage,
		MaxTextChars: options.maxText, MaxInteractiveElements: options.maxInteractive, Timeout: options.timeout,
	})
	if err != nil {
		return browserFailure(err), nil
	}
	return s.completeSnapshot(ctx, snapshot, options)
}

func (s *Service) HandleSnapshot(ctx context.Context, args map[string]any) (toolcore.Result, error) {
	if err := validateBrowserKeys(args, "session_id", "page_id", "full_page", "max_text_chars", "max_interactive_elements", "retention_seconds", "close_after", "timeout_ms"); err != nil {
		return browserFailure(err), nil
	}
	options, err := parseSnapshotOptions(args)
	if err != nil {
		return browserFailure(err), nil
	}

	snapshot, err := s.snapshot(ctx, SnapshotRequest{
		SessionID: options.sessionID, PageID: options.pageID, FullPage: options.fullPage,
		MaxTextChars: options.maxText, MaxInteractiveElements: options.maxInteractive, Timeout: options.timeout,
	})
	if err != nil {
		return browserFailure(err), nil
	}
	return s.completeSnapshot(ctx, snapshot, options)
}

type snapshotOptions struct {
	sessionID      string
	pageID         string
	fullPage       bool
	closeAfter     bool
	maxText        int
	maxInteractive int
	retention      int
	timeout        time.Duration
}

func parseSnapshotOptions(args map[string]any) (snapshotOptions, error) {
	var options snapshotOptions
	var err error
	if options.sessionID, err = requiredString(args, "session_id"); err != nil {
		return snapshotOptions{}, err
	}
	if options.pageID, err = optionalString(args, "page_id", ""); err != nil {
		return snapshotOptions{}, err
	}
	if options.fullPage, err = boolArgStrict(args, "full_page", false); err != nil {
		return snapshotOptions{}, err
	}
	if options.closeAfter, err = boolArgStrict(args, "close_after", false); err != nil {
		return snapshotOptions{}, err
	}
	if options.maxText, err = intArgRange(args, "max_text_chars", 8000, 1, 50000); err != nil {
		return snapshotOptions{}, err
	}
	if options.maxInteractive, err = intArgRange(args, "max_interactive_elements", 40, 1, 200); err != nil {
		return snapshotOptions{}, err
	}
	if options.retention, err = intArgRange(args, "retention_seconds", 0, 0, 604800); err != nil {
		return snapshotOptions{}, err
	}
	if options.timeout, err = durationArg(args, "timeout_ms", 30*time.Second, time.Millisecond, 5*time.Minute); err != nil {
		return snapshotOptions{}, err
	}
	return options, nil
}

func (s *Service) completeSnapshot(ctx context.Context, snapshot Snapshot, options snapshotOptions) (toolcore.Result, error) {
	// Artifact 发布成功后才能执行 close_after，否则发布故障会让调用方失去重试所需的会话。
	result, err := s.publishBrowserSnapshot(ctx, snapshot, options.retention)
	if err != nil {
		return nil, err
	}
	if options.closeAfter {
		if _, err := s.closeSession(CloseRequest{SessionID: options.sessionID}); err != nil {
			return browserFailure(err), nil
		}
		result["closed"] = true
	}
	return result, nil
}

func (s *Service) publishBrowserSnapshot(ctx context.Context, snapshot Snapshot, retention int) (toolcore.Result, error) {
	if s.publishScreenshot == nil {
		return nil, fmt.Errorf("browser screenshot publisher is not configured")
	}
	published, err := s.publishScreenshot(ctx, snapshot.PNG, retention)
	if err != nil {
		return nil, err
	}
	result := browserResultMap(snapshot)
	result["browser_ok"] = true
	result["screenshot"] = published
	return result, nil
}

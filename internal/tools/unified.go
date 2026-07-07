package tools

import (
	"context"
	"strings"
)

func (r *Runtime) sessionControl(args map[string]any) (Result, error) {
	switch strings.ToLower(stringArg(args, "action", "list")) {
	case "list", "sessions":
		return r.listSessions()
	case "status", "get":
		return r.sessionStatus(args)
	case "write", "stdin", "send", "send_stdin":
		return r.writeStdin(args)
	case "kill", "stop":
		return r.killSession(args)
	case "kill_all", "stop_all", "clear":
		return r.killAllSessions(args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported session_control action", "validation", map[string]any{"action": stringArg(args, "action", "")})
	}
}

func (r *Runtime) memoryEdit(ctx context.Context, args map[string]any) (Result, error) {
	switch strings.ToLower(stringArg(args, "action", "patch")) {
	case "append", "append_note", "note":
		return r.memoryAppendNote(ctx, args)
	case "write", "create", "replace":
		return r.memoryWrite(ctx, args)
	case "delete", "remove":
		return r.memoryDelete(ctx, args)
	case "diff", "preview":
		return r.memoryDiff(ctx, args)
	case "patch", "edit":
		return r.memoryPatch(ctx, args)
	case "update_fact", "fact":
		return r.memoryUpdateFact(ctx, args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported recall_write action", "validation", map[string]any{"action": stringArg(args, "action", "")})
	}
}

func (r *Runtime) gitInspect(ctx context.Context, args map[string]any) (Result, error) {
	switch strings.ToLower(stringArg(args, "action", "show")) {
	case "show":
		return r.gitShow(ctx, args)
	case "blame":
		return r.gitBlame(ctx, args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported git_inspect action", "validation", map[string]any{"action": stringArg(args, "action", "")})
	}
}

func (r *Runtime) gitRemote(ctx context.Context, args map[string]any) (Result, error) {
	switch strings.ToLower(stringArg(args, "action", "fetch")) {
	case "fetch":
		return r.gitFetch(ctx, args)
	case "pull":
		return r.gitPull(ctx, args)
	case "push":
		return r.gitPush(ctx, args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported git_remote action", "validation", map[string]any{"action": stringArg(args, "action", "")})
	}
}

func (r *Runtime) browserSession(ctx context.Context, args map[string]any) (Result, error) {
	switch strings.ToLower(stringArg(args, "action", "start")) {
	case "start", "open", "new":
		return r.browserRunnerCall(ctx, "session_start", args)
	case "close", "stop":
		return r.browserRunnerCall(ctx, "session_close", args)
	case "cleanup", "cleanup_stale":
		return r.browserRunnerCall(ctx, "session_cleanup", args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported browser_session action", "validation", map[string]any{"action": stringArg(args, "action", "")})
	}
}

func (r *Runtime) desktopObserve(ctx context.Context, args map[string]any) (Result, error) {
	action := strings.ToLower(stringArg(args, "action", ""))
	if action == "" {
		if stringArg(args, "app", "") != "" {
			action = "app_state"
		} else {
			action = "snapshot"
		}
	}
	switch action {
	case "preflight":
		return r.desktopPreflight(ctx, args)
	case "list_apps", "apps":
		return r.desktopListApps(ctx, args)
	case "app_state", "state", "get_app_state":
		return r.desktopGetAppState(ctx, args)
	case "window_list", "windows":
		return r.desktopWindowList(ctx, args)
	case "snapshot", "screen":
		return r.desktopSnapshot(ctx, args)
	case "snapshot_app", "app_snapshot":
		return r.desktopSnapshotApp(ctx, args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported desktop_observe action", "validation", map[string]any{"action": stringArg(args, "action", "")})
	}
}

func (r *Runtime) desktopAct(ctx context.Context, args map[string]any) (Result, error) {
	switch strings.ToLower(stringArg(args, "action", "")) {
	case "focus", "focus_app":
		return r.desktopFocusApp(ctx, args)
	case "move":
		return r.desktopMove(ctx, args)
	case "click":
		return r.desktopClick(ctx, args)
	case "double_click", "doubleclick":
		return r.desktopDoubleClick(ctx, args)
	case "scroll":
		return r.desktopScroll(ctx, args)
	case "drag":
		return r.desktopDrag(ctx, args)
	case "type", "text":
		return r.desktopType(ctx, args)
	case "set_value":
		return r.desktopSetValue(ctx, args)
	case "secondary_action", "perform_secondary_action", "accessibility_action":
		return r.desktopPerformSecondaryAction(ctx, args)
	case "hotkey", "shortcut":
		return r.desktopHotkey(ctx, args)
	case "wait":
		return r.desktopWait(ctx, args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported desktop_act action", "validation", map[string]any{"action": stringArg(args, "action", "")})
	}
}

func (r *Runtime) desktopClipboard(ctx context.Context, args map[string]any) (Result, error) {
	action := strings.ToLower(stringArg(args, "action", ""))
	if action == "" {
		if _, ok := args["text"]; ok {
			action = "set"
		} else {
			action = "get"
		}
	}
	switch action {
	case "get", "read":
		return r.desktopClipboardGet(ctx, args)
	case "set", "write":
		return r.desktopClipboardSet(ctx, args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported desktop_clipboard action", "validation", map[string]any{"action": stringArg(args, "action", "")})
	}
}

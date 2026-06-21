package tools

import (
	"context"
	"strings"
)

func (r *Runtime) sessionControl(args map[string]any) (Result, error) {
	switch strings.ToLower(stringArg(args, "action", "list")) {
	case "list", "sessions", "list_sessions":
		return r.listSessions()
	case "status", "get", "session_status":
		return r.sessionStatus(args)
	case "write", "stdin", "send", "send_stdin", "write_stdin":
		return r.writeStdin(args)
	case "kill", "stop", "kill_session":
		return r.killSession(args)
	case "kill_all", "stop_all", "clear", "kill_all_sessions":
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
	case "show", "git_show":
		return r.gitShow(ctx, args)
	case "blame", "git_blame":
		return r.gitBlame(ctx, args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported git_inspect action", "validation", map[string]any{"action": stringArg(args, "action", "")})
	}
}

func (r *Runtime) gitRemote(ctx context.Context, args map[string]any) (Result, error) {
	switch strings.ToLower(stringArg(args, "action", "fetch")) {
	case "fetch", "git_fetch":
		return r.gitFetch(ctx, args)
	case "pull", "git_pull":
		return r.gitPull(ctx, args)
	case "push", "git_push":
		return r.gitPush(ctx, args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported git_remote action", "validation", map[string]any{"action": stringArg(args, "action", "")})
	}
}

func (r *Runtime) browserSession(ctx context.Context, args map[string]any) (Result, error) {
	switch strings.ToLower(stringArg(args, "action", "start")) {
	case "start", "open", "new", "browser_session_start":
		return r.browserRunnerCall(ctx, "session_start", args)
	case "close", "stop", "browser_session_close":
		return r.browserRunnerCall(ctx, "session_close", args)
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
	case "preflight", "desktop_preflight":
		return r.desktopPreflight(ctx, args)
	case "list_apps", "apps", "desktop_list_apps":
		return r.desktopListApps(ctx, args)
	case "app_state", "state", "get_app_state", "desktop_get_app_state":
		return r.desktopGetAppState(ctx, args)
	case "window_list", "windows", "desktop_window_list":
		return r.desktopWindowList(ctx, args)
	case "snapshot", "screen", "desktop_snapshot":
		return r.desktopSnapshot(ctx, args)
	case "snapshot_app", "app_snapshot", "desktop_snapshot_app":
		return r.desktopSnapshotApp(ctx, args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported desktop_observe action", "validation", map[string]any{"action": stringArg(args, "action", "")})
	}
}

func (r *Runtime) desktopAct(ctx context.Context, args map[string]any) (Result, error) {
	switch strings.ToLower(stringArg(args, "action", "")) {
	case "focus", "focus_app", "desktop_focus_app":
		return r.desktopFocusApp(ctx, args)
	case "move", "desktop_move":
		return r.desktopMove(ctx, args)
	case "click", "desktop_click":
		return r.desktopClick(ctx, args)
	case "double_click", "doubleclick", "desktop_double_click":
		return r.desktopDoubleClick(ctx, args)
	case "scroll", "desktop_scroll":
		return r.desktopScroll(ctx, args)
	case "drag", "desktop_drag":
		return r.desktopDrag(ctx, args)
	case "type", "text", "desktop_type":
		return r.desktopType(ctx, args)
	case "set_value", "desktop_set_value":
		return r.desktopSetValue(ctx, args)
	case "secondary_action", "perform_secondary_action", "accessibility_action", "desktop_perform_secondary_action":
		return r.desktopPerformSecondaryAction(ctx, args)
	case "hotkey", "shortcut", "desktop_hotkey":
		return r.desktopHotkey(ctx, args)
	case "wait", "desktop_wait":
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
	case "get", "read", "desktop_clipboard_get":
		return r.desktopClipboardGet(ctx, args)
	case "set", "write", "desktop_clipboard_set":
		return r.desktopClipboardSet(ctx, args)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported desktop_clipboard action", "validation", map[string]any{"action": stringArg(args, "action", "")})
	}
}

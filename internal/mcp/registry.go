package mcp

type ToolDefinition struct {
	Name                   string
	Title                  string
	Description            string
	ReadOnly               bool
	Destructive            bool
	OpenWorld              bool
	FileArgRewritePaths    []string
	FileResultRewritePaths []string
}

var toolRegistry = []ToolDefinition{
	{Name: "server_info", Title: "Server info", Description: "Return server, workspace, auth, profile, sandbox, and exposed-tool metadata.", ReadOnly: true},
	{Name: "tool_descriptors", Title: "Tool descriptors", Description: "Return the current tool descriptor summary exposed by the runtime.", ReadOnly: true},
	{Name: "get_default_cwd", Title: "Get default cwd", Description: "Return the current default cwd inside the workspace.", ReadOnly: true},
	{Name: "set_default_cwd", Title: "Set default cwd", Description: "Set the default cwd for relative tool paths inside the workspace.", ReadOnly: true},
	{Name: "read_file", Title: "Read file", Description: "Read a UTF-8 text file slice inside the configured workspace.", ReadOnly: true},
	{Name: "list_dir", Title: "List directory", Description: "List directory entries inside the configured workspace.", ReadOnly: true},
	{Name: "list_files", Title: "List files", Description: "List workspace files using glob and ignore filters.", ReadOnly: true},
	{Name: "search_text", Title: "Search text", Description: "Search UTF-8 workspace files for text or regex matches.", ReadOnly: true},
	{Name: "apply_patch", Title: "Update workspace files", Description: "Update workspace files using structured edits.", Destructive: false},
	{Name: "edit_file", Title: "Edit file", Description: "Replace exact UTF-8 text in one workspace file with match-count checks and diff preview.", Destructive: false},
	{Name: "exec_command", Title: "Run workspace command", Description: "Run a bounded command in the workspace with sandbox and approval controls.", Destructive: false, OpenWorld: true},
	{Name: "session_control", Title: "Control command sessions", Description: "List, inspect, write to, or stop command sessions through one unified session action tool.", Destructive: false},
	{Name: "write_stdin", Title: "Send session input", Description: "Send input to a running command session.", Destructive: false},
	{Name: "session_status", Title: "Session status", Description: "Return output and status for a running command session.", ReadOnly: true},
	{Name: "list_sessions", Title: "List sessions", Description: "List currently running command sessions.", ReadOnly: true},
	{Name: "kill_session", Title: "Stop session", Description: "Stop a running command session.", Destructive: false},
	{Name: "kill_all_sessions", Title: "Stop all sessions", Description: "Stop all currently running command sessions.", Destructive: false},
	{Name: "configure_github_token", Title: "Configure GitHub token", Description: "Load a GitHub token from a workspace .env file and store a redacted HTTPS credential for git.", Destructive: false},
	{Name: "check_github_repo_access", Title: "Check GitHub repo access", Description: "Check stored GitHub credential authentication and repository visibility without exposing secrets.", ReadOnly: true, OpenWorld: true},
	{Name: "github_create_repo", Title: "Create GitHub repository", Description: "Create a GitHub repository using the stored credential without exposing secrets.", Destructive: false, OpenWorld: true},
	{Name: "task_manage", Title: "Manage recoverable tasks", Description: "Persist and resume substantial multi-step AgentDock tasks through a lightweight check, execute, verify, and closeout state machine. Use phase_checkpoint once per meaningful phase to batch step completions, condition evidence, and phase advancement; avoid per-command status updates. Use it for deployments, troubleshooting, restarts, cross-device work, or requests that require completion evidence; do not use it for simple reads or one-step calls. This tool records state only and never executes commands or other tools.", Destructive: false},
	{Name: "skill_manage", Title: "Manage AgentDock Skills", Description: "List, inspect, validate, install, run, or roll back AgentDock Skills through the local Skill Runtime.", Destructive: true, OpenWorld: true},
	{Name: "env_manage", Title: "Manage Skill environment", Description: "Manage redacted Skill environment variables through the local Nexus Env Registry.", Destructive: true, OpenWorld: true},
	{Name: "artifact_send", Title: "Send encrypted artifact", Description: "Encrypt and send a top-level file parameter or local file/directory through AgentDock Nexus to one or more registered devices. The target only writes to its controlled inbox or configured logical target and never executes the file.", Destructive: false, OpenWorld: true, FileArgRewritePaths: []string{"file"}},
	{Name: "artifact_fetch_create", Title: "Create artifact fetch", Description: "Create an asynchronous high-risk request for a registered device to list or encrypt an absolute-path file or directory under immutable deny rules.", Destructive: true, OpenWorld: true},
	{Name: "artifact_fetch_status", Title: "Artifact fetch status", Description: "Return status or a bounded directory listing for a local artifact fetch request.", ReadOnly: true, OpenWorld: true},
	{Name: "artifact_fetch_download", Title: "Download artifact fetch", Description: "Download and decrypt a ready artifact fetch, return a file resource, or confirm that the GPT sandbox mounted it so ciphertext can be deleted.", Destructive: true, OpenWorld: true, FileResultRewritePaths: []string{"file_path"}},
	{Name: "recall_bootstrap", Title: "Bootstrap RecallDock context", Description: "Load high-priority RecallDock context at the start of substantial AgentDock, project, deployment, debugging, or preference-sensitive tasks. max_bytes controls pack budget only; compact index/excerpt output is default, and full body requires include_body or targeted recall_read.", ReadOnly: true, OpenWorld: true},
	{Name: "recall_search", Title: "Search RecallDock", Description: "Search the RecallDock unified recall store. Supports Markdown memories, experience cards, and notes; use kind or prefix to narrow the search.", ReadOnly: true, OpenWorld: true},
	{Name: "recall_read", Title: "Read RecallDock entry", Description: "Read one Markdown, card, or note entry from the configured RecallDock store by path.", ReadOnly: true, OpenWorld: true},
	{Name: "recall_write", Title: "Write RecallDock entry", Description: "Plan, write, patch, diff, update facts, or delete content in the RecallDock store. kind selects the write mechanism: card, note, markdown, append_note, patch, diff, fact, or delete.", Destructive: false, OpenWorld: true},
	{Name: "recall_maintain", Title: "Maintain RecallDock", Description: "Run RecallDock maintenance actions such as sync_status, list, lint, embedding_status, reindex, or reindex_cards.", Destructive: false, OpenWorld: true},
	{Name: "memory_bootstrap", Title: "Bootstrap MemoryDock context", Description: "Load high-priority MemoryDock context at the start of substantial AgentDock, project, deployment, debugging, or preference-sensitive tasks. max_bytes controls pack budget only; compact index/excerpt output is default, and full body requires include_body or targeted memory_read.", ReadOnly: true, OpenWorld: true},
	{Name: "memory_list", Title: "List MemoryDock memories", Description: "List memories from the configured MemoryDock service.", ReadOnly: true, OpenWorld: true},
	{Name: "memory_read", Title: "Read MemoryDock memory", Description: "Read one Markdown memory from the configured MemoryDock service.", ReadOnly: true, OpenWorld: true},
	{Name: "memory_search", Title: "Search MemoryDock", Description: "Search memories by text and path through MemoryDock.", ReadOnly: true, OpenWorld: true},
	{Name: "memory_pack", Title: "Pack MemoryDock project context", Description: "Compatibility alias for memory_bootstrap. Prefer memory_bootstrap as the default context entry; memory_pack is only for older workflows that still ask for a packed project bundle.", ReadOnly: true, OpenWorld: true},
	{Name: "memory_card_capture", Title: "Plan Memory card capture", Description: "Create a reviewable Memory experience-card capture plan. It classifies one reusable action experience, checks similar cards, and never writes by itself.", ReadOnly: true, OpenWorld: true},
	{Name: "memory_card_write", Title: "Write Memory card", Description: "Write one atomic Memory experience card under cards/ after confirmation. Use memory_card_capture first to avoid duplicates and pollution.", Destructive: false, OpenWorld: true},
	{Name: "memory_edit", Title: "Edit MemoryDock memory", Description: "Unified MemoryDock edit tool for append, write, delete, diff, patch, and structured fact updates. Destructive actions still require confirmation.", Destructive: false, OpenWorld: true},
	{Name: "memory_append_note", Title: "Append MemoryDock note", Description: "Append a note to MemoryDock inbox or scoped memory area.", Destructive: false, OpenWorld: true},
	{Name: "memory_write", Title: "Write MemoryDock memory", Description: "Write a Markdown memory through MemoryDock; writing outside inbox requires confirmed=true.", Destructive: false, OpenWorld: true},
	{Name: "memory_delete", Title: "Delete MemoryDock memory", Description: "Delete a MemoryDock memory; requires confirmed=true.", Destructive: true, OpenWorld: true},
	{Name: "memory_sync_status", Title: "MemoryDock sync status", Description: "Return MemoryDock Git sync status.", ReadOnly: true, OpenWorld: true},
	{Name: "memory_diff", Title: "Preview MemoryDock diff", Description: "Preview a diff for a proposed MemoryDock memory edit without writing.", ReadOnly: true, OpenWorld: true},
	{Name: "memory_patch", Title: "Patch MemoryDock memory", Description: "Patch a MemoryDock memory by text, regex, section, append, or prepend. Defaults to dry-run unless confirmed=true.", Destructive: false, OpenWorld: true},
	{Name: "memory_update_fact", Title: "Update MemoryDock fact", Description: "Update structured key/value facts inside a MemoryDock memory. Defaults to dry-run unless confirmed=true.", Destructive: false, OpenWorld: true},
	{Name: "memory_lint", Title: "Lint MemoryDock memories", Description: "Scan MemoryDock memories for configured terms or regex patterns.", ReadOnly: true, OpenWorld: true},
	{Name: "notes_search", Title: "Search notes", Description: "Search notes with an index-first strategy over notes/questions or notes/github-learning, then fall back to MemoryDock search. Returns compact candidates by default; pass include_search_results only when raw search hits are needed.", ReadOnly: true, OpenWorld: true},
	{Name: "notes_capture", Title: "Plan notes capture", Description: "Create a reviewable capture plan for a question or learning note. This does not write memory by itself.", ReadOnly: true, OpenWorld: true},
	{Name: "notes_write", Title: "Write notes", Description: "Write an explicit Markdown note inside notes/ after confirmation. Use notes_capture first when classifying new knowledge.", Destructive: false, OpenWorld: true},
	{Name: "browser_session", Title: "Browser session", Description: "Start or close a browser automation session using one unified session tool.", Destructive: false, OpenWorld: true},
	{Name: "browser_act", Title: "Browser actions", Description: "Run browser automation actions such as goto, click, fill, wait, and screenshot snapshot.", Destructive: false, OpenWorld: true},
	{Name: "browser_session_start", Title: "Start browser session", Description: "Create a browser automation session using the optional workspace Node runner.", Destructive: false, OpenWorld: true},
	{Name: "browser_action", Title: "Run browser actions", Description: "Run browser automation actions such as goto, click, fill, wait, and screenshot snapshot.", Destructive: false, OpenWorld: true},
	{Name: "browser_snapshot", Title: "Browser snapshot", Description: "Capture the current browser page URL, title, text, screenshot, console errors, and network errors.", ReadOnly: true, OpenWorld: true},
	{Name: "browser_session_close", Title: "Close browser session", Description: "Close and remove a browser automation session.", Destructive: false, OpenWorld: true},
	{Name: "desktop_observe", Title: "Observe desktop", Description: "Unified macOS desktop observation tool for preflight, app list, app state, windows, screen snapshots, and app snapshots.", ReadOnly: true},
	{Name: "desktop_act", Title: "Act on desktop", Description: "Unified macOS desktop action tool for focus, move, click, double-click, scroll, drag, type, set value, accessibility actions, hotkeys, and waits.", Destructive: true},
	{Name: "desktop_clipboard", Title: "Desktop clipboard", Description: "Read or set the macOS clipboard text through one unified clipboard tool.", Destructive: true},
	{Name: "desktop_preflight", Title: "Desktop preflight", Description: "Check macOS desktop automation dependencies and likely permissions.", ReadOnly: true},
	{Name: "desktop_list_apps", Title: "Desktop apps", Description: "List running macOS apps and best-effort recent application usage metadata.", ReadOnly: true},
	{Name: "desktop_get_app_state", Title: "Desktop app state", Description: "Capture an app key window screenshot and accessibility tree. Call this before operating an app UI.", ReadOnly: true},
	{Name: "desktop_window_list", Title: "Desktop windows", Description: "List visible macOS applications and window titles using AppleScript.", ReadOnly: true},
	{Name: "desktop_snapshot", Title: "Desktop snapshot", Description: "Capture a macOS desktop screenshot as an AgentDock artifact. Screenshot coordinates are image pixels; desktop actions use macOS points.", ReadOnly: true},
	{Name: "desktop_snapshot_app", Title: "Desktop app snapshot", Description: "Capture a target macOS app window or window-relative crop. Returns action_coordinate_space and screenshot_coordinate_space for points-vs-pixels calibration.", ReadOnly: true},
	{Name: "desktop_clipboard_set", Title: "Set desktop clipboard", Description: "Set the macOS clipboard text using pbcopy.", Destructive: true},
	{Name: "desktop_clipboard_get", Title: "Get desktop clipboard", Description: "Read the macOS clipboard text using pbpaste.", ReadOnly: true},
	{Name: "desktop_focus_app", Title: "Focus desktop app", Description: "Activate a macOS application by name using AppleScript.", Destructive: false},
	{Name: "desktop_move", Title: "Desktop move pointer", Description: "Move the macOS mouse pointer using cliclick. ok=true only means the command was issued; use verify=true and inspect effect_verified/effect_changed to confirm UI change.", Destructive: false},
	{Name: "desktop_click", Title: "Desktop click", Description: "Click a macOS desktop coordinate using cliclick. ok=true only means command_ok; it does not prove the UI changed. Use verify=true for before/after screenshot verification.", Destructive: true},
	{Name: "desktop_double_click", Title: "Desktop double click", Description: "Double-click a macOS desktop coordinate using cliclick. Supports app + space=window and verify=true effect verification.", Destructive: true},
	{Name: "desktop_scroll", Title: "Desktop scroll", Description: "Scroll the macOS desktop using cliclick.", Destructive: false},
	{Name: "desktop_drag", Title: "Desktop drag", Description: "Drag between macOS desktop coordinates using cliclick. Supports app + space=window, duration_ms, steps, hold_ms, release_wait_ms, and verify=true effect verification.", Destructive: true},
	{Name: "desktop_type", Title: "Desktop type text", Description: "Type text into the focused macOS app using cliclick. Experimental and permission-sensitive.", Destructive: true},
	{Name: "desktop_set_value", Title: "Desktop set value", Description: "Set a settable accessibility element value by app and element_index.", Destructive: true},
	{Name: "desktop_perform_secondary_action", Title: "Desktop accessibility action", Description: "Perform an accessibility action exposed by a UI element.", Destructive: true},
	{Name: "desktop_hotkey", Title: "Desktop hotkey", Description: "Send a keyboard shortcut to the focused macOS app using cliclick. Experimental and permission-sensitive.", Destructive: true},
	{Name: "desktop_wait", Title: "Desktop wait", Description: "Wait for a bounded duration between desktop automation steps.", ReadOnly: true},
	{Name: "workspace_repos", Title: "Workspace repositories", Description: "List Git repositories found under the workspace.", ReadOnly: true},
	{Name: "git_repo_status", Title: "Git repository status", Description: "Return Git status for a selected repository path.", ReadOnly: true},
	{Name: "git_status", Title: "Git status", Description: "Return git working tree status for the workspace.", ReadOnly: true},
	{Name: "git_diff", Title: "Git diff", Description: "Return unified git diff for workspace changes.", ReadOnly: true},
	{Name: "git_log", Title: "Git log", Description: "Return recent git commits with bounded structured metadata.", ReadOnly: true},
	{Name: "git_show", Title: "Git show", Description: "Return bounded git show output for a revision.", ReadOnly: true},
	{Name: "git_blame", Title: "Git blame", Description: "Return bounded git blame metadata for a workspace file.", ReadOnly: true},
	{Name: "git_inspect", Title: "Git inspect", Description: "Inspect Git history through one unified show/blame tool.", ReadOnly: true},
	{Name: "git_remote", Title: "Git remote", Description: "Run Git remote operations through one unified fetch/pull/push tool.", Destructive: false, OpenWorld: true},
	{Name: "git_fetch", Title: "Git fetch", Description: "Fetch updates for a selected repository.", Destructive: false, OpenWorld: true},
	{Name: "git_pull", Title: "Git pull", Description: "Pull updates for a selected repository.", Destructive: false, OpenWorld: true},
	{Name: "git_push", Title: "Git push", Description: "Push a selected repository branch to a remote.", Destructive: false, OpenWorld: true},
	{Name: "git_clone", Title: "Git clone", Description: "Clone a Git repository into the workspace.", Destructive: false, OpenWorld: true},
	{Name: "git_commit", Title: "Git commit", Description: "Create a Git commit in a selected repository.", Destructive: false},
	{Name: "request_permissions", Title: "Request permissions", Description: "Request a scoped permission grant for runtime operations that require approval.", ReadOnly: true},
	{Name: "view_image", Title: "View image", Description: "Return a workspace image as MCP image content.", ReadOnly: true},
}

func toolDefinition(name string) (ToolDefinition, bool) {
	for _, def := range toolRegistry {
		if def.Name == name {
			return def, true
		}
	}
	return ToolDefinition{}, false
}

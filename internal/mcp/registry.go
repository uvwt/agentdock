package mcp

type ToolDefinition struct {
	Name        string
	Title       string
	Description string
	ReadOnly    bool
	Destructive bool
	OpenWorld   bool
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
	{Name: "exec_command", Title: "Run workspace command", Description: "Run a bounded command in the workspace with sandbox and approval controls.", Destructive: false, OpenWorld: true},
	{Name: "write_stdin", Title: "Send session input", Description: "Send input to a running command session.", Destructive: false},
	{Name: "session_status", Title: "Session status", Description: "Return output and status for a running command session.", ReadOnly: true},
	{Name: "list_sessions", Title: "List sessions", Description: "List currently running command sessions.", ReadOnly: true},
	{Name: "kill_session", Title: "Stop session", Description: "Stop a running command session.", Destructive: false},
	{Name: "kill_all_sessions", Title: "Stop all sessions", Description: "Stop all currently running command sessions.", Destructive: false},
	{Name: "configure_github_token", Title: "Configure GitHub token", Description: "Load a GitHub token from a workspace .env file and store a redacted HTTPS credential for git.", Destructive: false},
	{Name: "check_github_repo_access", Title: "Check GitHub repo access", Description: "Check stored GitHub credential authentication and repository visibility without exposing secrets.", ReadOnly: true, OpenWorld: true},
	{Name: "github_create_repo", Title: "Create GitHub repository", Description: "Create a GitHub repository using the stored credential without exposing secrets.", Destructive: false, OpenWorld: true},
	{Name: "connector_list", Title: "List connectors", Description: "List dynamic workspace connectors from the configured connector directory.", ReadOnly: true},
	{Name: "connector_describe", Title: "Describe connector", Description: "Describe one dynamic connector and its available actions.", ReadOnly: true},
	{Name: "connector_call", Title: "Call connector", Description: "Call a dynamic workspace connector action with structured arguments.", Destructive: false, OpenWorld: true},
	{Name: "memory_bootstrap", Title: "Bootstrap MemoryDock context", Description: "Load high-priority MemoryDock context at the start of substantial AgentDock, project, deployment, debugging, or preference-sensitive tasks.", ReadOnly: true, OpenWorld: true},
	{Name: "memory_list", Title: "List MemoryDock memories", Description: "List memories from the configured MemoryDock service.", ReadOnly: true, OpenWorld: true},
	{Name: "memory_read", Title: "Read MemoryDock memory", Description: "Read one Markdown memory from the configured MemoryDock service.", ReadOnly: true, OpenWorld: true},
	{Name: "memory_search", Title: "Search MemoryDock", Description: "Search memories by text and path through MemoryDock.", ReadOnly: true, OpenWorld: true},
	{Name: "memory_pack", Title: "Pack MemoryDock project context", Description: "Return a compact project context bundle from MemoryDock.", ReadOnly: true, OpenWorld: true},
	{Name: "memory_append_note", Title: "Append MemoryDock note", Description: "Append a note to MemoryDock inbox or scoped memory area.", Destructive: false, OpenWorld: true},
	{Name: "memory_write", Title: "Write MemoryDock memory", Description: "Write a Markdown memory through MemoryDock; writing outside inbox requires confirmed=true.", Destructive: false, OpenWorld: true},
	{Name: "memory_delete", Title: "Delete MemoryDock memory", Description: "Delete a MemoryDock memory; requires confirmed=true.", Destructive: true, OpenWorld: true},
	{Name: "memory_sync_status", Title: "MemoryDock sync status", Description: "Return MemoryDock Git sync status.", ReadOnly: true, OpenWorld: true},
	{Name: "browser_session_start", Title: "Start browser session", Description: "Create a browser automation session using the optional workspace Node runner.", Destructive: false, OpenWorld: true},
	{Name: "browser_action", Title: "Run browser actions", Description: "Run browser automation actions such as goto, click, fill, wait, and screenshot snapshot.", Destructive: false, OpenWorld: true},
	{Name: "browser_snapshot", Title: "Browser snapshot", Description: "Capture the current browser page URL, title, text, screenshot, console errors, and network errors.", ReadOnly: true, OpenWorld: true},
	{Name: "browser_session_close", Title: "Close browser session", Description: "Close and remove a browser automation session.", Destructive: false, OpenWorld: true},
	{Name: "desktop_preflight", Title: "Desktop preflight", Description: "Check macOS desktop automation dependencies and likely permissions.", ReadOnly: true},
	{Name: "desktop_window_list", Title: "Desktop windows", Description: "List visible macOS applications and window titles using AppleScript.", ReadOnly: true},
	{Name: "desktop_snapshot", Title: "Desktop snapshot", Description: "Capture a macOS desktop screenshot as an AgentDock artifact.", ReadOnly: true},
	{Name: "desktop_clipboard_set", Title: "Set desktop clipboard", Description: "Set the macOS clipboard text using pbcopy.", Destructive: true},
	{Name: "desktop_clipboard_get", Title: "Get desktop clipboard", Description: "Read the macOS clipboard text using pbpaste.", ReadOnly: true},
	{Name: "desktop_focus_app", Title: "Focus desktop app", Description: "Activate a macOS application by name using AppleScript.", Destructive: false},
	{Name: "desktop_move", Title: "Desktop move pointer", Description: "Move the macOS mouse pointer to a screen coordinate using cliclick.", Destructive: false},
	{Name: "desktop_click", Title: "Desktop click", Description: "Click a macOS desktop coordinate using cliclick. Experimental and permission-sensitive.", Destructive: true},
	{Name: "desktop_double_click", Title: "Desktop double click", Description: "Double-click a macOS desktop coordinate using cliclick.", Destructive: true},
	{Name: "desktop_scroll", Title: "Desktop scroll", Description: "Scroll the macOS desktop using cliclick.", Destructive: false},
	{Name: "desktop_drag", Title: "Desktop drag", Description: "Drag from one macOS desktop coordinate to another using cliclick.", Destructive: true},
	{Name: "desktop_type", Title: "Desktop type text", Description: "Type text into the focused macOS app using cliclick. Experimental and permission-sensitive.", Destructive: true},
	{Name: "desktop_hotkey", Title: "Desktop hotkey", Description: "Send a keyboard shortcut to the focused macOS app using cliclick. Experimental and permission-sensitive.", Destructive: true},
	{Name: "desktop_wait", Title: "Desktop wait", Description: "Wait for a bounded duration between desktop automation steps.", ReadOnly: true},
	{Name: "workspace_repos", Title: "Workspace repositories", Description: "List Git repositories found under the workspace.", ReadOnly: true},
	{Name: "git_repo_status", Title: "Git repository status", Description: "Return Git status for a selected repository path.", ReadOnly: true},
	{Name: "git_status", Title: "Git status", Description: "Return git working tree status for the workspace.", ReadOnly: true},
	{Name: "git_diff", Title: "Git diff", Description: "Return unified git diff for workspace changes.", ReadOnly: true},
	{Name: "git_log", Title: "Git log", Description: "Return recent git commits with bounded structured metadata.", ReadOnly: true},
	{Name: "git_show", Title: "Git show", Description: "Return bounded git show output for a revision.", ReadOnly: true},
	{Name: "git_blame", Title: "Git blame", Description: "Return bounded git blame metadata for a workspace file.", ReadOnly: true},
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

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
	{Name: "read_file", Title: "Read file", Description: "Read a UTF-8 text file slice inside the configured workspace.", ReadOnly: true},
	{Name: "list_dir", Title: "List directory", Description: "List directory entries inside the configured workspace.", ReadOnly: true},
	{Name: "list_files", Title: "List files", Description: "List workspace files using glob and ignore filters.", ReadOnly: true},
	{Name: "search_text", Title: "Search text", Description: "Search UTF-8 workspace files for text or regex matches.", ReadOnly: true},
	{Name: "apply_patch", Title: "Update workspace files", Description: "Update workspace files using structured edits.", Destructive: false},
	{Name: "edit_file", Title: "Edit file", Description: "Replace exact UTF-8 text in one workspace file with match-count checks and diff preview.", Destructive: false},
	{Name: "exec_command", Title: "Run workspace command", Description: "Run a bounded command in the workspace with sandbox and approval controls.", Destructive: false, OpenWorld: true},
	{Name: "session_control", Title: "Control command sessions", Description: "List, inspect, write to, or stop command sessions through one unified session action tool.", Destructive: false},
	{Name: "check_github_repo_access", Title: "Check GitHub repo access", Description: "Check stored GitHub credential authentication and repository visibility without exposing secrets.", ReadOnly: true, OpenWorld: true},
	{Name: "task_manage", Title: "Manage recoverable tasks", Description: "Persist and resume substantial AgentDock tasks with create/list/get/block/resume/final_review/complete_after_review plus template_match.", Destructive: false},
	{Name: "workflow_template_manage", Title: "Manage workflow templates", Description: "List, get, save, validate, publish, retire, or match AgentDock workflow templates.", Destructive: false},
	{Name: "skill_manage", Title: "Manage AgentDock Skills", Description: "List, inspect, validate, install, run, or roll back AgentDock Skills through the local Skill Runtime.", Destructive: true, OpenWorld: true},
	{Name: "env_manage", Title: "Manage Skill environment", Description: "Manage redacted Skill environment variables through the local Nexus Env Registry.", Destructive: true, OpenWorld: true},
	{Name: "artifact_send", Title: "Send encrypted artifact", Description: "Encrypt and send a top-level file parameter or local file/directory through AgentDock Nexus to one or more registered devices. The target only writes to its controlled inbox or configured logical target and never executes the file.", Destructive: false, OpenWorld: true, FileArgRewritePaths: []string{"file"}},
	{Name: "artifact_fetch_create", Title: "Create artifact fetch", Description: "Create an asynchronous high-risk request for a registered device to list or encrypt an absolute-path file or directory under immutable deny rules.", Destructive: true, OpenWorld: true},
	{Name: "artifact_fetch_status", Title: "Artifact fetch status", Description: "Return status or a bounded directory listing for a local artifact fetch request.", ReadOnly: true, OpenWorld: true},
	{Name: "artifact_fetch_download", Title: "Download artifact fetch", Description: "Download and decrypt a ready artifact fetch, return a file resource, or confirm that the GPT sandbox mounted it so ciphertext can be deleted.", Destructive: true, OpenWorld: true, FileResultRewritePaths: []string{"file_path"}},
	{Name: "recall_bootstrap", Title: "Bootstrap RecallDock context", Description: "Load high-priority RecallDock context at the start of substantial AgentDock, project, deployment, debugging, or preference-sensitive tasks. max_bytes controls pack budget only; compact index/excerpt output is default, and full body requires include_body or targeted recall_read.", ReadOnly: true, OpenWorld: true},
	{Name: "recall_search", Title: "Search RecallDock", Description: "Search RecallDock memories, cards, and notes. Use kind=all, markdown, card, or note; when kind=note, use note_scope=questions or github-learning. Backend handles internal routing such as prefix and scope.", ReadOnly: true, OpenWorld: true},
	{Name: "recall_read", Title: "Read RecallDock entry", Description: "Read one Markdown, card, or note entry from the configured RecallDock store by path.", ReadOnly: true, OpenWorld: true},
	{Name: "recall_write", Title: "Write RecallDock entry", Description: "Plan, write, patch, update facts, or delete RecallDock content. The model must choose kind=card, note, markdown, patch, fact, delete, or explicit auto for a non-writing plan.", Destructive: true, OpenWorld: true},
	{Name: "recall_maintain", Title: "Maintain RecallDock", Description: "Run RecallDock maintenance actions such as sync_status, list, lint, embedding_status, reindex, or reindex_cards.", Destructive: true, OpenWorld: true},
	{Name: "private_notes_search", Title: "Search private notes", Description: "Search the user private notes store. Returns titles, paths, metadata, and code-redacted snippets only; use private_notes_read for full plaintext.", ReadOnly: true, OpenWorld: true},
	{Name: "private_notes_read", Title: "Read private note", Description: "Read one plaintext private note from private-notes/notes. This explicit private-note access returns full plaintext by default.", ReadOnly: true, OpenWorld: true},
	{Name: "private_notes_write", Title: "Write private note", Description: "Write a plaintext private note under private-notes/notes and always generate a mandatory age encrypted .md.age backup under private-notes/encrypted. Do not use recall_write for private material.", Destructive: false, OpenWorld: true},
	{Name: "private_notes_maintain", Title: "Maintain private notes", Description: "Initialize age encryption, list, check, sync, or migrate encrypted backups for the private-notes store. Age .md.age backups are mandatory for every plaintext note.", Destructive: false, OpenWorld: true},
	{Name: "browser_session", Title: "Browser session", Description: "Start, close, or clean up a browser automation session; supports persistent profiles and injected cookies/localStorage.", Destructive: false, OpenWorld: true},
	{Name: "browser_act", Title: "Browser actions", Description: "Run browser automation actions and capture a structured screenshot snapshot; supports close_after, inline image, and storage state save.", Destructive: false, OpenWorld: true},
	{Name: "browser_snapshot", Title: "Browser snapshot", Description: "Capture page URL, title, text, screenshot, image, viewport, visible interactive elements, and browser errors.", ReadOnly: true, OpenWorld: true},
	{Name: "browser_profile", Title: "Browser profile", Description: "Open, inspect, or close a managed browser profile.", Destructive: false, OpenWorld: true},
	{Name: "desktop_observe", Title: "Observe desktop", Description: "Unified macOS desktop observation tool for preflight, app list, app state, windows, screen snapshots, and app snapshots.", ReadOnly: true},
	{Name: "desktop_act", Title: "Act on desktop", Description: "Unified macOS desktop action tool for focus, move, click, double-click, scroll, drag, type, set value, accessibility actions, hotkeys, and waits.", Destructive: true},
	{Name: "desktop_clipboard", Title: "Desktop clipboard", Description: "Read or set the macOS clipboard text through one unified clipboard tool.", Destructive: true},
	{Name: "workspace_repos", Title: "Workspace repositories", Description: "List Git repositories found under the workspace.", ReadOnly: true},
	{Name: "git_status", Title: "Git status", Description: "Return git working tree status for the workspace.", ReadOnly: true},
	{Name: "git_diff", Title: "Git diff", Description: "Return unified git diff for workspace changes.", ReadOnly: true},
	{Name: "git_log", Title: "Git log", Description: "Return recent git commits with bounded structured metadata.", ReadOnly: true},
	{Name: "git_inspect", Title: "Git inspect", Description: "Inspect Git history through one unified show/blame tool.", ReadOnly: true},
	{Name: "git_remote", Title: "Git remote", Description: "Run Git remote operations through one unified fetch/pull/push tool.", Destructive: false, OpenWorld: true},
	{Name: "git_clone", Title: "Git clone", Description: "Clone a Git repository into the workspace.", Destructive: false, OpenWorld: true},
	{Name: "git_commit", Title: "Git commit", Description: "Create a Git commit in a selected repository.", Destructive: false},
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

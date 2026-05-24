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
	{Name: "get_default_cwd", Title: "Get default cwd", Description: "Return the current default cwd inside the workspace.", ReadOnly: true},
	{Name: "set_default_cwd", Title: "Set default cwd", Description: "Set the default cwd for relative tool paths inside the workspace.", ReadOnly: true},
	{Name: "read_file", Title: "Read file", Description: "Read a UTF-8 text file slice inside the configured workspace.", ReadOnly: true},
	{Name: "list_dir", Title: "List directory", Description: "List directory entries inside the configured workspace.", ReadOnly: true},
	{Name: "list_files", Title: "List files", Description: "List workspace files using glob and ignore filters.", ReadOnly: true},
	{Name: "search_text", Title: "Search text", Description: "Search UTF-8 workspace files for text or regex matches.", ReadOnly: true},
	{Name: "apply_patch", Title: "Apply patch", Description: "Apply a patch envelope transactionally inside the workspace.", Destructive: true},
	{Name: "exec_command", Title: "Execute command", Description: "Run a bounded command in the workspace with policy and sandbox controls.", Destructive: true, OpenWorld: true},
	{Name: "write_stdin", Title: "Write stdin", Description: "Write characters to a running command session.", Destructive: true},
	{Name: "session_status", Title: "Session status", Description: "Return output and status for a running command session.", ReadOnly: true},
	{Name: "list_sessions", Title: "List sessions", Description: "List currently running command sessions.", ReadOnly: true},
	{Name: "kill_session", Title: "Kill session", Description: "Terminate a running command session.", Destructive: true},
	{Name: "configure_github_token", Title: "Configure GitHub token", Description: "Load a GitHub token from a workspace .env file and store a redacted HTTPS credential for git.", Destructive: true},
	{Name: "check_github_repo_access", Title: "Check GitHub repo access", Description: "Check stored GitHub credential authentication and repository visibility without exposing secrets.", ReadOnly: true, OpenWorld: true},
	{Name: "git_status", Title: "Git status", Description: "Return git working tree status for the workspace.", ReadOnly: true},
	{Name: "git_diff", Title: "Git diff", Description: "Return unified git diff for workspace changes.", ReadOnly: true},
	{Name: "git_log", Title: "Git log", Description: "Return recent git commits with bounded structured metadata.", ReadOnly: true},
	{Name: "git_show", Title: "Git show", Description: "Return bounded git show output for a revision.", ReadOnly: true},
	{Name: "git_blame", Title: "Git blame", Description: "Return bounded git blame metadata for a workspace file.", ReadOnly: true},
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

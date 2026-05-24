package mcp

func inputSchema(name string) map[string]any {
	props := map[string]any{}
	required := []string{}
	stringProp := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	intProp := func(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
	boolProp := func(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }

	switch name {
	case "tool_descriptors":
	case "read_file":
		props["path"] = stringProp("Workspace-relative file path.")
		props["start_line"] = intProp("1-based start line.")
		props["end_line"] = intProp("Inclusive end line.")
		props["max_bytes"] = intProp("Maximum output bytes.")
		required = []string{"path"}
	case "list_dir":
		props["path"] = stringProp("Workspace-relative directory path.")
		props["recursive"] = boolProp("List recursively.")
		props["max_depth"] = intProp("Maximum recursive depth.")
		props["max_entries"] = intProp("Maximum entries.")
		props["include_hidden"] = boolProp("Include dotfiles.")
		props["include_ignored"] = boolProp("Include normally skipped directories.")
	case "list_files":
		props["path"] = stringProp("Workspace-relative directory path.")
		props["patterns"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["glob"] = stringProp("Single glob pattern override.")
		props["exclude_patterns"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["max_results"] = intProp("Maximum files.")
		props["include_hidden"] = boolProp("Include dotfiles.")
		props["include_ignored"] = boolProp("Include normally skipped directories.")
	case "search_text":
		props["path"] = stringProp("Workspace-relative path.")
		props["query"] = stringProp("Text or regex query.")
		props["regex"] = boolProp("Treat query as regex.")
		props["case_sensitive"] = boolProp("Use case-sensitive search.")
		props["include_globs"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["glob"] = stringProp("Single include glob.")
		props["exclude_globs"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["context_lines"] = intProp("Context lines around each match.")
		props["max_results"] = intProp("Maximum matches.")
		required = []string{"query"}
	case "apply_patch":
		props["patch"] = stringProp("Unified diff or git-apply compatible patch.")
		props["dry_run"] = boolProp("Validate patch without writing.")
		required = []string{"patch"}
	case "exec_command":
		props["cmd"] = stringProp("Command to run.")
		props["workdir"] = stringProp("Workspace-relative working directory.")
		props["timeout_ms"] = intProp("Timeout in milliseconds.")
		props["yield_time_ms"] = intProp("Initial wait before returning running session.")
		props["wait_until_exit"] = boolProp("Wait until the command exits instead of returning a running session after yield_time_ms.")
		props["max_output_bytes"] = intProp("Maximum output bytes.")
		props["stdin"] = stringProp("Initial stdin.")
		props["tty"] = boolProp("Keep stdin open.")
		props["redact_patterns"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Additional regex patterns to redact from stdout/stderr/error."}
		required = []string{"cmd"}
	case "write_stdin", "session_status", "kill_session":
		props["session_id"] = stringProp("Session id returned by exec_command.")
		props["chars"] = stringProp("Characters to write to stdin.")
		props["max_output_bytes"] = intProp("Maximum output bytes.")
		required = []string{"session_id"}
	case "kill_all_sessions":
	case "configure_github_token":
		props["env_file"] = stringProp("Workspace-relative .env file containing GITHUB_TOKEN, GH_TOKEN, GITHUB_PAT, or TOKEN.")
		props["username"] = stringProp("GitHub username to store with the HTTPS credential. Defaults to GITHUB_USERNAME, GITHUB_USER, or x-access-token.")
	case "check_github_repo_access":
		props["repo"] = stringProp("GitHub repository as owner/name or https://github.com/owner/name.git.")
		props["repository"] = stringProp("Alias for repo.")
		props["timeout_ms"] = intProp("HTTP timeout in milliseconds.")
		required = []string{"repo"}
	case "github_create_repo":
		props["name"] = stringProp("Repository name to create.")
		props["owner"] = stringProp("Optional owner or organization. Defaults to the authenticated user.")
		props["repo"] = stringProp("Optional owner/name shorthand.")
		props["description"] = stringProp("Repository description.")
		props["private"] = boolProp("Create a private repository. Defaults to true.")
		props["auto_init"] = boolProp("Create the repository with an initial commit.")
		props["timeout_ms"] = intProp("HTTP timeout in milliseconds.")
		required = []string{"name"}
	case "workspace_repos":
		props["max_depth"] = intProp("Maximum directory depth to scan for repositories.")
	case "git_repo_status", "git_status":
		props["repo_path"] = stringProp("Workspace-relative repository path. Defaults to current workspace/default cwd.")
		props["max_output_bytes"] = intProp("Maximum output bytes.")
	case "git_diff":
		props["repo_path"] = stringProp("Workspace-relative repository path.")
		props["paths"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["max_bytes"] = intProp("Maximum output bytes.")
	case "git_log":
		props["repo_path"] = stringProp("Workspace-relative repository path.")
		props["limit"] = intProp("Maximum commits.")
		props["max_bytes"] = intProp("Maximum output bytes.")
	case "git_show":
		props["repo_path"] = stringProp("Workspace-relative repository path.")
		props["rev"] = stringProp("Revision to show.")
		props["max_bytes"] = intProp("Maximum output bytes.")
	case "git_blame":
		props["repo_path"] = stringProp("Workspace-relative repository path.")
		props["path"] = stringProp("Workspace-relative file path.")
		props["max_bytes"] = intProp("Maximum output bytes.")
		required = []string{"path"}
	case "git_fetch", "git_pull", "git_push":
		props["repo_path"] = stringProp("Workspace-relative repository path.")
		props["remote"] = stringProp("Remote name. Defaults to origin.")
		props["branch"] = stringProp("Branch name. Defaults to current branch where applicable.")
		props["max_bytes"] = intProp("Maximum output bytes.")
	case "git_clone":
		props["url"] = stringProp("Git repository URL to clone.")
		props["repo"] = stringProp("Alias for url.")
		props["dest"] = stringProp("Workspace-relative destination directory.")
		props["branch"] = stringProp("Branch to clone.")
		props["depth"] = intProp("Optional shallow clone depth.")
		props["max_bytes"] = intProp("Maximum output bytes.")
		required = []string{"url"}
	case "git_commit":
		props["repo_path"] = stringProp("Workspace-relative repository path.")
		props["message"] = stringProp("Commit message.")
		props["paths"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["all"] = boolProp("Stage all changes before committing.")
		props["max_bytes"] = intProp("Maximum output bytes.")
		required = []string{"message"}
	case "set_default_cwd", "view_image":
		props["path"] = stringProp("Workspace-relative path.")
		if name == "view_image" {
			props["max_bytes"] = intProp("Maximum image bytes.")
			props["max_width"] = intProp("Maximum image width.")
			props["max_height"] = intProp("Maximum image height.")
			props["auto_resize"] = boolProp("Resize when limits are exceeded.")
			props["output"] = stringProp("mcp_image or data_url.")
		}
	case "request_permissions":
		props["tool_name"] = stringProp("Tool needing permission.")
		props["permission"] = stringProp("Permission type.")
	}

	schema := map[string]any{"type": "object", "properties": props, "additionalProperties": true}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

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

func outputSchema(name string) map[string]any {
	props := map[string]any{}
	required := []string{"ok"}
	stringProp := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	intProp := func(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
	boolProp := func(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }
	arrayProp := func(desc string) map[string]any {
		return map[string]any{"type": "array", "description": desc, "items": map[string]any{"type": "object", "additionalProperties": true}}
	}
	objectProp := func(desc string) map[string]any {
		return map[string]any{"type": "object", "description": desc, "additionalProperties": true}
	}

	props["ok"] = boolProp("Whether the tool call completed successfully.")

	switch name {
	case "tool_descriptors":
		props["tools"] = arrayProp("Runtime-visible tool descriptors.")
		props["count"] = intProp("Tool count.")
	case "server_info":
		props["server"] = stringProp("Server identifier.")
		props["title"] = stringProp("Human-readable server title.")
		props["version"] = stringProp("Server version.")
		props["workspace"] = stringProp("Workspace root path.")
		props["default_cwd"] = stringProp("Default workspace-relative cwd.")
		props["tools"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["tool_count"] = intProp("Number of exposed tools.")
		props["sandbox"] = objectProp("Sandbox status metadata.")
	case "get_default_cwd", "set_default_cwd":
		props["path"] = stringProp("Workspace-relative cwd path.")
	case "read_file":
		props["path"] = stringProp("Workspace-relative file path.")
		props["content"] = stringProp("Text content slice.")
		props["encoding"] = stringProp("Detected text encoding.")
		props["size_bytes"] = intProp("File size in bytes.")
		props["truncated"] = boolProp("Whether output was truncated.")
		props["start_line"] = intProp("Returned start line.")
		props["end_line"] = intProp("Returned end line.")
		props["total_lines"] = intProp("Total line count.")
	case "list_dir":
		props["path"] = stringProp("Workspace-relative directory path.")
		props["entries"] = arrayProp("Directory entries.")
		props["truncated"] = boolProp("Whether entries were truncated.")
	case "list_files":
		props["path"] = stringProp("Workspace-relative directory path.")
		props["files"] = arrayProp("Matched files.")
		props["truncated"] = boolProp("Whether files were truncated.")
	case "search_text":
		props["matches"] = arrayProp("Text search matches.")
		props["truncated"] = boolProp("Whether matches were truncated.")
	case "apply_patch":
		props["summary"] = stringProp("Patch result summary.")
		props["dry_run"] = boolProp("Whether this was a dry run.")
	case "exec_command", "write_stdin", "session_status", "kill_session":
		props["session_id"] = stringProp("Command session id.")
		props["status"] = stringProp("Session status.")
		props["stdout"] = stringProp("Captured stdout segment.")
		props["stderr"] = stringProp("Captured stderr segment.")
		props["exit_code"] = intProp("Process exit code, when available.")
		props["elapsed_ms"] = intProp("Session elapsed milliseconds.")
		props["timed_out"] = boolProp("Whether the command timed out.")
		props["diagnostic"] = objectProp("Structured diagnostic for common failures.")
	case "list_sessions", "kill_all_sessions":
		props["sessions"] = arrayProp("Running command sessions.")
		props["count"] = intProp("Number of running sessions.")
	case "configure_github_token":
		props["token_found"] = boolProp("Whether a supported token variable was found.")
		props["username"] = stringProp("Stored GitHub username.")
		props["credential_helper"] = stringProp("Configured git credential helper.")
		props["password_stored"] = boolProp("Whether a token was stored without exposing it.")
	case "check_github_repo_access":
		props["credential_found"] = boolProp("Whether a stored GitHub credential was found.")
		props["username"] = stringProp("Stored GitHub username.")
		props["repo"] = stringProp("Checked repository.")
		props["auth_status"] = intProp("GitHub API auth HTTP status.")
		props["auth_login"] = stringProp("Authenticated GitHub login.")
		props["repo_status"] = intProp("GitHub API repo HTTP status.")
		props["repo_access"] = boolProp("Whether the repository is visible to the token.")
		props["diagnostic"] = objectProp("Structured access diagnostic.")
	case "workspace_repos":
		props["repos"] = arrayProp("Git repositories found under the workspace.")
		props["count"] = intProp("Repository count.")
	case "git_status", "git_repo_status":
		props["command"] = stringProp("Executed git command.")
		props["output"] = stringProp("Raw git output.")
		props["repo_path"] = stringProp("Workspace-relative repository path.")
		props["branch"] = stringProp("Branch status line.")
		props["upstream"] = stringProp("Upstream branch, when configured.")
		props["ahead"] = intProp("Commits ahead of upstream.")
		props["behind"] = intProp("Commits behind upstream.")
		props["files"] = arrayProp("Changed files.")
		props["clean"] = boolProp("Whether the worktree is clean.")
	case "git_diff", "git_log", "git_show", "git_blame", "git_fetch", "git_pull", "git_push", "git_clone", "git_commit":
		props["command"] = stringProp("Executed git command.")
		props["output"] = stringProp("Raw git output.")
		props["repo_path"] = stringProp("Workspace-relative repository path, when applicable.")
		props["truncated"] = boolProp("Whether output was truncated.")
		props["diagnostic"] = objectProp("Structured diagnostic for common failures.")
		if name == "git_diff" {
			props["files"] = arrayProp("Files in the diff.")
		}
		if name == "git_log" {
			props["commits"] = arrayProp("Parsed commits.")
		}
	case "request_permissions":
		props["permission_request"] = objectProp("Permission request metadata.")
	case "view_image":
		props["path"] = stringProp("Workspace-relative image path.")
		props["mime_type"] = stringProp("Image MIME type.")
		props["width"] = intProp("Image width.")
		props["height"] = intProp("Image height.")
		props["data_base64"] = stringProp("Base64-encoded image data when returned.")
	}

	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": true}
}

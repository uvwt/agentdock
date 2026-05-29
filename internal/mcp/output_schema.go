package mcp

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
	case "github_create_repo":
		props["credential_found"] = boolProp("Whether a stored GitHub credential was found.")
		props["username"] = stringProp("Stored GitHub username.")
		props["auth_login"] = stringProp("Authenticated GitHub login.")
		props["status"] = intProp("GitHub API HTTP status.")
		props["created"] = boolProp("Whether the repository was created.")
		props["full_name"] = stringProp("Created repository full name.")
		props["html_url"] = stringProp("Repository web URL.")
		props["clone_url"] = stringProp("Repository HTTPS clone URL.")
		props["ssh_url"] = stringProp("Repository SSH clone URL.")
		props["diagnostic"] = objectProp("Structured create diagnostic.")
	case "connector_list":
		props["connector_dir"] = stringProp("Workspace-relative connector directory.")
		props["connectors"] = arrayProp("Available connectors.")
		props["count"] = intProp("Connector count.")
	case "connector_describe":
		props["connector"] = stringProp("Connector name.")
		props["path"] = stringProp("Workspace-relative connector path.")
		props["description"] = stringProp("Connector description.")
		props["version"] = stringProp("Connector version.")
		props["actions"] = objectProp("Available connector actions.")
		props["secrets"] = objectProp("Declared secret environment variable presence.")
	case "connector_call":
		props["connector"] = stringProp("Connector name.")
		props["action"] = stringProp("Connector action name.")
		props["stdout"] = stringProp("Connector action output.")
		props["json"] = objectProp("Parsed JSON output when output=json.")
		props["duration_ms"] = intProp("Execution duration in milliseconds.")
		props["truncated"] = boolProp("Whether output was truncated.")
	case "memory_list":
		props["memory_endpoint"] = stringProp("Configured MemoryDock endpoint.")
		props["entries"] = arrayProp("Memory entries.")
		props["count"] = intProp("Memory entry count.")
	case "memory_read", "memory_write", "memory_append_note":
		props["memory_endpoint"] = stringProp("Configured MemoryDock endpoint.")
		props["memory"] = objectProp("Memory document returned by MemoryDock.")
	case "memory_search":
		props["memory_endpoint"] = stringProp("Configured MemoryDock endpoint.")
		props["query"] = stringProp("Search query.")
		props["results"] = arrayProp("Search results.")
		props["count"] = intProp("Search result count.")
	case "memory_bootstrap", "memory_pack":
		props["memory_endpoint"] = stringProp("Configured MemoryDock endpoint.")
		props["project"] = stringProp("Project key.")
		props["sections"] = arrayProp("Packed memory sections.")
		props["count"] = intProp("Section count.")
		props["bytes"] = intProp("Combined bytes.")
	case "memory_delete":
		props["memory_endpoint"] = stringProp("Configured MemoryDock endpoint.")
		props["path"] = stringProp("Deleted memory path.")
	case "memory_sync_status":
		props["memory_endpoint"] = stringProp("Configured MemoryDock endpoint.")
		props["git_repo"] = boolProp("Whether MemoryDock store is a Git repository.")
		props["auto_sync_enabled"] = boolProp("Whether MemoryDock auto sync is enabled.")
		props["pending_push"] = boolProp("Whether MemoryDock has pending push work.")
		props["last_pull_at"] = stringProp("Last pull timestamp.")
		props["last_push_at"] = stringProp("Last push timestamp.")
		props["last_error"] = stringProp("Last sync error.")
		props["conflict"] = boolProp("Whether MemoryDock detected a sync conflict.")
	case "browser_session_start", "browser_session_close":
		props["operation"] = stringProp("Browser operation name.")
		props["session_id"] = stringProp("Browser session id.")
		props["status"] = stringProp("Browser session status.")
		props["stdout"] = stringProp("Raw browser runner output.")
	case "browser_action", "browser_snapshot":
		props["operation"] = stringProp("Browser operation name.")
		props["session_id"] = stringProp("Browser session id.")
		props["url"] = stringProp("Current page URL.")
		props["title"] = stringProp("Current page title.")
		props["text"] = stringProp("Current page body text excerpt.")
		props["screenshot_path"] = stringProp("Workspace-relative screenshot path.")
		props["console_errors"] = arrayProp("Browser console errors captured during the operation.")
		props["network_errors"] = arrayProp("Network request failures captured during the operation.")
		props["stdout"] = stringProp("Raw browser runner output.")
		props["truncated"] = boolProp("Whether output was truncated.")
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
		props["size_bytes"] = intProp("Image byte size after optional resize checks.")
		props["original"] = objectProp("Original image metadata before optional resize.")
		props["resized"] = boolProp("Whether the image was resized for validation.")
		props["warnings"] = arrayProp("Warnings emitted while inspecting the image.")
		props["output"] = stringProp("Always metadata; inline image bytes are omitted.")
		props["data_omitted"] = boolProp("Whether binary image data was omitted from the response.")
		props["omitted_reason"] = stringProp("Why binary image data was omitted.")
	}

	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": true}
}

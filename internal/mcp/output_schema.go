package mcp

func outputSchema(name string) map[string]any {
	props := map[string]any{}
	required := []string{"ok"}
	stringProp := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	intProp := func(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
	floatProp := func(desc string) map[string]any { return map[string]any{"type": "number", "description": desc} }
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
	case "plugin_list":
		props["plugin_dir"] = stringProp("Workspace-relative plugin directory.")
		props["plugins"] = arrayProp("Available plugins.")
		props["count"] = intProp("Plugin count.")
	case "plugin_describe":
		props["plugin"] = stringProp("Plugin name.")
		props["path"] = stringProp("Workspace-relative plugin path.")
		props["description"] = stringProp("Plugin description.")
		props["version"] = stringProp("Plugin version.")
		props["actions"] = objectProp("Available plugin actions.")
		props["secrets"] = objectProp("Declared secret environment variable presence.")
	case "plugin_call":
		props["plugin"] = stringProp("Plugin name.")
		props["action"] = stringProp("Plugin action name.")
		props["stdout"] = stringProp("Plugin action output.")
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
	case "memory_diff", "memory_patch", "memory_update_fact":
		props["memory_endpoint"] = stringProp("Configured MemoryDock endpoint.")
		props["path"] = stringProp("Memory path.")
		props["changed"] = boolProp("Whether the proposed edit changes content.")
		props["dry_run"] = boolProp("Whether the operation only previewed changes.")
		props["confirmed"] = boolProp("Whether write confirmation was supplied.")
		props["written"] = boolProp("Whether the memory was written.")
		props["diff"] = stringProp("Unified diff preview.")
		props["truncated"] = boolProp("Whether the diff or findings were truncated.")
		props["updates"] = arrayProp("Fact update results.")
	case "memory_lint":
		props["memory_endpoint"] = stringProp("Configured MemoryDock endpoint.")
		props["terms"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["files_scanned"] = intProp("Files scanned.")
		props["finding_count"] = intProp("Finding count.")
		props["findings"] = arrayProp("Lint findings.")
		props["truncated"] = boolProp("Whether findings were truncated.")
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
	case "desktop_list_apps":
		props["running"] = arrayProp("Running visible applications.")
		props["recent"] = arrayProp("Best-effort recently used applications from Spotlight metadata.")
		props["count"] = intProp("Running application count.")
	case "desktop_get_app_state":
		props["app"] = stringProp("Requested app.")
		props["resolved_app"] = stringProp("Resolved running app name.")
		props["bundle_id"] = stringProp("App bundle identifier when available.")
		props["pid"] = intProp("Process id when available.")
		props["frontmost"] = boolProp("Whether the app is frontmost.")
		props["window"] = objectProp("Key window metadata.")
		props["accessibility_ok"] = boolProp("Whether accessibility tree capture succeeded.")
		props["accessibility_tree"] = arrayProp("Accessibility tree nodes with element indexes.")
		props["node_count"] = intProp("Accessibility tree node count.")
		props["coordinate_space"] = objectProp("Coordinate space metadata for screenshots and actions.")
		props["screenshot_path"] = stringProp("Saved original screenshot path.")
		props["screenshot_url"] = stringProp("Artifact URL for the original screenshot, when configured.")
		props["screenshot_artifact_id"] = stringProp("Screenshot artifact id.")
		props["mime_type"] = stringProp("Original screenshot MIME type.")
		props["width"] = intProp("Original screenshot width.")
		props["height"] = intProp("Original screenshot height.")
		props["size_bytes"] = intProp("Original screenshot size in bytes.")
		props["image_attached"] = boolProp("Whether compressed image content was attached to the MCP response.")
		props["image_base64"] = stringProp("Compressed Base64 image data when image_attached is true.")
		props["image_mime_type"] = stringProp("Compressed image MIME type when image_attached is true.")
		props["image_width"] = intProp("Compressed image width when image_attached is true.")
		props["image_height"] = intProp("Compressed image height when image_attached is true.")
		props["image_size_bytes"] = intProp("Compressed image size in bytes when image_attached is true.")
		props["original"] = objectProp("Original/crop metadata for attached image processing.")
		props["image_warnings"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	case "desktop_snapshot", "desktop_snapshot_app":
		props["operation"] = stringProp("Desktop operation name.")
		props["screenshot_path"] = stringProp("Saved screenshot path.")
		props["screenshot_url"] = stringProp("Artifact URL for the screenshot, when configured.")
		props["screenshot_artifact_id"] = stringProp("Screenshot artifact id.")
		props["mime_type"] = stringProp("Screenshot MIME type.")
		props["width"] = intProp("Screenshot width in pixels.")
		props["height"] = intProp("Screenshot height in pixels.")
		props["size_bytes"] = intProp("Screenshot size in bytes.")
		props["target_window"] = objectProp("Target app window metadata in macOS points, when applicable.")
		props["crop"] = objectProp("Window-relative crop rectangle in macOS points, when applicable.")
		props["screen_region"] = objectProp("Absolute screen region in macOS points used for capture, when applicable.")
		props["action_coordinate_space"] = stringProp("Coordinate space used by desktop action inputs, typically screen_points or window_points.")
		props["screenshot_coordinate_space"] = stringProp("Coordinate space of returned image, typically image_pixels.")
	case "desktop_click", "desktop_double_click", "desktop_move", "desktop_scroll", "desktop_drag", "desktop_type", "desktop_set_value", "desktop_perform_secondary_action", "desktop_hotkey":
		props["operation"] = stringProp("Desktop operation name.")
		props["command_ok"] = boolProp("Whether the underlying command exited successfully.")
		props["permission_ok"] = boolProp("Whether known macOS permission warnings were absent.")
		props["effect_verified"] = boolProp("Whether before/after screenshot verification ran successfully.")
		props["effect_changed"] = boolProp("Whether verification detected enough screenshot change after the command.")
		props["verification"] = stringProp("Verification mode/status, for example not_requested, screenshot_diff, or diff_failed.")
		props["diff_percent"] = floatProp("Approximate screenshot difference ratio when screenshot_diff verification ran.")
		props["diff_score"] = floatProp("Alias for diff_percent; simple screenshot difference ratio between 0 and 1.")
		props["before_snapshot_path"] = stringProp("Before screenshot path when captured.")
		props["after_snapshot_path"] = stringProp("After screenshot path when captured.")
		props["before_screenshot_path"] = stringProp("Alias for before_snapshot_path.")
		props["after_screenshot_path"] = stringProp("Alias for after_snapshot_path.")
		props["target_window"] = objectProp("Target app window metadata in macOS points, when app is provided.")
		props["action_coordinate_space"] = stringProp("Input coordinate mode: screen or window.")
		props["error_layer"] = stringProp("Failure layer: validation, focus, window, command, screenshot, verification, or runtime.")
		props["error_code"] = stringProp("Structured error code for known desktop failures.")
		props["warnings"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	case "request_permissions":
		props["permission_request"] = objectProp("Permission request metadata.")
	case "view_image":
		props["path"] = stringProp("Workspace-relative image path.")
		props["mime_type"] = stringProp("Returned image MIME type.")
		props["width"] = intProp("Returned image width.")
		props["height"] = intProp("Returned image height.")
		props["size_bytes"] = intProp("Returned image size in bytes after optional crop/resize/compression.")
		props["original"] = objectProp("Original/crop metadata.")
		props["resized"] = boolProp("Whether image bytes changed due to crop/resize/re-encode.")
		props["warnings"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["image_attached"] = boolProp("Whether MCP image content is attached.")
		props["data_base64"] = stringProp("Base64-encoded image data when returned.")
		props["data_url"] = stringProp("Data URL when output=data_url.")
	}

	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": true}
}

func addDesktopActionOutputProps(props map[string]any, stringProp func(string) map[string]any, intProp func(string) map[string]any, boolProp func(string) map[string]any, objectProp func(string) map[string]any, arrayProp func(string) map[string]any) {
	props["operation"] = stringProp("Desktop operation name.")
	props["command_ok"] = boolProp("Whether cliclick command execution succeeded. This does not prove the UI changed.")
	props["effect_verified"] = boolProp("Whether before/after screenshot verification ran successfully.")
	props["effect_changed"] = boolProp("Whether verification detected a screenshot change after the command.")
	props["verification"] = stringProp("Verification status such as not_requested, byte_diff, sha256_equal, before_screenshot_failed, after_screenshot_failed, or diff_failed.")
	props["error_layer"] = stringProp("Failure layer: validation, focus, window, command, screenshot, verification, or runtime.")
	props["before_screenshot_path"] = stringProp("Before screenshot path when verify=true.")
	props["after_screenshot_path"] = stringProp("After screenshot path when verify=true.")
	props["diff_score"] = map[string]any{"type": "number", "description": "Pixel-level screenshot difference ratio between 0 and 1."}
	props["changed_pixels"] = intProp("Number of changed pixels detected by pixel-level screenshot diff.")
	props["total_pixels"] = intProp("Total pixel denominator used for diff_score.")
	props["changed_bounds"] = objectProp("Bounding box of changed pixels in image coordinates when a change is detected.")
	props["size_mismatch"] = boolProp("Whether before/after screenshots had different pixel dimensions.")
	props["target_window"] = objectProp("Target app window metadata in macOS points when app is provided.")
	props["action_coordinate_space"] = stringProp("Input coordinate mode: screen or window.")
	props["points"] = arrayProp("Actual global macOS point coordinates sent to cliclick.")
	props["input_points"] = arrayProp("Original caller coordinates before window-relative conversion, when different from points.")
	props["stdout"] = stringProp("Captured command stdout/stderr.")
}

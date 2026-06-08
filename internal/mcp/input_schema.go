package mcp

func inputSchema(name string) map[string]any {
	props := map[string]any{}
	required := []string{}
	stringProp := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	intProp := func(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
	boolProp := func(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }
	objectProp := func(desc string) map[string]any {
		return map[string]any{"type": "object", "description": desc, "additionalProperties": true}
	}
	arrayProp := func(desc string) map[string]any {
		return map[string]any{"type": "array", "description": desc, "items": map[string]any{"type": "object"}}
	}

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
		props["include_hidden"] = boolProp("Include hidden files and directories.")
		props["include_globs"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["glob"] = stringProp("Single include glob.")
		props["exclude_globs"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["context_lines"] = intProp("Context lines around each match.")
		props["max_results"] = intProp("Maximum matches.")
		required = []string{"query"}
	case "apply_patch":
		props["patch"] = stringProp("Unified diff or git-apply compatible patch.")
		props["workdir"] = stringProp("Workspace-relative directory to apply the patch from. Defaults to current workspace/default cwd.")
		props["repo_path"] = stringProp("Alias for workdir when applying a patch inside a specific repository.")
		props["dry_run"] = boolProp("Validate patch without writing.")
		required = []string{"patch"}
	case "edit_file":
		props["path"] = stringProp("Workspace-relative file path.")
		props["old"] = stringProp("Exact UTF-8 text to replace.")
		props["new"] = stringProp("Replacement UTF-8 text.")
		props["replace_all"] = boolProp("Replace every match instead of only the first.")
		props["expected_matches"] = intProp("Required number of matches. Defaults to 1.")
		props["dry_run"] = boolProp("Preview the edit without writing.")
		props["max_diff_bytes"] = intProp("Maximum diff preview bytes.")
		required = []string{"path", "old", "new"}
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
	case "session_control":
		props["action"] = stringProp("Session action: list, status, write, kill, or kill_all.")
		props["session_id"] = stringProp("Session id returned by exec_command, required for status/write/kill.")
		props["chars"] = stringProp("Characters to write when action=write.")
		props["max_output_bytes"] = intProp("Maximum output bytes.")
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
	case "skill_manage":
		props["action"] = map[string]any{"type": "string", "description": "Skill action: list, inspect, install, run, or rollback.", "enum": []string{"list", "inspect", "install", "run", "rollback"}}
		props["skill"] = stringProp("Skill name for inspect, run, or rollback.")
		props["version"] = stringProp("Optional installed Skill version.")
		props["channel"] = map[string]any{"type": "string", "description": "Skill channel: development, canary, stable, or pinned.", "enum": []string{"development", "canary", "stable", "pinned"}}
		props["source"] = stringProp("Workspace/host path or HTTP(S) URL for install.")
		props["digest"] = stringProp("Optional expected SHA-256 digest for install.")
		props["activate"] = boolProp("Activate the installed version. Defaults to true.")
		props["confirmed_no_env"] = boolProp("Required for installing a Skill with no manifest permissions.env/secrets or compat env declarations; confirms the Skill needs no Env Manager configuration.")
		props["max_bytes"] = intProp("Maximum install package bytes.")
		props["operation"] = stringProp("Skill operation name for run.")
		props["input"] = map[string]any{"description": "JSON input value for the Skill operation."}
		props["input_json"] = stringProp("Alternative raw JSON input string for the Skill operation.")
		props["binding"] = stringProp("Optional binding name for run.")
		props["run_id"] = stringProp("Optional run identifier.")
		props["timeout_ms"] = intProp("Optional run timeout in milliseconds, capped by the operation timeout.")
		props["max_output_bytes"] = intProp("Maximum stdout/stderr bytes for run.")
		required = []string{"action"}
	case "env_manage":
		props["action"] = map[string]any{"type": "string", "description": "Env action: list, inspect, set, delete, verify, or migrate-from-agentdock-env.", "enum": []string{"list", "inspect", "set", "delete", "verify", "migrate-from-agentdock-env"}}
		props["skill"] = stringProp("Skill name for inspect, set, delete, or verify.")
		props["name"] = stringProp("Environment variable name for set/delete.")
		props["kind"] = map[string]any{"type": "string", "description": "Variable kind.", "enum": []string{"plain", "secret"}}
		props["value"] = stringProp("Variable value for set. Responses never echo this value.")
		props["operation"] = stringProp("Skill operation to run for verify. Defaults to status.")
		props["input_json"] = stringProp("Optional raw JSON input for verify operation.")
		props["binding"] = stringProp("Optional binding name for verify.")
		props["version"] = stringProp("Optional Skill version for verify.")
		props["channel"] = stringProp("Optional Skill channel for verify.")
		props["timeout_ms"] = intProp("Optional verify timeout in milliseconds.")
		props["max_output_bytes"] = intProp("Maximum verify stdout/stderr bytes.")
		props["env_file"] = stringProp("Path to agentdock.env for migrate-from-agentdock-env. Defaults to ~/agentdock-runtime/agentdock.env.")
		required = []string{"action"}
	case "memory_bootstrap":
		props["project"] = stringProp("Project key to bootstrap. Defaults to agentdock.")
		props["max_bytes"] = intProp("Maximum combined memory bytes. Defaults to 50000.")
	case "memory_list":
		props["prefix"] = stringProp("Optional memory-relative prefix to list, for example shared/projects/agentdock.")
		props["max_entries"] = intProp("Maximum entries to return.")
	case "memory_read":
		props["path"] = stringProp("Memory-relative Markdown/text path.")
		required = []string{"path"}
	case "memory_search":
		props["query"] = stringProp("Text query to search in MemoryDock files and paths.")
		props["prefix"] = stringProp("Optional memory-relative prefix to search under.")
		props["max_results"] = intProp("Maximum results to return.")
		required = []string{"query"}
	case "memory_pack":
		props["project"] = stringProp("Project key to pack, for example agentdock.")
		props["max_bytes"] = intProp("Maximum combined memory bytes.")
	case "memory_append_note":
		props["content"] = stringProp("Markdown note content to append.")
		props["scope"] = stringProp("Memory scope directory. Defaults to inbox.")
		props["name"] = stringProp("Optional file name. Defaults to timestamp-note.md.")
		required = []string{"content"}
	case "memory_edit":
		props["action"] = stringProp("Memory edit action: append_note, write, delete, diff, patch, or update_fact.")
		props["path"] = stringProp("Memory-relative Markdown/text path.")
		props["content"] = stringProp("Full proposed content, write content, or section replacement content.")
		props["old"] = stringProp("Literal text to replace.")
		props["new"] = stringProp("Replacement text for old.")
		props["pattern"] = stringProp("Regular expression pattern to replace.")
		props["replacement"] = stringProp("Replacement for pattern.")
		props["section"] = stringProp("Markdown heading title whose section body should be replaced.")
		props["section_content"] = stringProp("New body for the selected Markdown section.")
		props["append"] = stringProp("Text to append to the memory.")
		props["prepend"] = stringProp("Text to prepend to the memory.")
		props["operations"] = map[string]any{"type": "array", "description": "Patch operations.", "items": map[string]any{"type": "object", "additionalProperties": true}}
		props["facts"] = objectProp("Multiple key/value facts to update.")
		props["key"] = stringProp("Fact key to update.")
		props["value"] = stringProp("New fact value.")
		props["dry_run"] = boolProp("Preview without writing where supported.")
		props["confirmed"] = boolProp("Required for writes/deletes outside safe defaults.")
		props["all"] = boolProp("Replace all matches for old or pattern. Defaults to true.")
		props["max_bytes"] = intProp("Maximum diff/output bytes.")
		props["name"] = stringProp("Optional note file name when action=append_note.")
		props["scope"] = stringProp("Memory scope directory when action=append_note.")
	case "memory_write":
		props["path"] = stringProp("Memory-relative Markdown/text path. Writing outside inbox requires confirmed=true.")
		props["content"] = stringProp("Markdown content to write. Frontmatter is added by MemoryDock if omitted.")
		props["type"] = stringProp("Memory type for generated frontmatter.")
		props["scope"] = stringProp("Memory scope for generated frontmatter.")
		props["project"] = stringProp("Project key for generated frontmatter.")
		props["source"] = stringProp("Source for generated frontmatter.")
		props["confidence"] = stringProp("Confidence for generated frontmatter.")
		props["tags"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["confirmed"] = boolProp("Required when writing outside inbox.")
		props["overwrite"] = boolProp("Replace existing memory file.")
		required = []string{"content"}
	case "memory_delete":
		props["path"] = stringProp("Memory-relative Markdown/text path.")
		props["confirmed"] = boolProp("Required to delete a memory file.")
		required = []string{"path", "confirmed"}
	case "memory_sync_status":
	case "memory_diff", "memory_patch":
		props["path"] = stringProp("Memory-relative Markdown/text path.")
		props["content"] = stringProp("Full proposed content for diff, or section replacement content when section is provided.")
		props["old"] = stringProp("Literal text to replace.")
		props["new"] = stringProp("Replacement text for old.")
		props["pattern"] = stringProp("Regular expression pattern to replace.")
		props["replacement"] = stringProp("Replacement for pattern.")
		props["section"] = stringProp("Markdown heading title whose section body should be replaced.")
		props["section_content"] = stringProp("New body for the selected Markdown section.")
		props["append"] = stringProp("Text to append to the memory.")
		props["prepend"] = stringProp("Text to prepend to the memory.")
		props["operations"] = map[string]any{"type": "array", "description": "Patch operations. Each item supports type/op/kind values replace_text, replace_regex, replace_section, append, or prepend.", "items": map[string]any{"type": "object", "additionalProperties": true}}
		props["all"] = boolProp("Replace all matches for old or pattern. Defaults to true.")
		props["dry_run"] = boolProp("Preview without writing. memory_patch defaults to dry-run unless confirmed=true.")
		props["confirmed"] = boolProp("Required for memory_patch to write.")
		props["max_bytes"] = intProp("Maximum diff bytes.")
		required = []string{"path"}
	case "memory_update_fact":
		props["path"] = stringProp("Memory-relative Markdown/text path.")
		props["section"] = stringProp("Optional Markdown heading title to limit the update.")
		props["key"] = stringProp("Fact key to update, for example plugin_dir.")
		props["value"] = stringProp("New fact value.")
		props["facts"] = objectProp("Multiple key/value facts to update.")
		props["append_if_missing"] = boolProp("Append missing facts instead of failing.")
		props["dry_run"] = boolProp("Preview without writing. Defaults to true unless confirmed=true.")
		props["confirmed"] = boolProp("Required to write changes.")
		props["max_bytes"] = intProp("Maximum diff bytes.")
		required = []string{"path"}
	case "memory_lint":
		props["prefix"] = stringProp("Optional memory-relative prefix to scan.")
		props["terms"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Terms or regex patterns to scan for."}
		props["regex"] = boolProp("Treat terms as regex patterns.")
		props["max_entries"] = intProp("Maximum memory entries to scan.")
		props["max_findings"] = intProp("Maximum findings to return.")
	case "browser_session":
		props["action"] = stringProp("Browser session action: start or close.")
		props["url"] = stringProp("Initial URL when action=start. Defaults to about:blank.")
		props["headless"] = boolProp("Run browser headless. Defaults to true.")
		props["viewport"] = objectProp("Viewport object, for example {width:1280,height:800}.")
		props["session_id"] = stringProp("Browser session id.")
		props["timeout_ms"] = intProp("Operation timeout in milliseconds.")
	case "browser_act":
		props["session_id"] = stringProp("Browser session id.")
		props["actions"] = arrayProp("Actions to run: goto, click, fill, press, wait, wait_for_selector, select, scroll, reload, back, forward, evaluate.")
		props["full_page"] = boolProp("Capture full-page screenshot in the final snapshot.")
		props["max_text_chars"] = intProp("Maximum body text characters in snapshot.")
		props["include_screenshot_base64"] = boolProp("Include screenshot_base64 and screenshot_mime_type in the response. Disabled by default because screenshots can be large.")
		props["timeout_ms"] = intProp("Operation timeout in milliseconds.")
		required = []string{"session_id", "actions"}
	case "browser_session_start":
		props["url"] = stringProp("Initial URL. Defaults to about:blank.")
		props["headless"] = boolProp("Run browser headless. Defaults to true.")
		props["viewport"] = objectProp("Viewport object, for example {width:1280,height:800}.")
		props["session_id"] = stringProp("Optional caller-provided session id.")
		props["timeout_ms"] = intProp("Operation timeout in milliseconds.")
	case "browser_action":
		props["session_id"] = stringProp("Browser session id.")
		props["actions"] = arrayProp("Actions to run: goto, click, fill, press, wait, wait_for_selector, select, scroll, reload, back, forward, evaluate.")
		props["full_page"] = boolProp("Capture full-page screenshot in the final snapshot.")
		props["max_text_chars"] = intProp("Maximum body text characters in snapshot.")
		props["include_screenshot_base64"] = boolProp("Include screenshot_base64 and screenshot_mime_type in the response. Disabled by default because screenshots can be large.")
		props["timeout_ms"] = intProp("Operation timeout in milliseconds.")
		required = []string{"session_id", "actions"}
	case "browser_snapshot", "browser_session_close":
		props["session_id"] = stringProp("Browser session id.")
		props["full_page"] = boolProp("Capture full-page screenshot for snapshot.")
		props["max_text_chars"] = intProp("Maximum body text characters in snapshot.")
		props["include_screenshot_base64"] = boolProp("Include screenshot_base64 and screenshot_mime_type in the response. Disabled by default because screenshots can be large.")
		props["timeout_ms"] = intProp("Operation timeout in milliseconds.")
		required = []string{"session_id"}
	case "desktop_observe":
		props["action"] = stringProp("Observation action: preflight, list_apps, app_state, window_list, snapshot, or snapshot_app.")
		props["app"] = stringProp("App name, full .app path, or bundle identifier for app_state/snapshot_app.")
		props["activate"] = boolProp("Activate the app before reading state. Defaults to true for app_state.")
		props["include_image"] = boolProp("Attach a compressed screenshot as MCP image content where supported.")
		props["include_image_base64"] = boolProp("Alias for include_image.")
		props["max_bytes"] = intProp("Maximum encoded image bytes.")
		props["max_width"] = intProp("Maximum image width.")
		props["max_height"] = intProp("Maximum image height.")
		props["format"] = stringProp("Output image format: jpeg or png.")
		props["quality"] = intProp("JPEG quality when format is jpeg.")
		props["crop"] = map[string]any{"type": "object", "description": "Optional crop rectangle.", "additionalProperties": true}
		props["ax_max_depth"] = intProp("Maximum accessibility tree depth for app_state.")
		props["ax_max_nodes"] = intProp("Maximum accessibility tree nodes for app_state.")
		props["max_recent"] = intProp("Maximum recent apps for list_apps.")
		props["check_screenshot"] = boolProp("Try a real screencapture check for preflight.")
		props["check_applescript"] = boolProp("Try a real AppleScript/System Events check for preflight.")
	case "desktop_act":
		props["action"] = stringProp("Desktop action: focus, move, click, double_click, scroll, drag, type, set_value, secondary_action, hotkey, or wait.")
		props["app"] = stringProp("Optional app name, full .app path, or bundle identifier.")
		props["element_index"] = stringProp("Accessibility element index from desktop_observe action=app_state.")
		props["x"] = intProp("X coordinate for move/click.")
		props["y"] = intProp("Y coordinate for move/click.")
		props["from_x"] = intProp("Start X coordinate for drag.")
		props["from_y"] = intProp("Start Y coordinate for drag.")
		props["to_x"] = intProp("End X coordinate for drag.")
		props["to_y"] = intProp("End Y coordinate for drag.")
		props["space"] = stringProp("Coordinate space: screen or window.")
		props["verify"] = boolProp("When true, capture before/after screenshots and report effect verification.")
		props["verify_region"] = map[string]any{"type": "object", "description": "Optional screenshot diff region.", "additionalProperties": true}
		props["wait_ms"] = intProp("Milliseconds to wait before after-screenshot.")
		props["focus_if_needed"] = boolProp("Activate the target app before the action.")
		props["require_frontmost"] = boolProp("Fail if the target app is not frontmost after optional focus.")
		props["fail_if_window_not_visible"] = boolProp("Fail when app is set but no visible app window can be found.")
		props["fail_if_coordinate_outside_window"] = boolProp("With space=window, fail when coordinates are outside the target window bounds.")
		props["click_count"] = intProp("Click count for click.")
		props["mouse_button"] = stringProp("Mouse button for click: left, right, or middle.")
		props["direction"] = stringProp("Scroll direction: up, down, left, or right.")
		props["pages"] = intProp("Scroll pages/amount multiplier.")
		props["dx"] = intProp("Horizontal scroll amount.")
		props["dy"] = intProp("Vertical scroll amount.")
		props["amount"] = intProp("Alias for dy or scroll amount.")
		props["duration_ms"] = intProp("Total drag movement duration.")
		props["steps"] = intProp("Number of intermediate move steps for drag.")
		props["hold_ms"] = intProp("Wait after mouse down before moving.")
		props["release_wait_ms"] = intProp("Wait before mouse up after movement.")
		props["text"] = stringProp("Text for type action.")
		props["strategy"] = stringProp("Input strategy for type: auto, keyboard, or clipboard.")
		props["value"] = stringProp("Value for set_value action.")
		props["keys"] = stringProp("Shortcut for hotkey action, for example cmd+space.")
		props["ms"] = intProp("Milliseconds for wait action.")
		props["timeout_ms"] = intProp("Alias for wait milliseconds.")
	case "desktop_clipboard":
		props["action"] = stringProp("Clipboard action: get or set.")
		props["text"] = stringProp("Text to place into the macOS clipboard when action=set.")
		props["verify"] = boolProp("Read back pbpaste after writing and report verified. Defaults to true.")
	case "desktop_preflight":
		props["check_screenshot"] = boolProp("Try a real screencapture check. Defaults to true.")
		props["check_applescript"] = boolProp("Try a real AppleScript/System Events check. Defaults to true.")
	case "desktop_list_apps":
		props["max_recent"] = intProp("Maximum recent apps to include. Defaults to 50.")
	case "desktop_get_app_state":
		props["app"] = stringProp("App name, full .app path, or bundle identifier.")
		props["activate"] = boolProp("Activate the app before reading state. Defaults to true.")
		props["include_image"] = boolProp("Attach a compressed screenshot as MCP image content. Defaults to false.")
		props["include_image_base64"] = boolProp("Alias for include_image. Defaults to false.")
		props["max_bytes"] = intProp("Maximum encoded image bytes when include_image is true. Defaults to 750000.")
		props["max_width"] = intProp("Maximum image width when include_image is true. Defaults to 1280.")
		props["max_height"] = intProp("Maximum image height when include_image is true. Defaults to 1280.")
		props["format"] = stringProp("Output image format when include_image is true: jpeg or png. Defaults to jpeg.")
		props["quality"] = intProp("JPEG quality when format is jpeg. Defaults to 72.")
		props["crop"] = map[string]any{"type": "object", "description": "Optional crop rectangle {x,y,width,height} before resizing.", "additionalProperties": true}
		props["ax_max_depth"] = intProp("Maximum accessibility tree depth. Defaults to 8.")
		props["ax_max_nodes"] = intProp("Maximum accessibility tree nodes. Defaults to 300.")
		required = []string{"app"}
	case "desktop_window_list":
	case "desktop_snapshot":
		props["include_image"] = boolProp("Attach a compressed screenshot as MCP image content. Defaults to false.")
		props["include_image_base64"] = boolProp("Alias for include_image. Defaults to false.")
		props["max_bytes"] = intProp("Maximum encoded image bytes when include_image is true. Defaults to 750000.")
		props["max_width"] = intProp("Maximum image width when include_image is true. Defaults to 1280.")
		props["max_height"] = intProp("Maximum image height when include_image is true. Defaults to 1280.")
		props["format"] = stringProp("Output image format when include_image is true: jpeg or png. Defaults to jpeg.")
		props["quality"] = intProp("JPEG quality when format is jpeg. Defaults to 72.")
		props["crop"] = map[string]any{"type": "object", "description": "Optional crop rectangle {x,y,width,height} before resizing.", "additionalProperties": true}
	case "desktop_snapshot_app":
		props["app"] = stringProp("Target macOS application name, full .app path, or bundle identifier.")
		props["crop"] = map[string]any{"type": "object", "description": "Optional window-relative crop rectangle {x,y,width,height}; defaults to the whole target window.", "additionalProperties": true}
		props["focus_if_needed"] = boolProp("Activate the app before querying/capturing its window.")
		props["require_frontmost"] = boolProp("Fail if the target app is not frontmost after optional focus.")
		props["fail_if_window_not_visible"] = boolProp("Fail when the target app has no visible window.")
		required = []string{"app"}
	case "desktop_clipboard_set":
		props["text"] = stringProp("Text to place into the macOS clipboard.")
		props["verify"] = boolProp("Read back pbpaste after writing and report verified. Defaults to true.")
		required = []string{"text"}
	case "desktop_clipboard_get":
	case "desktop_focus_app":
		props["app"] = stringProp("macOS application name to activate, for example WeChat or Safari.")
		required = []string{"app"}
	case "desktop_move", "desktop_click", "desktop_double_click":
		props["app"] = stringProp("Optional app name, full .app path, or bundle identifier. Used for AX element targeting, focus/window assertions, and space=window conversion.")
		props["element_index"] = stringProp("Accessibility element index from desktop_get_app_state. Preferred over x/y for desktop_click.")
		props["x"] = intProp("X coordinate. Defaults to global macOS screen points; when space=window, this is relative to the target app window.")
		props["y"] = intProp("Y coordinate. Defaults to global macOS screen points; when space=window, this is relative to the target app window.")
		props["space"] = stringProp("Coordinate space: screen or window. window converts app-window-relative points into global macOS points.")
		props["verify"] = boolProp("When true, capture before/after screenshots, wait wait_ms, and return effect_verified/effect_changed/diff_score.")
		props["verify_region"] = map[string]any{"type": "object", "description": "Optional screenshot diff region {x,y,width,height,space}. space may be screen or window.", "additionalProperties": true}
		props["wait_ms"] = intProp("Milliseconds to wait before after-screenshot; defaults to 250 when verify=true.")
		props["focus_if_needed"] = boolProp("Activate the target app before the action.")
		props["require_frontmost"] = boolProp("Fail if the target app is not frontmost after optional focus.")
		props["fail_if_window_not_visible"] = boolProp("Fail when app is set but no visible app window can be found.")
		props["fail_if_coordinate_outside_window"] = boolProp("With space=window, fail when coordinate is outside the target window bounds.")
		props["click_count"] = intProp("Click count for desktop_click. Defaults to 1.")
		props["mouse_button"] = stringProp("Mouse button for desktop_click: left, right, or middle. Defaults to left.")
	case "desktop_scroll":
		props["app"] = stringProp("Optional app name, full .app path, or bundle identifier. When element_index is provided, call desktop_get_app_state first.")
		props["element_index"] = stringProp("Accessibility element index from desktop_get_app_state to scroll at.")
		props["direction"] = stringProp("Scroll direction: up, down, left, or right.")
		props["pages"] = intProp("Scroll pages/amount multiplier. Defaults to 1.")
		props["dx"] = intProp("Horizontal scroll amount.")
		props["dy"] = intProp("Vertical scroll amount.")
		props["amount"] = intProp("Alias for dy.")
	case "desktop_drag":
		props["app"] = stringProp("Optional app name, full .app path, or bundle identifier. Used for focus/window assertions and space=window conversion.")
		props["from_x"] = intProp("Start X coordinate. Defaults to global macOS screen points; when space=window, this is relative to the target app window.")
		props["from_y"] = intProp("Start Y coordinate. Defaults to global macOS screen points; when space=window, this is relative to the target app window.")
		props["to_x"] = intProp("End X coordinate. Defaults to global macOS screen points; when space=window, this is relative to the target app window.")
		props["to_y"] = intProp("End Y coordinate. Defaults to global macOS screen points; when space=window, this is relative to the target app window.")
		props["space"] = stringProp("Coordinate space: screen or window. window converts app-window-relative points into global macOS points.")
		props["verify"] = boolProp("When true, capture before/after screenshots, wait wait_ms, and return effect_verified/effect_changed/diff_score.")
		props["verify_region"] = map[string]any{"type": "object", "description": "Optional screenshot diff region {x,y,width,height,space}. space may be screen or window.", "additionalProperties": true}
		props["wait_ms"] = intProp("Milliseconds to wait before after-screenshot; defaults to 250 when verify=true.")
		props["focus_if_needed"] = boolProp("Activate the target app before the action.")
		props["require_frontmost"] = boolProp("Fail if the target app is not frontmost after optional focus.")
		props["fail_if_window_not_visible"] = boolProp("Fail when app is set but no visible app window can be found.")
		props["fail_if_coordinate_outside_window"] = boolProp("With space=window, fail when any coordinate is outside the target window bounds.")
		props["duration_ms"] = intProp("Total drag movement duration distributed across steps; capped at 30000.")
		props["steps"] = intProp("Number of intermediate move steps for slower drags; capped at 200.")
		props["hold_ms"] = intProp("Wait after mouse down before moving; capped at 10000.")
		props["release_wait_ms"] = intProp("Wait before mouse up after movement; capped at 10000.")
		required = []string{"from_x", "from_y", "to_x", "to_y"}
	case "desktop_type":
		props["app"] = stringProp("Optional app name, full .app path, or bundle identifier. When provided, call desktop_get_app_state first.")
		props["text"] = stringProp("Text to input into the focused macOS app.")
		props["strategy"] = stringProp("Input strategy: auto, keyboard, or clipboard. Auto uses clipboard for long, multiline, or non-ASCII text.")
		required = []string{"text"}
	case "desktop_set_value":
		props["app"] = stringProp("App name, full .app path, or bundle identifier.")
		props["element_index"] = stringProp("Accessibility element index from desktop_get_app_state.")
		props["value"] = stringProp("Value to set on the accessibility element.")
		required = []string{"app", "element_index", "value"}
	case "desktop_perform_secondary_action":
		props["app"] = stringProp("App name, full .app path, or bundle identifier.")
		props["element_index"] = stringProp("Accessibility element index from desktop_get_app_state.")
		props["action"] = stringProp("Accessibility action name, for example AXPress or AXShowMenu.")
		required = []string{"app", "element_index", "action"}
	case "desktop_hotkey":
		props["keys"] = stringProp("Shortcut, for example cmd+space, cmd+v, enter.")
		required = []string{"keys"}
	case "desktop_wait":
		props["ms"] = intProp("Milliseconds to wait.")
		props["timeout_ms"] = intProp("Alias for ms; capped at 60000.")
	case "workspace_repos":
		props["path"] = stringProp("Directory to scan. In workspace path policy this must be workspace-relative; in host path policy absolute paths and ~/ paths are allowed. Defaults to current workspace/default cwd.")
		props["max_depth"] = intProp("Maximum directory depth to scan for repositories.")
	case "git_repo_status", "git_status":
		props["repo_path"] = stringProp("Repository path. In workspace path policy this must be workspace-relative; in host path policy absolute paths and ~/ paths are allowed. Defaults to current workspace/default cwd.")
		props["max_output_bytes"] = intProp("Maximum output bytes.")
	case "git_diff":
		props["repo_path"] = stringProp("Repository path. In workspace path policy this must be workspace-relative; in host path policy absolute paths and ~/ paths are allowed.")
		props["paths"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["max_bytes"] = intProp("Maximum output bytes.")
	case "git_log":
		props["repo_path"] = stringProp("Repository path. In workspace path policy this must be workspace-relative; in host path policy absolute paths and ~/ paths are allowed.")
		props["limit"] = intProp("Maximum commits.")
		props["max_bytes"] = intProp("Maximum output bytes.")
	case "git_show":
		props["repo_path"] = stringProp("Repository path. In workspace path policy this must be workspace-relative; in host path policy absolute paths and ~/ paths are allowed.")
		props["rev"] = stringProp("Revision to show.")
		props["max_bytes"] = intProp("Maximum output bytes.")
	case "git_blame":
		props["repo_path"] = stringProp("Repository path. In workspace path policy this must be workspace-relative; in host path policy absolute paths and ~/ paths are allowed.")
		props["path"] = stringProp("File path. In workspace path policy this must be workspace-relative; in host path policy absolute paths and ~/ paths are allowed.")
		props["max_bytes"] = intProp("Maximum output bytes.")
		required = []string{"path"}
	case "git_inspect":
		props["action"] = stringProp("Inspect action: show or blame.")
		props["repo_path"] = stringProp("Repository path.")
		props["rev"] = stringProp("Revision to show when action=show.")
		props["path"] = stringProp("File path when action=blame.")
		props["max_bytes"] = intProp("Maximum output bytes.")
	case "git_remote":
		props["action"] = stringProp("Remote action: fetch, pull, or push.")
		props["repo_path"] = stringProp("Repository path.")
		props["remote"] = stringProp("Remote name. Defaults to origin.")
		props["branch"] = stringProp("Branch name. Defaults to current branch where applicable.")
		props["max_bytes"] = intProp("Maximum output bytes.")
	case "git_fetch", "git_pull", "git_push":
		props["repo_path"] = stringProp("Repository path. In workspace path policy this must be workspace-relative; in host path policy absolute paths and ~/ paths are allowed.")
		props["remote"] = stringProp("Remote name. Defaults to origin.")
		props["branch"] = stringProp("Branch name. Defaults to current branch where applicable.")
		props["max_bytes"] = intProp("Maximum output bytes.")
	case "git_clone":
		props["url"] = stringProp("Git repository URL to clone.")
		props["repo"] = stringProp("Alias for url.")
		props["dest"] = stringProp("Destination directory. In workspace path policy this must be workspace-relative; in host path policy absolute paths and ~/ paths are allowed.")
		props["branch"] = stringProp("Branch to clone.")
		props["depth"] = intProp("Optional shallow clone depth.")
		props["max_bytes"] = intProp("Maximum output bytes.")
		required = []string{"url"}
	case "git_commit":
		props["repo_path"] = stringProp("Repository path. In workspace path policy this must be workspace-relative; in host path policy absolute paths and ~/ paths are allowed.")
		props["message"] = stringProp("Commit message.")
		props["paths"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["all"] = boolProp("Stage all changes before committing.")
		props["max_bytes"] = intProp("Maximum output bytes.")
		required = []string{"message"}
	case "set_default_cwd", "view_image":
		props["path"] = stringProp("Workspace-relative path.")
		if name == "view_image" {
			props["max_bytes"] = intProp("Maximum encoded image bytes. Defaults to 750000.")
			props["max_width"] = intProp("Maximum image width. Defaults to 1280.")
			props["max_height"] = intProp("Maximum image height. Defaults to 1280.")
			props["auto_resize"] = boolProp("Resize/compress when limits are exceeded. Defaults to true.")
			props["format"] = stringProp("Output image format: jpeg or png. Defaults to jpeg.")
			props["quality"] = intProp("JPEG quality when format is jpeg. Defaults to 72.")
			props["crop"] = map[string]any{"type": "object", "description": "Optional crop rectangle {x,y,width,height} before resizing.", "additionalProperties": true}
			props["output"] = stringProp("mcp_image or data_url. Defaults to mcp_image.")
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

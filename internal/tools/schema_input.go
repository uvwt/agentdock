package tools

func InputSchema(name string) map[string]any {
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
	case "workspace_edit":
		props["action"] = map[string]any{"type": "string", "description": "Workspace edit action.", "enum": []string{"replace", "patch", "add", "delete", "move"}}
		props["path"] = stringProp("Workspace-relative path for replace, add, delete, or move.")
		props["old"] = stringProp("Exact UTF-8 text to replace.")
		props["new"] = stringProp("Replacement UTF-8 text for replace, or content alias for add.")
		props["replace_all"] = boolProp("Replace every match instead of only the first.")
		props["expected_matches"] = intProp("Required number of matches. Defaults to 1.")
		props["content"] = stringProp("Text content for action=add.")
		props["new_path"] = stringProp("Destination path for action=move.")
		props["overwrite"] = boolProp("Allow add or move to replace an existing destination file.")
		props["recursive"] = boolProp("Required for deleting directories.")
		props["patch"] = stringProp("Patch text for action=patch.")
		props["workdir"] = stringProp("Patch working directory.")
		props["repo_path"] = stringProp("Alias for workdir.")
		props["dry_run"] = boolProp("Preview or validate without writing.")
		props["max_diff_bytes"] = intProp("Maximum diff preview bytes.")
		required = []string{"action"}

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
	case "session_observe":
		props["action"] = map[string]any{"type": "string", "description": "Read-only session action.", "enum": []string{"list", "status"}}
		props["session_id"] = stringProp("Session id returned by exec_command, required for status.")
		props["max_output_bytes"] = intProp("Maximum output bytes.")
	case "session_act":
		props["action"] = map[string]any{"type": "string", "description": "Mutating session action.", "enum": []string{"write", "kill", "kill_all"}}
		props["session_id"] = stringProp("Session id returned by exec_command, required for write/kill.")
		props["chars"] = stringProp("Characters to write when action=write.")
		props["max_output_bytes"] = intProp("Maximum output bytes.")
	case "check_github_repo_access":
		props["repo"] = stringProp("GitHub repository as owner/name or https://github.com/owner/name.git.")
		props["repository"] = stringProp("Alias for repo.")
		props["timeout_ms"] = intProp("HTTP timeout in milliseconds.")
		required = []string{"repo"}
	case "task_manage":
		props["action"] = map[string]any{"type": "string", "description": "Task lifecycle action. Template discovery and authoring live in workflow_template_manage.", "enum": []string{"create", "list", "get", "block", "resume", "final_review", "complete_after_review"}}
		props["task_id"] = stringProp("Persistent task id for get, block, resume, final_review, or complete_after_review.")
		props["title"] = stringProp("Short task title for create.")
		props["goal"] = stringProp("Fixed task goal for create.")
		props["completion_conditions"] = map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "string"}, "description": "Review checklist items used by final_review."}
		props["status"] = map[string]any{"type": "string", "description": "Optional list filter.", "enum": []string{"active", "blocked", "completed"}}
		props["limit"] = intProp("Maximum tasks returned by list. Defaults to 50 and is capped at 200.")
		props["summary"] = stringProp("Concise task, blocker, resume, or final review summary.")
		props["blocker"] = stringProp("Explicit blocker that prevents further progress.")
		props["evidence"] = stringProp("Evidence required only when blocking a task.")
		props["review_status"] = map[string]any{"type": "string", "description": "Final review status. Use pass only after real verification.", "enum": []string{"pass", "failed"}}
		props["verified_facts"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Facts verified during final_review. Required when final_review passes."}
		props["open_risks"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Risks that remain after final review."}
		props["missing_checks"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Checks not completed. Must be empty when final_review passes."}
		props["template_id"] = stringProp("Workflow template id for create.")
		props["template_version"] = stringProp("Workflow template version for create.")
		props["selected_reason"] = stringProp("Why the model selected this template.")
		props["template_candidates"] = map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": true}, "description": "Candidate templates and scores considered before selection."}
		required = []string{"action"}

	case "workflow_template_manage":
		props["action"] = map[string]any{"type": "string", "description": "Workflow template action.", "enum": []string{"save", "validate", "publish", "retire", "list", "get", "match"}}
		props["template"] = map[string]any{"type": "object", "additionalProperties": true, "description": "Complete draft workflow template for save."}
		props["template_id"] = stringProp("Workflow template id.")
		props["template_version"] = stringProp("Workflow template version.")
		props["template_status"] = map[string]any{"type": "string", "enum": []string{"draft", "active", "retired"}, "description": "Optional list status filter."}
		props["allow_long_template"] = boolProp("Allow a workflow template to exceed default guardrails. Provide long_template_reason when true.")
		props["long_template_reason"] = stringProp("Reason required when allow_long_template=true.")
		props["goal"] = stringProp("Goal text for match.")
		props["device"] = stringProp("Optional device hint for match.")
		props["type"] = stringProp("Optional workflow type hint for match. This maps to template match.type.")
		required = []string{"action"}

	case "skill_manage":
		props["action"] = map[string]any{"type": "string", "description": "Skill action: list, inspect, validate, install, run, or rollback.", "enum": []string{"list", "inspect", "validate", "install", "run", "rollback"}}
		props["skill"] = stringProp("Skill name for inspect, run, or rollback.")
		props["version"] = stringProp("Optional installed Skill version.")
		props["channel"] = map[string]any{"type": "string", "description": "Skill channel: development, canary, stable, or pinned.", "enum": []string{"development", "canary", "stable", "pinned"}}
		props["source"] = stringProp("Workspace/host path or HTTP(S) URL for validate/install.")
		props["digest"] = stringProp("Optional expected SHA-256 digest for validate/install.")
		props["activate"] = boolProp("Activate the installed version. Defaults to true.")
		props["confirmed_no_env"] = boolProp("Required for validating/installing a Skill with no manifest permissions.env declarations; confirms the Skill needs no Env Manager configuration.")
		props["max_bytes"] = intProp("Maximum validate/install package bytes.")
		props["operation"] = stringProp("Skill operation name for run.")
		props["input"] = map[string]any{"description": "JSON input value for the Skill operation."}
		props["input_json"] = stringProp("Alternative raw JSON input string for the Skill operation.")
		props["binding"] = stringProp("Optional binding name for run.")
		props["run_id"] = stringProp("Optional run identifier.")
		props["timeout_ms"] = intProp("Optional run timeout in milliseconds, capped by the operation timeout.")
		props["max_output_bytes"] = intProp("Maximum stdout/stderr bytes for run.")
		required = []string{"action"}
	case "artifact_fetch_create":
		props["source_device_id"] = stringProp("Registered source device id.")
		props["source_path"] = stringProp("Absolute path on the source device. Immutable core deny rules and configured additions always apply.")
		props["archive"] = boolProp("For a directory, create a tar.gz. Defaults to false, which returns a bounded immediate listing.")
		props["retention_seconds"] = intProp("Fetch retention between 3600 and 604800 seconds. Defaults to 86400.")
		required = []string{"source_device_id", "source_path"}
	case "artifact_fetch_status":
		props["fetch_id"] = stringProp("Artifact fetch id returned by artifact_fetch_create.")
		required = []string{"fetch_id"}
	case "artifact_fetch_download":
		props["fetch_id"] = stringProp("Ready artifact fetch id.")
		props["mounted"] = boolProp("Set true only after the GPT sandbox has mounted the returned file; this deletes Nexus ciphertext and local transient state.")
		required = []string{"fetch_id"}
	case "artifact_send":
		props["file"] = map[string]any{"type": "string", "format": "binary", "description": "Top-level file parameter. Connector runtimes must upload the file body and pass the mounted local path; do not pass a remote /mnt/data path as plain text."}
		props["path"] = stringProp("Alternative local file or directory path visible to this AgentDock instance. Directories are packed as tar.gz before encryption.")
		props["target_devices"] = map[string]any{"type": "array", "minItems": 1, "maxItems": 32, "items": map[string]any{"type": "string"}, "description": "Registered Nexus device IDs that should receive independent deliveries."}
		props["dispatch"] = boolProp("Immediately queue artifact.pull commands after upload. Defaults to true.")
		props["retention_seconds"] = intProp("Ciphertext retention between 3600 and 604800 seconds. Defaults to 86400.")
		props["delete_after_all_delivered"] = boolProp("Delete Nexus ciphertext once all deliveries complete, expire, or are cancelled.")
		props["conflict_policy"] = map[string]any{"type": "string", "enum": []string{"reject", "rename", "overwrite"}, "description": "Target conflict policy. Defaults to reject."}
		props["extract"] = boolProp("Safely extract a tar.gz after verification. Defaults to false.")
		props["logical_target"] = stringProp("Target-side logical destination mapping. Defaults to inbox; arbitrary absolute paths are not accepted.")
		required = []string{"target_devices"}
	case "env_manage":
		props["action"] = map[string]any{"type": "string", "description": "Env action: list, inspect, set, delete, or verify.", "enum": []string{"list", "inspect", "set", "delete", "verify"}}
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
		required = []string{"action"}
	case "recall_bootstrap":
		props["max_bytes"] = intProp("Maximum combined RecallDock pack bytes. Does not expose section bodies by itself; use include_body or recall_read when body text is needed.")
		props["include_raw"] = boolProp("Include raw Markdown as raw_content. Defaults to false to avoid duplicating body/content tokens.")
		props["include_body"] = boolProp("Include section body text in recall_bootstrap. Defaults to false; prefer recall_read for targeted full text.")
	case "recall_search":
		props["query"] = stringProp("Text query to search in RecallDock files and paths.")
		props["kind"] = map[string]any{"type": "string", "description": "Search kind. Defaults to all.", "enum": []string{"all", "markdown", "card", "note"}}
		props["note_scope"] = map[string]any{"type": "string", "description": "Note only: notes scope to search or capture.", "enum": []string{"questions", "github-learning"}}
		props["max_results"] = intProp("Maximum results to return.")
		required = []string{"query"}
	case "recall_read":
		props["path"] = stringProp("RecallDock-relative Markdown/card/note path.")
		props["include_raw"] = boolProp("Include raw Markdown as raw_content. Defaults to false to avoid duplicating body/content tokens.")
		required = []string{"path"}
	case "recall_write":
		props["target"] = map[string]any{"type": "string", "description": "Recall target selected by the model.", "enum": []string{"card", "note", "markdown"}}
		props["action"] = map[string]any{"type": "string", "description": "Recall action selected by the model.", "enum": []string{"plan", "create", "replace", "append", "patch", "update_fact", "diff", "delete"}}
		props["confirmed"] = boolProp("Required for true writes/deletes. card/note create with confirmed=false returns a review plan.")
		props["path"] = stringProp("RecallDock-relative path when reading, updating, deleting, or writing a known entry.")
		props["content"] = stringProp("Memory content, note content, Markdown content, or proposed replacement content.")
		props["title"] = stringProp("Short title for a card or Markdown entry.")
		props["summary"] = stringProp("Short summary for a card or note.")
		props["query"] = stringProp("Search question or topic used by note planning.")
		props["note_scope"] = map[string]any{"type": "string", "description": "Note only: notes scope to search, capture, or write. Defaults to questions; github-learning is for GitHub learning notes.", "enum": []string{"questions", "github-learning"}}
		props["conclusion"] = stringProp("Note only: optional conclusion or current answer for the captured question.")
		props["open_questions"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Note only: unresolved follow-up questions."}
		props["overwrite"] = boolProp("Replace an existing entry when supported.")
		props["allow_warnings"] = boolProp("Card only: after reviewing warnings, allow writing a warned card. Do not use by default.")
		props["old"] = stringProp("Patch only: literal text to replace.")
		props["new"] = stringProp("Patch only: replacement text for old.")
		props["append"] = stringProp("Append/patch only: text to append to the recall document.")
		props["section"] = stringProp("Patch/update_fact only: Markdown heading title whose section should be updated.")
		props["section_content"] = stringProp("Patch only: new body for the selected Markdown section.")
		props["key"] = stringProp("Update_fact only: fact key to update.")
		props["value"] = stringProp("Update_fact only: new fact value.")
		props["facts"] = objectProp("Update_fact only: multiple key/value facts to update.")
		props["append_if_missing"] = boolProp("Update_fact only: append missing keys to the selected section or document instead of failing.")
		props["max_bytes"] = intProp("Maximum diff/output bytes.")
		required = []string{"target", "action"}
	case "recall_maintain":
		props["action"] = map[string]any{"type": "string", "description": "Maintenance action.", "enum": []string{"sync_status", "list", "lint", "embedding_status", "reindex", "reindex_cards"}}
		props["prefix"] = stringProp("Optional RecallDock-relative prefix.")
		props["terms"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Terms or regex patterns for lint."}
		props["regex"] = boolProp("Treat terms as regex patterns for lint.")
		props["max_entries"] = intProp("Maximum entries to list or scan.")
		props["max_findings"] = intProp("Maximum lint findings to return.")
		props["max_results"] = intProp("Maximum results where supported.")
	case "private_notes_search":
		props["query"] = stringProp("Text query to search in private notes.")
		props["max_results"] = intProp("Maximum results to return.")
		required = []string{"query"}
	case "private_notes_read":
		props["path"] = stringProp("Path under notes/.")
		props["category"] = stringProp("Optional category used with title when path is omitted.")
		props["title"] = stringProp("Optional title used to derive path when path is omitted.")
		props["max_bytes"] = intProp("Maximum bytes to return.")
	case "private_notes_write":
		props["path"] = stringProp("Path under notes/. If omitted, category + title derive the path.")
		props["category"] = stringProp("Category such as services, accounts, recovery, or networking. Defaults to services.")
		props["title"] = stringProp("Title.")
		props["summary"] = stringProp("Optional summary.")
		props["tags"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["content"] = stringProp("Content to write.")
		props["confirmed"] = boolProp("Required for true writes.")
		props["overwrite"] = boolProp("Replace an existing note.")
		required = []string{"content", "confirmed"}
	case "private_notes_status":
		props["action"] = map[string]any{"type": "string", "description": "Read-only private notes status action.", "enum": []string{"check", "list"}}
	case "private_notes_maintain":
		props["action"] = map[string]any{"type": "string", "description": "Mutating private notes maintenance action.", "enum": []string{"init", "init-encryption", "sync-encrypted", "encrypt-all", "migrate-enc-to-age"}}
	case "browser_session":
		props["action"] = map[string]any{"type": "string", "description": "Browser session action.", "enum": []string{"start", "close", "cleanup_stale"}}
		props["url"] = stringProp("Initial URL when action=start. Defaults to about:blank.")
		props["backend"] = stringProp("Browser backend: playwright or cdp. Defaults to playwright.")
		props["browser"] = stringProp("Browser family: chromium, chrome, edge, or msedge. edge/msedge selects Microsoft Edge.")
		props["channel"] = stringProp("Optional Playwright Chromium channel, such as msedge or chrome.")
		props["cdp_url"] = stringProp("CDP endpoint, required when backend=cdp.")
		props["headless"] = boolProp("Run browser headless. Defaults to true.")
		props["viewport"] = objectProp("Viewport object, for example {width:1280,height:800}.")
		props["session_id"] = stringProp("Browser session id.")
		props["profile_id"] = stringProp("Optional persistent browser profile id stored under browser artifacts. Reuses cookies/localStorage across runs.")
		props["cookies"] = arrayProp("Optional Playwright cookies to add to the browser context.")
		props["local_storage"] = objectProp("Optional localStorage map by origin, for example origin to key/value object.")
		props["storage_state"] = objectProp("Optional Playwright storageState object.")
		props["save_storage_state"] = boolProp("Save context storage state after action/snapshot and return storage_state_path.")
		props["reload_after_local_storage"] = boolProp("Reload the page after applying localStorage. Defaults to true.")
		props["max_age_ms"] = intProp("When action=cleanup_stale, remove sessions older than this age. Defaults to 6 hours.")
		props["timeout_ms"] = intProp("Operation timeout in milliseconds.")
	case "browser_act":
		props["session_id"] = stringProp("Browser session id.")
		props["actions"] = arrayProp("Actions to run: goto, click, fill, press, wait, wait_for_selector, select, scroll, reload, back, or forward. Page script actions are disabled by the default runner.")
		props["full_page"] = boolProp("Capture full-page screenshot in the final snapshot.")
		props["max_text_chars"] = intProp("Maximum body text characters in snapshot.")
		props["include_screenshot_base64"] = boolProp("Include screenshot_base64 and screenshot_mime_type in the response. Disabled by default because screenshots can be large.")
		props["include_image"] = boolProp("Attach screenshot as MCP image content when supported and below max_image_bytes.")
		props["include_image_base64"] = boolProp("Alias for include_image.")
		props["max_image_bytes"] = intProp("Maximum inline image bytes. Defaults to 750000.")
		props["close_after"] = boolProp("Close and remove the browser session after the action/snapshot succeeds.")
		props["save_storage_state"] = boolProp("Save context storage state and return storage_state_path.")
		props["max_interactive_elements"] = intProp("Maximum visible interactive elements to return. Defaults to 40.")
		props["timeout_ms"] = intProp("Operation timeout in milliseconds.")
		required = []string{"session_id", "actions"}
	case "browser_snapshot":
		props["session_id"] = stringProp("Browser session id.")
		props["full_page"] = boolProp("Capture full-page screenshot for snapshot.")
		props["max_text_chars"] = intProp("Maximum body text characters in snapshot.")
		props["include_screenshot_base64"] = boolProp("Include screenshot_base64 and screenshot_mime_type in the response. Disabled by default because screenshots can be large.")
		props["include_image"] = boolProp("Attach screenshot as MCP image content when supported and below max_image_bytes.")
		props["include_image_base64"] = boolProp("Alias for include_image.")
		props["max_image_bytes"] = intProp("Maximum inline image bytes. Defaults to 750000.")
		props["close_after"] = boolProp("Close and remove the browser session after snapshot succeeds.")
		props["save_storage_state"] = boolProp("Save context storage state and return storage_state_path.")
		props["max_interactive_elements"] = intProp("Maximum visible interactive elements to return. Defaults to 40.")
		props["timeout_ms"] = intProp("Operation timeout in milliseconds.")
		required = []string{"session_id"}
	case "git_read":
		props["action"] = map[string]any{"type": "string", "description": "Read action.", "enum": []string{"repos", "status", "diff", "log", "show", "blame"}}
		props["path"] = stringProp("Directory path for repos or file path for blame.")
		props["repo_path"] = stringProp("Repository path.")
		props["paths"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["rev"] = stringProp("Revision for show.")
		props["limit"] = intProp("Maximum commits for log.")
		props["max_depth"] = intProp("Maximum scan depth for repos.")
		props["max_bytes"] = intProp("Maximum output bytes.")
		props["max_output_bytes"] = intProp("Alias for status output limit.")
		required = []string{"action"}
	case "git_write":
		props["action"] = map[string]any{"type": "string", "description": "Write action.", "enum": []string{"clone", "commit", "fetch", "pull", "push"}}
		props["repo_path"] = stringProp("Repository path.")
		props["url"] = stringProp("Repository URL for clone.")
		props["repo"] = stringProp("Alias for url.")
		props["dest"] = stringProp("Destination directory for clone.")
		props["message"] = stringProp("Commit message.")
		props["paths"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["all"] = boolProp("Stage all changes before commit.")
		props["remote"] = stringProp("Remote name. Defaults to origin.")
		props["branch"] = stringProp("Branch name where applicable.")
		props["depth"] = intProp("Shallow clone depth.")
		props["max_bytes"] = intProp("Maximum output bytes.")
		required = []string{"action"}

	case "view_image":
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
	}

	schema := map[string]any{"type": "object", "properties": props, "additionalProperties": true}
	switch name {
	case "recall_bootstrap", "recall_search", "recall_read", "recall_write", "recall_maintain":
		// Recall public schemas are intentionally closed: legacy/advanced args remain
		// accepted by the runtime for compatibility, but should not be suggested to models.
		schema["additionalProperties"] = false
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

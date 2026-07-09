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
		props["path"] = stringProp("Host path. Relative paths resolve from ~/AgentDock.")
		props["start_line"] = intProp("1-based start line.")
		props["end_line"] = intProp("Inclusive end line.")
		props["max_bytes"] = intProp("Maximum output bytes.")
		required = []string{"path"}
	case "agentdock_context":

	case "list_dir":
		props["path"] = stringProp("Host directory path. Relative paths resolve from ~/AgentDock.")
		props["recursive"] = boolProp("List recursively.")
		props["max_depth"] = intProp("Maximum recursive depth.")
		props["max_entries"] = intProp("Maximum entries.")
		props["include_hidden"] = boolProp("Include dotfiles.")
		props["include_ignored"] = boolProp("Include normally skipped directories.")
	case "list_files":
		props["path"] = stringProp("Host directory path. Relative paths resolve from ~/AgentDock.")
		props["patterns"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["glob"] = stringProp("Single glob pattern override.")
		props["exclude_patterns"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["max_results"] = intProp("Maximum files.")
		props["include_hidden"] = boolProp("Include dotfiles.")
		props["include_ignored"] = boolProp("Include normally skipped directories.")
	case "search_text":
		props["path"] = stringProp("Host path. Relative paths resolve from ~/AgentDock.")
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
	case "file_edit":
		props["action"] = map[string]any{"type": "string", "description": "File edit action.", "enum": []string{"replace", "patch", "add", "delete", "move"}}
		props["path"] = stringProp("Host path for replace, add, delete, or move. Relative paths resolve from ~/AgentDock.")
		props["old"] = stringProp("Exact UTF-8 text to replace.")
		props["new"] = stringProp("Replacement UTF-8 text for action=replace.")
		props["replace_all"] = boolProp("Replace every match instead of only the first.")
		props["expected_matches"] = intProp("Required number of matches. Defaults to 1.")
		props["content"] = stringProp("Text content for action=add.")
		props["new_path"] = stringProp("Destination path for action=move.")
		props["overwrite"] = boolProp("Allow add or move to replace an existing destination file.")
		props["recursive"] = boolProp("Required for deleting directories.")
		props["patch"] = stringProp("Patch text for action=patch.")
		props["workdir"] = stringProp("Patch working directory.")
		props["dry_run"] = boolProp("Preview or validate without writing.")
		props["max_diff_bytes"] = intProp("Maximum diff preview bytes.")
		required = []string{"action"}

	case "exec_command":
		props["cmd"] = stringProp("Command to run.")
		props["workdir"] = stringProp("Host working directory. Relative paths resolve from ~/AgentDock.")
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
		props["action"] = map[string]any{"type": "string", "description": "Workflow template action.", "enum": []string{"save", "validate", "publish", "retire", "list", "get", "match", "vector_index"}}
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

	case "skill_read":
		props["action"] = map[string]any{"type": "string", "description": "Read-only Skill discovery action.", "enum": []string{"list", "inspect"}}
		props["skill"] = stringProp("Skill name for inspect.")
		props["version"] = stringProp("Optional installed Skill version for inspect.")
		props["channel"] = map[string]any{"type": "string", "description": "Optional Skill channel for inspect: development, canary, stable, or pinned.", "enum": []string{"development", "canary", "stable", "pinned"}}
		required = []string{"action"}
	case "skill_package":
		props["action"] = map[string]any{"type": "string", "description": "Skill package lifecycle action.", "enum": []string{"validate", "install", "rollback"}}
		props["skill"] = stringProp("Skill name for rollback.")
		props["channel"] = map[string]any{"type": "string", "description": "Skill channel: development, canary, stable, or pinned.", "enum": []string{"development", "canary", "stable", "pinned"}}
		props["source"] = stringProp("Host path or HTTP(S) URL for validate/install.")
		props["digest"] = stringProp("Optional expected SHA-256 digest for validate/install.")
		props["activate"] = boolProp("Activate the installed version. Defaults to true.")
		props["confirmed_no_env"] = boolProp("Required for validating/installing a Skill with no manifest permissions.env declarations; confirms the Skill needs no Skill Env Manager configuration.")
		props["max_bytes"] = intProp("Maximum validate/install package bytes.")
		required = []string{"action"}
	case "skill_run":
		props["action"] = map[string]any{"type": "string", "description": "Optional action. Omit by default; when present it must be run.", "enum": []string{"run"}}
		props["skill"] = stringProp("Skill name to run.")
		props["operation"] = stringProp("Skill operation name to run.")
		props["version"] = stringProp("Optional installed Skill version.")
		props["channel"] = map[string]any{"type": "string", "description": "Optional Skill channel: development, canary, stable, or pinned.", "enum": []string{"development", "canary", "stable", "pinned"}}
		props["input"] = map[string]any{"description": "JSON input value for the Skill operation."}
		props["input_json"] = stringProp("Alternative raw JSON input string for the Skill operation.")
		props["binding"] = stringProp("Optional binding name for run.")
		props["run_id"] = stringProp("Optional run identifier.")
		props["timeout_ms"] = intProp("Optional run timeout in milliseconds, capped by the operation timeout.")
		props["max_output_bytes"] = intProp("Maximum stdout/stderr bytes for run.")
		required = []string{"skill", "operation"}
	case "file_publish":
		props["file"] = map[string]any{"type": "string", "format": "binary", "description": "Top-level file parameter. Connector runtimes should pass the mounted local path when available."}
		props["path"] = stringProp("Local file or directory path visible to this AgentDock instance. Relative paths resolve from ~/AgentDock.")
		props["retention_seconds"] = intProp("Signed URL retention in seconds. Defaults to 86400 and is capped at 604800.")
		required = []string{}
	case "skill_env_manage":
		props["action"] = map[string]any{"type": "string", "description": "Skill env registry action: list, inspect, set, delete, or verify.", "enum": []string{"list", "inspect", "set", "delete", "verify"}}
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
	case "private_note_manage":
		props["action"] = map[string]any{"type": "string", "description": "Private note action. Do not use by default; use only for explicit private/local-only/non-synced notes or clearly sensitive secrets.", "enum": []string{"search", "read", "write", "status", "maintain"}}
		props["query"] = stringProp("Search query for action=search.")
		props["max_results"] = intProp("Maximum search results to return.")
		props["path"] = stringProp("Path under notes/ for action=read or action=write.")
		props["category"] = stringProp("Optional category used with title when path is omitted. Defaults to services.")
		props["title"] = stringProp("Title used for frontmatter or to derive the path when path is omitted.")
		props["summary"] = stringProp("Optional summary for action=write.")
		props["tags"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["content"] = stringProp("Plaintext private note content for action=write.")
		props["confirmed"] = boolProp("Required for action=write true writes.")
		props["overwrite"] = boolProp("Replace an existing note for action=write.")
		props["max_bytes"] = intProp("Maximum bytes to return for action=read.")
		props["status_action"] = map[string]any{"type": "string", "description": "Read-only status action when action=status.", "enum": []string{"check", "list"}}
		props["maintenance_action"] = map[string]any{"type": "string", "description": "Maintenance operation when action=maintain.", "enum": []string{"init", "init-encryption", "sync-encrypted", "encrypt-all"}}
		required = []string{"action"}
	case "browser_session":
		props["action"] = map[string]any{"type": "string", "description": "Browser session action.", "enum": []string{"start", "close", "cleanup_stale"}}
		props["url"] = stringProp("Initial URL when action=start. Defaults to about:blank.")
		props["backend"] = stringProp("Browser backend: playwright or cdp. Defaults to playwright.")
		props["browser"] = stringProp("Browser family: chromium, chrome, edge, or msedge. On macOS prefer chrome to use system Google Chrome; edge/msedge selects Microsoft Edge. Do not run or suggest the Playwright browser install command for missing bundled Chromium.")
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
		props["actions"] = browserActionsProp()
		props["full_page"] = boolProp("Capture full-page screenshot in the final snapshot.")
		props["max_text_chars"] = intProp("Maximum body text characters in snapshot.")
		props["screenshot_return_mode"] = stringProp("Screenshot return mode: none, url, mcp_image, base64, data_url, or both. Defaults to url.")
		props["max_inline_bytes"] = intProp("Maximum inline image bytes for base64/data_url/mcp_image. Defaults to 750000 and is capped at 2097152.")
		props["retention_seconds"] = intProp("Signed screenshot URL retention in seconds. Defaults to 86400 and is capped at 604800.")
		props["close_after"] = boolProp("Close and remove the browser session after the action/snapshot succeeds.")
		props["save_storage_state"] = boolProp("Save context storage state and return storage_state_path.")
		props["max_interactive_elements"] = intProp("Maximum visible interactive elements to return. Defaults to 40.")
		props["timeout_ms"] = intProp("Operation timeout in milliseconds.")
		required = []string{"session_id", "actions"}
	case "browser_snapshot":
		props["session_id"] = stringProp("Browser session id.")
		props["full_page"] = boolProp("Capture full-page screenshot for snapshot.")
		props["max_text_chars"] = intProp("Maximum body text characters in snapshot.")
		props["screenshot_return_mode"] = stringProp("Screenshot return mode: none, url, mcp_image, base64, data_url, or both. Defaults to url.")
		props["max_inline_bytes"] = intProp("Maximum inline image bytes for base64/data_url/mcp_image. Defaults to 750000 and is capped at 2097152.")
		props["retention_seconds"] = intProp("Signed screenshot URL retention in seconds. Defaults to 86400 and is capped at 604800.")
		props["close_after"] = boolProp("Close and remove the browser session after snapshot succeeds.")
		props["save_storage_state"] = boolProp("Save context storage state and return storage_state_path.")
		props["max_interactive_elements"] = intProp("Maximum visible interactive elements to return. Defaults to 40.")
		props["timeout_ms"] = intProp("Operation timeout in milliseconds.")
		required = []string{"session_id"}
	case "git_read":
		props["action"] = map[string]any{"type": "string", "description": "Read action.", "enum": gitReadActions}
		props["path"] = stringProp("Directory path for repos or file path for blame.")
		props["repo_path"] = stringProp("Repository path.")
		props["paths"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["rev"] = stringProp("Revision for show.")
		props["limit"] = intProp("Maximum commits for log.")
		props["max_depth"] = intProp("Maximum scan depth for repos.")
		props["max_bytes"] = intProp("Maximum output bytes.")
		props["repo"] = stringProp("GitHub repository as owner/name or https://github.com/owner/name.git; used by action=github_repo_access.")
		props["timeout_ms"] = intProp("HTTP timeout in milliseconds; used by action=github_repo_access.")
		required = []string{"action"}
	case "git_write":
		props["action"] = map[string]any{"type": "string", "description": "Write action.", "enum": []string{"clone", "commit", "fetch", "pull", "push"}}
		props["repo_path"] = stringProp("Repository path.")
		props["url"] = stringProp("Repository URL for clone.")
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
		props["path"] = stringProp("Host path. Relative paths resolve from ~/AgentDock.")
		if name == "view_image" {
			props["return_mode"] = stringProp("Image return mode: none, url, mcp_image, base64, data_url, or both. Defaults to url.")
			props["retention_seconds"] = intProp("Signed image URL retention in seconds. Defaults to 86400 and is capped at 604800.")
			props["max_inline_bytes"] = intProp("Maximum inline image bytes for base64/data_url/mcp_image. Defaults to 750000 and is capped at 2097152.")
			props["max_bytes"] = intProp("Maximum processed image bytes. Defaults to 750000.")
			props["max_width"] = intProp("Maximum image width. Defaults to 1280.")
			props["max_height"] = intProp("Maximum image height. Defaults to 1280.")
			props["auto_resize"] = boolProp("Resize/compress when limits are exceeded. Defaults to true.")
			props["format"] = stringProp("Processed image format: jpeg or png. Defaults to jpeg.")
			props["quality"] = intProp("JPEG quality when format is jpeg. Defaults to 72.")
			props["crop"] = map[string]any{"type": "object", "description": "Optional crop rectangle {x,y,width,height} before resizing.", "additionalProperties": true}
		}
	}

	schema := map[string]any{"type": "object", "properties": props, "additionalProperties": true}
	switch name {
	case "recall_bootstrap", "recall_search", "recall_read", "recall_write", "recall_maintain", "private_note_manage":
		// Recall public schemas are intentionally closed: only model-facing fields are accepted.
		schema["additionalProperties"] = false
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func browserActionsProp() map[string]any {
	return map[string]any{
		"type":        "array",
		"description": "Browser actions. Every item must use the action field; type/ms are not accepted.",
		"items": map[string]any{
			"type":                 "object",
			"additionalProperties": true,
			"required":             []string{"action"},
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"description": "Required action name. Use this field, not type.",
					"enum":        []string{"goto", "click", "fill", "press", "wait", "wait_for_selector", "select", "scroll", "reload", "back", "forward"},
				},
				"url":        map[string]any{"type": "string", "description": "URL for action=goto."},
				"selector":   map[string]any{"type": "string", "description": "CSS selector for click/fill/press/wait_for_selector/select."},
				"value":      map[string]any{"description": "Value for fill/select, or wait duration in milliseconds for action=wait."},
				"key":        map[string]any{"type": "string", "description": "Keyboard key for action=press."},
				"timeout_ms": map[string]any{"type": "integer", "description": "Timeout for action=wait_for_selector."},
				"wait_until": map[string]any{"type": "string", "description": "Navigation wait state for goto/reload/back/forward, such as domcontentloaded or load."},
				"delta_x":    map[string]any{"type": "integer", "description": "Horizontal wheel delta for action=scroll."},
				"delta_y":    map[string]any{"type": "integer", "description": "Vertical wheel delta for action=scroll."},
			},
		},
	}
}

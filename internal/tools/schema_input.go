package tools

func InputSchema(name string) map[string]any {
	props := map[string]any{}
	required := []string{}
	stringProp := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	intProp := func(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
	boundedIntProp := func(desc string, minimum, maximum int) map[string]any {
		return map[string]any{"type": "integer", "description": desc, "minimum": minimum, "maximum": maximum}
	}
	boolProp := func(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }
	objectProp := func(desc string) map[string]any {
		return map[string]any{"type": "object", "description": desc, "additionalProperties": true}
	}
	arrayProp := func(desc string) map[string]any {
		return map[string]any{"type": "array", "description": desc, "items": map[string]any{"type": "object"}}
	}

	switch name {
	case "read_file":
		props["path"] = stringProp(filePathDescription("Host path. Relative paths resolve from ~/AgentDock."))
		addFileRuntimeProperties(props)
		props["start_line"] = intProp("1-based start line.")
		props["end_line"] = intProp("Inclusive end line.")
		props["max_bytes"] = boundedIntProp("Maximum output bytes. Defaults to 262144 and is capped at 4194304.", 1, maxTextOutputBytes)
		required = []string{"path"}
	case "agentdock_context":

	case "list_dir":
		props["path"] = stringProp(filePathDescription("Host directory path. Relative paths resolve from ~/AgentDock."))
		addFileRuntimeProperties(props)
		props["recursive"] = boolProp("List recursively.")
		props["max_depth"] = boundedIntProp("Maximum recursive depth. Defaults to 1 and is capped at 20.", 1, 20)
		props["max_entries"] = boundedIntProp("Maximum entries. Defaults to 200 and is capped at 2000.", 1, 2000)
		props["include_hidden"] = boolProp("Include dotfiles.")
		props["include_ignored"] = boolProp("Include normally skipped directories.")
	case "list_files":
		props["path"] = stringProp(filePathDescription("Host directory path. Relative paths resolve from ~/AgentDock."))
		addFileRuntimeProperties(props)
		props["patterns"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["glob"] = stringProp("Single glob pattern override.")
		props["exclude_patterns"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["max_results"] = boundedIntProp("Maximum files. Defaults to 500 and is capped at 5000.", 1, 5000)
		props["include_hidden"] = boolProp("Include dotfiles.")
		props["include_ignored"] = boolProp("Include normally skipped directories.")
	case "search_text":
		props["path"] = stringProp(filePathDescription("Host path. Relative paths resolve from ~/AgentDock."))
		addFileRuntimeProperties(props)
		props["query"] = stringProp("Text or regex query.")
		props["regex"] = boolProp("Treat query as regex.")
		props["case_sensitive"] = boolProp("Use case-sensitive search.")
		props["include_hidden"] = boolProp("Include hidden files and directories.")
		props["include_globs"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["glob"] = stringProp("Single include glob.")
		props["exclude_globs"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["context_lines"] = boundedIntProp("Context lines around each match. Capped at 20.", 0, 20)
		props["max_results"] = boundedIntProp("Maximum matches. Defaults to 100 and is capped at 1000.", 1, 1000)
		required = []string{"query"}
	case "file_edit":
		props["action"] = map[string]any{"type": "string", "description": "File edit action.", "enum": []string{"replace", "patch", "add", "delete", "move"}}
		props["path"] = stringProp(filePathDescription("Host path for replace, add, delete, or move. Relative paths resolve from ~/AgentDock."))
		addFileRuntimeProperties(props)
		props["old"] = stringProp("Exact UTF-8 text to replace.")
		props["new"] = stringProp("Replacement UTF-8 text for action=replace.")
		props["replace_all"] = boolProp("Replace every match instead of only the first.")
		props["expected_matches"] = map[string]any{"type": "integer", "description": "Required number of matches. Defaults to 1; zero asserts no matches.", "minimum": 0}
		props["content"] = stringProp("Text content for action=add.")
		props["new_path"] = stringProp(filePathDescription("Destination path for action=move."))
		props["overwrite"] = boolProp("Allow add or move to replace an existing destination file.")
		props["recursive"] = boolProp("Required for deleting directories.")
		props["patch"] = stringProp(filePatchDescription("Patch text for action=patch."))
		props["workdir"] = stringProp(filePathDescription("Patch working directory."))
		props["dry_run"] = boolProp("Preview or validate without writing.")
		props["max_diff_bytes"] = boundedIntProp("Maximum diff preview bytes. Defaults to 65536 and is capped at 4194304.", 1, maxTextOutputBytes)
		required = []string{"action"}

	case "exec_command":
		props["cmd"] = stringProp("Command to run.")
		props["workdir"] = stringProp(execCommandWorkdirDescription())
		addExecCommandRuntimeProperties(props)
		props["skill"] = stringProp("Optional active Skill context. When workdir is omitted, the command runs from the active installed Skill root and loads that Skill isolated environment.")
		props["skill_env"] = stringProp("Optional Skill name whose isolated environment is loaded without changing workdir. Kept for environment-only compatibility.")
		props["env"] = map[string]any{"type": "object", "description": "Explicit command environment values. These override the selected Skill environment.", "additionalProperties": map[string]any{"type": "string"}}
		props["timeout_ms"] = boundedIntProp("Timeout in milliseconds. Must be positive and is capped at 86400000.", 1, 86400000)
		props["yield_time_ms"] = boundedIntProp("Initial wait before returning a running session. Capped at 30000 milliseconds.", 0, 30000)
		props["wait_until_exit"] = boolProp("Wait until the command exits instead of returning a running session after yield_time_ms.")
		props["max_output_bytes"] = boundedIntProp("Maximum output bytes. Defaults to 65536 and is capped at 4194304.", 1, maxCommandOutputBytes)
		props["stdin"] = stringProp("Initial stdin.")
		props["tty"] = boolProp("Keep stdin open.")
		props["redact_patterns"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Additional regex patterns to redact from stdout/stderr/error."}
		required = []string{"cmd"}
	case "session_observe":
		props["action"] = map[string]any{"type": "string", "description": "Read-only session action.", "enum": []string{"list", "status"}}
		props["session_id"] = stringProp("Session id returned by exec_command, required for status.")
		props["max_output_bytes"] = boundedIntProp("Maximum output bytes. Defaults to 65536 and is capped at 4194304.", 1, maxCommandOutputBytes)
	case "session_act":
		props["action"] = map[string]any{"type": "string", "description": "Mutating session action.", "enum": []string{"write", "kill", "kill_all"}}
		props["session_id"] = stringProp("Session id returned by exec_command, required for write/kill.")
		props["chars"] = stringProp("Characters to write when action=write.")
		props["max_output_bytes"] = boundedIntProp("Maximum output bytes. Defaults to 65536 and is capped at 4194304.", 1, maxCommandOutputBytes)
	case "task_manage":
		props["action"] = map[string]any{"type": "string", "description": "Task lifecycle action. Use checkpoint to update live step progress.", "enum": []string{"create", "list", "get", "checkpoint", "block", "resume", "final_review", "complete"}}
		props["task_id"] = stringProp("Persistent task id for get, checkpoint, block, resume, final_review, or complete.")
		props["title"] = stringProp("Short task title for create.")
		props["goal"] = stringProp("Fixed task goal for create.")
		props["completion_conditions"] = map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "string"}, "description": "Conditions that must be true before final_review can pass."}
		props["steps"] = map[string]any{
			"type": "array", "maxItems": 12, "description": "Concrete task steps. Required when composing multiple source templates.",
			"items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"id", "title"}, "properties": map[string]any{"id": stringProp("Stable step id."), "title": stringProp("Human-readable step title.")}},
		}
		props["template_id"] = stringProp("Single active workflow template to apply. Its current active version is resolved automatically.")
		props["source_template_ids"] = map[string]any{"type": "array", "minItems": 2, "maxItems": 3, "items": map[string]any{"type": "string"}, "description": "Two or three templates already composed by the model into steps and completion_conditions."}
		props["step_id"] = stringProp("Task step id for a single-step checkpoint.")
		props["completed_step_ids"] = map[string]any{"type": "array", "minItems": 1, "maxItems": 12, "uniqueItems": true, "items": map[string]any{"type": "string"}, "description": "Task step ids to mark completed in one atomic batch checkpoint."}
		props["current_step_id"] = stringProp("Single task step id to mark in_progress in a batch checkpoint.")
		props["status"] = map[string]any{"type": "string", "description": "Action-specific status: task list filter, single-step checkpoint status, or final review status.", "enum": []string{"active", "blocked", "completed", "pending", "in_progress", "pass", "failed"}}
		props["limit"] = intProp("Maximum tasks returned by list. Defaults to 50 and is capped at 200.")
		props["summary"] = stringProp("Current progress, blocker, resume, or final review summary.")
		props["verified"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Facts verified during final_review. Required when status=pass."}
		props["risks"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Remaining risks. Required when final_review status=failed."}
		required = []string{"action"}

	case "workflow_template_manage":
		props["action"] = map[string]any{"type": "string", "description": "Workflow template action. get_many returns full active templates that the model must compose before task creation.", "enum": []string{"save", "validate", "publish", "retire", "list", "get", "get_many", "match", "vector_index"}}
		props["template"] = map[string]any{"type": "object", "additionalProperties": true, "description": "Complete draft workflow template for save."}
		props["template_id"] = stringProp("Workflow template id.")
		props["template_ids"] = map[string]any{"type": "array", "minItems": 2, "maxItems": 3, "items": map[string]any{"type": "string"}, "description": "Two or three active template ids for get_many. The returned templates must be pruned, deduplicated, ordered, and combined by the model."}
		props["template_version"] = stringProp("Workflow template version for exact get, validate, publish, or retire actions.")
		props["template_status"] = map[string]any{"type": "string", "enum": []string{"draft", "active", "retired"}, "description": "Optional list status filter."}
		props["allow_long_template"] = boolProp("Allow a workflow template to exceed default guardrails. Provide long_template_reason when true.")
		props["long_template_reason"] = stringProp("Reason required when allow_long_template=true.")
		props["goal"] = stringProp("Goal text for match.")
		props["device"] = stringProp("Optional device hint for match.")
		props["type"] = stringProp("Optional workflow type hint for match. This maps to template match.type.")
		required = []string{"action"}

	case "skill_package":
		props["action"] = map[string]any{"type": "string", "description": "Skill package or isolated environment action.", "enum": []string{"validate", "install", "rollback", "env_set", "env_unset", "env_list"}}
		props["skill"] = stringProp("Skill name for rollback or environment management.")
		props["key"] = stringProp("Environment variable name for env_set/env_unset.")
		props["value"] = stringProp("Environment variable value for env_set. Secret values are never returned.")
		props["channel"] = map[string]any{"type": "string", "description": "Skill channel: development, canary, stable, or pinned.", "enum": []string{"development", "canary", "stable", "pinned"}}
		props["source"] = stringProp("Host path or HTTP(S) URL for validate/install.")
		props["digest"] = stringProp("Optional expected SHA-256 digest for validate/install.")
		props["activate"] = boolProp("Activate the installed version. Defaults to true.")
		props["max_bytes"] = intProp("Maximum validate/install package bytes.")
		required = []string{"action"}
	case "mcp_manage":
		props["action"] = map[string]any{"type": "string", "description": "Dynamic MCP server or isolated environment action.", "enum": []string{"list", "inspect", "add", "remove", "enable", "disable", "env_set", "env_unset", "env_list", "refresh"}}
		props["name"] = stringProp("Dynamic MCP server name. Use a stable short identifier such as figma or github.")
		props["description"] = stringProp("Short capability description shown in agentdock_context.")
		props["transport"] = map[string]any{"type": "string", "description": "MCP transport for action=add.", "enum": []string{"streamable_http", "stdio"}}
		props["url"] = stringProp("Absolute MCP endpoint URL for transport=streamable_http.")
		props["command"] = stringProp("Executable name or path for transport=stdio.")
		props["args"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Command arguments for transport=stdio."}
		props["cwd"] = stringProp("Optional absolute working directory for transport=stdio.")
		props["header_env"] = map[string]any{"type": "object", "description": "HTTP header name to host environment variable name. Secret values are never stored in the MCP registry.", "additionalProperties": map[string]any{"type": "string"}}
		props["env_from_env"] = map[string]any{"type": "object", "description": "Child process environment variable name to host environment variable name for stdio. Secret values are never stored in the MCP registry.", "additionalProperties": map[string]any{"type": "string"}}
		props["key"] = stringProp("Environment variable name for env_set/env_unset.")
		props["value"] = stringProp("Environment variable value for env_set. Secret values are never returned.")
		props["enabled"] = boolProp("Enable the server after registration. Defaults to true.")
		props["timeout_ms"] = boundedIntProp("Per-request timeout. Defaults to 30000 and is capped at 300000.", 1, 300000)
		required = []string{"action"}
	case "mcp_tool_search":
		props["query"] = stringProp("Capability or tool query.")
		props["server"] = stringProp("Optional dynamic MCP server name from agentdock_context.")
		props["limit"] = boundedIntProp("Maximum matching tools. Defaults to 10 and is capped at 100.", 1, 100)
		required = []string{"query"}
	case "mcp_tool_inspect":
		props["name"] = stringProp("Qualified dynamic MCP tool name in <server>:<tool> form.")
		required = []string{"name"}
	case "mcp_tool_call":
		props["name"] = stringProp("Qualified dynamic MCP tool name in <server>:<tool> form.")
		props["arguments"] = map[string]any{"type": "object", "description": "Arguments matching the schema returned by mcp_tool_inspect.", "additionalProperties": true}
		required = []string{"name", "arguments"}
	case "file_publish":
		props["file"] = map[string]any{"type": "string", "format": "binary", "description": "Top-level file parameter. Connector runtimes should pass the mounted local path when available."}
		props["path"] = stringProp("Local file or directory path visible to this AgentDock instance. Relative paths resolve from ~/AgentDock.")
		props["retention_seconds"] = intProp("Signed URL retention in seconds. Defaults to 86400 and is capped at 604800.")
		required = []string{}
	case "recall_bootstrap":
		props["max_bytes"] = intProp("Maximum combined NexusDock Recall pack bytes. Does not expose section bodies by itself; use include_body or recall_read when body text is needed.")
		props["include_raw"] = boolProp("Include raw Markdown as raw_content. Defaults to false to avoid duplicating body/content tokens.")
		props["include_body"] = boolProp("Include section body text in recall_bootstrap. Defaults to false; prefer recall_read for targeted full text.")
	case "recall_search":
		props["query"] = stringProp("Text query to search in NexusDock Recall files and paths.")
		props["kind"] = map[string]any{"type": "string", "description": "Search kind. Defaults to all.", "enum": []string{"all", "markdown", "card", "note"}}
		props["note_scope"] = map[string]any{"type": "string", "description": "Note only: notes scope to search or capture.", "enum": []string{"questions", "github-learning"}}
		props["max_results"] = intProp("Maximum results to return.")
		required = []string{"query"}
	case "recall_read":
		props["path"] = stringProp("NexusDock Recall-relative Markdown/card/note path.")
		props["include_raw"] = boolProp("Include raw Markdown as raw_content. Defaults to false to avoid duplicating body/content tokens.")
		required = []string{"path"}
	case "recall_write":
		props["target"] = map[string]any{"type": "string", "description": "Recall target selected by the model.", "enum": []string{"card", "note", "markdown"}}
		props["action"] = map[string]any{"type": "string", "description": "Recall action selected by the model.", "enum": []string{"plan", "create", "replace", "append", "patch", "update_fact", "diff", "delete"}}
		props["confirmed"] = boolProp("Required for true writes/deletes. card/note create with confirmed=false returns a review plan.")
		props["path"] = stringProp("NexusDock Recall-relative path when reading, updating, deleting, or writing a known entry.")
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
		props["prefix"] = stringProp("Optional NexusDock Recall-relative prefix.")
		props["terms"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Terms or regex patterns for lint."}
		props["regex"] = boolProp("Treat terms as regex patterns for lint.")
		props["max_entries"] = intProp("Maximum entries to list or scan.")
		props["max_findings"] = intProp("Maximum lint findings to return.")
		props["max_results"] = intProp("Maximum results where supported.")
	case "private_note_manage":
		props["action"] = map[string]any{"type": "string", "description": "Private note action. Do not use by default; use only for explicit private/local-only/non-synced notes or clearly sensitive secrets.", "enum": []string{"search", "read", "write", "status", "maintain"}}
		props["query"] = stringProp("Search query for action=search.")
		props["max_results"] = boundedIntProp("Maximum search results to return. Defaults to 8 and is capped at 100.", 1, maxPrivateNoteSearchResults)
		props["path"] = stringProp("Path under notes/ for action=read or action=write.")
		props["category"] = stringProp("Optional category used with title when path is omitted. Defaults to services.")
		props["title"] = stringProp("Title used for frontmatter or to derive the path when path is omitted.")
		props["summary"] = stringProp("Optional summary for action=write.")
		props["tags"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["content"] = stringProp("Plaintext private note content for action=write.")
		props["confirmed"] = boolProp("Required for action=write true writes.")
		props["overwrite"] = boolProp("Replace an existing note for action=write.")
		props["max_bytes"] = boundedIntProp("Maximum bytes to return for action=read. Defaults to 256000 and is capped at 1048576.", 1, maxPrivateNoteReadBytes)
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
		props["timeout_ms"] = boundedIntProp("Operation timeout in milliseconds. Defaults to 30000 and is capped at 300000.", 1, 300000)
	case "browser_act":
		props["session_id"] = stringProp("Browser session id.")
		props["actions"] = browserActionsProp()
		props["full_page"] = boolProp("Capture full-page screenshot in the final snapshot.")
		props["max_text_chars"] = intProp("Maximum body text characters in snapshot.")
		props["retention_seconds"] = intProp("Signed screenshot URL retention in seconds. Defaults to 86400 and is capped at 604800.")
		props["close_after"] = boolProp("Close and remove the browser session after the action/snapshot succeeds.")
		props["save_storage_state"] = boolProp("Save context storage state and return storage_state_path.")
		props["max_interactive_elements"] = intProp("Maximum visible interactive elements to return. Defaults to 40.")
		props["timeout_ms"] = boundedIntProp("Operation timeout in milliseconds. Defaults to 30000 and is capped at 300000.", 1, 300000)
		required = []string{"session_id", "actions"}
	case "browser_snapshot":
		props["session_id"] = stringProp("Browser session id.")
		props["full_page"] = boolProp("Capture full-page screenshot for snapshot.")
		props["max_text_chars"] = intProp("Maximum body text characters in snapshot.")
		props["retention_seconds"] = intProp("Signed screenshot URL retention in seconds. Defaults to 86400 and is capped at 604800.")
		props["close_after"] = boolProp("Close and remove the browser session after snapshot succeeds.")
		props["save_storage_state"] = boolProp("Save context storage state and return storage_state_path.")
		props["max_interactive_elements"] = intProp("Maximum visible interactive elements to return. Defaults to 40.")
		props["timeout_ms"] = boundedIntProp("Operation timeout in milliseconds. Defaults to 30000 and is capped at 300000.", 1, 300000)
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
		props["timeout_ms"] = boundedIntProp("HTTP timeout in milliseconds for github_repo_access. Defaults to 15000 and is capped at 120000.", 1, 120000)
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
		props["artifact_id"] = stringProp("Artifact id returned by an AgentDock image-producing tool.")
		props["path"] = stringProp("Host image path. Relative paths resolve from ~/AgentDock.")
		props["url"] = stringProp("Absolute HTTP(S) image URL.")
		props["max_source_bytes"] = intProp("Maximum source bytes before processing. Defaults to 20971520 and is capped at 104857600.")
		props["source_timeout_ms"] = boundedIntProp("HTTP(S) source timeout in milliseconds. Defaults to 15000 and is capped at 120000.", 1, 120000)
		props["max_bytes"] = intProp("Maximum processed image bytes returned to the model. Defaults to 750000 and is capped at 2097152.")
		props["max_width"] = intProp("Maximum image width. Defaults to 1280.")
		props["max_height"] = intProp("Maximum image height. Defaults to 1280.")
		props["auto_resize"] = boolProp("Resize/compress when limits are exceeded. Defaults to true.")
		props["format"] = stringProp("Processed image format: jpeg or png. Defaults to jpeg.")
		props["quality"] = intProp("JPEG quality when format is jpeg. Defaults to 72.")
		props["crop"] = map[string]any{"type": "object", "description": "Optional crop rectangle {x,y,width,height} before resizing.", "additionalProperties": true}
	}

	schema := map[string]any{"type": "object", "properties": props, "additionalProperties": true}
	if name == "view_image" {
		schema["oneOf"] = []map[string]any{
			{"required": []string{"artifact_id"}},
			{"required": []string{"path"}},
			{"required": []string{"url"}},
		}
	}
	switch name {
	case "recall_bootstrap", "recall_search", "recall_read", "recall_write", "recall_maintain", "private_note_manage", "mcp_manage", "mcp_tool_search", "mcp_tool_inspect", "mcp_tool_call":
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

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
		props["action"] = map[string]any{"type": "string", "description": "File edit action.", "enum": []string{"replace", "patch", "add", "delete", "move", "atomic_write"}}
		props["path"] = stringProp(filePathDescription("Host path for replace, add, delete, move, or atomic_write. Relative paths resolve from ~/AgentDock."))
		addFileRuntimeProperties(props)
		props["old"] = stringProp("Exact UTF-8 text to replace.")
		props["new"] = stringProp("Replacement UTF-8 text for action=replace.")
		props["replace_all"] = boolProp("Replace every match instead of only the first.")
		props["expected_matches"] = map[string]any{"type": "integer", "description": "Required number of matches. Defaults to 1; zero asserts no matches.", "minimum": 0}
		props["content"] = stringProp("Text content for action=add or action=atomic_write.")
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
		props["execution_mode"] = map[string]any{"type": "string", "description": "Execution mode. Defaults to auto: wait up to yield_time_ms, then return a running session. sync waits for exit; async returns a session immediately.", "enum": []string{"auto", "sync", "async"}}
		props["yield_time_ms"] = boundedIntProp("Foreground wait threshold for execution_mode=auto. Defaults to 5000 and is capped at 30000 milliseconds.", 0, 30000)
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

	case "goal_manage":
		props["action"] = map[string]any{
			"type":        "string",
			"description": "Goal Mode lifecycle action. Long-running verifiable outcomes should use goal_manage instead of task_manage.",
			"enum": []string{
				"create", "list", "get", "commit_turn", "request_approval", "resolve_approval", "update_constraints",
				"pause", "resume", "cancel", "mark_blocked", "mark_completed", "get_evidence",
				"acquire_lease", "release_lease", "add_evidence", "verify", "check_policy",
				"execute_steps", "run_workflow", "bind", "unbind", "store_artifact",
				"request_reasoning", "chatgpt_wake", "chatgpt_worker_status", "set_auto_wake", "set_auto_approve_tools", "chatgpt_force_rotate",
				"orchestrate_start", "orchestrate_stop", "orchestrate_status",
			},
		}
		props["goal_id"] = stringProp("Persistent goal id for get, lease, commit, lifecycle, and evidence actions.")
		props["title"] = stringProp("Short goal title for create.")
		props["objective"] = stringProp("Fixed objective for create. Required.")
		props["workspace_id"] = stringProp("Optional workspace label for create.")
		props["device_id"] = stringProp("Optional device label for create.")
		props["mode"] = map[string]any{"type": "string", "enum": []string{"guarded", "autopilot", "readonly"}, "description": "Policy mode. Defaults to guarded."}
		props["base_git_sha"] = stringProp("Optional baseline git SHA recorded at create.")
		props["success_criteria"] = map[string]any{
			"type": "array", "minItems": 1, "maxItems": 32,
			"description": "Machine-oriented success criteria for create. Each item needs type and expression.",
			"items": map[string]any{
				"type": "object", "additionalProperties": false,
				"required": []string{"expression"},
				"properties": map[string]any{
					"id":         stringProp("Stable criterion id."),
					"type":       map[string]any{"type": "string", "enum": []string{"command", "metric", "browser", "manual"}, "description": "Criterion kind."},
					"expression": stringProp("Checkable expression, e.g. test_exit_code == 0 or url_contains:/dashboard."),
				},
			},
		}
		props["constraints"] = map[string]any{
			"type": "array", "maxItems": 32,
			"description": "Goal constraints for create or update_constraints.",
			"items": map[string]any{
				"type": "object", "additionalProperties": false,
				"required": []string{"type", "value"},
				"properties": map[string]any{
					"type":  map[string]any{"type": "string", "enum": []string{"prohibition", "quality", "approval", "budget"}},
					"value": stringProp("Constraint value such as no_git_push."),
				},
			},
		}
		props["milestones"] = map[string]any{
			"type": "array", "maxItems": 24,
			"description": "Optional milestones for create.",
			"items": map[string]any{
				"type": "object", "additionalProperties": false,
				"required": []string{"title"},
				"properties": map[string]any{
					"id":    stringProp("Stable milestone id."),
					"title": stringProp("Human-readable milestone title."),
				},
			},
		}
		props["budget"] = objectProp("Optional budget overrides for create: max_reasoning_turns, max_replans, max_conversation_rotations, max_runtime_minutes, max_identical_failures, max_browser_retries, max_changed_files.")
		props["worker_id"] = stringProp("Reasoning worker id for acquire_lease.")
		props["reasoning_lease_id"] = stringProp("Active lease id required by commit_turn and optional for release_lease.")
		props["lease_id"] = stringProp("Alias of reasoning_lease_id.")
		props["lease_ttl_seconds"] = boundedIntProp("Lease TTL in seconds for acquire_lease. Defaults to 1800.", 1, 86400)
		props["expected_capsule_version"] = intProp("Capsule version the worker read before commit_turn. Required for commit_turn.")
		props["decision"] = map[string]any{"type": "string", "enum": []string{"continue", "block", "complete", "replan", "pause", "verify"}, "description": "Structured decision for commit_turn."}
		props["summary"] = stringProp("Progress, blocker, approval, completion, or commit summary.")
		props["next_milestone"] = stringProp("Milestone id to activate on commit_turn.")
		props["current_problem"] = stringProp("Latest problem statement stored into the capsule.")
		props["current_request"] = stringProp("Latest request for the next reasoning worker.")
		props["completed"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Notes of completed work to append on commit_turn."}
		props["steps"] = map[string]any{
			"type": "array", "maxItems": 24,
			"description": "Whitelist commit steps. action must be a known StepAction; arbitrary shell strings are rejected.",
			"items": map[string]any{
				"type": "object", "additionalProperties": false,
				"required": []string{"action"},
				"properties": map[string]any{
					"action": map[string]any{
						"type": "string",
						"enum": []string{
							"inspect_files", "prepare_patch", "apply_patch", "run_tests", "run_command", "start_process",
							"browser_navigate", "browser_act", "browser_verify", "collect_logs", "collect_metrics",
							"create_checkpoint", "request_approval", "mark_blocked", "enter_verify", "replan",
						},
					},
					"targets":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional file paths, selectors, or command labels."},
					"summary":         stringProp("Optional step summary."),
					"milestone_id":    stringProp("Optional milestone id."),
					"idempotency_key": stringProp("Optional stable key to avoid duplicate steps on reconnect."),
				},
			},
		}
		props["reason"] = stringProp("Blocker reason for mark_blocked.")
		props["tried"] = stringProp("What was already tried for mark_blocked.")
		props["evidence_text"] = stringProp("Evidence summary text for mark_blocked.")
		props["need_user"] = stringProp("What the user must do for mark_blocked.")
		props["approval_action"] = stringProp("High-risk action name for request_approval.")
		props["risk"] = stringProp("Risk level for request_approval, e.g. medium.")
		props["evidence_ids"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Evidence ids referenced by mark_completed."}
		props["evidence_kind"] = stringProp("Evidence kind for add_evidence, e.g. test_log or screenshot.")
		props["evidence_summary"] = stringProp("Evidence summary for add_evidence.")
		props["evidence_uri"] = stringProp("Optional evidence URI such as artifact://logs/tests-01.")
		props["status"] = stringProp("Optional list status filter.")
		props["limit"] = intProp("Maximum goals returned by list. Defaults to 50 and is capped at 200.")
		props["full"] = boolProp("When true, get also returns the full durable goal document in addition to the capsule.")
		props["approval_id"] = stringProp("Approval id or action key for resolve_approval.")
		props["approval_decision"] = map[string]any{"type": "string", "enum": []string{"approved", "rejected"}, "description": "Decision for resolve_approval."}
		props["evidence_data"] = objectProp("Structured evidence fields for verifier, e.g. exit_code, url, metrics, criterion_id.")
		props["policy_action"] = stringProp("Step action name for check_policy, e.g. run_command or apply_patch.")
		props["policy_targets"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Targets/command fragments for check_policy."}
		props["workflow"] = objectProp("Deterministic workflow for run_workflow: {name, steps:[{type, command, observation, expression, criterion_id,...}]}.")
		props["work_dir"] = stringProp("Working directory for execute_steps/run_workflow. Defaults to AgentDock workspace root.")
		props["artifact_path"] = stringProp("Local file path for store_artifact.")
		props["artifact_text"] = stringProp("Inline text content for store_artifact when artifact_path is not set.")
		props["artifact_filename"] = stringProp("Filename for inline artifact_text.")
		props["artifact_content_type"] = stringProp("Content type for inline artifact_text.")
		props["criterion_id"] = stringProp("Optional success criterion id to attach when store_artifact also links evidence to a goal.")
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
		props["action"] = map[string]any{"type": "string", "description": "Skill package or isolated environment action.", "enum": []string{"validate", "install", "activate", "rollback", "env_set", "env_unset", "env_list"}}
		props["skill"] = stringProp("Skill name for activate, rollback, or environment management.")
		props["version"] = stringProp("Installed Skill version for activate.")
		props["key"] = stringProp("Environment variable name for env_set/env_unset.")
		props["value"] = stringProp("Environment variable value for env_set. Secret values are never returned.")
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
		props["action"] = map[string]any{"type": "string", "description": "NexusDock private note action. Do not use by default; use only for explicit private note access or clearly sensitive secrets, credentials, or personal information.", "enum": []string{"search", "read", "write", "delete", "status", "maintain"}}
		props["query"] = stringProp("Metadata-only query for action=search. Matches title, summary, tags, category, and path; never searches plaintext body.")
		props["max_results"] = boundedIntProp("Maximum metadata search results to return. Defaults to 8 and is capped at 100.", 1, maxPrivateNoteSearchResults)
		props["path"] = stringProp("Path under notes/ for action=read, action=write, or action=delete.")
		props["category"] = stringProp("Optional category used with title when path is omitted. Defaults to services.")
		props["title"] = stringProp("Title used for frontmatter or to derive the path when path is omitted.")
		props["summary"] = stringProp("Optional human-maintained safe summary for metadata-only search.")
		props["tags"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional safe tags for metadata-only search."}
		props["content"] = stringProp("Plaintext private note content for action=write.")
		props["confirmed"] = boolProp("Required for true action=write and action=delete mutations.")
		props["overwrite"] = boolProp("Replace an existing note for action=write.")
		props["max_bytes"] = boundedIntProp("Maximum bytes to return for explicit action=read. Defaults to 256000 and is capped at 1048576.", 1, maxPrivateNoteReadBytes)
		props["status_action"] = map[string]any{"type": "string", "description": "Read-only status action when action=status.", "enum": []string{"check", "list"}}
		props["maintenance_action"] = map[string]any{"type": "string", "description": "NexusDock encryption maintenance operation when action=maintain.", "enum": []string{"init", "init-encryption", "sync-encrypted", "encrypt-all"}}
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
		props["storage_state_path"] = stringProp("Optional Playwright storage state JSON path to load when action=start. When saving, an explicitly supplied path is reused as the destination.")
		props["save_storage_state"] = boolProp("Save context storage state after action/snapshot and return storage_state_path.")
		props["reload_after_local_storage"] = boolProp("Reload the page after applying localStorage. Defaults to true.")
		props["max_age_ms"] = intProp("When action=cleanup_stale, remove sessions older than this age. Defaults to 6 hours.")
		props["timeout_ms"] = boundedIntProp("Operation timeout in milliseconds. Defaults to 30000 and is capped at 300000.", 1, 300000)
	case "browser_act":
		props["session_id"] = stringProp("Browser session id.")
		props["page_id"] = stringProp("Page id returned by browser_session, browser_act, or browser_snapshot. Selects which page receives the actions.")
		props["actions"] = browserActionsProp()
		props["full_page"] = boolProp("Capture full-page screenshot in the final snapshot.")
		props["max_text_chars"] = intProp("Maximum body text characters in snapshot.")
		props["retention_seconds"] = intProp("Signed screenshot URL retention in seconds. Defaults to 86400 and is capped at 604800.")
		props["close_after"] = boolProp("Close and remove the browser session after the action/snapshot succeeds.")
		props["save_storage_state"] = boolProp("Save context storage state and return storage_state_path.")
		props["storage_state_path"] = stringProp("Optional destination path when save_storage_state=true. Relative paths resolve under browser artifacts.")
		props["max_interactive_elements"] = intProp("Maximum visible interactive elements to return. Defaults to 40.")
		props["timeout_ms"] = boundedIntProp("Operation timeout in milliseconds. Defaults to 30000 and is capped at 300000.", 1, 300000)
		required = []string{"session_id", "actions"}
	case "browser_snapshot":
		props["session_id"] = stringProp("Browser session id.")
		props["page_id"] = stringProp("Page id returned by browser_session, browser_act, or browser_snapshot. Selects which page is captured.")
		props["full_page"] = boolProp("Capture full-page screenshot for snapshot.")
		props["max_text_chars"] = intProp("Maximum body text characters in snapshot.")
		props["retention_seconds"] = intProp("Signed screenshot URL retention in seconds. Defaults to 86400 and is capped at 604800.")
		props["close_after"] = boolProp("Close and remove the browser session after snapshot succeeds.")
		props["save_storage_state"] = boolProp("Save context storage state and return storage_state_path.")
		props["storage_state_path"] = stringProp("Optional destination path when save_storage_state=true. Relative paths resolve under browser artifacts.")
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
		// 严格的工具调用供应商会独立校验 oneOf 分支类型；每个分支都显式声明为对象，
		// 避免把只有 required 的分支识别成可能接受非对象值。
		schema["oneOf"] = []map[string]any{
			{"type": "object", "required": []string{"artifact_id"}},
			{"type": "object", "required": []string{"path"}},
			{"type": "object", "required": []string{"url"}},
		}
	}
	switch name {
	case "exec_command", "recall_bootstrap", "recall_search", "recall_read", "recall_write", "recall_maintain", "private_note_manage", "mcp_manage", "mcp_tool_search", "mcp_tool_inspect", "mcp_tool_call":
		// 这些工具的参数契约需要严格收敛，避免删除或拼错的字段被静默忽略。
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
					"enum":        []string{"goto", "click", "fill", "press", "wait", "wait_for_selector", "wait_for_url", "wait_for_text", "wait_for_response", "select", "scroll", "reload", "back", "forward"},
				},
				"url":         map[string]any{"type": "string", "description": "URL for action=goto, glob/substring for wait_for_url, or URL substring for wait_for_response."},
				"selector":    map[string]any{"type": "string", "description": "CSS selector for click/fill/press/wait_for_selector/select."},
				"value":       map[string]any{"description": "Value for fill/select, wait duration for wait, or fallback text for wait_for_text."},
				"text":        map[string]any{"type": "string", "description": "Text to wait for when action=wait_for_text."},
				"exact":       map[string]any{"type": "boolean", "description": "Require exact text match for wait_for_text."},
				"state":       map[string]any{"type": "string", "description": "Playwright locator state for wait_for_text, such as visible or hidden."},
				"url_pattern": map[string]any{"type": "string", "description": "Regular expression matched against response URLs for wait_for_response."},
				"method":      map[string]any{"type": "string", "description": "Optional HTTP method for wait_for_response."},
				"status":      map[string]any{"type": "integer", "description": "Optional HTTP status for wait_for_response."},
				"key":         map[string]any{"type": "string", "description": "Keyboard key for action=press."},
				"timeout_ms":  map[string]any{"type": "integer", "description": "Timeout for wait_for_selector, wait_for_url, wait_for_text, or wait_for_response."},
				"wait_until":  map[string]any{"type": "string", "description": "Navigation wait state for goto/reload/back/forward/wait_for_url, such as domcontentloaded or load."},
				"delta_x":     map[string]any{"type": "integer", "description": "Horizontal wheel delta for action=scroll."},
				"delta_y":     map[string]any{"type": "integer", "description": "Vertical wheel delta for action=scroll."},
			},
		},
	}
}

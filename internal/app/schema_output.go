package app

import (
	mcpcontract "github.com/uvwt/agentdock-protocol/mcpcontract"
	toolfile "github.com/uvwt/agentdock/internal/tool/file"
)

func OutputSchema(name string) map[string]any {
	if name == mcpcontract.ToolAgentDockContext {
		return mcpcontract.LocalAgentDockContextOutputSchema()
	}
	if schema, ok := mcpcontract.OutputSchema(name); ok {
		return schema
	}
	props := map[string]any{}
	// MCP envelope 的 isError 已表达工具调用错误；这里只描述领域结果，
	// 不再要求含义模糊的通用 ok 字段。
	required := []string{}
	stringProp := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	intProp := func(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
	boolProp := func(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }
	arrayProp := func(desc string) map[string]any {
		return map[string]any{"type": "array", "description": desc, "items": map[string]any{"type": "object", "additionalProperties": true}}
	}
	objectProp := func(desc string) map[string]any {
		return map[string]any{"type": "object", "description": desc, "additionalProperties": true}
	}

	switch name {
	case "server_info":
		props["server"] = stringProp("Server identifier.")
		props["title"] = stringProp("Human-readable server title.")
		props["version"] = stringProp("Server version.")
		props["protocol_version"] = stringProp("AgentDock protocol version.")
		props["os"] = stringProp("Host operating system reported by the Go runtime.")
		props["arch"] = stringProp("Host architecture reported by the Go runtime.")
		props["go_version"] = stringProp("Go runtime version.")
		props["agentdock_home"] = stringProp("AgentDock state and configuration directory.")
		props["agentdock_default_dir"] = stringProp("AgentDock default working directory.")
		props["default_cwd"] = stringProp("Default cwd relative to ~/AgentDock when applicable.")
		props["path_model"] = stringProp("Path resolution model used by host tools.")
		props["recall_enabled"] = boolProp("Whether NexusDock Recall integration is enabled.")
		props["nexus_endpoint"] = stringProp("Configured NexusDock endpoint.")
		props["recall_bootstrap_recommended"] = boolProp("Whether clients should load Recall bootstrap context.")
		props["recall_bootstrap_tool"] = stringProp("Tool name used to load Recall bootstrap context.")
		props["recall_bootstrap_args"] = objectProp("Recommended arguments for Recall bootstrap.")
		props["tools"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["tool_count"] = intProp("Number of exposed tools.")
		props["task_state_dir"] = stringProp("Local directory containing persistent recoverable task state.")
		props["command_session_limits"] = objectProp("Concurrent and retained command-session limits.")
		props["browser_enabled"] = boolProp("Whether browser tools are enabled by host configuration.")
		props["acp_enabled"] = boolProp("Whether the built-in ACP runtime is enabled by host configuration.")
		props["acp_agent"] = stringProp("Configured local ACP agent profile name.")
		props["trusted_proxy_cidrs"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		props["auth_enabled"] = boolProp("Whether MCP authentication is required.")
		props["endpoint_path"] = stringProp("HTTP path used by the MCP endpoint.")
	case "read_file":
		props["path"] = stringProp("Host path or skill:// resource URI. Relative Host paths resolve from ~/AgentDock.")
		props["content"] = stringProp("Text content slice.")
		props["encoding"] = stringProp("Detected text encoding.")
		props["size_bytes"] = intProp("File size in bytes.")
		props["truncated"] = boolProp("Whether output was truncated.")
		props["truncated_reason"] = stringProp("Reason output was truncated.")
		props["start_line"] = intProp("Returned start line.")
		props["end_line"] = intProp("Returned end line.")
		props["next_start_line"] = intProp("Next line to read when output was truncated.")
		props["total_lines"] = intProp("Total line count.")
	case "list_dir":
		props["path"] = stringProp("Host directory path. Relative paths resolve from ~/AgentDock.")
		props["entries"] = arrayProp("Directory entries.")
		props["truncated"] = boolProp("Whether entries were truncated.")
	case "list_files":
		props["path"] = stringProp("Host directory path. Relative paths resolve from ~/AgentDock.")
		props["files"] = arrayProp("Matched files.")
		props["truncated"] = boolProp("Whether files were truncated.")
		props["partial"] = boolProp("Whether unreadable descendant paths were skipped while returning readable matches.")
		props["skipped_paths"] = arrayProp("Unreadable descendant paths skipped during traversal.")
	case "search_text":
		props["matches"] = arrayProp("Text search matches.")
		props["engine"] = stringProp("Search engine used: rg or go_fallback.")
		props["truncated"] = boolProp("Whether matches were truncated.")
	case "file_edit":
		props["action"] = stringProp("File edit action.")
		props["summary"] = stringProp("Result summary.")
		props["path"] = stringProp("Host path. Relative paths resolve from ~/AgentDock.")
		props["new_path"] = stringProp("Move destination path.")
		props["workdir"] = stringProp("Patch working directory.")
		props["affected_files"] = map[string]any{"type": "array", "description": "Files affected by a patch.", "items": map[string]any{"type": "string"}}
		props["dry_run"] = boolProp("Whether this was a dry run.")
		props["matches"] = intProp("Match count for replace.")
		props["changed"] = boolProp("Whether content changed.")
		props["recursive"] = boolProp("Whether delete was allowed to remove a directory recursively.")
		props["diff_preview"] = stringProp("Diff preview.")
		props["truncated"] = boolProp("Whether the diff preview was truncated.")
		props["files_changed"] = intProp("Changed file count.")
		props["insertions"] = intProp("Inserted line count.")
		props["deletions"] = intProp("Deleted line count.")

	case "exec_command", "session_observe", "session_act":
		props["sessions"] = arrayProp("Command session summaries returned by list or bulk session actions.")
		props["count"] = intProp("Command session count when a list or bulk action returns multiple sessions.")
		props["session_id"] = stringProp("Command session id.")
		props["status"] = stringProp("Session status.")
		props["runtime"] = stringProp("Command runtime when reported by the host, such as windows or wsl.")
		props["wsl_distribution"] = stringProp("WSL distribution selected for the command when explicitly configured.")
		props["workdir"] = stringProp("Logical command working directory in the selected runtime.")
		props["stdout"] = stringProp("Captured stdout segment.")
		props["stderr"] = stringProp("Captured stderr segment.")
		props["command_ok"] = boolProp("Whether a completed command exited successfully. Omitted while the command is still running.")
		props["command_error"] = stringProp("Command process error when execution did not succeed.")
		props["exit_code"] = intProp("Process exit code, when available.")
		props["elapsed_ms"] = intProp("Session elapsed milliseconds.")
		props["timed_out"] = boolProp("Whether the command timed out.")
		if name == "exec_command" {
			props["session_reason"] = stringProp("Why exec_command returned a session instead of a completed result.")
			props["observe_after_ms"] = intProp("Suggested delay before inspecting the returned session.")
		}
	case "task_manage":
		props["action"] = stringProp("Completed task action.")
		props["task_id"] = stringProp("Persistent task id returned by create and usable with task lifecycle actions.")
		props["task"] = objectProp("Full persistent task state returned only by get.")
		props["task_summary"] = objectProp("Compact task summary returned by task lifecycle actions.")
		props["next_required_action"] = stringProp("Concise guidance for checkpoint progress, template composition, or final review.")
		props["review_status"] = stringProp("Final review status when present: not_started, pass, or failed.")
		props["final_review"] = objectProp("Compact final review state with status and counts.")
		props["tasks"] = arrayProp("Compact task summaries ordered by most recent update.")
		props["count"] = intProp("Returned item count.")
		props["state_dir"] = stringProp("Local AgentDock task state directory.")
		props["guidance_context"] = arrayProp("Mature evolution records automatically recalled before task execution.")
		props["review_revision"] = stringProp("Immutable final_review revision used to bind evolution evidence.")
		props["evolution_candidates"] = arrayProp("Read-only candidate experiences that the saved final_review may verify.")
		props["evolution_warning"] = stringProp("Non-blocking evolution-side warning; Task lifecycle still succeeded.")
	case "evolve":
		props["intent"] = stringProp("Completed evolution intent.")
		props["evolution_id"] = stringProp("Stable evolution id.")
		props["status"] = stringProp("Lifecycle status computed by AgentDock policy.")
		props["revision"] = intProp("Nexus-backed lifecycle revision.")
		props["policy_version"] = stringProp("AgentDock policy version used for the transition.")
		props["support_count"] = intProp("Independent support evidence count computed by AgentDock.")
		props["contradict_count"] = intProp("Independent contradiction evidence count computed by AgentDock.")
		props["changed"] = boolProp("Whether durable evolution state changed.")
		props["idempotent"] = boolProp("Whether the request resolved to already-applied state.")
		props["message"] = stringProp("Short non-sensitive result explanation.")
	case "acp_session":
		props["action"] = stringProp("Completed ACP session action.")
		props["protocol_version"] = intProp("Negotiated ACP protocol version for info.")
		props["auth_method_id"] = stringProp("Authentication method selected by authenticate.")
		props["authenticated"] = boolProp("Whether the advertised authentication method completed successfully.")
		props["agent"] = objectProp("Configured ACP agent identity.")
		props["capabilities"] = objectProp("Capabilities reported by the ACP agent during initialize.")
		props["auth_methods"] = arrayProp("Authentication methods advertised by the ACP agent during initialize.")
		props["context_policy"] = objectProp("Transcript ownership and restart policy used to avoid duplicated or replay-corrupted context.")
		props["event_policy"] = objectProp("Bounded incremental event delivery policy.")
		props["interaction_policy"] = objectProp("In-memory permission interaction bounds and local authorization policy.")
		props["steering_policy"] = objectProp("Native steering and observable host-owned fallback policy.")
		props["session"] = objectProp("Persistent AgentDock ACP session record.")
		props["sessions"] = arrayProp("Persistent ACP session records ordered by most recent update.")
		props["session_id"] = stringProp("AgentDock ACP session id.")
		props["modes"] = objectProp("Session modes returned by the ACP agent when present.")
		props["config_options"] = arrayProp("Current session configuration options returned by the ACP agent when present.")
		props["mode_id"] = stringProp("Session mode id applied by set_mode.")
		props["config_id"] = stringProp("Session configuration option id applied by set_config.")
		props["changed"] = boolProp("Whether a session mode or configuration option was changed.")
		props["count"] = intProp("Returned ACP session count.")
		props["deleted"] = boolProp("Whether the persistent ACP session was deleted.")
	case "acp_prompt":
		props["action"] = stringProp("Completed ACP prompt action.")
		props["run_id"] = stringProp("AgentDock ACP prompt run id.")
		props["session_id"] = stringProp("AgentDock ACP session id.")
		props["status"] = stringProp("ACP prompt run status.")
		props["events"] = arrayProp("Ordered ACP session events with monotonic seq values.")
		props["next_seq"] = intProp("Cursor to pass unchanged as after_seq on the next events call.")
		props["first_seq"] = intProp("Oldest event sequence still retained in the bounded run event ring.")
		props["latest_seq"] = intProp("Newest event sequence observed for the run when this page was read.")
		props["dropped_count"] = intProp("Number of oldest events evicted from the bounded run event ring.")
		props["has_more"] = boolProp("Whether more retained events are immediately available after next_seq.")
		props["truncated"] = boolProp("Whether requested event history was older than the retained event ring.")
		props["started_at"] = stringProp("Prompt run start timestamp.")
		props["ended_at"] = stringProp("Prompt run end timestamp when settled.")
		props["stop_reason"] = stringProp("ACP stop reason when supplied by the agent.")
		props["error_code"] = stringProp("AgentDock ACP error code when the run failed.")
		props["message"] = stringProp("ACP run error message when present.")
		props["steering"] = objectProp("ACP steering outcome.")
		props["cancel_requested"] = boolProp("Whether cancellation was requested.")
	case "acp_interaction":
		props["action"] = stringProp("Completed ACP interaction action.")
		props["interaction"] = objectProp("ACP permission interaction state.")
		props["interactions"] = arrayProp("ACP permission interactions.")
		props["count"] = intProp("Returned ACP interaction count.")
		props["responded"] = boolProp("Whether a permission option response was accepted.")
		props["cancelled"] = boolProp("Whether the interaction was cancelled.")
	case "skill_package":
		props["action"] = stringProp("Completed Skill package action.")
		props["skill"] = stringProp("Skill name.")
		props["name"] = stringProp("Skill name for environment actions.")
		props["key"] = stringProp("Environment variable name. Secret values are never returned.")
		props["configured"] = boolProp("Whether the environment variable has a non-empty configured value.")
		props["removed"] = boolProp("Whether the environment variable was removed.")
		props["items"] = arrayProp("Environment variable names and configured status without values.")
		props["count"] = intProp("Returned environment variable count.")
		props["valid"] = boolProp("Whether a Skill source passed validation.")
		props["source"] = stringProp("Resolved Skill source label.")
		props["digest"] = stringProp("Computed Skill package digest.")
		props["issues"] = arrayProp("Structured validation issues.")
		props["document"] = objectProp("Parsed SKILL.md frontmatter and body metadata.")
		props["result"] = objectProp("Install, activate, or rollback result.")
	case "mcp_manage":
		props["action"] = stringProp("Completed dynamic MCP management action.")
		props["servers"] = arrayProp("Registered dynamic MCP server summaries.")
		props["server"] = objectProp("Dynamic MCP server summary.")
		props["config"] = objectProp("Dynamic MCP server configuration containing only non-secret values and environment variable names.")
		props["tools"] = arrayProp("Discovered lightweight MCP tool summaries.")
		props["tool_count"] = intProp("Discovered tool count.")
		props["count"] = intProp("Registered server count.")
		props["name"] = stringProp("Dynamic MCP server name.")
		props["removed"] = boolProp("Whether the server or environment variable was removed.")
		props["key"] = stringProp("Environment variable name. Secret values are never returned.")
		props["configured"] = boolProp("Whether the environment variable has a non-empty configured value.")
		props["items"] = arrayProp("Environment variable names and configured status without values.")
	case "mcp_tool_search":
		props["query"] = stringProp("Capability query used.")
		props["server"] = stringProp("Optional server filter used.")
		props["tools"] = arrayProp("Matching lightweight MCP tool summaries.")
		props["count"] = intProp("Matching tool count.")
	case "mcp_tool_inspect":
		props["name"] = stringProp("Qualified MCP tool name.")
		props["server"] = stringProp("Dynamic MCP server name.")
		props["tool_name"] = stringProp("Upstream MCP tool name.")
		props["title"] = stringProp("Tool title.")
		props["description"] = stringProp("Tool description.")
		props["input_schema"] = objectProp("Complete upstream MCP tool input schema.")
		props["output_schema"] = objectProp("Optional upstream MCP tool output schema.")
		props["annotations"] = objectProp("Optional upstream MCP tool annotations.")
	case "mcp_tool_call":
		props["name"] = stringProp("Qualified MCP tool name.")
		props["result"] = objectProp("Raw upstream MCP tools/call result, including content and structuredContent when supplied.")
	case "file_publish":
		props["artifact_id"] = stringProp("Published artifact id.")
		props["url"] = stringProp("Optional temporary signed download URL when a reachable base URL is available.")
		props["expires_at"] = stringProp("Signed URL expiry timestamp.")
		props["sha256"] = stringProp("Snapshot payload SHA-256.")
		props["size_bytes"] = intProp("Snapshot payload size in bytes.")
		props["mime_type"] = stringProp("Payload media type.")
		props["filename"] = stringProp("Download filename used for Content-Disposition and signature verification.")
		props["archive"] = boolProp("Whether the source directory was packaged as tar.gz.")
		props["width"] = intProp("Image width when the payload is an image.")
		props["height"] = intProp("Image height when the payload is an image.")
	case "browser_session":
		props["browser_ok"] = boolProp("Whether the native Go CDP browser operation succeeded.")
		props["error"] = objectProp("Structured browser error with code, message, phase, and optional details.")
		props["code"] = stringProp("One of the native browser error codes.")
		props["session_id"] = stringProp("Current-process browser session id.")
		props["page_id"] = stringProp("Active CDP target id.")
		props["pages"] = arrayProp("Current page targets.")
		props["url"] = stringProp("Active page URL after start.")
		props["title"] = stringProp("Active page title after start.")
		props["profile_id"] = stringProp("Normalized persistent profile id when configured.")
		props["connection_mode"] = stringProp("How action=start obtained the browser: owned, external_explicit, external_configured, or external_discovered.")
		props["closed"] = boolProp("Whether action=close closed the AgentDock browser session. External browsers remain running.")
		props["removed_count"] = intProp("Number of current-process stale sessions terminated by cleanup_stale.")
		props["removed_sessions"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Session ids terminated by cleanup_stale."}
	case "browser_act", "browser_snapshot":
		props["browser_ok"] = boolProp("Whether the native Go CDP browser operation succeeded.")
		props["error"] = objectProp("Structured browser error with code, message, phase, and optional details.")
		props["code"] = stringProp("One of the native browser error codes.")
		props["session_id"] = stringProp("Current-process browser session id.")
		props["page_id"] = stringProp("Active CDP target id.")
		props["pages"] = arrayProp("Current page targets with active state.")
		props["url"] = stringProp("Current page URL.")
		props["title"] = stringProp("Current page title.")
		props["text"] = stringProp("Normalized current page body text excerpt.")
		props["viewport"] = objectProp("Viewport dimensions in CSS pixels.")
		props["page_size"] = objectProp("Scrollable page dimensions in CSS pixels.")
		props["focused_element"] = objectProp("Focused DOM element summary when available.")
		props["interactive_elements"] = arrayProp("Visible interactive DOM element summaries.")
		props["screenshot"] = objectProp("Published PNG Artifact reference.")
		props["console_errors"] = arrayProp("Console errors captured for this operation.")
		props["network_errors"] = arrayProp("Network loading failures captured for this operation.")
		props["page_errors"] = arrayProp("Unhandled page exceptions captured for this operation.")
		props["closed"] = boolProp("Whether close_after terminated the session after successful Artifact publication.")
	case "view_image":
		props["source"] = objectProp("Resolved artifact, path, or URL source metadata.")
		props["image"] = objectProp("Processed image metadata attached as standard MCP image content.")
		props["original"] = objectProp("Original/crop metadata.")
		props["resized"] = boolProp("Whether image bytes changed due to crop/resize/re-encode.")
		props["warnings"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	}

	switch name {
	case "read_file", "list_dir", "list_files", "search_text", "file_edit":
		toolfile.AddRuntimeOutputProperties(props)
	}
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": true}
}

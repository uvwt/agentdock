package app

func acpInputSchema(name string) map[string]any {
	props := map[string]any{}
	required := []string{}
	switch name {
	case "acp_session":
		props["action"] = map[string]any{"type": "string", "description": "ACP session action.", "enum": []string{"info", "authenticate", "new", "load", "resume", "fork", "set_mode", "set_config", "list", "inspect", "close", "delete"}}
		props["auth_method_id"] = schemaStringProperty("Authentication method id advertised by initialize, required for authenticate.")
		props["session_id"] = schemaStringProperty("AgentDock ACP session id for load, resume, fork, set_mode, set_config, inspect, close, or delete.")
		props["cwd"] = schemaStringProperty("Working directory for new or fork. Relative paths resolve from AgentDock's default directory; absolute paths may use any host-accessible directory.")
		props["additional_directories"] = map[string]any{"type": "array", "maxItems": 16, "uniqueItems": true, "items": map[string]any{"type": "string"}, "description": "Additional workspace directories for new or fork. Each path must resolve to a host-accessible directory."}
		props["mode_id"] = schemaStringProperty("Agent-advertised session mode id for set_mode.")
		props["config_id"] = schemaStringProperty("Agent-advertised session configuration option id for set_config.")
		props["config_value"] = map[string]any{"description": "String value id or boolean value for set_config.", "oneOf": []map[string]any{{"type": "string"}, {"type": "boolean"}}}
		required = []string{"action"}
	case "acp_prompt":
		props["action"] = map[string]any{"type": "string", "description": "ACP prompt action.", "enum": []string{"start", "events", "steer", "cancel"}}
		props["session_id"] = schemaStringProperty("AgentDock ACP session id for start, steer, or cancel.")
		props["run_id"] = schemaStringProperty("ACP prompt run id for events or cancel.")
		props["text"] = schemaStringProperty("Prompt text for start or steer. Capped at 256 KiB for start.")
		props["after_seq"] = map[string]any{"type": "integer", "description": "Return events with seq greater than this value. Defaults to 0. Pass next_seq unchanged; values newer than latest_seq are rejected to prevent cursor poisoning.", "minimum": 0}
		props["limit"] = schemaBoundedIntegerProperty("Maximum events to return. Defaults to 100 and is capped at 200.", 1, 200)
		props["wait_ms"] = schemaBoundedIntegerProperty("Bounded long-poll duration for events. Defaults to 0 and is capped at 25000 milliseconds.", 0, 25000)
		required = []string{"action"}
	case "acp_interaction":
		props["action"] = map[string]any{"type": "string", "description": "ACP interaction action.", "enum": []string{"list", "inspect", "respond", "cancel"}}
		props["session_id"] = schemaStringProperty("Optional ACP session filter for list.")
		props["interaction_id"] = schemaStringProperty("Pending ACP interaction id for inspect, respond, or cancel.")
		props["option_id"] = schemaStringProperty("Permission option id for respond. It must be currently offered and permitted by local policy.")
		props["pending_only"] = schemaBooleanProperty("Return only pending interactions for list. Defaults to true.")
		required = []string{"action"}
	}
	return finalizeInputSchema(name, props, required)
}

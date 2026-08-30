package acp

import toolcontract "github.com/uvwt/agentdock/internal/tool/contract"

const (
	ToolSession     = "acp_session"
	ToolPrompt      = "acp_prompt"
	ToolInteraction = "acp_interaction"
)

func InputSchema(name string) (map[string]any, bool) {
	stringProp := toolcontract.String
	boolProp := toolcontract.Boolean
	boundedIntProp := toolcontract.BoundedInteger
	props := map[string]any{}
	var required []string

	switch name {
	case ToolSession:
		props["action"] = map[string]any{"type": "string", "description": "ACP session action.", "enum": []string{"info", "authenticate", "new", "load", "resume", "fork", "set_mode", "set_config", "list", "inspect", "close", "delete"}}
		props["auth_method_id"] = stringProp("Authentication method id advertised by initialize, required for authenticate.")
		props["session_id"] = stringProp("AgentDock ACP session id for load, resume, fork, set_mode, set_config, inspect, close, or delete.")
		props["cwd"] = stringProp("Working directory for new or fork. Relative paths resolve from AgentDock's default directory; absolute paths may use any host-accessible directory.")
		props["additional_directories"] = map[string]any{"type": "array", "maxItems": 16, "uniqueItems": true, "items": map[string]any{"type": "string"}, "description": "Additional workspace directories for new or fork. Each path must resolve to a host-accessible directory."}
		props["mode_id"] = stringProp("Agent-advertised session mode id for set_mode.")
		props["config_id"] = stringProp("Agent-advertised session configuration option id for set_config.")
		props["config_value"] = map[string]any{"description": "String value id or boolean value for set_config.", "oneOf": []map[string]any{{"type": "string"}, {"type": "boolean"}}}
		required = []string{"action"}
	case ToolPrompt:
		props["action"] = map[string]any{"type": "string", "description": "ACP prompt action.", "enum": []string{"start", "events", "steer", "cancel"}}
		props["session_id"] = stringProp("AgentDock ACP session id for start, steer, or cancel.")
		props["run_id"] = stringProp("ACP prompt run id for events or cancel.")
		props["text"] = stringProp("Prompt text for start or steer. Capped at 256 KiB for start.")
		props["after_seq"] = map[string]any{"type": "integer", "description": "Return events with seq greater than this value. Defaults to 0. Pass next_seq unchanged; values newer than latest_seq are rejected to prevent cursor poisoning.", "minimum": 0}
		props["limit"] = boundedIntProp("Maximum events to return. Defaults to 100 and is capped at 200.", 1, 200)
		props["wait_ms"] = boundedIntProp("Bounded long-poll duration for events. Defaults to 0 and is capped at 25000 milliseconds.", 0, 25000)
		required = []string{"action"}
	case ToolInteraction:
		props["action"] = map[string]any{"type": "string", "description": "ACP interaction action.", "enum": []string{"list", "inspect", "respond", "cancel"}}
		props["session_id"] = stringProp("Optional ACP session filter for list.")
		props["interaction_id"] = stringProp("Pending ACP interaction id for inspect, respond, or cancel.")
		props["option_id"] = stringProp("Permission option id for respond. It must be currently offered and permitted by local policy.")
		props["pending_only"] = boolProp("Return only pending interactions for list. Defaults to true.")
		required = []string{"action"}
	default:
		return nil, false
	}
	return toolcontract.InputObject(props, required...), true
}

func OutputSchema(name string) (map[string]any, bool) {
	stringProp := toolcontract.String
	intProp := toolcontract.Integer
	boolProp := toolcontract.Boolean
	arrayProp := toolcontract.ObjectArray
	objectProp := toolcontract.OpenObject
	props := map[string]any{}

	switch name {
	case ToolSession:
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
	case ToolPrompt:
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
	case ToolInteraction:
		props["action"] = stringProp("Completed ACP interaction action.")
		props["interaction"] = objectProp("ACP permission interaction state.")
		props["interactions"] = arrayProp("ACP permission interactions.")
		props["count"] = intProp("Returned ACP interaction count.")
		props["responded"] = boolProp("Whether a permission option response was accepted.")
		props["cancelled"] = boolProp("Whether the interaction was cancelled.")
	default:
		return nil, false
	}
	return toolcontract.OutputObject(props), true
}

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
		props["profile_id"] = stringProp("Configured ACP profile id. Omit to use the default profile.")
		props["action"] = map[string]any{"type": "string", "description": "AgentDock ACP session management action.", "enum": []string{"info", "new", "list", "inspect", "open", "update", "close", "delete"}}
		props["auth_method_id"] = stringProp("Optional authentication method advertised by info. info authenticates and then returns refreshed agent metadata; other session actions authenticate before performing the requested action.")
		props["session_id"] = stringProp("AgentDock managed session id for inspect, open, update, close, or delete.")
		props["remote_session_id"] = stringProp("Adapter-native session id for inspect, open, or delete without requiring a prior AgentDock mapping.")
		props["from_session_id"] = stringProp("Optional managed source session for new. When present and the Adapter supports fork, new creates an independent fork.")
		props["cwd"] = stringProp("Working directory for new, list filtering, or remote inspection fallback. Relative paths resolve from AgentDock's default directory.")
		props["additional_directories"] = map[string]any{"type": "array", "maxItems": 16, "uniqueItems": true, "items": map[string]any{"type": "string"}, "description": "Additional workspace directories for new or forked sessions."}
		props["cursor"] = stringProp("Adapter session/list cursor. Pass next_cursor unchanged to continue native-session pagination.")
		props["include_history"] = boolProp("For inspect, load Adapter-owned history through standard session/load. Defaults to false.")
		props["mode_id"] = stringProp("Agent-advertised session mode id for update. Provide exactly one of mode_id or config_id.")
		props["config_id"] = stringProp("Agent-advertised session configuration option id for update. Provide exactly one of mode_id or config_id.")
		props["config_value"] = map[string]any{"description": "String value id or boolean value when update uses config_id.", "oneOf": []map[string]any{{"type": "string"}, {"type": "boolean"}}}
		required = []string{"action"}
	case ToolPrompt:
		props["profile_id"] = stringProp("Configured ACP profile id. Omit to use the default profile.")
		props["action"] = map[string]any{"type": "string", "description": "AgentDock ACP prompt Run action.", "enum": []string{"start", "events", "cancel"}}
		props["session_id"] = stringProp("AgentDock managed session id for start or cancel.")
		props["run_id"] = stringProp("AgentDock prompt Run id for events or cancel.")
		props["prompt"] = map[string]any{
			"type": "array", "minItems": 1, "maxItems": 64,
			"items":       map[string]any{"type": "object", "additionalProperties": true},
			"description": "ACP ContentBlock array. Text and resource_link are baseline; image/audio/resource are capability-gated from initialize.promptCapabilities.",
		}
		props["after_seq"] = map[string]any{"type": "integer", "description": "Return events with seq greater than this value. Defaults to 0. Pass next_seq unchanged; values newer than latest_seq are rejected to prevent cursor poisoning.", "minimum": 0}
		props["limit"] = boundedIntProp("Maximum events to return. Defaults to 100 and is capped at 200.", 1, 200)
		props["wait_ms"] = boundedIntProp("Bounded long-poll duration for events. Defaults to 0 and is capped at 25000 milliseconds.", 0, 25000)
		required = []string{"action"}
	case ToolInteraction:
		props["profile_id"] = stringProp("Configured ACP profile id. Omit to use the default profile.")
		props["action"] = map[string]any{"type": "string", "description": "ACP human interaction action.", "enum": []string{"list", "respond"}}
		props["session_id"] = stringProp("Optional managed session filter for list.")
		props["interaction_id"] = stringProp("Pending interaction id for respond.")
		props["response"] = map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action":    map[string]any{"type": "string", "enum": []string{"cancel"}, "description": "Cancel the pending interaction."},
				"option_id": stringProp("Permission option id offered by the Adapter and allowed by local AgentDock policy."),
			},
			"additionalProperties": false,
			"description":          "Permission response. Provide option_id to choose an offered option, or action=cancel to cancel it.",
		}
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
		props["profile_id"] = stringProp("ACP profile id that handled the request.")
		props["action"] = stringProp("Completed AgentDock ACP session action.")
		props["protocol_version"] = intProp("Negotiated ACP protocol version for info.")
		props["auth_method_id"] = stringProp("Authentication method completed by info when auth_method_id was supplied.")
		props["authenticated"] = boolProp("Whether the requested Adapter authentication completed successfully.")
		props["agent"] = objectProp("Configured ACP agent identity.")
		props["capabilities"] = objectProp("Capabilities reported by the ACP agent during initialize.")
		props["auth_methods"] = arrayProp("Authentication methods advertised by the ACP agent during initialize.")
		props["context_policy"] = objectProp("Transcript ownership and restart policy.")
		props["event_policy"] = objectProp("Bounded incremental Run event delivery policy.")
		props["interaction_policy"] = objectProp("In-memory interaction bounds and local authorization policy.")
		props["steering_policy"] = objectProp("Capability-driven steering policy used internally by acp_prompt start.")
		props["session"] = objectProp("AgentDock managed ACP session record.")
		props["runtime_state"] = objectProp("Process-local projection of non-transcript ACP session/update state such as mode, config, commands, info, and usage.")
		props["remote_session"] = objectProp("Adapter-native session metadata.")
		props["sessions"] = arrayProp("Canonical session list combining managed AgentDock mappings and Adapter-native sessions. Each item declares source, managed, session_id, and remote_session_id as applicable.")
		props["count"] = intProp("Number of unique session rows returned by list.")
		props["managed_count"] = intProp("Number of AgentDock managed sessions considered by list.")
		props["remote_count"] = intProp("Number of native sessions returned on this page.")
		props["remote_available"] = boolProp("Whether the Adapter advertises standard session/list.")
		props["remote_error"] = objectProp("Capability error explaining why native listing is unavailable.")
		props["next_cursor"] = stringProp("Adapter-native pagination cursor for list.")
		props["attached"] = boolProp("Whether open created a new AgentDock mapping for a native session.")
		props["history_source"] = stringProp("History source. Adapter is the only transcript source.")
		props["history_events"] = arrayProp("Raw public session/update events replayed by standard session/load.")
		props["history_truncated"] = boolProp("Whether AgentDock bounded an oversized history replay response.")
		props["modes"] = objectProp("Session modes returned by the ACP agent when present.")
		props["config_options"] = arrayProp("Current session configuration options returned by the ACP agent when present.")
		props["changed"] = boolProp("Whether update changed at least one session setting.")
		props["deleted"] = boolProp("Whether the Adapter-native session was deleted.")
	case ToolPrompt:
		props["profile_id"] = stringProp("ACP profile id that handled the request.")
		props["action"] = stringProp("Completed ACP prompt action.")
		props["run_id"] = stringProp("AgentDock ACP prompt run id.")
		props["session_id"] = stringProp("AgentDock ACP session id.")
		props["status"] = stringProp("ACP prompt Run status.")
		props["disposition"] = stringProp("How start handled the input: started, steered, or restarted.")
		props["events"] = arrayProp("Ordered AgentDock Run events with monotonic seq values. Each event includes source=acp or source=agentdock.")
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
		props["cancel_requested"] = boolProp("Whether cancellation has been requested while the Run is still draining final ACP updates.")
	case ToolInteraction:
		props["profile_id"] = stringProp("ACP profile id that handled the request.")
		props["action"] = stringProp("Completed ACP interaction action.")
		props["interaction"] = objectProp("ACP human interaction state.")
		props["interactions"] = arrayProp("ACP human interactions. Permission is supported; elicitation is not advertised until its full lifecycle is implemented.")
		props["count"] = intProp("Returned ACP interaction count.")
		props["responded"] = boolProp("Whether the interaction response was accepted by AgentDock.")
	default:
		return nil, false
	}
	return toolcontract.OutputObject(props), true
}

package app

func acpOutputSchema(name string) map[string]any {
	props := map[string]any{}
	required := []string{}
	switch name {
	case "acp_session":
		props["action"] = schemaStringProperty("Completed ACP session action.")
		props["protocol_version"] = schemaIntegerProperty("Negotiated ACP protocol version for info.")
		props["auth_method_id"] = schemaStringProperty("Authentication method selected by authenticate.")
		props["authenticated"] = schemaBooleanProperty("Whether the advertised authentication method completed successfully.")
		props["agent"] = schemaOpenObjectProperty("Configured ACP agent identity.")
		props["capabilities"] = schemaOpenObjectProperty("Capabilities reported by the ACP agent during initialize.")
		props["auth_methods"] = schemaArrayObjectsProperty("Authentication methods advertised by the ACP agent during initialize.")
		props["context_policy"] = schemaOpenObjectProperty("Transcript ownership and restart policy used to avoid duplicated or replay-corrupted context.")
		props["event_policy"] = schemaOpenObjectProperty("Bounded incremental event delivery policy.")
		props["interaction_policy"] = schemaOpenObjectProperty("In-memory permission interaction bounds and local authorization policy.")
		props["steering_policy"] = schemaOpenObjectProperty("Native steering and observable host-owned fallback policy.")
		props["session"] = schemaOpenObjectProperty("Persistent AgentDock ACP session record.")
		props["sessions"] = schemaArrayObjectsProperty("Persistent ACP session records ordered by most recent update.")
		props["session_id"] = schemaStringProperty("AgentDock ACP session id.")
		props["modes"] = schemaOpenObjectProperty("Session modes returned by the ACP agent when present.")
		props["config_options"] = schemaArrayObjectsProperty("Current session configuration options returned by the ACP agent when present.")
		props["mode_id"] = schemaStringProperty("Session mode id applied by set_mode.")
		props["config_id"] = schemaStringProperty("Session configuration option id applied by set_config.")
		props["changed"] = schemaBooleanProperty("Whether a session mode or configuration option was changed.")
		props["count"] = schemaIntegerProperty("Returned ACP session count.")
		props["deleted"] = schemaBooleanProperty("Whether the persistent ACP session was deleted.")
	case "acp_prompt":
		props["action"] = schemaStringProperty("Completed ACP prompt action.")
		props["run_id"] = schemaStringProperty("AgentDock ACP prompt run id.")
		props["session_id"] = schemaStringProperty("AgentDock ACP session id.")
		props["status"] = schemaStringProperty("ACP prompt run status.")
		props["events"] = schemaArrayObjectsProperty("Ordered ACP session events with monotonic seq values.")
		props["next_seq"] = schemaIntegerProperty("Cursor to pass unchanged as after_seq on the next events call.")
		props["first_seq"] = schemaIntegerProperty("Oldest event sequence still retained in the bounded run event ring.")
		props["latest_seq"] = schemaIntegerProperty("Newest event sequence observed for the run when this page was read.")
		props["dropped_count"] = schemaIntegerProperty("Number of oldest events evicted from the bounded run event ring.")
		props["has_more"] = schemaBooleanProperty("Whether more retained events are immediately available after next_seq.")
		props["truncated"] = schemaBooleanProperty("Whether requested event history was older than the retained event ring.")
		props["started_at"] = schemaStringProperty("Prompt run start timestamp.")
		props["ended_at"] = schemaStringProperty("Prompt run end timestamp when settled.")
		props["stop_reason"] = schemaStringProperty("ACP stop reason when supplied by the agent.")
		props["error_code"] = schemaStringProperty("AgentDock ACP error code when the run failed.")
		props["message"] = schemaStringProperty("ACP run error message when present.")
		props["steering"] = schemaOpenObjectProperty("ACP steering outcome.")
		props["cancel_requested"] = schemaBooleanProperty("Whether cancellation was requested.")
	case "acp_interaction":
		props["action"] = schemaStringProperty("Completed ACP interaction action.")
		props["interaction"] = schemaOpenObjectProperty("ACP permission interaction state.")
		props["interactions"] = schemaArrayObjectsProperty("ACP permission interactions.")
		props["count"] = schemaIntegerProperty("Returned ACP interaction count.")
		props["responded"] = schemaBooleanProperty("Whether a permission option response was accepted.")
		props["cancelled"] = schemaBooleanProperty("Whether the interaction was cancelled.")
	}
	return finalizeOutputSchema(props, required)
}

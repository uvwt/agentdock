package app

func browserOutputSchema(name string) map[string]any {
	props := map[string]any{}
	required := []string{}
	switch name {
	case "browser_session":
		props["browser_ok"] = schemaBooleanProperty("Whether the native Go CDP browser operation succeeded.")
		props["error"] = schemaOpenObjectProperty("Structured browser error with code, message, phase, and optional details.")
		props["code"] = schemaStringProperty("One of the native browser error codes.")
		props["session_id"] = schemaStringProperty("Current-process browser session id.")
		props["page_id"] = schemaStringProperty("Active CDP target id.")
		props["pages"] = schemaArrayObjectsProperty("Current page targets.")
		props["url"] = schemaStringProperty("Active page URL after start.")
		props["title"] = schemaStringProperty("Active page title after start.")
		props["profile_id"] = schemaStringProperty("Normalized persistent profile id when configured.")
		props["connection_mode"] = schemaStringProperty("How action=start obtained the browser: owned, external_explicit, external_configured, or external_discovered.")
		props["closed"] = schemaBooleanProperty("Whether action=close closed the AgentDock browser session. External browsers remain running.")
		props["removed_count"] = schemaIntegerProperty("Number of current-process stale sessions terminated by cleanup_stale.")
		props["removed_sessions"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Session ids terminated by cleanup_stale."}
	case "browser_act", "browser_snapshot":
		props["browser_ok"] = schemaBooleanProperty("Whether the native Go CDP browser operation succeeded.")
		props["error"] = schemaOpenObjectProperty("Structured browser error with code, message, phase, and optional details.")
		props["code"] = schemaStringProperty("One of the native browser error codes.")
		props["session_id"] = schemaStringProperty("Current-process browser session id.")
		props["page_id"] = schemaStringProperty("Active CDP target id.")
		props["pages"] = schemaArrayObjectsProperty("Current page targets with active state.")
		props["url"] = schemaStringProperty("Current page URL.")
		props["title"] = schemaStringProperty("Current page title.")
		props["text"] = schemaStringProperty("Normalized current page body text excerpt.")
		props["viewport"] = schemaOpenObjectProperty("Viewport dimensions in CSS pixels.")
		props["page_size"] = schemaOpenObjectProperty("Scrollable page dimensions in CSS pixels.")
		props["focused_element"] = schemaOpenObjectProperty("Focused DOM element summary when available.")
		props["interactive_elements"] = schemaArrayObjectsProperty("Visible interactive DOM element summaries.")
		props["screenshot"] = schemaOpenObjectProperty("Published PNG Artifact reference.")
		props["console_errors"] = schemaArrayObjectsProperty("Console errors captured for this operation.")
		props["network_errors"] = schemaArrayObjectsProperty("Network loading failures captured for this operation.")
		props["page_errors"] = schemaArrayObjectsProperty("Unhandled page exceptions captured for this operation.")
		props["closed"] = schemaBooleanProperty("Whether close_after terminated the session after successful Artifact publication.")
	}
	return finalizeOutputSchema(props, required)
}

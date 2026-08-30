package app

func browserInputSchema(name string) map[string]any {
	props := map[string]any{}
	required := []string{}
	switch name {
	case "browser_session":
		props["action"] = map[string]any{"type": "string", "description": "Browser session action.", "enum": []string{"start", "close", "cleanup_stale"}}
		props["url"] = schemaStringProperty("Initial URL for action=start. Defaults to about:blank in the AgentDock-managed target.")
		props["browser"] = map[string]any{"type": "string", "description": "Chromium-family browser to launch. Defaults to auto.", "enum": []string{"auto", "chrome", "chromium", "edge"}}
		props["headless"] = schemaBooleanProperty("Run the AgentDock-owned browser headless. Defaults to true.")
		props["viewport"] = map[string]any{
			"type": "object", "description": "Viewport for action=start.", "additionalProperties": false,
			"properties": map[string]any{
				"width":  schemaBoundedIntegerProperty("Viewport width in CSS pixels.", 320, 7680),
				"height": schemaBoundedIntegerProperty("Viewport height in CSS pixels.", 200, 4320),
			},
		}
		props["session_id"] = schemaStringProperty("In-memory browser session id for action=close.")
		props["profile_id"] = schemaStringProperty("Optional persistent profile id stored under ~/.agentdock/browser/profiles/<id>; not valid with an external CDP browser.")
		props["cdp_url"] = schemaStringProperty("Optional loopback Chromium CDP endpoint (http(s) root or ws(s) browser websocket). Remote CDP endpoints must be configured by the user through AgentDock settings. When set, AgentDock attaches and manages only its own dedicated target instead of launching a browser.")
		props["cookies"] = map[string]any{
			"type": "array", "description": "Cookies injected when action=start.",
			"items": map[string]any{
				"type": "object", "additionalProperties": false, "required": []string{"name", "value"},
				"properties": map[string]any{
					"name": schemaStringProperty("Cookie name."), "value": schemaStringProperty("Cookie value."),
					"url": schemaStringProperty("URL used to scope the cookie."), "domain": schemaStringProperty("Cookie domain; provide url or domain."),
					"path": schemaStringProperty("Cookie path."), "expires": map[string]any{"type": "number", "minimum": 0},
					"http_only": schemaBooleanProperty("Set HttpOnly."), "secure": schemaBooleanProperty("Set Secure."),
					"same_site": map[string]any{"type": "string", "enum": []string{"strict", "lax", "none"}},
				},
			},
		}
		props["local_storage"] = map[string]any{
			"type": "object", "description": "Origin to string key/value localStorage map.",
			"additionalProperties": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
		}
		props["reload_after_local_storage"] = schemaBooleanProperty("Reload the final URL after localStorage injection. Defaults to true.")
		props["max_age_ms"] = map[string]any{"type": "integer", "description": "For cleanup_stale, remove current-process sessions inactive for this age. Defaults to 6 hours.", "minimum": 1, "maximum": 31536000000}
		props["timeout_ms"] = schemaBoundedIntegerProperty("Operation timeout in milliseconds. Defaults to 30000 and is capped at 300000.", 1, 300000)
		required = []string{"action"}
	case "browser_act":
		props["session_id"] = schemaStringProperty("In-memory browser session id.")
		props["page_id"] = schemaStringProperty("Optional CDP target id. Omit to use the active page.")
		props["actions"] = browserActionsProp()
		props["full_page"] = schemaBooleanProperty("Capture the full page in the final PNG screenshot.")
		props["max_text_chars"] = schemaBoundedIntegerProperty("Maximum normalized body text characters. Defaults to 8000.", 1, 50000)
		props["max_interactive_elements"] = schemaBoundedIntegerProperty("Maximum visible interactive elements. Defaults to 40.", 1, 200)
		props["retention_seconds"] = schemaBoundedIntegerProperty("Screenshot Artifact retention seconds. Zero uses the Artifact default; capped at 604800.", 0, 604800)
		props["close_after"] = schemaBooleanProperty("Close the session only after all actions, final snapshot, and screenshot Artifact publication succeed.")
		props["timeout_ms"] = schemaBoundedIntegerProperty("Overall operation timeout in milliseconds. Defaults to 30000 and is capped at 300000.", 1, 300000)
		required = []string{"session_id", "actions"}
	case "browser_snapshot":
		props["session_id"] = schemaStringProperty("In-memory browser session id.")
		props["page_id"] = schemaStringProperty("Optional CDP target id. Omit to use the active page.")
		props["full_page"] = schemaBooleanProperty("Capture the full page directly through CDP as PNG.")
		props["max_text_chars"] = schemaBoundedIntegerProperty("Maximum normalized body text characters. Defaults to 8000.", 1, 50000)
		props["max_interactive_elements"] = schemaBoundedIntegerProperty("Maximum visible interactive elements. Defaults to 40.", 1, 200)
		props["retention_seconds"] = schemaBoundedIntegerProperty("Screenshot Artifact retention seconds. Zero uses the Artifact default; capped at 604800.", 0, 604800)
		props["close_after"] = schemaBooleanProperty("Close the session only after snapshot and screenshot Artifact publication succeed.")
		props["timeout_ms"] = schemaBoundedIntegerProperty("Operation timeout in milliseconds. Defaults to 30000 and is capped at 300000.", 1, 300000)
		required = []string{"session_id"}
	}
	return finalizeInputSchema(name, props, required)
}

func browserActionsProp() map[string]any {
	intProp := func(desc string, minimum, maximum int) map[string]any {
		return map[string]any{"type": "integer", "description": desc, "minimum": minimum, "maximum": maximum}
	}
	actionObject := func(name string, required []string, properties map[string]any) map[string]any {
		properties["action"] = map[string]any{"type": "string", "const": name}
		return map[string]any{"type": "object", "additionalProperties": false, "required": append([]string{"action"}, required...), "properties": properties}
	}
	waitUntil := map[string]any{"type": "string", "enum": []string{"domcontentloaded", "load"}, "description": "Real CDP navigation lifecycle event to await."}
	state := map[string]any{"type": "string", "enum": []string{"visible", "hidden", "attached", "detached"}}
	timeout := intProp("Per-action timeout in milliseconds.", 1, 300000)
	selector := schemaStringProperty("CSS selector. Playwright locators and XPath are not accepted.")

	return map[string]any{
		"type": "array", "minItems": 1, "maxItems": 100,
		"description": "Strict browser actions executed through native Go CDP.",
		"items": map[string]any{"oneOf": []map[string]any{
			actionObject("goto", []string{"url"}, map[string]any{"url": schemaStringProperty("Destination URL."), "wait_until": waitUntil, "timeout_ms": timeout}),
			actionObject("click", []string{"selector"}, map[string]any{"selector": selector}),
			actionObject("fill", []string{"selector", "value"}, map[string]any{"selector": selector, "value": schemaStringProperty("Replacement input value.")}),
			actionObject("press", []string{"key"}, map[string]any{"selector": selector, "key": schemaStringProperty("Key name or text to send.")}),
			actionObject("wait", []string{"value"}, map[string]any{"value": intProp("Duration in milliseconds.", 0, 300000)}),
			actionObject("wait_for_selector", []string{"selector"}, map[string]any{"selector": selector, "state": state, "timeout_ms": timeout}),
			actionObject("wait_for_url", []string{"url"}, map[string]any{"url": schemaStringProperty("URL substring or * / ? wildcard pattern."), "timeout_ms": timeout}),
			actionObject("wait_for_text", []string{"text"}, map[string]any{"text": schemaStringProperty("Text matched against each individual DOM element after whitespace normalization."), "exact": schemaBooleanProperty("Require one element's normalized text to equal text."), "state": state, "timeout_ms": timeout}),
			actionObject("wait_for_response", nil, map[string]any{"url": schemaStringProperty("Response URL substring."), "url_pattern": schemaStringProperty("Response URL regular expression."), "method": schemaStringProperty("Optional HTTP method."), "status": intProp("Optional HTTP status.", 100, 599), "timeout_ms": timeout}),
			actionObject("select", []string{"selector", "value"}, map[string]any{"selector": selector, "value": schemaStringProperty("Select element value.")}),
			actionObject("scroll", nil, map[string]any{"delta_x": intProp("Horizontal scroll delta.", -100000, 100000), "delta_y": intProp("Vertical scroll delta.", -100000, 100000)}),
			actionObject("reload", nil, map[string]any{"wait_until": waitUntil, "timeout_ms": timeout}),
			actionObject("back", nil, map[string]any{"wait_until": waitUntil, "timeout_ms": timeout}),
			actionObject("forward", nil, map[string]any{"wait_until": waitUntil, "timeout_ms": timeout}),
		}},
	}
}

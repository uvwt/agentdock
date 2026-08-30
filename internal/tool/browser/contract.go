package browser

import toolcontract "github.com/uvwt/agentdock/internal/tool/contract"

const (
	ToolSession  = "browser_session"
	ToolAct      = "browser_act"
	ToolSnapshot = "browser_snapshot"
)

func InputSchema(name string) (map[string]any, bool) {
	stringProp := toolcontract.String
	boolProp := toolcontract.Boolean
	boundedIntProp := toolcontract.BoundedInteger
	props := map[string]any{}
	var required []string

	switch name {
	case ToolSession:
		props["action"] = map[string]any{"type": "string", "description": "Browser session action.", "enum": []string{"start", "close", "cleanup_stale"}}
		props["url"] = stringProp("Initial URL for action=start. Defaults to about:blank in the AgentDock-managed target.")
		props["browser"] = map[string]any{"type": "string", "description": "Chromium-family browser to launch. Defaults to auto.", "enum": []string{"auto", "chrome", "chromium", "edge"}}
		props["headless"] = boolProp("Run the AgentDock-owned browser headless. Defaults to true.")
		props["viewport"] = map[string]any{
			"type": "object", "description": "Viewport for action=start.", "additionalProperties": false,
			"properties": map[string]any{
				"width":  boundedIntProp("Viewport width in CSS pixels.", 320, 7680),
				"height": boundedIntProp("Viewport height in CSS pixels.", 200, 4320),
			},
		}
		props["session_id"] = stringProp("In-memory browser session id for action=close.")
		props["profile_id"] = stringProp("Optional persistent profile id stored under ~/.agentdock/browser/profiles/<id>; not valid with an external CDP browser.")
		props["cdp_url"] = stringProp("Optional loopback Chromium CDP endpoint (http(s) root or ws(s) browser websocket). Remote CDP endpoints must be configured by the user through AgentDock settings. When set, AgentDock attaches and manages only its own dedicated target instead of launching a browser.")
		props["cookies"] = map[string]any{
			"type": "array", "description": "Cookies injected when action=start.",
			"items": map[string]any{
				"type": "object", "additionalProperties": false, "required": []string{"name", "value"},
				"properties": map[string]any{
					"name": stringProp("Cookie name."), "value": stringProp("Cookie value."),
					"url": stringProp("URL used to scope the cookie."), "domain": stringProp("Cookie domain; provide url or domain."),
					"path": stringProp("Cookie path."), "expires": map[string]any{"type": "number", "minimum": 0},
					"http_only": boolProp("Set HttpOnly."), "secure": boolProp("Set Secure."),
					"same_site": map[string]any{"type": "string", "enum": []string{"strict", "lax", "none"}},
				},
			},
		}
		props["local_storage"] = map[string]any{
			"type": "object", "description": "Origin to string key/value localStorage map.",
			"additionalProperties": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
		}
		props["reload_after_local_storage"] = boolProp("Reload the final URL after localStorage injection. Defaults to true.")
		props["max_age_ms"] = map[string]any{"type": "integer", "description": "For cleanup_stale, remove current-process sessions inactive for this age. Defaults to 6 hours.", "minimum": 1, "maximum": 31536000000}
		props["timeout_ms"] = boundedIntProp("Operation timeout in milliseconds. Defaults to 30000 and is capped at 300000.", 1, 300000)
		required = []string{"action"}
	case ToolAct:
		props["session_id"] = stringProp("In-memory browser session id.")
		props["page_id"] = stringProp("Optional CDP target id. Omit to use the active page.")
		props["actions"] = browserActionsSchema()
		props["full_page"] = boolProp("Capture the full page in the final PNG screenshot.")
		props["max_text_chars"] = boundedIntProp("Maximum normalized body text characters. Defaults to 8000.", 1, 50000)
		props["max_interactive_elements"] = boundedIntProp("Maximum visible interactive elements. Defaults to 40.", 1, 200)
		props["retention_seconds"] = boundedIntProp("Screenshot Artifact retention seconds. Zero uses the Artifact default; capped at 604800.", 0, 604800)
		props["close_after"] = boolProp("Close the session only after all actions, final snapshot, and screenshot Artifact publication succeed.")
		props["timeout_ms"] = boundedIntProp("Overall operation timeout in milliseconds. Defaults to 30000 and is capped at 300000.", 1, 300000)
		required = []string{"session_id", "actions"}
	case ToolSnapshot:
		props["session_id"] = stringProp("In-memory browser session id.")
		props["page_id"] = stringProp("Optional CDP target id. Omit to use the active page.")
		props["full_page"] = boolProp("Capture the full page directly through CDP as PNG.")
		props["max_text_chars"] = boundedIntProp("Maximum normalized body text characters. Defaults to 8000.", 1, 50000)
		props["max_interactive_elements"] = boundedIntProp("Maximum visible interactive elements. Defaults to 40.", 1, 200)
		props["retention_seconds"] = boundedIntProp("Screenshot Artifact retention seconds. Zero uses the Artifact default; capped at 604800.", 0, 604800)
		props["close_after"] = boolProp("Close the session only after snapshot and screenshot Artifact publication succeed.")
		props["timeout_ms"] = boundedIntProp("Operation timeout in milliseconds. Defaults to 30000 and is capped at 300000.", 1, 300000)
		required = []string{"session_id"}
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
	props := map[string]any{
		"browser_ok": boolProp("Whether the native Go CDP browser operation succeeded."),
		"error":      objectProp("Structured browser error with code, message, phase, and optional details."),
		"code":       stringProp("One of the native browser error codes."),
		"session_id": stringProp("Current-process browser session id."),
		"page_id":    stringProp("Active CDP target id."),
		"pages":      arrayProp("Current page targets with active state."),
	}

	switch name {
	case ToolSession:
		props["pages"] = arrayProp("Current page targets.")
		props["url"] = stringProp("Active page URL after start.")
		props["title"] = stringProp("Active page title after start.")
		props["profile_id"] = stringProp("Normalized persistent profile id when configured.")
		props["connection_mode"] = stringProp("How action=start obtained the browser: owned, external_explicit, external_configured, or external_discovered.")
		props["closed"] = boolProp("Whether action=close closed the AgentDock browser session. External browsers remain running.")
		props["removed_count"] = intProp("Number of current-process stale sessions terminated by cleanup_stale.")
		props["removed_sessions"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Session ids terminated by cleanup_stale."}
	case ToolAct, ToolSnapshot:
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
	default:
		return nil, false
	}
	return toolcontract.OutputObject(props), true
}

func browserActionsSchema() map[string]any {
	stringProp := toolcontract.String
	intProp := func(desc string, minimum, maximum int) map[string]any {
		return toolcontract.BoundedInteger(desc, minimum, maximum)
	}
	boolProp := toolcontract.Boolean
	actionObject := func(name string, required []string, properties map[string]any) map[string]any {
		properties["action"] = map[string]any{"type": "string", "const": name}
		return map[string]any{"type": "object", "additionalProperties": false, "required": append([]string{"action"}, required...), "properties": properties}
	}
	waitUntil := map[string]any{"type": "string", "enum": []string{"domcontentloaded", "load"}, "description": "Real CDP navigation lifecycle event to await."}
	state := map[string]any{"type": "string", "enum": []string{"visible", "hidden", "attached", "detached"}}
	timeout := intProp("Per-action timeout in milliseconds.", 1, 300000)
	selector := stringProp("CSS selector. Playwright locators and XPath are not accepted.")

	return map[string]any{
		"type": "array", "minItems": 1, "maxItems": 100,
		"description": "Strict browser actions executed through native Go CDP.",
		"items": map[string]any{"oneOf": []map[string]any{
			actionObject("goto", []string{"url"}, map[string]any{"url": stringProp("Destination URL."), "wait_until": waitUntil, "timeout_ms": timeout}),
			actionObject("click", []string{"selector"}, map[string]any{"selector": selector}),
			actionObject("fill", []string{"selector", "value"}, map[string]any{"selector": selector, "value": stringProp("Replacement input value.")}),
			actionObject("press", []string{"key"}, map[string]any{"selector": selector, "key": stringProp("Key name or text to send.")}),
			actionObject("wait", []string{"value"}, map[string]any{"value": intProp("Duration in milliseconds.", 0, 300000)}),
			actionObject("wait_for_selector", []string{"selector"}, map[string]any{"selector": selector, "state": state, "timeout_ms": timeout}),
			actionObject("wait_for_url", []string{"url"}, map[string]any{"url": stringProp("URL substring or * / ? wildcard pattern."), "timeout_ms": timeout}),
			actionObject("wait_for_text", []string{"text"}, map[string]any{"text": stringProp("Text matched against each individual DOM element after whitespace normalization."), "exact": boolProp("Require one element's normalized text to equal text."), "state": state, "timeout_ms": timeout}),
			actionObject("wait_for_response", nil, map[string]any{"url": stringProp("Response URL substring."), "url_pattern": stringProp("Response URL regular expression."), "method": stringProp("Optional HTTP method."), "status": intProp("Optional HTTP status.", 100, 599), "timeout_ms": timeout}),
			actionObject("select", []string{"selector", "value"}, map[string]any{"selector": selector, "value": stringProp("Select element value.")}),
			actionObject("scroll", nil, map[string]any{"delta_x": intProp("Horizontal scroll delta.", -100000, 100000), "delta_y": intProp("Vertical scroll delta.", -100000, 100000)}),
			actionObject("reload", nil, map[string]any{"wait_until": waitUntil, "timeout_ms": timeout}),
			actionObject("back", nil, map[string]any{"wait_until": waitUntil, "timeout_ms": timeout}),
			actionObject("forward", nil, map[string]any{"wait_until": waitUntil, "timeout_ms": timeout}),
		}},
	}
}

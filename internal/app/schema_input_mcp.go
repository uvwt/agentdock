package app

func mcpInputSchema(name string) map[string]any {
	props := map[string]any{}
	required := []string{}
	switch name {
	case "mcp_manage":
		props["action"] = map[string]any{"type": "string", "description": "Dynamic MCP server or isolated environment action.", "enum": []string{"list", "inspect", "add", "remove", "enable", "disable", "env_set", "env_unset", "env_list", "refresh"}}
		props["name"] = schemaStringProperty("Dynamic MCP server name. Use a stable short identifier such as figma or github.")
		props["description"] = schemaStringProperty("Short capability description shown in agentdock_context.")
		props["transport"] = map[string]any{"type": "string", "description": "MCP transport for action=add.", "enum": []string{"streamable_http", "stdio"}}
		props["url"] = schemaStringProperty("Absolute MCP endpoint URL for transport=streamable_http.")
		props["command"] = schemaStringProperty("Executable name or path for transport=stdio.")
		props["args"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Command arguments for transport=stdio."}
		props["cwd"] = schemaStringProperty("Optional absolute working directory for transport=stdio.")
		props["header_env"] = map[string]any{"type": "object", "description": "HTTP header name to host environment variable name. Secret values are never stored in the MCP registry.", "additionalProperties": map[string]any{"type": "string"}}
		props["env_from_env"] = map[string]any{"type": "object", "description": "Child process environment variable name to host environment variable name for stdio. Secret values are never stored in the MCP registry.", "additionalProperties": map[string]any{"type": "string"}}
		props["key"] = schemaStringProperty("Environment variable name for env_set/env_unset.")
		props["value"] = schemaStringProperty("Environment variable value for env_set. Secret values are never returned.")
		props["enabled"] = schemaBooleanProperty("Enable the server after registration. Defaults to true.")
		props["timeout_ms"] = schemaBoundedIntegerProperty("Per-request timeout. Defaults to 30000 and is capped at 300000.", 1, 300000)
		required = []string{"action"}
	case "mcp_tool_search":
		props["query"] = schemaStringProperty("Capability or tool query.")
		props["server"] = schemaStringProperty("Optional dynamic MCP server name from agentdock_context.")
		props["limit"] = schemaBoundedIntegerProperty("Maximum matching tools. Defaults to 10 and is capped at 100.", 1, 100)
		required = []string{"query"}
	case "mcp_tool_inspect":
		props["name"] = schemaStringProperty("Qualified dynamic MCP tool name in <server>:<tool> form.")
		required = []string{"name"}
	case "mcp_tool_call":
		props["name"] = schemaStringProperty("Qualified dynamic MCP tool name in <server>:<tool> form.")
		props["arguments"] = map[string]any{"type": "object", "description": "Arguments matching the schema returned by mcp_tool_inspect.", "additionalProperties": true}
		required = []string{"name", "arguments"}
	}
	return finalizeInputSchema(name, props, required)
}

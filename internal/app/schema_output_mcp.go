package app

func mcpOutputSchema(name string) map[string]any {
	props := map[string]any{}
	required := []string{}
	switch name {
	case "mcp_manage":
		props["action"] = schemaStringProperty("Completed dynamic MCP management action.")
		props["servers"] = schemaArrayObjectsProperty("Registered dynamic MCP server summaries.")
		props["server"] = schemaOpenObjectProperty("Dynamic MCP server summary.")
		props["config"] = schemaOpenObjectProperty("Dynamic MCP server configuration containing only non-secret values and environment variable names.")
		props["tools"] = schemaArrayObjectsProperty("Discovered lightweight MCP tool summaries.")
		props["tool_count"] = schemaIntegerProperty("Discovered tool count.")
		props["count"] = schemaIntegerProperty("Registered server count.")
		props["name"] = schemaStringProperty("Dynamic MCP server name.")
		props["removed"] = schemaBooleanProperty("Whether the server or environment variable was removed.")
		props["key"] = schemaStringProperty("Environment variable name. Secret values are never returned.")
		props["configured"] = schemaBooleanProperty("Whether the environment variable has a non-empty configured value.")
		props["items"] = schemaArrayObjectsProperty("Environment variable names and configured status without values.")
	case "mcp_tool_search":
		props["query"] = schemaStringProperty("Capability query used.")
		props["server"] = schemaStringProperty("Optional server filter used.")
		props["tools"] = schemaArrayObjectsProperty("Matching lightweight MCP tool summaries.")
		props["count"] = schemaIntegerProperty("Matching tool count.")
	case "mcp_tool_inspect":
		props["name"] = schemaStringProperty("Qualified MCP tool name.")
		props["server"] = schemaStringProperty("Dynamic MCP server name.")
		props["tool_name"] = schemaStringProperty("Upstream MCP tool name.")
		props["title"] = schemaStringProperty("Tool title.")
		props["description"] = schemaStringProperty("Tool description.")
		props["input_schema"] = schemaOpenObjectProperty("Complete upstream MCP tool input schema.")
		props["output_schema"] = schemaOpenObjectProperty("Optional upstream MCP tool output schema.")
		props["annotations"] = schemaOpenObjectProperty("Optional upstream MCP tool annotations.")
	case "mcp_tool_call":
		props["name"] = schemaStringProperty("Qualified MCP tool name.")
		props["result"] = schemaOpenObjectProperty("Raw upstream MCP tools/call result, including content and structuredContent when supplied.")
	}
	return finalizeOutputSchema(props, required)
}

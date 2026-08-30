package app

import mcpcontract "github.com/uvwt/agentdock-protocol/mcpcontract"

func OutputSchema(name string) map[string]any {
	if name == mcpcontract.ToolAgentDockContext {
		return mcpcontract.LocalAgentDockContextOutputSchema()
	}
	if schema, ok := mcpcontract.OutputSchema(name); ok {
		return schema
	}
	switch name {
	case "read_file", "list_dir", "search_text", "file_edit":
		return fileOutputSchema(name)
	case "exec_command", "session_observe", "session_act":
		return commandOutputSchema(name)
	case "task_manage":
		return taskOutputSchema(name)
	case "evolve":
		return evolutionOutputSchema(name)
	case "acp_session", "acp_prompt", "acp_interaction":
		return acpOutputSchema(name)
	case "skill_package":
		return skillOutputSchema(name)
	case "mcp_manage", "mcp_tool_search", "mcp_tool_inspect", "mcp_tool_call":
		return mcpOutputSchema(name)
	case "file_publish", "view_image":
		return mediaOutputSchema(name)
	case "browser_session", "browser_act", "browser_snapshot":
		return browserOutputSchema(name)
	default:
		return finalizeOutputSchema(map[string]any{}, nil)
	}
}

func finalizeOutputSchema(props map[string]any, required []string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": true}
}

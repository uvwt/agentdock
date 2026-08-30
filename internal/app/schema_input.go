package app

import mcpcontract "github.com/uvwt/agentdock-protocol/mcpcontract"

func InputSchema(name string) map[string]any {
	if schema, ok := mcpcontract.InputSchema(name); ok {
		return schema
	}
	switch name {
	case "read_file", "list_dir", "search_text", "file_edit":
		return fileInputSchema(name)
	case "exec_command", "session_observe", "session_act":
		return commandInputSchema(name)
	case "task_manage":
		return taskInputSchema(name)
	case "evolve":
		return evolutionInputSchema(name)
	case "acp_session", "acp_prompt", "acp_interaction":
		return acpInputSchema(name)
	case "skill_package":
		return skillInputSchema(name)
	case "mcp_manage", "mcp_tool_search", "mcp_tool_inspect", "mcp_tool_call":
		return mcpInputSchema(name)
	case "file_publish", "view_image":
		return mediaInputSchema(name)
	case "browser_session", "browser_act", "browser_snapshot":
		return browserInputSchema(name)
	default:
		return finalizeInputSchema(name, map[string]any{}, nil)
	}
}

func finalizeInputSchema(name string, props map[string]any, required []string) map[string]any {
	schema := map[string]any{"type": "object", "properties": props, "additionalProperties": true}
	switch name {
	case "list_dir", "exec_command", "acp_session", "acp_prompt", "acp_interaction", "mcp_manage", "mcp_tool_search", "mcp_tool_inspect", "mcp_tool_call", "browser_session", "browser_act", "browser_snapshot":
		// 这些工具的参数契约需要严格收敛，避免删除或拼错的字段被静默忽略。
		schema["additionalProperties"] = false
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

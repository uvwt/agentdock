package app

import (
	"context"

	toolmcp "github.com/uvwt/agentdock/internal/tool/mcp"
)

func dynamicMCPToolSpecs() []ToolSpec {
	return []ToolSpec{
		{Name: "mcp_manage", Title: "Manage dynamic MCP servers", Description: "Register, inspect, enable, disable, refresh, remove, or manage the isolated environment of dynamic MCP servers. Dynamic MCP tools remain separate from AgentDock built-in tools.", Annotations: mutatingToolAnnotations(true, true), Handler: typedToolHandler("mcp_manage", func(ctx context.Context, r *Runtime, request toolmcp.ManageRequest) (Result, error) {
			return r.dynamicMCP.Manage(ctx, request)
		})},
		{Name: "mcp_tool_search", Title: "Search dynamic MCP tools", Description: "Search lightweight tool summaries from enabled dynamic MCP servers. Use a server name from agentdock_context when possible.", Annotations: readOnlyToolAnnotations(true), Handler: typedToolHandler("mcp_tool_search", func(ctx context.Context, r *Runtime, request toolmcp.SearchRequest) (Result, error) {
			return r.dynamicMCP.Search(ctx, request)
		})},
		{Name: "mcp_tool_inspect", Title: "Inspect a dynamic MCP tool", Description: "Read the complete schema for one dynamic MCP tool identified as <server>:<tool>.", Annotations: readOnlyToolAnnotations(true), Handler: typedToolHandler("mcp_tool_inspect", func(ctx context.Context, r *Runtime, request toolmcp.InspectRequest) (Result, error) {
			return r.dynamicMCP.Inspect(ctx, request)
		})},
		{Name: "mcp_tool_call", Title: "Call a dynamic MCP tool", Description: "Call one previously discovered dynamic MCP tool identified as <server>:<tool>. Arguments are validated against the discovered tool schema before forwarding.", Annotations: mutatingToolAnnotations(true, true), Handler: typedToolHandler("mcp_tool_call", func(ctx context.Context, r *Runtime, request toolmcp.CallRequest) (Result, error) {
			return r.dynamicMCP.Call(ctx, request)
		})},
	}
}

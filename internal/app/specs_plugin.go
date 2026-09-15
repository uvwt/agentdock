package app

import (
	"context"

	toolplugin "github.com/uvwt/agentdock/internal/tool/plugin"
)

func pluginToolSpecs() []ToolSpec {
	return []ToolSpec{
		{
			Name: "plugin_manage", Contract: pluginToolContract,
			Title:       "Manage heavy plugins",
			Description: "Create, inspect, enable, disable, or remove heavy plugins that group related document Skills and dynamic MCP servers behind one domain capability.",
			Annotations: mutatingToolAnnotations(true, false),
			Handler: typedToolHandler("plugin_manage", func(ctx context.Context, r *Runtime, request toolplugin.ManageRequest) (Result, error) {
				return r.plugins.Manage(ctx, request)
			}),
		},
		{
			Name: "plugin_load", Contract: pluginToolContract,
			Title:       "Load a heavy plugin",
			Description: "Expand one enabled plugin from agentdock_context and reveal its contained Skill descriptions and dynamic MCP server descriptions. Load the plugin before using a plugin-owned member.",
			Annotations: readOnlyToolAnnotations(false),
			Handler: typedToolHandler("plugin_load", func(ctx context.Context, r *Runtime, request toolplugin.LoadRequest) (Result, error) {
				return r.plugins.Load(ctx, request)
			}),
		},
	}
}

package app

import (
	"context"

	toolplugin "github.com/uvwt/agentdock/internal/tool/plugin"
)

func pluginToolSpecs() []ToolSpec {
	return []ToolSpec{{
		Name: "plugin_manage", Contract: pluginToolContract, Title: "Manage Plugins",
		Description: "Validate local Plugin directories or ZIP archives, inspect installed Plugins, and install, update, enable, disable, or remove portable/OpenAI/Claude Plugins. Remote sources must be fetched locally before validation; Plugin Skills and MCP servers enter the existing runtimes directly.",
		Annotations: mutatingToolAnnotations(true, true),
		Handler: typedToolHandler("plugin_manage", func(ctx context.Context, r *Runtime, request toolplugin.ManageRequest) (Result, error) {
			return r.plugins.Manage(ctx, request)
		}),
	}}
}

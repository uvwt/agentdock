package app

import (
	"context"

	toolplugin "github.com/uvwt/agentdock/internal/tool/plugin"
)

func pluginToolSpecs() []ToolSpec {
	return []ToolSpec{{
		Name: "plugin_manage", Contract: pluginToolContract, Title: "Manage Plugins",
		Description: "Validate, inspect, install, update, enable, disable, remove, or browse read-only catalogs for portable/OpenAI/Claude Plugins from local, Git, or ZIP sources. Plugin Skills and MCP servers enter the existing runtimes directly.",
		Annotations: mutatingToolAnnotations(true, true),
		Handler: typedToolHandler("plugin_manage", func(ctx context.Context, r *Runtime, request toolplugin.ManageRequest) (Result, error) {
			return r.plugins.Manage(ctx, request)
		}),
	}}
}

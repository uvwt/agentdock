package app

import (
	"context"

	toolplugin "github.com/uvwt/agentdock/internal/tool/plugin"
)

func pluginToolSpecs() []ToolSpec {
	return []ToolSpec{{
		Name: "plugin_manage", Contract: pluginToolContract, Title: "Manage Plugins",
		Description: "Validate, install, inspect, update, enable, disable, or remove versioned Plugins that own bundled Skills and MCP servers. Plugin components enter the normal Skill/MCP runtimes directly.",
		Annotations: mutatingToolAnnotations(true, true),
		Handler: typedToolHandler("plugin_manage", func(ctx context.Context, r *Runtime, request toolplugin.ManageRequest) (Result, error) {
			return r.plugins.Manage(ctx, request)
		}),
	}}
}

package plugin

import toolcontract "github.com/uvwt/agentdock/internal/tool/contract"

const ToolManage = "plugin_manage"

func InputSchema(name string) (map[string]any, bool) {
	if name != ToolManage {
		return nil, false
	}
	return toolcontract.InputObject(map[string]any{
		"action": map[string]any{
			"type":        "string",
			"description": "Plugin lifecycle action.",
			"enum":        []string{"list", "inspect", "validate", "install", "update", "enable", "disable", "remove"},
		},
		"name":         toolcontract.String("Installed Plugin name for inspect/enable/disable/remove."),
		"source":       toolcontract.String("Local Plugin directory or ZIP archive. AgentDock auto-detects Portable, OpenAI and Claude Plugin formats and converts supported external formats to a canonical Portable package before review. Relative paths are resolved against the workspace."),
		"enabled":      toolcontract.Boolean("Initial enabled state for install. Defaults to true."),
		"review_token": toolcontract.String("Required for install/update. Copy the exact review_token returned by plugin_manage validate; it binds confirmation to the reviewed package content."),
		"data_policy": map[string]any{
			"type":        "string",
			"description": "Required for remove. keep preserves Plugin data and Plugin-owned MCP environment; purge deletes both.",
			"enum":        []string{"keep", "purge"},
		},
	}, "action"), true
}

func OutputSchema(name string) (map[string]any, bool) {
	if name != ToolManage {
		return nil, false
	}
	return toolcontract.OutputObject(map[string]any{
		"action":         toolcontract.String("Completed Plugin action."),
		"name":           toolcontract.String("Plugin name."),
		"version":        toolcontract.String("Current Plugin version."),
		"enabled":        toolcontract.Boolean("Whether the Plugin is enabled."),
		"changed":        toolcontract.Boolean("Whether the operation changed installed state."),
		"package_digest": toolcontract.String("Plugin package content digest; validate returns the digest bound by review_token."),
		"review_token":   toolcontract.String("Exact security-review token returned by validate and required unchanged for install/update."),
		"data_policy":    toolcontract.String("Applied removal data policy."),
		"plugin":         toolcontract.OpenObject("Installed Plugin details and component provenance."),
		"plugins":        toolcontract.ObjectArray("Installed Plugin lightweight states."),
		"review":         toolcontract.OpenObject("Canonical Plugin validation, detected-format compatibility and security review."),
		"count":          toolcontract.Integer("Installed Plugin count."),
	}), true
}

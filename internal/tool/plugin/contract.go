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
		"name":                    toolcontract.String("Installed Plugin name for inspect/enable/disable/remove."),
		"source":                  toolcontract.String("Plugin source. P2 accepts a local portable Plugin directory; P3 adds external adapters and Git/archive sources."),
		"enabled":                 toolcontract.Boolean("Initial enabled state for install. Defaults to true."),
		"confirmed":               toolcontract.Boolean("Required for install/update after reviewing plugin_manage validate output."),
		"confirmed_source_change": toolcontract.Boolean("For update, explicitly confirm rebinding an installed Plugin to a different source."),
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
		"package_digest": toolcontract.String("Plugin package content digest."),
		"data_policy":    toolcontract.String("Applied removal data policy."),
		"plugin":         toolcontract.OpenObject("Installed Plugin details and component provenance."),
		"plugins":        toolcontract.ObjectArray("Installed Plugin lightweight states."),
		"review":         toolcontract.OpenObject("Static Plugin validation and security review."),
		"count":          toolcontract.Integer("Installed Plugin count."),
	}), true
}

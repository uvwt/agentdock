package plugin

import toolcontract "github.com/uvwt/agentdock/internal/tool/contract"

const (
	ToolManage = "plugin_manage"
	ToolLoad   = "plugin_load"
)

func InputSchema(name string) (map[string]any, bool) {
	stringProp := toolcontract.String
	switch name {
	case ToolManage:
		return toolcontract.InputObject(map[string]any{
			"action":      map[string]any{"type": "string", "description": "Plugin registry action.", "enum": []string{"list", "inspect", "upsert", "remove", "enable", "disable"}},
			"name":        stringProp("Stable plugin identifier."),
			"description": stringProp("Short domain capability description exposed before plugin loading."),
			"enabled":     toolcontract.Boolean("Plugin master switch. Defaults to true for upsert."),
			"skills":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Installed document Skill names owned by the plugin."},
			"mcp_servers": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Registered dynamic MCP server names owned by the plugin."},
		}, "action"), true
	case ToolLoad:
		return toolcontract.InputObject(map[string]any{
			"name": stringProp("Enabled plugin name from agentdock_context."),
		}, "name"), true
	default:
		return nil, false
	}
}

func OutputSchema(name string) (map[string]any, bool) {
	stringProp := toolcontract.String
	intProp := toolcontract.Integer
	boolProp := toolcontract.Boolean
	arrayProp := toolcontract.ObjectArray
	stringArrayProp := toolcontract.StringArray
	objectProp := toolcontract.OpenObject
	switch name {
	case ToolManage:
		return toolcontract.OutputObject(map[string]any{
			"action":  stringProp("Completed plugin registry action."),
			"plugins": arrayProp("Registered plugin definitions."),
			"plugin":  objectProp("Plugin definition."),
			"count":   intProp("Registered plugin count."),
			"name":    stringProp("Plugin name."),
			"removed": boolProp("Whether the plugin definition was removed."),
		}), true
	case ToolLoad:
		return toolcontract.OutputObject(map[string]any{
			"plugin":              objectProp("Loaded plugin summary."),
			"skills":              arrayProp("Enabled Skill descriptions and skill:// entry points."),
			"mcp_servers":         arrayProp("Enabled dynamic MCP server descriptions and their lazily loaded MCP tool names, qualified names, and descriptions."),
			"unavailable_members": arrayProp("Configured members that are missing, disabled at their base level, or whose MCP tool discovery failed."),
			"instructions":        stringArrayProp("Progressive-disclosure next actions."),
		}), true
	default:
		return nil, false
	}
}

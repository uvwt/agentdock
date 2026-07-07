package mcp

import "github.com/uvwt/agentdock/internal/tools"

type ToolDefinition = tools.ToolDefinition

var toolRegistry = tools.ToolDefinitions()

func toolDefinition(name string) (ToolDefinition, bool) {
	for _, def := range toolRegistry {
		if def.Name == name {
			return def, true
		}
	}
	return ToolDefinition{}, false
}

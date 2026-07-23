package mcp

import "github.com/uvwt/agentdock/internal/app"

type ToolDefinition = app.ToolDefinition

var toolRegistry = app.ToolDefinitions()

func toolDefinition(name string) (ToolDefinition, bool) {
	for _, def := range toolRegistry {
		if def.Name == name {
			return def, true
		}
	}
	return ToolDefinition{}, false
}

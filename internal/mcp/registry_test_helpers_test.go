package mcp

import "github.com/uvwt/agentdock/internal/app"

var toolRegistry = app.ToolDefinitions()

func toolDefinition(name string) (ToolDefinition, bool) {
	for _, definition := range toolRegistry {
		if definition.Name == name {
			return definition, true
		}
	}
	return ToolDefinition{}, false
}

func inputSchema(name string) map[string]any {
	definition, ok := toolDefinition(name)
	if !ok {
		return nil
	}
	return definition.InputSchema
}

func outputSchema(name string) map[string]any {
	definition, ok := toolDefinition(name)
	if !ok {
		return nil
	}
	return definition.OutputSchema
}

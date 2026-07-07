package mcp

import "github.com/uvwt/agentdock/internal/tools"

type ToolDefinition struct {
	Name                   string
	Title                  string
	Description            string
	ReadOnly               bool
	Destructive            bool
	OpenWorld              bool
	FileArgRewritePaths    []string
	FileResultRewritePaths []string
}

var toolRegistry = buildToolRegistry()

func buildToolRegistry() []ToolDefinition {
	defs := tools.ToolDefinitions()
	out := make([]ToolDefinition, 0, len(defs))
	for _, def := range defs {
		out = append(out, ToolDefinition{
			Name:                   def.Name,
			Title:                  def.Title,
			Description:            def.Description,
			ReadOnly:               def.ReadOnly,
			Destructive:            def.Destructive,
			OpenWorld:              def.OpenWorld,
			FileArgRewritePaths:    append([]string(nil), def.FileArgRewritePaths...),
			FileResultRewritePaths: append([]string(nil), def.FileResultRewritePaths...),
		})
	}
	return out
}

func toolDefinition(name string) (ToolDefinition, bool) {
	for _, def := range toolRegistry {
		if def.Name == name {
			return def, true
		}
	}
	return ToolDefinition{}, false
}

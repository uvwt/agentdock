package app

import (
	"maps"

	"github.com/uvwt/agentdock/internal/agentinstructions"
	"github.com/uvwt/agentdock/internal/config"
)

type contextRequest struct {
	Workdir string `json:"workdir,omitempty"`
}

func contextToolSpecs() []ToolSpec {
	return []ToolSpec{{
		Name: "agentdock_context", Contract: contextToolContract, Title: "AgentDock context",
		Description: "Return structured AgentDock bootstrap context including capabilities, integrations, rules, and automatically loaded global/workspace AGENTS.md content. Call before project operations; pass workdir when selecting another workspace or refreshing changed rules. Selection is request-local and never changes command defaults.",
		Handler:     ctxToolHandler((*Runtime).agentDockContextTool),
	}}
}

// The standalone entrypoint adds optional local fields without changing the
// shared Nexus Bridge contract. All existing canonical fields remain identical.
func contextToolContract(name string, cfg config.Config) (ToolContract, bool) {
	contract, ok := canonicalToolContract(name, cfg)
	if !ok {
		return ToolContract{}, false
	}
	contract.InputSchema = maps.Clone(contract.InputSchema)
	input := maps.Clone(contract.InputSchema["properties"].(map[string]any))
	input["workdir"] = map[string]any{
		"type": "string", "maxLength": 4096,
		"description": "Existing host workspace directory. Omit or use an empty string for the current default; relative and ~/ paths use Host resolution. Does not change any session or command working directory.",
	}
	contract.InputSchema["properties"] = input
	contract.OutputSchema = maps.Clone(contract.OutputSchema)
	output := maps.Clone(contract.OutputSchema["properties"].(map[string]any))
	output["instruction_files"] = instructionFilesSchema()
	contract.OutputSchema["properties"] = output
	return contract, true
}

func instructionFilesSchema() map[string]any {
	return map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"auto_load", "workdir", "workspace_root", "files"},
		"properties": map[string]any{
			"auto_load":      map[string]any{"type": "boolean"},
			"workdir":        map[string]any{"type": "string"},
			"workspace_root": map[string]any{"type": "string"},
			"files": map[string]any{
				"type": "array", "maxItems": agentinstructions.MaxDirectories + 1,
				"items": map[string]any{
					"type": "object", "additionalProperties": false,
					"required": []string{"scope", "path", "status"},
					"properties": map[string]any{
						"scope":        map[string]any{"type": "string", "enum": []string{"global", "workspace"}},
						"path":         map[string]any{"type": "string"},
						"status":       map[string]any{"type": "string", "enum": []string{"loaded", "not_found", "empty", "duplicate", "skipped", "error"}},
						"content":      map[string]any{"type": "string", "maxLength": agentinstructions.MaxFileBytes},
						"sha256":       map[string]any{"type": "string", "pattern": "^[0-9a-f]{64}$"},
						"size_bytes":   map[string]any{"type": "integer", "minimum": 0},
						"reason":       map[string]any{"type": "string"},
						"duplicate_of": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
}

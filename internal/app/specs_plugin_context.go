package app

func pluginIndexSchema() map[string]any {
	return map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object", "additionalProperties": false,
			"required": []string{"name", "description", "skill_count", "mcp_server_count"},
			"properties": map[string]any{
				"name":             map[string]any{"type": "string"},
				"description":      map[string]any{"type": "string"},
				"skill_count":      map[string]any{"type": "integer", "minimum": 0},
				"mcp_server_count": map[string]any{"type": "integer", "minimum": 0},
			},
		},
	}
}

package app

func evolutionInputSchema(name string) map[string]any {
	props := map[string]any{}
	required := []string{}
	switch name {
	case "evolve":
		props["intent"] = map[string]any{"type": "string", "description": "Evolution intent.", "enum": []string{"propose", "bind", "supersede", "retract"}}
		props["candidate"] = map[string]any{
			"type": "object", "description": "Candidate knowledge for propose. Lifecycle state and evidence counts are intentionally not accepted.", "additionalProperties": false,
			"properties": map[string]any{
				"type": map[string]any{
					"type":        "string",
					"description": "Knowledge type accepted by the Evolution policy.",
					"enum": []string{
						"preference", "user_preference", "decision", "explicit_decision", "constraint",
						"runbook", "bug_pattern", "deploy_note", "project_trap", "architecture", "anti_pattern",
						"operational_lesson", "technical_fact", "workflow_template", "skill",
					},
				},
				"statement":     schemaStringProperty("Bounded reusable statement to learn."),
				"scope":         schemaStringProperty("Knowledge scope such as user, shared, project or device."),
				"project":       schemaStringProperty("Project identifier when scope is project."),
				"device":        schemaStringProperty("Device identifier when scope is device."),
				"canonical_key": schemaStringProperty("Optional stable exact-deduplication key."),
				"source":        schemaStringProperty("Short provenance label; never include hidden prompts, secrets, or raw conversation transcripts."),
				"tags":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			"required": []string{"type", "statement"},
		}
		props["evolution_id"] = schemaStringProperty("Stable evolution id for bind, supersede or retract.")
		props["task_id"] = schemaStringProperty("Task id for bind. The learning check must be bound before Task execution begins.")
		props["learning_check"] = map[string]any{
			"type": "object", "description": "Required for bind. Predeclare what a later Task pass or failure means before execution; Task outcome has no learning meaning by itself.", "additionalProperties": false,
			"properties": map[string]any{
				"on_success": map[string]any{"type": "string", "enum": []string{"support", "contradict", "none"}},
				"on_failure": map[string]any{"type": "string", "enum": []string{"support", "contradict", "none"}},
			},
			"required": []string{"on_success", "on_failure"},
		}
		props["superseded_by"] = schemaStringProperty("Replacement evolution_id for supersede when already known.")
		required = []string{"intent"}
	}
	return finalizeInputSchema(name, props, required)
}

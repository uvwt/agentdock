package app

func taskInputSchema(name string) map[string]any {
	props := map[string]any{}
	required := []string{}
	switch name {
	case "task_manage":
		props["action"] = map[string]any{"type": "string", "description": "Task lifecycle action. Use checkpoint to update live step progress.", "enum": []string{"create", "list", "get", "checkpoint", "block", "resume", "final_review", "complete"}}
		props["task_id"] = schemaStringProperty("Persistent task id for get, checkpoint, block, resume, final_review, or complete.")
		props["title"] = schemaStringProperty("Short task title for create.")
		props["goal"] = schemaStringProperty("Fixed task goal for create.")
		props["project"] = schemaStringProperty("Optional project identifier used to hard-scope Evolution guidance and evidence candidates. Omit only for global tasks.")
		props["device"] = schemaStringProperty("Optional device identifier used to hard-scope device-specific Evolution guidance and evidence candidates.")
		props["completion_conditions"] = map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "string"}, "description": "Conditions that must be true before final_review can pass."}
		props["steps"] = map[string]any{
			"type": "array", "maxItems": 12, "description": "Concrete task steps. Required when composing multiple source templates.",
			"items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"id", "title"}, "properties": map[string]any{"id": schemaStringProperty("Stable step id."), "title": schemaStringProperty("Human-readable step title.")}},
		}
		props["template_id"] = schemaStringProperty("Single active workflow template to apply. Its current active version is resolved automatically.")
		props["source_template_ids"] = map[string]any{"type": "array", "minItems": 2, "maxItems": 3, "items": map[string]any{"type": "string"}, "description": "Two or three templates already composed by the model into steps and completion_conditions."}
		props["learning_checks"] = map[string]any{
			"type": "array", "maxItems": 3, "description": "Advanced create-only blinded validation checks. Bind these Evolution ids before Guidance is generated; support-bearing targets are withheld from this Task's Guidance and may be assessed only from its frozen final_review.",
			"items": map[string]any{
				"type": "object", "additionalProperties": false, "required": []string{"evolution_id", "on_success", "on_failure"},
				"properties": map[string]any{
					"evolution_id": schemaStringProperty("Evolution id intentionally selected for this pre-execution validation."),
					"on_success":   map[string]any{"type": "string", "enum": []string{"support", "contradict", "none"}},
					"on_failure":   map[string]any{"type": "string", "enum": []string{"support", "contradict", "none"}},
				},
			},
		}
		props["step_id"] = schemaStringProperty("Task step id for a single-step checkpoint.")
		props["completed_step_ids"] = map[string]any{"type": "array", "minItems": 1, "maxItems": 12, "uniqueItems": true, "items": map[string]any{"type": "string"}, "description": "Task step ids to mark completed in one atomic batch checkpoint."}
		props["current_step_id"] = schemaStringProperty("Single task step id to mark in_progress in a batch checkpoint.")
		props["status"] = map[string]any{"type": "string", "description": "Action-specific status: task list filter, single-step checkpoint status, or final review status.", "enum": []string{"active", "blocked", "completed", "pending", "in_progress", "pass", "failed"}}
		props["limit"] = schemaIntegerProperty("Maximum tasks returned by list. Defaults to 50 and is capped at 200.")
		props["summary"] = schemaStringProperty("Current progress, blocker, resume, or final review summary.")
		props["verified"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Facts verified during final_review. Required when status=pass."}
		props["risks"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Remaining risks. Required when final_review status=failed."}
		required = []string{"action"}
	}
	return finalizeInputSchema(name, props, required)
}

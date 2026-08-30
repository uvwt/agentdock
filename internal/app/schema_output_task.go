package app

func taskOutputSchema(name string) map[string]any {
	props := map[string]any{}
	required := []string{}
	switch name {
	case "task_manage":
		props["action"] = schemaStringProperty("Completed task action.")
		props["task_id"] = schemaStringProperty("Persistent task id returned by create and usable with task lifecycle actions.")
		props["task"] = schemaOpenObjectProperty("Full persistent task state returned only by get.")
		props["task_summary"] = schemaOpenObjectProperty("Compact task summary returned by task lifecycle actions.")
		props["next_required_action"] = schemaStringProperty("Concise guidance for checkpoint progress, template composition, or final review.")
		props["review_status"] = schemaStringProperty("Final review status when present: not_started, pass, or failed.")
		props["final_review"] = schemaOpenObjectProperty("Compact final review state with status and counts.")
		props["tasks"] = schemaArrayObjectsProperty("Compact task summaries ordered by most recent update.")
		props["count"] = schemaIntegerProperty("Returned item count.")
		props["state_dir"] = schemaStringProperty("Local AgentDock task state directory.")
		props["guidance_context"] = schemaArrayObjectsProperty("Mature evolution records automatically recalled before task execution.")
		props["review_revision"] = schemaStringProperty("Immutable final_review revision used to bind evolution evidence.")
		props["evolution_candidates"] = schemaArrayObjectsProperty("Read-only candidate experiences that the saved final_review may verify.")
		props["evolution_warning"] = schemaStringProperty("Non-blocking evolution-side warning; Task lifecycle still succeeded.")
	}
	return finalizeOutputSchema(props, required)
}

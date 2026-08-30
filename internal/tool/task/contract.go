package task

import (
	"github.com/uvwt/agentdock/internal/config"
	toolcontract "github.com/uvwt/agentdock/internal/tool/contract"
)

const ToolTaskManage = "task_manage"

func ManageInputSchema(cfg config.Config) map[string]any {
	stringProp := toolcontract.String
	boundedIntProp := toolcontract.BoundedInteger
	props := map[string]any{
		"action":                map[string]any{"type": "string", "description": "Task lifecycle action. Use checkpoint to update live step progress.", "enum": []string{"create", "list", "get", "checkpoint", "block", "resume", "final_review", "complete"}},
		"task_id":               stringProp("Persistent task id for get, checkpoint, block, resume, final_review, or complete."),
		"title":                 stringProp("Short task title for create."),
		"goal":                  stringProp("Fixed task goal for create."),
		"completion_conditions": map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "string"}, "description": "Conditions that must be true before final_review can pass."},
		"step_id":               stringProp("Task step id for a single-step checkpoint."),
		"completed_step_ids":    map[string]any{"type": "array", "minItems": 1, "maxItems": 12, "uniqueItems": true, "items": map[string]any{"type": "string"}, "description": "Task step ids to mark completed in one atomic batch checkpoint."},
		"current_step_id":       stringProp("Single task step id to mark in_progress in a batch checkpoint."),
		"status":                map[string]any{"type": "string", "description": "Action-specific status: task list filter, single-step checkpoint status, or final review status.", "enum": []string{"active", "blocked", "completed", "pending", "in_progress", "pass", "failed"}},
		"limit":                 boundedIntProp("Maximum tasks returned by list. Defaults to 50 and is capped at 200.", 1, 200),
		"summary":               stringProp("Current progress, blocker, resume, or final review summary."),
		"verified":              map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Facts verified during final_review. Required when status=pass."},
		"risks":                 map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Remaining risks. Required when final_review status=failed."},
	}

	if cfg.NexusEndpoint == "" {
		props["project"] = stringProp("Optional project identifier stored with the task.")
		props["device"] = stringProp("Optional device identifier stored with the task.")
		props["steps"] = map[string]any{
			"type": "array", "maxItems": 12, "description": "Concrete task steps.",
			"items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"id", "title"}, "properties": map[string]any{"id": stringProp("Stable step id."), "title": stringProp("Human-readable step title.")}},
		}
		return toolcontract.InputObject(props, "action")
	}

	props["project"] = stringProp("Optional project identifier used to hard-scope Evolution guidance and evidence candidates. Omit only for global tasks.")
	props["device"] = stringProp("Optional device identifier used to hard-scope device-specific Evolution guidance and evidence candidates.")
	props["steps"] = map[string]any{
		"type": "array", "maxItems": 12, "description": "Concrete task steps. Required when composing multiple source templates.",
		"items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"id", "title"}, "properties": map[string]any{"id": stringProp("Stable step id."), "title": stringProp("Human-readable step title.")}},
	}
	props["template_id"] = stringProp("Single active workflow template to apply. Its current active version is resolved automatically.")
	props["source_template_ids"] = map[string]any{"type": "array", "minItems": 2, "maxItems": 3, "items": map[string]any{"type": "string"}, "description": "Two or three templates already composed by the model into steps and completion_conditions."}
	props["learning_checks"] = map[string]any{
		"type": "array", "maxItems": 3, "description": "Advanced create-only blinded validation checks. Bind these Evolution ids before Guidance is generated; support-bearing targets are withheld from this Task's Guidance and may be assessed only from its frozen final_review.",
		"items": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"evolution_id", "on_success", "on_failure"},
			"properties": map[string]any{
				"evolution_id": stringProp("Evolution id intentionally selected for this pre-execution validation."),
				"on_success":   map[string]any{"type": "string", "enum": []string{"support", "contradict", "none"}},
				"on_failure":   map[string]any{"type": "string", "enum": []string{"support", "contradict", "none"}},
			},
		},
	}
	return toolcontract.InputObject(props, "action")
}

func ManageOutputSchema(cfg config.Config) map[string]any {
	stringProp := toolcontract.String
	intProp := toolcontract.Integer
	arrayProp := toolcontract.ObjectArray
	objectProp := toolcontract.OpenObject
	props := map[string]any{
		"action":               stringProp("Completed task action."),
		"task_id":              stringProp("Persistent task id returned by create and usable with task lifecycle actions."),
		"task":                 objectProp("Full persistent task state returned only by get."),
		"task_summary":         objectProp("Compact task summary returned by task lifecycle actions."),
		"review_status":        stringProp("Final review status when present: not_started, pass, or failed."),
		"final_review":         objectProp("Compact final review state with status and counts."),
		"tasks":                arrayProp("Compact task summaries ordered by most recent update."),
		"count":                intProp("Returned item count."),
		"state_dir":            stringProp("Local AgentDock task state directory."),
		"next_required_action": stringProp("Concise guidance for checkpoint progress or final review."),
	}
	if cfg.NexusEndpoint != "" {
		props["next_required_action"] = stringProp("Concise guidance for checkpoint progress, template composition, or final review.")
		props["guidance_context"] = arrayProp("Mature evolution records automatically recalled before task execution.")
		props["review_revision"] = stringProp("Immutable final_review revision used to bind evolution evidence.")
		props["evolution_candidates"] = arrayProp("Read-only candidate experiences that the saved final_review may verify.")
		props["evolution_warning"] = stringProp("Non-blocking evolution-side warning; Task lifecycle still succeeded.")
	}
	return toolcontract.OutputObject(props)
}

func InputSchema(name string, cfg config.Config) (map[string]any, bool) {
	if name != ToolTaskManage {
		return nil, false
	}
	return ManageInputSchema(cfg), true
}

func OutputSchema(name string, cfg config.Config) (map[string]any, bool) {
	if name != ToolTaskManage {
		return nil, false
	}
	return ManageOutputSchema(cfg), true
}

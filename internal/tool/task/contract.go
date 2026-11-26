package task

import (
	"github.com/uvwt/agentdock/internal/config"
	toolcontract "github.com/uvwt/agentdock/internal/tool/contract"
)

const (
	ToolTaskCreate = "task_create"
	ToolTaskManage = "task_manage"
)

func taskCreateInputProperties(cfg config.Config) map[string]any {
	stringProp := toolcontract.String
	props := map[string]any{
		"title":                 stringProp("Short task title."),
		"goal":                  stringProp("Fixed task goal."),
		"completion_conditions": map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "string"}, "description": "Conditions that must be true before final_review can pass."},
		"project":               stringProp("Optional project identifier stored with the task."),
		"device":                stringProp("Optional device identifier stored with the task."),
		"steps": map[string]any{
			"type": "array", "maxItems": 12, "description": "Concrete task steps.",
			"items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"id", "title"}, "properties": map[string]any{"id": stringProp("Stable step id."), "title": stringProp("Human-readable step title.")}},
		},
	}
	if cfg.NexusEndpoint != "" {
		props["steps"] = map[string]any{
			"type": "array", "maxItems": 12, "description": "Concrete task steps. Required when composing multiple source templates.",
			"items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"id", "title"}, "properties": map[string]any{"id": stringProp("Stable step id."), "title": stringProp("Human-readable step title.")}},
		}
		props["template_id"] = stringProp("Single active workflow template to apply. Its current active version is resolved automatically.")
		props["source_template_ids"] = map[string]any{"type": "array", "minItems": 2, "maxItems": 3, "items": map[string]any{"type": "string"}, "description": "Two or three templates already composed by the model into steps and completion_conditions."}
	}
	return props
}

func CreateInputSchema(cfg config.Config) map[string]any {
	return toolcontract.InputObject(taskCreateInputProperties(cfg), "title", "goal", "completion_conditions")
}

func ManageInputSchema(cfg config.Config) map[string]any {
	stringProp := toolcontract.String
	boundedIntProp := toolcontract.BoundedInteger
	props := map[string]any{
		"action":             map[string]any{"type": "string", "description": "Task lifecycle action. Use checkpoint to update live step progress.", "enum": []string{"create", "list", "get", "checkpoint", "block", "resume", "final_review", "complete"}},
		"task_id":            stringProp("Persistent task id for get, checkpoint, block, resume, final_review, or complete."),
		"step_id":            stringProp("Task step id for a single-step checkpoint."),
		"completed_step_ids": map[string]any{"type": "array", "minItems": 1, "maxItems": 12, "uniqueItems": true, "items": map[string]any{"type": "string"}, "description": "Task step ids to mark completed in one atomic batch checkpoint."},
		"current_step_id":    stringProp("Single task step id to mark in_progress in a batch checkpoint."),
		"status":             map[string]any{"type": "string", "description": "Action-specific status: task list filter, single-step checkpoint status, or final review status.", "enum": []string{"active", "blocked", "completed", "pending", "in_progress", "pass", "failed"}},
		"limit":              boundedIntProp("Maximum tasks returned by list. Defaults to 50 and is capped at 200.", 1, 200),
		"summary":            stringProp("Current progress, blocker, resume, or final review summary."),
		"verified":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Facts verified during final_review. Required when status=pass."},
		"risks":              map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Remaining risks. Required when final_review status=failed."},
	}
	for name, schema := range taskCreateInputProperties(cfg) {
		props[name] = schema
	}
	return taskManageInputObject(props)
}

func taskManageInputObject(props map[string]any) map[string]any {
	schema := toolcontract.InputObject(props, "action")
	schema["allOf"] = []any{map[string]any{
		"if": map[string]any{
			"properties": map[string]any{"action": map[string]any{"const": "create"}},
			"required":   []string{"action"},
		},
		"then": map[string]any{"required": []string{"title", "goal", "completion_conditions"}},
	}}
	return schema
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
	}
	return toolcontract.OutputObject(props)
}

func InputSchema(name string, cfg config.Config) (map[string]any, bool) {
	switch name {
	case ToolTaskCreate:
		return CreateInputSchema(cfg), true
	case ToolTaskManage:
		return ManageInputSchema(cfg), true
	default:
		return nil, false
	}
}

func OutputSchema(name string, cfg config.Config) (map[string]any, bool) {
	switch name {
	case ToolTaskCreate:
		return ManageOutputSchema(cfg), true
	case ToolTaskManage:
		return ManageOutputSchema(cfg), true
	default:
		return nil, false
	}
}

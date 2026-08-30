package evolution

import toolcontract "github.com/uvwt/agentdock/internal/tool/contract"

const ToolName = "evolve"

func ToolInputSchema() map[string]any {
	stringProp := toolcontract.String
	props := map[string]any{
		"intent": map[string]any{"type": "string", "description": "Evolution intent.", "enum": []string{"propose", "bind", "supersede", "retract"}},
		"candidate": map[string]any{
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
				"statement":     stringProp("Bounded reusable statement to learn."),
				"scope":         stringProp("Knowledge scope such as user, shared, project or device."),
				"project":       stringProp("Project identifier when scope is project."),
				"device":        stringProp("Device identifier when scope is device."),
				"canonical_key": stringProp("Optional stable exact-deduplication key."),
				"source":        stringProp("Short provenance label; never include hidden prompts, secrets, or raw conversation transcripts."),
				"tags":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			"required": []string{"type", "statement"},
		},
		"evolution_id": stringProp("Stable evolution id for bind, supersede or retract."),
		"task_id":      stringProp("Task id for bind. The learning check must be bound before Task execution begins."),
		"learning_check": map[string]any{
			"type": "object", "description": "Required for bind. Predeclare what a later Task pass or failure means before execution; Task outcome has no learning meaning by itself.", "additionalProperties": false,
			"properties": map[string]any{
				"on_success": map[string]any{"type": "string", "enum": []string{"support", "contradict", "none"}},
				"on_failure": map[string]any{"type": "string", "enum": []string{"support", "contradict", "none"}},
			},
			"required": []string{"on_success", "on_failure"},
		},
		"superseded_by": stringProp("Replacement evolution_id for supersede when already known."),
	}
	return toolcontract.InputObject(props, "intent")
}

func ToolOutputSchema() map[string]any {
	stringProp := toolcontract.String
	intProp := toolcontract.Integer
	boolProp := toolcontract.Boolean
	return toolcontract.OutputObject(map[string]any{
		"intent":           stringProp("Completed evolution intent."),
		"evolution_id":     stringProp("Stable evolution id."),
		"status":           stringProp("Lifecycle status computed by AgentDock policy."),
		"revision":         intProp("Nexus-backed lifecycle revision."),
		"policy_version":   stringProp("AgentDock policy version used for the transition."),
		"support_count":    intProp("Independent support evidence count computed by AgentDock."),
		"contradict_count": intProp("Independent contradiction evidence count computed by AgentDock."),
		"changed":          boolProp("Whether durable evolution state changed."),
		"idempotent":       boolProp("Whether the request resolved to already-applied state."),
		"message":          stringProp("Short non-sensitive result explanation."),
	})
}

func InputSchema(name string) (map[string]any, bool) {
	if name != ToolName {
		return nil, false
	}
	return ToolInputSchema(), true
}

func OutputSchema(name string) (map[string]any, bool) {
	if name != ToolName {
		return nil, false
	}
	return ToolOutputSchema(), true
}

package app

import (
	toolcommand "github.com/uvwt/agentdock/internal/tool/command"
)

func commandInputSchema(name string) map[string]any {
	props := map[string]any{}
	required := []string{}
	switch name {
	case "exec_command":
		props["cmd"] = schemaStringProperty("Command to run.")
		props["workdir"] = schemaStringProperty(toolcommand.WorkdirDescription())
		toolcommand.AddRuntimeProperties(props)
		props["skill"] = schemaStringProperty("Optional active Skill context. When workdir is omitted, the command runs from the active installed Skill root and loads that Skill isolated environment.")
		props["skill_env"] = schemaStringProperty("Optional Skill name whose isolated environment is loaded without changing workdir. Kept for environment-only compatibility.")
		props["env"] = map[string]any{"type": "object", "description": "Explicit command environment values. These override the selected Skill environment.", "additionalProperties": map[string]any{"type": "string"}}
		props["timeout_ms"] = schemaBoundedIntegerProperty("Timeout in milliseconds. Must be positive and is capped at 86400000.", 1, 86400000)
		props["execution_mode"] = map[string]any{"type": "string", "description": "Execution mode. Defaults to auto: wait up to yield_time_ms, then return a running session. sync waits for exit; async returns a session immediately.", "enum": []string{"auto", "sync", "async"}}
		props["yield_time_ms"] = schemaBoundedIntegerProperty("Foreground wait threshold for execution_mode=auto. Defaults to 5000 and is capped at 30000 milliseconds.", 0, 30000)
		props["max_output_bytes"] = schemaBoundedIntegerProperty("Maximum output bytes. Defaults to 65536 and is capped at 4194304.", 1, toolcommand.MaxOutputBytes)
		props["stdin"] = schemaStringProperty("Initial stdin.")
		props["tty"] = schemaBooleanProperty("Keep stdin open.")
		required = []string{"cmd"}
	case "session_observe":
		props["action"] = map[string]any{"type": "string", "description": "Read-only session action.", "enum": []string{"list", "status"}}
		props["session_id"] = schemaStringProperty("Session id returned by exec_command, required for status.")
		props["max_output_bytes"] = schemaBoundedIntegerProperty("Maximum output bytes. Defaults to 65536 and is capped at 4194304.", 1, toolcommand.MaxOutputBytes)
	case "session_act":
		props["action"] = map[string]any{"type": "string", "description": "Mutating session action.", "enum": []string{"write", "kill", "kill_all"}}
		props["session_id"] = schemaStringProperty("Session id returned by exec_command, required for write/kill.")
		props["chars"] = schemaStringProperty("Characters to write when action=write.")
		props["max_output_bytes"] = schemaBoundedIntegerProperty("Maximum output bytes. Defaults to 65536 and is capped at 4194304.", 1, toolcommand.MaxOutputBytes)
	}
	return finalizeInputSchema(name, props, required)
}

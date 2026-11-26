package command

import toolcontract "github.com/uvwt/agentdock/internal/tool/contract"

const (
	ToolExecCommand    = "exec_command"
	ToolSessionObserve = "session_observe"
	ToolSessionAct     = "session_act"
)

func InputSchema(name string) (map[string]any, bool) {
	stringProp := toolcontract.String
	boolProp := toolcontract.Boolean
	boundedIntProp := toolcontract.BoundedInteger
	props := map[string]any{}
	var required []string

	switch name {
	case ToolExecCommand:
		props["cmd"] = stringProp("Command to run.")
		props["workdir"] = stringProp(WorkdirDescription())
		AddRuntimeProperties(props)
		props["skill_ref"] = stringProp("Optional exact Skill reference returned by AgentDock context. When workdir is omitted, the command runs from that resolved Skill root and keeps its content read-locked for the command lifetime. Managed Skills also receive a private persistent SKILL_DATA_DIR.")
		props["env"] = map[string]any{"type": "object", "description": "Explicit command environment values. These override the selected managed Skill environment, except reserved runtime values such as SKILL_DATA_DIR.", "additionalProperties": map[string]any{"type": "string"}}
		props["timeout_ms"] = boundedIntProp("Timeout in milliseconds. Must be positive and is capped at 86400000.", 1, 86400000)
		props["execution_mode"] = map[string]any{"type": "string", "description": "Execution mode. Defaults to auto: wait up to yield_time_ms, then return a running session. sync waits for exit; async returns a session immediately.", "enum": []string{"auto", "sync", "async"}}
		props["yield_time_ms"] = boundedIntProp("Foreground wait threshold for execution_mode=auto. Defaults to 5000 and is capped at 30000 milliseconds.", 0, 30000)
		props["max_output_bytes"] = boundedIntProp("Maximum output bytes. Defaults to 65536 and is capped at 4194304.", 1, MaxOutputBytes)
		props["stdin"] = stringProp("Initial stdin.")
		props["tty"] = boolProp("Keep stdin open.")
		required = []string{"cmd"}
	case ToolSessionObserve:
		props["action"] = map[string]any{"type": "string", "description": "Read-only session action.", "enum": []string{"list", "status"}}
		props["session_id"] = stringProp("Session id returned by exec_command, required for status.")
		props["max_output_bytes"] = boundedIntProp("Maximum output bytes. Defaults to 65536 and is capped at 4194304.", 1, MaxOutputBytes)
	case ToolSessionAct:
		props["action"] = map[string]any{"type": "string", "description": "Mutating session action.", "enum": []string{"write", "kill", "kill_all"}}
		props["session_id"] = stringProp("Session id returned by exec_command, required for write/kill.")
		props["chars"] = stringProp("Characters to write when action=write.")
		props["max_output_bytes"] = boundedIntProp("Maximum output bytes. Defaults to 65536 and is capped at 4194304.", 1, MaxOutputBytes)
	default:
		return nil, false
	}
	return toolcontract.InputObject(props, required...), true
}

func OutputSchema(name string) (map[string]any, bool) {
	stringProp := toolcontract.String
	intProp := toolcontract.Integer
	boolProp := toolcontract.Boolean
	arrayProp := toolcontract.ObjectArray
	props := map[string]any{
		"sessions":             arrayProp("Command session summaries returned by list or bulk session actions."),
		"count":                intProp("Command session count when a list or bulk action returns multiple sessions."),
		"session_id":           stringProp("Command session id."),
		"status":               stringProp("Session status."),
		"runtime":              stringProp("Command runtime when reported by the host, such as windows or wsl."),
		"wsl_distribution":     stringProp("WSL distribution selected for the command when explicitly configured."),
		"workdir":              stringProp("Logical command working directory in the selected runtime."),
		"stdout":               stringProp("Captured stdout segment."),
		"stderr":               stringProp("Captured stderr segment when non-empty or truncated."),
		"command_ok":           boolProp("Whether a completed command exited successfully. Omitted while the command is still running."),
		"command_error":        stringProp("Command process error when execution did not succeed."),
		"exit_code":            intProp("Process exit code, when available."),
		"elapsed_ms":           intProp("Session elapsed milliseconds."),
		"timed_out":            boolProp("True when the command timed out; omitted otherwise."),
		"terminal":             stringProp("Non-default terminal backend such as conpty; omitted when using standard pipes."),
		"stdout_truncated":     boolProp("True when stdout was truncated, dropped, or omitted; omitted otherwise."),
		"stdout_output_bytes":  intProp("Returned stdout bytes when stdout was truncated."),
		"stdout_total_bytes":   intProp("Total stdout bytes captured when stdout was truncated."),
		"stdout_dropped_bytes": intProp("Oldest stdout bytes dropped from the session buffer when stdout overflowed."),
		"stdout_omitted_bytes": intProp("Unread stdout bytes omitted by max_output_bytes when stdout was truncated."),
		"stderr_truncated":     boolProp("True when stderr was truncated, dropped, or omitted; omitted otherwise."),
		"stderr_output_bytes":  intProp("Returned stderr bytes when stderr was truncated."),
		"stderr_total_bytes":   intProp("Total stderr bytes captured when stderr was truncated."),
		"stderr_dropped_bytes": intProp("Oldest stderr bytes dropped from the session buffer when stderr overflowed."),
		"stderr_omitted_bytes": intProp("Unread stderr bytes omitted by max_output_bytes when stderr was truncated."),
	}
	switch name {
	case ToolExecCommand:
		props["session_reason"] = stringProp("Why exec_command returned a session instead of a completed result.")
		props["observe_after_ms"] = intProp("Suggested delay before inspecting the returned session.")
	case ToolSessionObserve:
	case ToolSessionAct:
		props["kill_operation_ms"] = intProp("Milliseconds spent terminating the session for action=kill.")
	default:
		return nil, false
	}
	return toolcontract.OutputObject(props), true
}

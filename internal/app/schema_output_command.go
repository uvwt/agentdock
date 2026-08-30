package app

func commandOutputSchema(name string) map[string]any {
	props := map[string]any{}
	required := []string{}
	switch name {
	case "exec_command", "session_observe", "session_act":
		props["sessions"] = schemaArrayObjectsProperty("Command session summaries returned by list or bulk session actions.")
		props["count"] = schemaIntegerProperty("Command session count when a list or bulk action returns multiple sessions.")
		props["session_id"] = schemaStringProperty("Command session id.")
		props["status"] = schemaStringProperty("Session status.")
		props["runtime"] = schemaStringProperty("Command runtime when reported by the host, such as windows or wsl.")
		props["wsl_distribution"] = schemaStringProperty("WSL distribution selected for the command when explicitly configured.")
		props["workdir"] = schemaStringProperty("Logical command working directory in the selected runtime.")
		props["stdout"] = schemaStringProperty("Captured stdout segment.")
		props["stderr"] = schemaStringProperty("Captured stderr segment.")
		props["command_ok"] = schemaBooleanProperty("Whether a completed command exited successfully. Omitted while the command is still running.")
		props["command_error"] = schemaStringProperty("Command process error when execution did not succeed.")
		props["exit_code"] = schemaIntegerProperty("Process exit code, when available.")
		props["elapsed_ms"] = schemaIntegerProperty("Session elapsed milliseconds.")
		props["timed_out"] = schemaBooleanProperty("Whether the command timed out.")
		if name == "exec_command" {
			props["session_reason"] = schemaStringProperty("Why exec_command returned a session instead of a completed result.")
			props["observe_after_ms"] = schemaIntegerProperty("Suggested delay before inspecting the returned session.")
		}
	}
	return finalizeOutputSchema(props, required)
}

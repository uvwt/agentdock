package app

func evolutionOutputSchema(name string) map[string]any {
	props := map[string]any{}
	required := []string{}
	switch name {
	case "evolve":
		props["intent"] = schemaStringProperty("Completed evolution intent.")
		props["evolution_id"] = schemaStringProperty("Stable evolution id.")
		props["status"] = schemaStringProperty("Lifecycle status computed by AgentDock policy.")
		props["revision"] = schemaIntegerProperty("Nexus-backed lifecycle revision.")
		props["policy_version"] = schemaStringProperty("AgentDock policy version used for the transition.")
		props["support_count"] = schemaIntegerProperty("Independent support evidence count computed by AgentDock.")
		props["contradict_count"] = schemaIntegerProperty("Independent contradiction evidence count computed by AgentDock.")
		props["changed"] = schemaBooleanProperty("Whether durable evolution state changed.")
		props["idempotent"] = schemaBooleanProperty("Whether the request resolved to already-applied state.")
		props["message"] = schemaStringProperty("Short non-sensitive result explanation.")
	}
	return finalizeOutputSchema(props, required)
}

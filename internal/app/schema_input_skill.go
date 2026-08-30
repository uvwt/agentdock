package app

func skillInputSchema(name string) map[string]any {
	props := map[string]any{}
	required := []string{}
	switch name {
	case "skill_package":
		props["action"] = map[string]any{"type": "string", "description": "Skill package or isolated environment action.", "enum": []string{"validate", "install", "activate", "rollback", "env_set", "env_unset", "env_list"}}
		props["skill"] = schemaStringProperty("Skill name for activate, rollback, or environment management.")
		props["version"] = schemaStringProperty("Installed Skill version for activate.")
		props["key"] = schemaStringProperty("Environment variable name for env_set/env_unset.")
		props["value"] = schemaStringProperty("Environment variable value for env_set. Secret values are never returned.")
		props["source"] = schemaStringProperty("Host path or HTTP(S) URL for validate/install.")
		props["digest"] = schemaStringProperty("Optional expected SHA-256 digest for validate/install.")
		props["activate"] = schemaBooleanProperty("Activate the installed version. Defaults to true.")
		props["max_bytes"] = schemaIntegerProperty("Maximum validate/install package bytes.")
		required = []string{"action"}
	}
	return finalizeInputSchema(name, props, required)
}

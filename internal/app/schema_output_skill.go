package app

func skillOutputSchema(name string) map[string]any {
	props := map[string]any{}
	required := []string{}
	switch name {
	case "skill_package":
		props["action"] = schemaStringProperty("Completed Skill package action.")
		props["skill"] = schemaStringProperty("Skill name.")
		props["name"] = schemaStringProperty("Skill name for environment actions.")
		props["key"] = schemaStringProperty("Environment variable name. Secret values are never returned.")
		props["configured"] = schemaBooleanProperty("Whether the environment variable has a non-empty configured value.")
		props["removed"] = schemaBooleanProperty("Whether the environment variable was removed.")
		props["items"] = schemaArrayObjectsProperty("Environment variable names and configured status without values.")
		props["count"] = schemaIntegerProperty("Returned environment variable count.")
		props["valid"] = schemaBooleanProperty("Whether a Skill source passed validation.")
		props["source"] = schemaStringProperty("Resolved Skill source label.")
		props["digest"] = schemaStringProperty("Computed Skill package digest.")
		props["issues"] = schemaArrayObjectsProperty("Structured validation issues.")
		props["document"] = schemaOpenObjectProperty("Parsed SKILL.md frontmatter and body metadata.")
		props["result"] = schemaOpenObjectProperty("Install, activate, or rollback result.")
	}
	return finalizeOutputSchema(props, required)
}

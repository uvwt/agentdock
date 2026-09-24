package skill

import toolcontract "github.com/uvwt/agentdock/internal/tool/contract"

const ToolManage = "skill_manage"

func ManageInputSchema() map[string]any {
	stringProp := toolcontract.String
	return toolcontract.InputObject(map[string]any{
		"action":    map[string]any{"type": "string", "description": "Managed Skill action. Environment actions also support Plugin-owned Skills through exact skill_ref.", "enum": []string{"install", "remove", "env_set", "env_unset", "env_list"}},
		"skill":     stringProp("Standalone managed Skill name for remove."),
		"skill_ref": stringProp("Exact managed/plugin Skill reference returned by AgentDock. Required for env_set, env_unset, and env_list."),
		"key":       stringProp("Environment variable name for env_set/env_unset. SKILL_DATA_DIR is reserved by the managed Skill runtime and cannot be configured."),
		"value":     stringProp("Environment variable value for env_set. Secret values are never returned."),
		"source":    stringProp("Host path or HTTP(S) URL for install. Reinstalling the same name updates current content."),
		"digest":    stringProp("Optional expected SHA-256 source digest for install integrity checking."),
		"purge":     toolcontract.Boolean("For remove, also delete the managed Skill environment and persistent data."),
	}, "action")
}

func ManageOutputSchema() map[string]any {
	stringProp := toolcontract.String
	boolProp := toolcontract.Boolean
	arrayProp := toolcontract.ObjectArray
	return toolcontract.OutputObject(map[string]any{
		"action":         stringProp("Completed managed Skill action."),
		"skill":          stringProp("Managed Skill name."),
		"skill_ref":      stringProp("Exact managed/plugin Skill reference for environment actions."),
		"key":            stringProp("Environment variable name. Secret values are never returned."),
		"configured":     boolProp("Whether the environment variable has a non-empty configured value."),
		"removed":        boolProp("Whether the requested content or environment variable was removed."),
		"purged":         boolProp("Whether preserved managed Skill environment and data were also removed."),
		"items":          arrayProp("Environment variable names and configured status without values."),
		"content_digest": stringProp("Current managed Skill package content digest."),
		"changed":        boolProp("Whether install changed the current managed Skill content."),
	})
}

func InputSchema(name string) (map[string]any, bool) {
	if name != ToolManage {
		return nil, false
	}
	return ManageInputSchema(), true
}

func OutputSchema(name string) (map[string]any, bool) {
	if name != ToolManage {
		return nil, false
	}
	return ManageOutputSchema(), true
}

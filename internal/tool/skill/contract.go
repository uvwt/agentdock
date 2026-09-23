package skill

import toolcontract "github.com/uvwt/agentdock/internal/tool/contract"

const ToolManage = "skill_manage"

func ManageInputSchema() map[string]any {
	stringProp := toolcontract.String
	return toolcontract.InputObject(map[string]any{
		"action":    map[string]any{"type": "string", "description": "Managed Skill action. Environment actions also support Plugin-owned Skills through exact skill_ref.", "enum": []string{"install", "remove", "env_set", "env_unset", "env_list"}},
		"skill":     stringProp("Standalone managed Skill name for remove or environment management."),
		"skill_ref": stringProp("Exact managed/plugin Skill reference returned by AgentDock for environment management. When omitted, skill refers to a standalone managed Skill."),
		"key":       stringProp("Environment variable name for env_set/env_unset. SKILL_DATA_DIR is reserved by the managed Skill runtime and cannot be configured."),
		"value":     stringProp("Environment variable value for env_set. Secret values are never returned."),
		"source":    stringProp("Host path or HTTP(S) URL for install. Reinstalling the same name updates current content."),
		"digest":    stringProp("Optional expected SHA-256 source digest for install integrity checking."),
		"purge":     toolcontract.Boolean("For remove, also delete the managed Skill environment and persistent data."),
		"max_bytes": toolcontract.Integer("Maximum install package bytes."),
		"max_files": toolcontract.Integer("Maximum install package regular files."),
	}, "action")
}

func ManageOutputSchema() map[string]any {
	stringProp := toolcontract.String
	intProp := toolcontract.Integer
	boolProp := toolcontract.Boolean
	arrayProp := toolcontract.ObjectArray
	objectProp := toolcontract.OpenObject
	return toolcontract.OutputObject(map[string]any{
		"action":         stringProp("Completed managed Skill action."),
		"skill":          stringProp("Managed Skill name."),
		"skill_ref":      stringProp("Exact managed/plugin Skill reference for environment actions."),
		"name":           stringProp("Managed Skill name for environment actions."),
		"key":            stringProp("Environment variable name. Secret values are never returned."),
		"configured":     boolProp("Whether the environment variable has a non-empty configured value."),
		"removed":        boolProp("Whether the requested content or environment variable was removed."),
		"purged":         boolProp("Whether preserved managed Skill environment and data were also removed."),
		"items":          arrayProp("Environment variable names and configured status without values."),
		"count":          intProp("Returned environment variable count."),
		"content_digest": stringProp("Current managed Skill package content digest."),
		"changed":        boolProp("Whether install changed the current managed Skill content."),
		"result":         objectProp("Managed Skill install or remove result."),
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

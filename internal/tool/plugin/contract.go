package plugin

import toolcontract "github.com/uvwt/agentdock/internal/tool/contract"

const ToolManage = "plugin_manage"

func InputSchema(name string) (map[string]any, bool) {
	if name != ToolManage {
		return nil, false
	}
	return toolcontract.InputObject(map[string]any{
		"action": map[string]any{
			"type":        "string",
			"description": "Plugin lifecycle action.",
			"enum":        []string{"list", "inspect", "validate", "install", "update", "enable", "disable", "remove", "catalog"},
		},
		"name":   toolcontract.String("Installed Plugin name for inspect/enable/disable/remove."),
		"source": toolcontract.String("Plugin or external catalog source. Local paths are resolved against the workspace; Git and archive sources are staged before validation."),
		"source_type": map[string]any{
			"type": "string", "description": "Source transport. auto detects local/Git/ZIP; catalog resolves one read-only catalog entry.",
			"enum": []string{"auto", "local", "git", "archive", "catalog"},
		},
		"source_adapter": map[string]any{
			"type": "string", "description": "Plugin format adapter. auto detects portable/OpenAI/Claude.",
			"enum": []string{"auto", "portable", "openai", "claude"},
		},
		"source_version": toolcontract.String("SemVer fallback for an external manifest that omits version."),
		"git_ref":        toolcontract.String("Optional Git branch/tag to resolve. A full git_commit pin takes precedence for identity verification."),
		"git_commit":     toolcontract.String("Optional full 40-character Git commit pin."),
		"subdir":         toolcontract.String("Optional safe subdirectory inside a Git/archive source."),
		"sha256":         toolcontract.String("Optional expected SHA-256 pin for an HTTPS ZIP archive."),
		"catalog": map[string]any{
			"type": "string", "description": "Catalog format for action=catalog or source_type=catalog.",
			"enum": []string{"auto", "openai", "claude"},
		},
		"catalog_item":            toolcontract.String("Catalog entry name when source_type=catalog."),
		"enabled":                 toolcontract.Boolean("Initial enabled state for install. Defaults to true."),
		"review_token":            toolcontract.String("Required for install/update. Copy the exact review_token returned by plugin_manage validate; it binds confirmation to the staged package digest and security review."),
		"confirmed_source_change": toolcontract.Boolean("For update, explicitly confirm rebinding an installed Plugin to a different source."),
		"data_policy": map[string]any{
			"type":        "string",
			"description": "Required for remove. keep preserves Plugin data and Plugin-owned MCP environment; purge deletes both.",
			"enum":        []string{"keep", "purge"},
		},
	}, "action"), true
}

func OutputSchema(name string) (map[string]any, bool) {
	if name != ToolManage {
		return nil, false
	}
	return toolcontract.OutputObject(map[string]any{
		"action":         toolcontract.String("Completed Plugin action."),
		"name":           toolcontract.String("Plugin name."),
		"version":        toolcontract.String("Current Plugin version."),
		"enabled":        toolcontract.Boolean("Whether the Plugin is enabled."),
		"changed":        toolcontract.Boolean("Whether the operation changed installed state."),
		"package_digest": toolcontract.String("Plugin package content digest; validate returns the digest bound by review_token."),
		"review_token":   toolcontract.String("Exact security-review token returned by validate and required unchanged for install/update."),
		"data_policy":    toolcontract.String("Applied removal data policy."),
		"plugin":         toolcontract.OpenObject("Installed Plugin details and component provenance."),
		"plugins":        toolcontract.ObjectArray("Installed Plugin lightweight states."),
		"review":         toolcontract.OpenObject("Static Plugin validation, compatibility, source-pin and security review."),
		"catalog":        toolcontract.OpenObject("Read-only external marketplace catalog and stable install source descriptors."),
		"count":          toolcontract.Integer("Installed Plugin or catalog entry count."),
	}), true
}

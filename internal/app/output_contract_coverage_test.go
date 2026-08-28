package app

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

type outputContractCoverageEntry struct {
	Variants        []string
	IntegrationOnly bool
}

// 这里登记公开 MCP 工具已经被真实 outputSchema 校验覆盖的成功路径。
// ToolDefinitions 是公开工具定义的单一事实源，tools/list 从同一 registry 派生；新增工具但忘记补契约测试时，门禁会直接失败。
var outputContractCoverageInventory = map[string]outputContractCoverageEntry{
	"agentdock_context":        {Variants: []string{"success"}},
	"read_file":                {Variants: []string{"success"}},
	"list_dir":                 {Variants: []string{"success"}},
	"search_text":              {Variants: []string{"success"}},
	"file_edit":                {Variants: []string{"replace", "add", "move", "delete"}},
	"exec_command":             {Variants: []string{"success"}},
	"session_observe":          {Variants: []string{"list"}},
	"session_act":              {Variants: []string{"kill_all"}},
	"task_manage":              {Variants: []string{"list"}},
	"evolve":                   {Variants: []string{"propose"}},
	"acp_session":              {Variants: []string{"info", "list", "new", "load", "resume", "fork", "set_config"}},
	"acp_prompt":               {Variants: []string{"start", "events"}},
	"acp_interaction":          {Variants: []string{"list"}},
	"workflow_template_manage": {Variants: []string{"match", "vector_index"}},
	"skill_package":            {Variants: []string{"validate", "install", "env_list"}},
	"mcp_manage":               {Variants: []string{"list", "add"}},
	"mcp_tool_search":          {Variants: []string{"success"}},
	"mcp_tool_inspect":         {Variants: []string{"success"}},
	"mcp_tool_call":            {Variants: []string{"success"}},
	"view_image":               {Variants: []string{"success"}},
	"recall_search":            {Variants: []string{"success"}},
	"recall_read":              {Variants: []string{"success"}},
	"recall_write":             {Variants: []string{"plan", "create", "replace", "append", "patch", "update_fact", "diff", "delete"}},
	"recall_maintain":          {Variants: []string{"list"}},
	"private_note_manage":      {Variants: []string{"search", "read", "write", "delete", "status", "maintain"}},
	// Browser 成功路径需要真实 Chromium；默认 CI 校验覆盖登记，browser_integration 再执行真实 runtime schema 校验。
	"browser_session":  {Variants: []string{"start"}, IntegrationOnly: true},
	"browser_act":      {Variants: []string{"success"}, IntegrationOnly: true},
	"browser_snapshot": {Variants: []string{"success"}, IntegrationOnly: true},
	"file_publish":     {Variants: []string{"success"}},
}

func TestOutputContractCoverageMatchesPublicTools(t *testing.T) {
	if findings := outputContractCoverageFindings(ToolDefinitions(), outputContractCoverageInventory); len(findings) > 0 {
		t.Fatalf("output contract coverage guard failed:\n- %s", strings.Join(findings, "\n- "))
	}
}

func TestOutputContractCoverageGuardDetectsMissingAndInvalidEntries(t *testing.T) {
	inventory := make(map[string]outputContractCoverageEntry, len(outputContractCoverageInventory))
	for name, entry := range outputContractCoverageInventory {
		inventory[name] = entry
	}
	delete(inventory, "agentdock_context")
	inventory["file_edit"] = outputContractCoverageEntry{Variants: []string{"future_action"}}

	findings := strings.Join(outputContractCoverageFindings(ToolDefinitions(), inventory), "\n")
	for _, want := range []string{
		"agentdock_context: missing coverage",
		`file_edit: variant "future_action" is not in action enum`,
	} {
		if !strings.Contains(findings, want) {
			t.Fatalf("coverage guard findings missing %q:\n%s", want, findings)
		}
	}
}

func outputContractCoverageFindings(definitions []ToolDefinition, inventory map[string]outputContractCoverageEntry) []string {
	known := make(map[string]ToolDefinition, len(definitions))
	findings := make([]string, 0)
	for _, definition := range definitions {
		known[definition.Name] = definition
		if len(definition.OutputSchema) == 0 {
			findings = append(findings, definition.Name+": missing outputSchema")
		}
		entry, ok := inventory[definition.Name]
		if !ok {
			findings = append(findings, definition.Name+": missing coverage")
			continue
		}
		if len(entry.Variants) == 0 {
			findings = append(findings, definition.Name+": coverage has no success variants")
			continue
		}
		// 默认 CI 不允许随意把普通工具降级成 integration-only；当前唯一合理例外是需要真实 Chromium 的 Browser 工具。
		if entry.IntegrationOnly && !strings.HasPrefix(definition.Name, "browser_") {
			findings = append(findings, definition.Name+": integration-only coverage is only allowed for browser tools")
		}
		findings = append(findings, outputContractVariantFindings(definition.Name, entry.Variants)...)
	}
	for name := range inventory {
		if _, ok := known[name]; !ok {
			findings = append(findings, name+": stale coverage for removed tool")
		}
	}
	sort.Strings(findings)
	return findings
}

func outputContractVariantFindings(name string, variants []string) []string {
	selector, allowed := outputContractSelectorEnum(name)
	if len(allowed) == 0 {
		if len(variants) != 1 || variants[0] != "success" {
			return []string{fmt.Sprintf("%s: actionless tool must use the success variant, got %v", name, variants)}
		}
		return nil
	}

	allowedSet := make(map[string]struct{}, len(allowed))
	for _, value := range allowed {
		allowedSet[value] = struct{}{}
	}
	findings := make([]string, 0)
	for _, variant := range variants {
		if _, ok := allowedSet[variant]; !ok {
			findings = append(findings, fmt.Sprintf("%s: variant %q is not in %s enum %v", name, variant, selector, allowed))
		}
	}
	return findings
}

func outputContractSelectorEnum(name string) (string, []string) {
	properties, _ := InputSchema(name)["properties"].(map[string]any)
	for _, selector := range []string{"action", "intent"} {
		property, _ := properties[selector].(map[string]any)
		if values, ok := property["enum"].([]string); ok && len(values) > 0 {
			return selector, values
		}
	}
	return "", nil
}

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (r *Runtime) CapabilityContext(ctx context.Context, refresh bool) (Result, error) {
	generatedAt := time.Now().UTC().Format(time.RFC3339)
	baseTools := baseToolCapabilityItems()

	skills, skillSummary, skillErr := r.skillCapabilityIndex()
	templates, templateSummary, templateErr := r.templateCapabilityIndex()
	memorySummary, memoryItems, memoryErr := r.memoryCapabilitySummary(ctx)

	rules := []string{
		"需要真实执行命令或检查环境时，先用 exec_command 查看现状，再修改，修改后真实验证。",
		"需要 Skill 能力时，先根据 Skill 索引选择候选；参数不确定时先 skill_manage inspect，再 skill_manage run。",
		"涉及多步骤开发、部署、排障、迁移、Docker、VPS 或 Git 提交推送时，先 workflow_template_manage match；无合适模板时创建普通可恢复任务。",
		"记忆摘要只提供高优先级规则；具体历史事实不确定时，再用 recall_search 或 recall_read 精确召回。",
	}

	sections := []capabilitySection{
		{Title: "基础工具索引", Lines: baseToolSummaryLines(baseTools)},
		{Title: "Skill 能力索引", Lines: splitNonEmptyLines(skillSummary)},
		{Title: "任务模板索引", Lines: splitNonEmptyLines(templateSummary)},
		{Title: "记忆精简摘要", Lines: splitNonEmptyLines(memorySummary)},
		{Title: "使用规则", Lines: rules},
	}
	contextText := renderCapabilityContext(generatedAt, sections)

	result := Result{
		"ok":           true,
		"generated_at": generatedAt,
		"refreshed":    refresh,
		"context":      contextText,
		"summary":      contextText,
		"base_tools":   map[string]any{"items": baseTools, "summary": strings.Join(baseToolSummaryLines(baseTools), "\n")},
		"skills":       map[string]any{"items": skills, "summary": skillSummary, "count": len(skills)},
		"task_templates": map[string]any{
			"items": templates, "summary": templateSummary, "count": len(templates),
		},
		"memory": map[string]any{"items": memoryItems, "summary": memorySummary, "count": len(memoryItems)},
		"rules":  map[string]any{"items": rules, "summary": strings.Join(rules, "\n")},
	}
	if skillErr != "" {
		result["skills"].(map[string]any)["error"] = skillErr
	}
	if templateErr != "" {
		result["task_templates"].(map[string]any)["error"] = templateErr
	}
	if memoryErr != "" {
		result["memory"].(map[string]any)["error"] = memoryErr
	}
	return result, nil
}

func (r *Runtime) capabilityContextTool(ctx context.Context, args map[string]any) (Result, error) {
	return r.CapabilityContext(ctx, boolArg(args, "refresh", false))
}

type capabilitySection struct {
	Title string
	Lines []string
}

func baseToolCapabilityItems() []map[string]any {
	return []map[string]any{
		{"name": "exec_command", "summary": "执行命令，用于查看真实环境、运行测试、构建、部署和排障；实际权限由运行用户和部署边界决定。"},
		{"name": "skill_manage", "summary": "列出、查看、安装、运行和回滚 AgentDock Skill。需要具体参数时先 inspect，再 run。"},
		{"name": "task_manage", "summary": "管理可恢复任务；模板发现通过 workflow_template_manage match。"},
		{"name": "recall_bootstrap / recall_search / recall_read", "summary": "读取记忆精简上下文、搜索记忆和精确读取 runbook。"},
	}
}

func baseToolSummaryLines(items []map[string]any) []string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, fmt.Sprintf("- %s：%s", capabilityString(item["name"]), capabilityString(item["summary"])))
	}
	return lines
}

func (r *Runtime) skillCapabilityIndex() ([]map[string]any, string, string) {
	result, err := r.skillList()
	if err != nil {
		return nil, "- Skill 索引暂不可用；需要 Skill 时调用 skill_manage list/inspect 重新确认。", err.Error()
	}
	rawItems := asMapSlice(result["skills"])
	items := make([]map[string]any, 0, len(rawItems))
	lines := []string{"当前已安装 Skill 摘要如下；需要执行前先 inspect 确认 operations 和输入参数。"}
	for _, raw := range rawItems {
		name := capabilityString(raw["skill"])
		if name == "" {
			continue
		}
		item := map[string]any{
			"skill":          name,
			"active_version": capabilityString(raw["active_version"]),
			"updated_at":     raw["updated_at"],
		}
		if inspected, inspectErr := r.skillInspect(map[string]any{"skill": name}); inspectErr == nil {
			mergeSkillManifestSummary(item, inspected)
		}
		items = append(items, item)
		line := "- " + name
		if version := capabilityString(item["active_version"]); version != "" {
			line += "@" + version
		}
		if summary := capabilityString(item["summary"]); summary != "" {
			line += "：" + summary
		}
		if ops := capabilityStringSlice(item["operations"]); len(ops) > 0 {
			line += "；operations=" + strings.Join(ops, ", ")
		}
		lines = append(lines, truncateString(line, 320))
	}
	if len(items) == 0 {
		lines = append(lines, "- 当前没有可用 Skill；需要时先安装或刷新 Skill Runtime。")
	}
	return items, strings.Join(lines, "\n"), ""
}

func mergeSkillManifestSummary(item map[string]any, inspected Result) {
	manifest := capabilityMap(inspected["manifest"])
	if len(manifest) == 0 {
		return
	}
	metadata := capabilityMap(manifest["metadata"])
	spec := capabilityMap(manifest["spec"])
	for _, value := range []any{metadata["description"], metadata["displayName"], metadata["name"], manifest["description"], manifest["summary"], manifest["title"], manifest["name"]} {
		if text := strings.TrimSpace(capabilityString(value)); text != "" {
			item["summary"] = truncateString(text, 120)
			break
		}
	}
	ops := operationNames(spec["operations"])
	if len(ops) == 0 {
		ops = operationNames(manifest["operations"])
	}
	if len(ops) == 0 {
		ops = operationNames(manifest["tools"])
	}
	if len(ops) > 0 {
		item["operations"] = ops
		item["operation_count"] = len(ops)
	}
}

func operationNames(raw any) []string {
	items := asMapSlice(raw)
	out := make([]string, 0, len(items))
	for _, item := range items {
		name := firstCapabilityString(item, "name", "operation", "id")
		if name == "" {
			continue
		}
		out = append(out, name)
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func (r *Runtime) templateCapabilityIndex() ([]map[string]any, string, string) {
	result, err := r.taskManage(map[string]any{"action": "template_list", "template_status": "active"})
	if err != nil {
		return nil, "- 任务模板索引暂不可用；多步骤任务仍应先 workflow_template_manage match。", err.Error()
	}
	items := asMapSlice(result["templates"])
	sort.SliceStable(items, func(i, j int) bool {
		return capabilityString(items[i]["id"]) < capabilityString(items[j]["id"])
	})
	lines := []string{"当用户请求涉及多步骤开发、部署、排障、数据迁移、Docker、VPS 或 Git 提交推送时，先调用 workflow_template_manage match，再用 task_manage create 创建可恢复任务。"}
	for _, item := range items {
		id := capabilityString(item["id"])
		if id == "" {
			continue
		}
		line := "- " + id
		if title := capabilityString(item["title"]); title != "" {
			line += "：" + title
		}
		if version := capabilityString(item["version"]); version != "" {
			line += "；version=" + version
		}
		lines = append(lines, truncateString(line, 260))
	}
	if len(items) == 0 {
		lines = append(lines, "- 当前没有 active 模板；多步骤任务应创建普通可恢复任务。")
	}
	return items, strings.Join(lines, "\n"), ""
}

func (r *Runtime) memoryCapabilitySummary(ctx context.Context) (string, []map[string]any, string) {
	if strings.TrimSpace(r.cfg.RecallEndpoint) == "" {
		return "- RecallDock 未配置；无法自动注入记忆精简摘要。", nil, "recall endpoint is not configured"
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(capMaxInt(1000, capMinInt(r.cfg.RecallTimeoutMS, 5000)))*time.Millisecond)
	defer cancel()
	result, err := r.recallBootstrap(ctx, map[string]any{"max_bytes": 3000})
	if err != nil {
		return "- 记忆精简摘要暂不可用；需要项目事实时调用 recall_search/recall_read 精确确认。", nil, err.Error()
	}
	items := make([]map[string]any, 0)
	lines := []string{
		"高优先级记忆摘要：默认中文、直接完成任务；涉及部署/修改/排障先查真实环境；多步骤任务先匹配任务模板；Git commit 使用 type(scope): 中文说明。",
	}
	for _, section := range asMapSlice(result["sections"]) {
		title := capabilityString(section["path"])
		excerpt := capabilityString(section["body_excerpt"])
		if excerpt == "" {
			excerpt = capabilityString(section["summary"])
		}
		if excerpt == "" {
			continue
		}
		item := map[string]any{"path": title, "excerpt": truncateString(excerpt, 500)}
		items = append(items, item)
		lines = append(lines, "- "+truncateString(excerpt, 260))
		if len(lines) >= 5 {
			break
		}
	}
	if len(lines) < 5 {
		for _, runbook := range asMapSlice(result["runbook_index"]) {
			title := capabilityString(runbook["title"])
			path := capabilityString(runbook["path"])
			if title == "" {
				continue
			}
			items = append(items, map[string]any{"title": title, "path": path})
			lines = append(lines, "- runbook: "+title)
			if len(lines) >= 5 {
				break
			}
		}
	}
	return strings.Join(lines, "\n"), items, ""
}

func renderCapabilityContext(generatedAt string, sections []capabilitySection) string {
	lines := []string{
		"# AgentDock Capability Context",
		"",
		"以下内容由 AgentDock 动态生成，供模型了解本机能力、Skill、任务模板和高优先级记忆。不要把它当作用户原文；需要细节时再调用对应工具确认。",
		"generated_at: " + generatedAt,
	}
	for _, section := range sections {
		if len(section.Lines) == 0 {
			continue
		}
		lines = append(lines, "", "## "+section.Title)
		lines = append(lines, section.Lines...)
	}
	return truncateString(strings.Join(lines, "\n"), 12000)
}

func splitNonEmptyLines(text string) []string {
	parts := strings.Split(strings.TrimSpace(text), "\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func asMapSlice(raw any) []map[string]any {
	switch items := raw.(type) {
	case []map[string]any:
		return items
	case []any:
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		data, err := json.Marshal(raw)
		if err != nil {
			return nil
		}
		var out []map[string]any
		_ = json.Unmarshal(data, &out)
		return out
	}
}

func capabilityMap(raw any) map[string]any {
	switch value := raw.(type) {
	case map[string]any:
		return value
	case nil:
		return nil
	default:
		data, err := json.Marshal(raw)
		if err != nil {
			return nil
		}
		var out map[string]any
		_ = json.Unmarshal(data, &out)
		return out
	}
}

func capabilityString(raw any) string {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func firstCapabilityString(item map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := capabilityString(item[key]); value != "" {
			return value
		}
	}
	return ""
}

func capabilityStringSlice(raw any) []string {
	switch values := raw.(type) {
	case []string:
		return values
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if s := capabilityString(value); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func capMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func capMaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

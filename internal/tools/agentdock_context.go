package tools

import (
	"context"
	"sort"
	"strings"
	"time"
)

func (r *Runtime) AgentDockContext(ctx context.Context) (Result, error) {
	baseTools := baseToolCapabilityItems()
	baseToolLines := baseToolSummaryLines(baseTools)

	_, skillSummary, _ := r.skillCapabilityIndex()
	_, templateSummary, _ := r.templateCapabilityIndex()
	memorySummary, _, _ := r.memoryCapabilitySummary(ctx)

	rules := []string{
		"需要真实执行命令或检查环境时，先用 exec_command 查看现状，再修改，修改后真实验证。",
		"需要 Skill 能力时，先用 skill_read list/inspect 做只读发现；包生命周期用 skill_package；执行 operation 用 skill_run；Skill 环境变量用 skill_env_manage。",
		"涉及多步骤开发、部署、排障、迁移、Docker、VPS 或 Git 提交推送时，先 workflow_template_manage match；无合适模板时创建普通可恢复任务。",
		"记忆摘要只提供高优先级规则；具体历史事实不确定时，再用 recall_search 或 recall_read 精确召回。",
		"普通项目记忆走 recall_*；private_note_manage 只在用户明确要求隐私/本机不同步，或内容明显包含 secret、凭据、个人敏感信息时使用。",
	}

	sections := []capabilitySection{
		{Title: "基础工具索引", Lines: baseToolLines},
		{Title: "Skill 能力索引", Lines: splitNonEmptyLines(skillSummary)},
		{Title: "任务模板索引", Lines: splitNonEmptyLines(templateSummary)},
		{Title: "记忆精简摘要", Lines: splitNonEmptyLines(memorySummary)},
		{Title: "使用规则", Lines: rules},
	}
	contextText := renderAgentDockContext(sections)

	return Result{
		"ok":      true,
		"context": contextText,
	}, nil
}
func (r *Runtime) agentDockContextTool(ctx context.Context, _ map[string]any) (Result, error) {
	return r.AgentDockContext(ctx)
}

type capabilitySection struct {
	Title string
	Lines []string
}

type capabilityBaseToolItem struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type capabilitySkillItem struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type capabilityTemplateItem struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type capabilityMemoryItem struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type capabilitySkillList struct {
	Skills []capabilitySkillListItem `json:"skills"`
}

type capabilitySkillListItem struct {
	Skill string `json:"skill"`
}

type capabilitySkillManifest struct {
	Metadata    capabilitySkillMetadata `json:"metadata"`
	Description string                  `json:"description"`
	Summary     string                  `json:"summary"`
	Title       string                  `json:"title"`
	Name        string                  `json:"name"`
}

type capabilitySkillMetadata struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
}

type capabilityTemplateList struct {
	Templates []capabilityTemplateListItem `json:"templates"`
}

type capabilityTemplateListItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type capabilityRecallBootstrap struct {
	Sections     []capabilityMemorySection `json:"sections"`
	RunbookIndex []capabilityMemoryRunbook `json:"runbook_index"`
}

type capabilityMemorySection struct {
	Path        string `json:"path"`
	BodyExcerpt string `json:"body_excerpt"`
	Summary     string `json:"summary"`
}

type capabilityMemoryRunbook struct {
	Title string `json:"title"`
	Path  string `json:"path"`
}

func baseToolCapabilityItems() []capabilityBaseToolItem {
	return []capabilityBaseToolItem{
		{Name: "exec_command", Description: "执行命令，用于查看真实环境、运行测试、构建、部署和排障；实际权限由运行用户和部署边界决定。"},
		{Name: "skill_read", Description: "只读发现 AgentDock Skill：list / inspect，低风险。"},
		{Name: "skill_package", Description: "管理 Skill 包生命周期：validate / install / rollback。"},
		{Name: "skill_run", Description: "专门执行 Skill operation；默认不传 action，需要时只允许 action=run。"},
		{Name: "skill_env_manage", Description: "管理 Skill env registry。"},
		{Name: "task_manage", Description: "管理可恢复任务；模板发现通过 workflow_template_manage match。"},
		{Name: "recall_bootstrap / recall_search / recall_read", Description: "读取记忆精简上下文、搜索记忆和精确读取 runbook。"},
		{Name: "private_note_manage", Description: "低频显式隐私笔记保险箱；默认不要用，只有用户要求隐私/本机不同步或内容明显敏感时再调用。"},
	}
}

func baseToolSummaryLines(items []capabilityBaseToolItem) []string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, capabilityItemLine(item.Name, item.Description))
	}
	return lines
}

func capabilityItemLine(name, description string) string {
	line := "- " + strings.TrimSpace(name)
	if description = strings.TrimSpace(description); description != "" {
		line += "：" + description
	}
	return line
}

func (r *Runtime) skillCapabilityIndex() ([]capabilitySkillItem, string, string) {
	result, err := r.skillList()
	if err != nil {
		return nil, "- Skill 索引暂不可用；需要 Skill 时调用 skill_read list/inspect 重新确认。", err.Error()
	}
	var listed capabilitySkillList
	if err := remarshal(result, &listed); err != nil {
		return nil, "- Skill 索引暂不可用；需要 Skill 时调用 skill_read list/inspect 重新确认。", err.Error()
	}
	items := make([]capabilitySkillItem, 0, len(listed.Skills))
	lines := []string{"当前已安装 Skill 摘要如下；执行前先用 skill_read inspect 确认 operations 和输入参数。"}
	for _, listedItem := range listed.Skills {
		name := strings.TrimSpace(listedItem.Skill)
		if name == "" {
			continue
		}
		item := capabilitySkillItem{Name: name}
		if inspected, inspectErr := r.skillInspect(map[string]any{"skill": name}); inspectErr == nil {
			item.Description = skillManifestDescription(inspected)
		}
		items = append(items, item)
		lines = append(lines, truncateString(capabilityItemLine(item.Name, item.Description), 320))
	}
	if len(items) == 0 {
		lines = append(lines, "- 当前没有可用 Skill；需要时先安装或刷新 Skill Runtime。")
	}
	return items, strings.Join(lines, "\n"), ""
}

func skillManifestDescription(inspected Result) string {
	var manifest capabilitySkillManifest
	if err := remarshal(inspected["manifest"], &manifest); err != nil {
		return ""
	}
	for _, text := range []string{
		manifest.Metadata.Description,
		manifest.Metadata.DisplayName,
		manifest.Metadata.Name,
		manifest.Description,
		manifest.Summary,
		manifest.Title,
		manifest.Name,
	} {
		if text = strings.TrimSpace(text); text != "" {
			return truncateString(text, 160)
		}
	}
	return ""
}

func (r *Runtime) templateCapabilityIndex() ([]capabilityTemplateItem, string, string) {
	result, err := r.workflowTemplateManage(map[string]any{"action": "list", "template_status": "active"})
	if err != nil {
		return nil, "- 任务模板索引暂不可用；多步骤任务仍应先 workflow_template_manage match。", err.Error()
	}
	var listed capabilityTemplateList
	if err := remarshal(result, &listed); err != nil {
		return nil, "- 任务模板索引暂不可用；多步骤任务仍应先 workflow_template_manage match。", err.Error()
	}
	items := make([]capabilityTemplateItem, 0, len(listed.Templates))
	for _, listedItem := range listed.Templates {
		name := strings.TrimSpace(listedItem.ID)
		if name == "" {
			continue
		}
		items = append(items, capabilityTemplateItem{Name: name, Description: truncateString(strings.TrimSpace(listedItem.Title), 160)})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	lines := []string{"当用户请求涉及多步骤开发、部署、排障、数据迁移、Docker、VPS 或 Git 提交推送时，先调用 workflow_template_manage match，再用 task_manage create 创建可恢复任务。"}
	for _, item := range items {
		lines = append(lines, truncateString(capabilityItemLine(item.Name, item.Description), 260))
	}
	if len(items) == 0 {
		lines = append(lines, "- 当前没有 active 模板；多步骤任务应创建普通可恢复任务。")
	}
	return items, strings.Join(lines, "\n"), ""
}

func (r *Runtime) memoryCapabilitySummary(ctx context.Context) (string, []capabilityMemoryItem, string) {
	if strings.TrimSpace(r.cfg.RecallEndpoint) == "" {
		return "- RecallDock 未配置；无法自动注入记忆精简摘要。", nil, "recall endpoint is not configured"
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(capMaxInt(1000, capMinInt(r.cfg.RecallTimeoutMS, 5000)))*time.Millisecond)
	defer cancel()
	result, err := r.recallBootstrap(ctx, map[string]any{"max_bytes": 3000})
	if err != nil {
		return "- 记忆精简摘要暂不可用；需要项目事实时调用 recall_search/recall_read 精确确认。", nil, err.Error()
	}
	var bootstrap capabilityRecallBootstrap
	if err := remarshal(result, &bootstrap); err != nil {
		return "- 记忆精简摘要暂不可用；需要项目事实时调用 recall_search/recall_read 精确确认。", nil, err.Error()
	}
	items := make([]capabilityMemoryItem, 0)
	lines := make([]string, 0, 5)
	for _, section := range bootstrap.Sections {
		excerpt := strings.TrimSpace(section.BodyExcerpt)
		if excerpt == "" {
			excerpt = strings.TrimSpace(section.Summary)
		}
		if excerpt == "" {
			continue
		}
		items = append(items, capabilityMemoryItem{Name: section.Path, Description: truncateString(excerpt, 500)})
		lines = append(lines, "- "+truncateString(excerpt, 260))
		if len(lines) >= 5 {
			break
		}
	}
	if len(lines) < 5 {
		for _, runbook := range bootstrap.RunbookIndex {
			title := strings.TrimSpace(runbook.Title)
			if title == "" {
				continue
			}
			items = append(items, capabilityMemoryItem{Name: title, Description: runbook.Path})
			lines = append(lines, "- runbook: "+title)
			if len(lines) >= 5 {
				break
			}
		}
	}
	if len(lines) == 0 {
		return "- 记忆精简摘要暂无条目；需要项目事实时调用 recall_search/recall_read 精确确认。", items, ""
	}
	return strings.Join(lines, "\n"), items, ""
}

func renderAgentDockContext(sections []capabilitySection) string {
	lines := []string{
		"# AgentDock Context",
		"",
		"以下内容由 AgentDock 动态生成，供模型首次接入时了解本机能力、Skill、任务模板、运行规则和高优先级记忆。不要把它当作用户原文；需要细节时再调用对应工具确认。",
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

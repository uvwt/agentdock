package tools

import (
	"context"
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
		"需要 Skill 能力时，先用 skill_read list/inspect 做只读发现；包生命周期用 skill_package；执行 operation 用 skill_run；Skill 环境变量用 skill_env_manage。",
		"涉及多步骤开发、部署、排障、迁移、Docker、VPS 或 Git 提交推送时，先 workflow_template_manage match；无合适模板时创建普通可恢复任务。",
		"记忆摘要只提供高优先级规则；具体历史事实不确定时，再用 recall_search 或 recall_read 精确召回。",
		"普通项目记忆走 recall_*；private_note_manage 只在用户明确要求隐私/本机不同步，或内容明显包含 secret、凭据、个人敏感信息时使用。",
	}

	sections := []capabilitySection{
		{Title: "基础工具索引", Lines: baseToolSummaryLines(baseTools)},
		{Title: "Skill 能力索引", Lines: splitNonEmptyLines(skillSummary)},
		{Title: "任务模板索引", Lines: splitNonEmptyLines(templateSummary)},
		{Title: "记忆精简摘要", Lines: splitNonEmptyLines(memorySummary)},
		{Title: "使用规则", Lines: rules},
	}
	contextText := renderCapabilityContext(generatedAt, sections)

	skillBlock := capabilitySkillBlock{Items: skills, Summary: skillSummary, Count: len(skills), Error: skillErr}
	templateBlock := capabilityTemplateBlock{Items: templates, Summary: templateSummary, Count: len(templates), Error: templateErr}
	memoryBlock := capabilityMemoryBlock{Items: memoryItems, Summary: memorySummary, Count: len(memoryItems), Error: memoryErr}

	return Result{
		"ok":             true,
		"generated_at":   generatedAt,
		"refreshed":      refresh,
		"context":        contextText,
		"summary":        contextText,
		"base_tools":     capabilityBaseToolBlock{Items: baseTools, Summary: strings.Join(baseToolSummaryLines(baseTools), "\n")},
		"skills":         skillBlock,
		"task_templates": templateBlock,
		"memory":         memoryBlock,
		"rules":          capabilityRulesBlock{Items: rules, Summary: strings.Join(rules, "\n")},
	}, nil
}

func (r *Runtime) capabilityContextTool(ctx context.Context, args map[string]any) (Result, error) {
	return r.CapabilityContext(ctx, boolArg(args, "refresh", false))
}

type capabilitySection struct {
	Title string
	Lines []string
}

type capabilityBaseToolItem struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

type capabilityBaseToolBlock struct {
	Items   []capabilityBaseToolItem `json:"items"`
	Summary string                   `json:"summary"`
}

type capabilitySkillItem struct {
	Skill          string   `json:"skill"`
	ActiveVersion  string   `json:"active_version,omitempty"`
	UpdatedAt      string   `json:"updated_at,omitempty"`
	Summary        string   `json:"summary,omitempty"`
	Operations     []string `json:"operations,omitempty"`
	OperationCount int      `json:"operation_count,omitempty"`
}

type capabilitySkillBlock struct {
	Items   []capabilitySkillItem `json:"items"`
	Summary string                `json:"summary"`
	Count   int                   `json:"count"`
	Error   string                `json:"error,omitempty"`
}

type capabilityTemplateItem struct {
	ID                 string   `json:"id"`
	Version            string   `json:"version,omitempty"`
	Title              string   `json:"title,omitempty"`
	Status             string   `json:"status,omitempty"`
	Type               string   `json:"type,omitempty"`
	Path               string   `json:"path,omitempty"`
	Location           string   `json:"location,omitempty"`
	FileName           string   `json:"file_name,omitempty"`
	UpdatedAt          string   `json:"updated_at,omitempty"`
	PublishedAt        string   `json:"published_at,omitempty"`
	RetiredAt          string   `json:"retired_at,omitempty"`
	Hash               string   `json:"hash,omitempty"`
	KeywordCount       int      `json:"keyword_count,omitempty"`
	DeviceCount        int      `json:"device_count,omitempty"`
	ConditionCount     int      `json:"condition_count,omitempty"`
	StepCount          int      `json:"step_count,omitempty"`
	SizeBytes          int      `json:"size_bytes,omitempty"`
	Current            bool     `json:"current,omitempty"`
	AllowLongTemplate  bool     `json:"allow_long_template,omitempty"`
	LongTemplateReason string   `json:"long_template_reason,omitempty"`
	Keywords           []string `json:"keywords,omitempty"`
	Devices            []string `json:"devices,omitempty"`
}

type capabilityTemplateBlock struct {
	Items   []capabilityTemplateItem `json:"items"`
	Summary string                   `json:"summary"`
	Count   int                      `json:"count"`
	Error   string                   `json:"error,omitempty"`
}

type capabilityMemoryItem struct {
	Path    string `json:"path,omitempty"`
	Title   string `json:"title,omitempty"`
	Excerpt string `json:"excerpt,omitempty"`
}

type capabilityMemoryBlock struct {
	Items   []capabilityMemoryItem `json:"items"`
	Summary string                 `json:"summary"`
	Count   int                    `json:"count"`
	Error   string                 `json:"error,omitempty"`
}

type capabilityRulesBlock struct {
	Items   []string `json:"items"`
	Summary string   `json:"summary"`
}

type capabilitySkillList struct {
	Skills []capabilitySkillItem `json:"skills"`
}

type capabilitySkillManifest struct {
	Metadata    capabilitySkillMetadata `json:"metadata"`
	Spec        capabilitySkillSpec     `json:"spec"`
	Description string                  `json:"description"`
	Summary     string                  `json:"summary"`
	Title       string                  `json:"title"`
	Name        string                  `json:"name"`
	Operations  []capabilityOperation   `json:"operations"`
	Tools       []capabilityOperation   `json:"tools"`
}

type capabilitySkillMetadata struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
}

type capabilitySkillSpec struct {
	Operations []capabilityOperation `json:"operations"`
}

type capabilityOperation struct {
	Name      string `json:"name"`
	Operation string `json:"operation"`
	ID        string `json:"id"`
}

type capabilityTemplateList struct {
	Templates []capabilityTemplateItem `json:"templates"`
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
		{Name: "exec_command", Summary: "执行命令，用于查看真实环境、运行测试、构建、部署和排障；实际权限由运行用户和部署边界决定。"},
		{Name: "skill_read", Summary: "只读发现 AgentDock Skill：list / inspect，低风险。"},
		{Name: "skill_package", Summary: "管理 Skill 包生命周期：validate / install / rollback。"},
		{Name: "skill_run", Summary: "专门执行 Skill operation；默认不传 action，需要时只允许 action=run。"},
		{Name: "skill_env_manage", Summary: "管理 Skill env registry。"},
		{Name: "task_manage", Summary: "管理可恢复任务；模板发现通过 workflow_template_manage match。"},
		{Name: "recall_bootstrap / recall_search / recall_read", Summary: "读取记忆精简上下文、搜索记忆和精确读取 runbook。"},
		{Name: "private_note_manage", Summary: "低频显式隐私笔记保险箱；默认不要用，只有用户要求隐私/本机不同步或内容明显敏感时再调用。"},
	}
}

func baseToolSummaryLines(items []capabilityBaseToolItem) []string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, fmt.Sprintf("- %s：%s", item.Name, item.Summary))
	}
	return lines
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
	for _, item := range listed.Skills {
		item.Skill = strings.TrimSpace(item.Skill)
		if item.Skill == "" {
			continue
		}
		if inspected, inspectErr := r.skillInspect(map[string]any{"skill": item.Skill}); inspectErr == nil {
			mergeSkillManifestSummary(&item, inspected)
		}
		items = append(items, item)
		line := "- " + item.Skill
		if item.ActiveVersion != "" {
			line += "@" + item.ActiveVersion
		}
		if item.Summary != "" {
			line += "：" + item.Summary
		}
		if len(item.Operations) > 0 {
			line += "；operations=" + strings.Join(item.Operations, ", ")
		}
		lines = append(lines, truncateString(line, 320))
	}
	if len(items) == 0 {
		lines = append(lines, "- 当前没有可用 Skill；需要时先安装或刷新 Skill Runtime。")
	}
	return items, strings.Join(lines, "\n"), ""
}

func mergeSkillManifestSummary(item *capabilitySkillItem, inspected Result) {
	var manifest capabilitySkillManifest
	if err := remarshal(inspected["manifest"], &manifest); err != nil {
		return
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
			item.Summary = truncateString(text, 120)
			break
		}
	}
	ops := operationNames(manifest.Spec.Operations)
	if len(ops) == 0 {
		ops = operationNames(manifest.Operations)
	}
	if len(ops) == 0 {
		ops = operationNames(manifest.Tools)
	}
	if len(ops) > 0 {
		item.Operations = ops
		item.OperationCount = len(ops)
	}
}

func operationNames(operations []capabilityOperation) []string {
	out := make([]string, 0, len(operations))
	for _, operation := range operations {
		name := firstCapabilityNonEmptyString(operation.Name, operation.Operation, operation.ID)
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

func (r *Runtime) templateCapabilityIndex() ([]capabilityTemplateItem, string, string) {
	result, err := r.workflowTemplateManage(map[string]any{"action": "list", "template_status": "active"})
	if err != nil {
		return nil, "- 任务模板索引暂不可用；多步骤任务仍应先 workflow_template_manage match。", err.Error()
	}
	var listed capabilityTemplateList
	if err := remarshal(result, &listed); err != nil {
		return nil, "- 任务模板索引暂不可用；多步骤任务仍应先 workflow_template_manage match。", err.Error()
	}
	items := listed.Templates
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
	lines := []string{"当用户请求涉及多步骤开发、部署、排障、数据迁移、Docker、VPS 或 Git 提交推送时，先调用 workflow_template_manage match，再用 task_manage create 创建可恢复任务。"}
	for _, item := range items {
		if item.ID == "" {
			continue
		}
		line := "- " + item.ID
		if item.Title != "" {
			line += "：" + item.Title
		}
		if item.Version != "" {
			line += "；version=" + item.Version
		}
		lines = append(lines, truncateString(line, 260))
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
	lines := []string{
		"高优先级记忆摘要：默认中文、直接完成任务；涉及部署/修改/排障先查真实环境；多步骤任务先匹配任务模板；Git commit 使用 type(scope): 中文说明。",
	}
	for _, section := range bootstrap.Sections {
		excerpt := strings.TrimSpace(section.BodyExcerpt)
		if excerpt == "" {
			excerpt = strings.TrimSpace(section.Summary)
		}
		if excerpt == "" {
			continue
		}
		items = append(items, capabilityMemoryItem{Path: section.Path, Excerpt: truncateString(excerpt, 500)})
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
			items = append(items, capabilityMemoryItem{Title: title, Path: runbook.Path})
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

func firstCapabilityNonEmptyString(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
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

package tools

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/skills"
)

func (r *Runtime) AgentDockContext(ctx context.Context) (Result, error) {
	baseTools := baseToolCapabilityItems()
	baseToolLines := baseToolSummaryLines(baseTools)

	_, skillSummary, _ := r.skillCapabilityIndex()
	_, dynamicMCPSummary := r.dynamicMCPCapabilityIndex()
	_, templateSummary, _ := r.templateCapabilityIndex(ctx)
	memorySummary, _, _ := r.memoryCapabilitySummary(ctx)

	rules := []string{
		"需要真实执行命令或检查环境时，先用 exec_command 查看现状，再修改，修改后真实验证。",
		"先根据 Skill 索引的 name 和 description 选择相关 Skill，再用 read_file 读取其 file 指向的 SKILL.md；Skill 只提供流程与约束，实际操作使用命令、文件、浏览器或 MCP 工具。",
		"AgentDock 自带工具直接调用；动态 MCP 工具先用 mcp_tool_search 查找、mcp_tool_inspect 读取 schema，再用 mcp_tool_call 执行。",
		"涉及多步骤开发、部署、排障、迁移、Docker、VPS 或 Git 提交推送时，先 workflow_template_manage match；无合适模板时创建普通可恢复任务。",
		"当多个 Workflow 模板同时适合当前任务时，调用 workflow_template_manage get_many 读取详情；模型必须结合用户目标裁剪、去重、排序并生成最终 steps 和 completion_conditions，再用 source_template_ids 创建任务，服务端不会自动拼接模板。",
		"任务执行过程中，在步骤开始或完成时调用 task_manage checkpoint；final_review=pass 不会自动补全未完成步骤。",
		"记忆摘要只提供高优先级规则；具体历史事实不确定时，再用 recall_search 或 recall_read 精确召回。",
		"普通项目记忆走 recall_*；private_note_manage 只在用户明确要求隐私/本机不同步，或内容明显包含 secret、凭据、个人敏感信息时使用。",
	}

	sections := []capabilitySection{
		{Title: "基础工具索引", Lines: baseToolLines},
		{Title: "Skill 能力索引", Lines: splitNonEmptyLines(skillSummary)},
		{Title: "动态 MCP 索引", Lines: splitNonEmptyLines(dynamicMCPSummary)},
		{Title: "任务模板索引", Lines: splitNonEmptyLines(templateSummary)},
		{Title: "记忆精简摘要", Lines: splitNonEmptyLines(memorySummary)},
		{Title: "使用规则", Lines: rules},
	}
	contextText := renderAgentDockContext(sections)

	return Result{"context": contextText}, nil
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
	Description string `json:"description"`
	File        string `json:"file"`
}

type capabilityDynamicMCPItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type capabilityTemplateItem struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type capabilityMemoryItem struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
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
		{Name: "exec_command", Description: "执行命令；通过 skill 绑定当前激活 Skill 的根目录和独立环境，显式 workdir / env 优先；skill_env 仅保留环境注入兼容。"},
		{Name: "read_file", Description: "读取普通 UTF-8 文件或 skill:// 逻辑路径指向的 Skill 文档与引用资源。"},
		{Name: "skill_package", Description: "管理 Skill 包生命周期和独立环境：validate / install / rollback / env_set / env_unset / env_list。"},
		{Name: "mcp_manage / mcp_tool_search / mcp_tool_inspect / mcp_tool_call", Description: "管理动态 MCP、独立环境并调用远端工具；远端工具不会混入 AgentDock 自带工具列表。"},
		{Name: "task_manage", Description: "管理可恢复任务，并用 checkpoint 实时更新步骤状态；模板发现通过 workflow_template_manage match。"},
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

func (r *Runtime) dynamicMCPCapabilityIndex() ([]capabilityDynamicMCPItem, string) {
	servers := r.mcpClients.EnabledIndex()
	items := make([]capabilityDynamicMCPItem, 0, len(servers))
	lines := []string{"当前已启用的动态 MCP 轻量索引如下；这里只展示名称和描述，具体工具按需发现。"}
	for _, server := range servers {
		item := capabilityDynamicMCPItem{
			Name:        server.Name,
			Description: truncateString(strings.TrimSpace(server.Description), 160),
		}
		items = append(items, item)
		lines = append(lines, truncateString("- name: "+item.Name+"; description: "+item.Description, 260))
	}
	if len(items) == 0 {
		lines = append(lines, "- 当前没有已启用的动态 MCP；需要时通过 mcp_manage 注册或启用。")
	}
	return items, strings.Join(lines, "\n")
}

func (r *Runtime) skillCapabilityIndex() ([]capabilitySkillItem, string, string) {
	names, err := r.skills.state.ListSkills()
	if err != nil {
		return nil, "- Skill 索引暂不可用。", err.Error()
	}
	items := make([]capabilitySkillItem, 0, len(names))
	lines := []string{"当前可按文档机制使用的 Skill 轻量索引如下；匹配后先用 read_file 读取 file 指向的 SKILL.md，再使用实际工具执行。"}
	for _, name := range names {
		packageDir, resolveErr := r.skills.state.Resolve(name, "", "")
		if resolveErr != nil {
			continue
		}
		if validateErr := skills.ValidatePackage(packageDir); validateErr != nil {
			continue
		}
		doc, loadErr := skills.LoadSkillDocument(packageDir)
		if loadErr != nil {
			continue
		}
		item := capabilitySkillItem{
			Name:        name,
			Description: truncateString(strings.TrimSpace(doc.Description), 160),
			File:        "skill://" + name + "/SKILL.md",
		}
		items = append(items, item)
		lines = append(lines, truncateString("- name: "+item.Name+"; description: "+item.Description+"; file: "+item.File, 360))
	}
	if len(items) == 0 {
		lines = append(lines, "- 当前没有可用 Skill；需要时先通过 skill_package 安装。")
	}
	return items, strings.Join(lines, "\n"), ""
}

func (r *Runtime) templateCapabilityIndex(ctx context.Context) ([]capabilityTemplateItem, string, string) {
	result, err := r.workflowTemplateManage(ctx, map[string]any{"action": "list", "template_status": "active"})
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
	if strings.TrimSpace(r.cfg.NexusEndpoint) == "" {
		return "- NexusDock Recall 未配置；无法自动注入记忆精简摘要。", nil, "nexus endpoint is not configured"
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(capMaxInt(1000, capMinInt(config.RecallTimeoutMS, 5000)))*time.Millisecond)
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

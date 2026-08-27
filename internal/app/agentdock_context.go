package app

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/config"
)

func (r *Runtime) AgentDockContext(ctx context.Context) (Result, error) {
	return r.agentDockContext(ctx, false)
}

// AgentDockLocalContext 仅供 Nexus Bridge 使用。它不读取 Nexus 统一管理的
// Workflow/Recall，避免 fleet 聚合时按节点重复回灌共享上下文。
func (r *Runtime) AgentDockLocalContext(ctx context.Context) (Result, error) {
	return r.agentDockContext(ctx, true)
}

func (r *Runtime) agentDockContext(ctx context.Context, nexusLocalOnly bool) (Result, error) {
	skills, skillErr := r.skillCapabilityIndex()
	contextResult := capabilityContext{
		Skills:            skills,
		DynamicMCP:        r.dynamicMCPCapabilityIndex(),
		WorkflowTemplates: []capabilityTemplateItem{},
		Rules: []string{
			"需要真实执行命令或检查环境时，先用 exec_command 查看现状，再修改，修改后真实验证。",
			"先根据 Skill 索引的 name 和 description 选择相关 Skill，再用 read_file 读取其 file 指向的 SKILL.md；Skill 只提供流程与约束，实际操作使用命令、文件、浏览器或 MCP 工具。",
			"AgentDock 自带工具直接调用；动态 MCP 工具先用 mcp_tool_search 查找、mcp_tool_inspect 读取 schema，再用 mcp_tool_call 执行。",
		},
	}
	if skillErr != nil {
		contextResult.Warnings = append(contextResult.Warnings, capabilityWarning{Source: "skills", Message: "Skill 索引暂不可用。"})
	}

	if requiresACP(r.cfg) {
		contextResult.ACP = &capabilityACPContext{
			Enabled: true,
			Agent:   r.cfg.ACPAgentName,
			Description: "本机 Coding Agent 通道（Agent Client Protocol）。仅当用户明确要求时使用，可用来获取独特见解与编排任务；" +
				"不是动态 MCP，不要用 mcp_tool_*。",
		}
	}

	if requiresNexus(r.cfg) && !nexusLocalOnly {
		templates, templateErr := r.templateCapabilityIndex(ctx)
		if templateErr != nil {
			contextResult.Warnings = append(contextResult.Warnings, capabilityWarning{Source: "workflow_templates", Message: "工作流模板索引暂不可用；多步骤任务仍应先 workflow_template_manage match。"})
		}
		memoryItems, memoryErr := r.memoryCapabilityIndex(ctx)
		if memoryErr != nil {
			contextResult.Warnings = append(contextResult.Warnings, capabilityWarning{Source: "recall", Message: "记忆精简摘要暂不可用；需要项目事实时调用 recall_search/recall_read 精确确认。"})
		}
		contextResult.WorkflowTemplates = templates
		contextResult.Recall = &capabilityRecallContext{Enabled: true, Items: memoryItems}
		contextResult.Rules = append(contextResult.Rules,
			"涉及多步骤开发、部署、排障、迁移、Docker、VPS 或 Git 提交推送时，先 workflow_template_manage match；无合适模板时创建普通可恢复任务。",
			"当多个工作流模板同时适合当前任务时，调用 workflow_template_manage get_many 读取详情；模型必须结合用户目标裁剪、去重、排序并生成最终 steps 和 completion_conditions，再用 source_template_ids 创建任务，服务端不会自动拼接模板。",
			"普通项目记忆走 recall_*；private_note_manage 只在用户明确要求私密笔记，或内容明显包含 secret、凭据、个人敏感信息时使用。私密检索只返回名称、简介、标签、分类和路径等元数据；正文必须显式 read，Git 只备份 age 密文。",
		)
	}

	contextResult.Rules = append(contextResult.Rules, "任务执行过程中，在形成有恢复价值的断点时调用 task_manage checkpoint；可用 completed_step_ids/current_step_id 原子批量更新，final_review=pass 不会自动补全未完成步骤。")
	if requiresNexus(r.cfg) && !nexusLocalOnly {
		contextResult.Rules = append(contextResult.Rules, "记忆摘要只提供高优先级规则；具体历史事实不确定时，再用 recall_search 或 recall_read 精确召回。")
	}

	var result Result
	if err := remarshal(contextResult, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Runtime) agentDockContextTool(ctx context.Context, _ map[string]any) (Result, error) {
	return r.AgentDockContext(ctx)
}

type capabilityContext struct {
	Skills            []capabilitySkillItem      `json:"skills"`
	DynamicMCP        []capabilityDynamicMCPItem `json:"dynamic_mcp"`
	ACP               *capabilityACPContext      `json:"acp,omitempty"`
	WorkflowTemplates []capabilityTemplateItem   `json:"workflow_templates"`
	Recall            *capabilityRecallContext   `json:"recall,omitempty"`
	Rules             []string                   `json:"rules"`
	Warnings          []capabilityWarning        `json:"warnings,omitempty"`
}

type capabilitySkillItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	File        string `json:"file"`
	Bundled     bool   `json:"bundled,omitempty"`
}

type capabilityDynamicMCPItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type capabilityACPContext struct {
	Enabled     bool   `json:"enabled"`
	Agent       string `json:"agent"`
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

type capabilityRecallContext struct {
	Enabled bool                   `json:"enabled"`
	Items   []capabilityMemoryItem `json:"items"`
}

type capabilityWarning struct {
	Source  string `json:"source"`
	Message string `json:"message"`
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

func (r *Runtime) dynamicMCPCapabilityIndex() []capabilityDynamicMCPItem {
	servers := r.mcpClients.EnabledIndex()
	items := make([]capabilityDynamicMCPItem, 0, len(servers))
	for _, server := range servers {
		items = append(items, capabilityDynamicMCPItem{
			Name:        server.Name,
			Description: truncateString(strings.TrimSpace(server.Description), 160),
		})
	}
	return items
}

func (r *Runtime) skillCapabilityIndex() ([]capabilitySkillItem, error) {
	skillItems, err := r.skills.CapabilityItems()
	if err != nil {
		return []capabilitySkillItem{}, err
	}
	items := make([]capabilitySkillItem, 0, len(skillItems))
	for _, skill := range skillItems {
		items = append(items, capabilitySkillItem{
			Name:        skill.Name,
			Description: truncateString(strings.TrimSpace(skill.Description), 160),
			File:        skill.File,
			Bundled:     skill.Bundled,
		})
	}
	return items, nil
}

func (r *Runtime) templateCapabilityIndex(ctx context.Context) ([]capabilityTemplateItem, error) {
	result, err := r.workflowTemplateManage(ctx, map[string]any{"action": "list", "template_status": "active"})
	if err != nil {
		return []capabilityTemplateItem{}, err
	}
	var listed capabilityTemplateList
	if err := remarshal(result, &listed); err != nil {
		return []capabilityTemplateItem{}, err
	}
	items := make([]capabilityTemplateItem, 0, len(listed.Templates))
	for _, listedItem := range listed.Templates {
		name := strings.TrimSpace(listedItem.ID)
		if name == "" {
			continue
		}
		items = append(items, capabilityTemplateItem{Name: name, Description: truncateString(strings.TrimSpace(listedItem.Title), 160)})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func (r *Runtime) memoryCapabilityIndex(ctx context.Context) ([]capabilityMemoryItem, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(capMaxInt(1000, capMinInt(config.RecallTimeoutMS, 5000)))*time.Millisecond)
	defer cancel()
	result, err := r.recallBootstrap(ctx, map[string]any{"max_bytes": 3000})
	if err != nil {
		return []capabilityMemoryItem{}, err
	}
	var bootstrap capabilityRecallBootstrap
	if err := remarshal(result, &bootstrap); err != nil {
		return []capabilityMemoryItem{}, err
	}
	items := make([]capabilityMemoryItem, 0, 5)
	for _, section := range bootstrap.Sections {
		excerpt := strings.TrimSpace(section.BodyExcerpt)
		if excerpt == "" {
			excerpt = strings.TrimSpace(section.Summary)
		}
		if excerpt == "" {
			continue
		}
		items = append(items, capabilityMemoryItem{Name: section.Path, Description: truncateString(excerpt, 500)})
		if len(items) >= 5 {
			return items, nil
		}
	}
	for _, runbook := range bootstrap.RunbookIndex {
		title := strings.TrimSpace(runbook.Title)
		if title == "" {
			continue
		}
		items = append(items, capabilityMemoryItem{Name: title, Description: runbook.Path})
		if len(items) >= 5 {
			break
		}
	}
	return items, nil
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

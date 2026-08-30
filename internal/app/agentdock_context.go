package app

import (
	"context"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/buildinfo"
	"github.com/uvwt/agentdock/internal/config"
	tooltask "github.com/uvwt/agentdock/internal/tool/task"
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
	if !nexusLocalOnly {
		// runtime 只保留模型操作主机所需的稳定环境事实；Nexus Bridge 已通过 Hello 持有这些节点事实，
		// 私有 context.local 不重复传输，避免两个来源长期漂移。
		contextResult.Runtime = &capabilityRuntimeContext{
			Version: buildinfo.Version, OS: runtime.GOOS, Arch: runtime.GOARCH,
			AgentDockHome: r.cfg.AgentDockHome, AgentDockDefaultDir: r.cfg.AgentDockDefaultDir,
			DefaultCWD: r.ws.DefaultDisplay(), PathModel: config.PathModel,
		}
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
		contextResult.Rules = append(contextResult.Rules,
			"记忆启动索引只提供紧凑背景与资料入口；索引已给出具体 path 时优先 recall_read 该条目，只有索引未覆盖且任务依赖具体历史事实时才 recall_search，索引信息已足够时不要机械检索。",
		)
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
	Runtime           *capabilityRuntimeContext  `json:"runtime,omitempty"`
	Skills            []capabilitySkillItem      `json:"skills"`
	DynamicMCP        []capabilityDynamicMCPItem `json:"dynamic_mcp"`
	ACP               *capabilityACPContext      `json:"acp,omitempty"`
	WorkflowTemplates []capabilityTemplateItem   `json:"workflow_templates"`
	Recall            *capabilityRecallContext   `json:"recall,omitempty"`
	Rules             []string                   `json:"rules"`
	Warnings          []capabilityWarning        `json:"warnings,omitempty"`
}

type capabilityRuntimeContext struct {
	Version             string `json:"version"`
	OS                  string `json:"os"`
	Arch                string `json:"arch"`
	AgentDockHome       string `json:"agentdock_home"`
	AgentDockDefaultDir string `json:"agentdock_default_dir"`
	DefaultCWD          string `json:"default_cwd"`
	PathModel           string `json:"path_model"`
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

type capabilityRecallContextIndexResponse struct {
	ContextIndex capabilityRecallContextIndex `json:"context_index"`
}

type capabilityRecallContextIndex struct {
	Items     []capabilityRecallIndexItem `json:"items"`
	Truncated bool                        `json:"truncated"`
}

type capabilityRecallIndexItem struct {
	Kind     string   `json:"kind"`
	Path     string   `json:"path"`
	Title    string   `json:"title"`
	Summary  string   `json:"summary"`
	Keywords []string `json:"keywords"`
	Aliases  []string `json:"aliases"`
	Tags     []string `json:"tags"`
	CardType string   `json:"card_type"`
}

func (r *Runtime) dynamicMCPCapabilityIndex() []capabilityDynamicMCPItem {
	servers := r.dynamicMCP.CapabilityItems()
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
	result, err := r.taskTools.WorkflowManage(ctx, tooltask.WorkflowRequest{Action: "list", TemplateStatus: "active"})
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
	result, err := r.recall.ContextIndex(ctx, 3000)
	if err != nil {
		return []capabilityMemoryItem{}, err
	}
	var response capabilityRecallContextIndexResponse
	if err := remarshal(result, &response); err != nil {
		return []capabilityMemoryItem{}, err
	}
	items := make([]capabilityMemoryItem, 0, len(response.ContextIndex.Items))
	seen := make(map[string]struct{}, len(response.ContextIndex.Items))
	for _, item := range response.ContextIndex.Items {
		path := strings.TrimSpace(item.Path)
		if path == "" {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		items = append(items, capabilityMemoryItem{Name: path, Description: recallIndexDescription(item)})
	}
	return items, nil
}

func recallIndexDescription(item capabilityRecallIndexItem) string {
	if summary := strings.TrimSpace(item.Summary); summary != "" {
		if title := strings.TrimSpace(item.Title); title != "" {
			return truncateString(title+" — "+summary, 360)
		}
		return truncateString(summary, 360)
	}
	parts := []string{}
	if title := strings.TrimSpace(item.Title); title != "" {
		parts = append(parts, title)
	}
	if kind := strings.TrimSpace(item.Kind); kind != "" {
		parts = append(parts, kind)
	}
	if cardType := strings.TrimSpace(item.CardType); cardType != "" {
		parts = append(parts, cardType)
	}
	labels := append(append(append([]string{}, item.Keywords...), item.Aliases...), item.Tags...)
	if len(labels) > 0 {
		parts = append(parts, strings.Join(labels, ", "))
	}
	return truncateString(strings.Join(parts, " · "), 360)
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

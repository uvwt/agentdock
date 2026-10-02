package app

import (
	"context"
	"strings"

	"github.com/uvwt/agentdock/internal/buildinfo"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/observability"
	toolmcp "github.com/uvwt/agentdock/internal/tool/mcp"
	toolplugin "github.com/uvwt/agentdock/internal/tool/plugin"
)

const runtimeAPISource = "agentdock-api"

func (r *Runtime) RuntimeStatus() Result {
	tools := r.ToolNames()
	return Result{
		"ok":                    true,
		"source":                runtimeAPISource,
		"service":               config.ServerName,
		"version":               buildinfo.Version,
		"agentdock_home":        r.cfg.AgentDockHome,
		"agentdock_default_dir": r.cfg.AgentDockDefaultDir,
		"path_model":            config.PathModel,
		"auth_enabled":          r.cfg.AuthRequired(),
		"browser_enabled":       r.cfg.BrowserEnabled,
		"memory_enabled":        r.cfg.NexusEndpoint != "",
		"nexus_enabled":         strings.TrimSpace(r.cfg.NexusEndpoint) != "",
		"tool_count":            len(tools),
		"tools":                 tools,
	}
}

func (r *Runtime) RuntimeAnalytics() Result {
	snapshot := r.observer.Snapshot()
	return Result{
		"ok":              true,
		"source":          runtimeAPISource,
		"started_at":      snapshot.StartedAt,
		"recent_capacity": snapshot.RecentCapacity,
		"window_calls":    snapshot.WindowCalls,
		"total_calls":     snapshot.TotalCalls,
		"total_errors":    snapshot.TotalErrors,
		"active_calls":    snapshot.ActiveCalls,
		"tool_stats":      snapshot.ToolStats,
		"recent_calls":    snapshot.RecentCalls,
		"process":         snapshot.Process,
	}
}

// RuntimeDiagnostics 只暴露最近调用的零 Payload 投影，供 Nexus 按需远程排障。
// 本地 analytics 的进程指标与聚合统计不进入跨节点契约。
func (r *Runtime) RuntimeDiagnostics() Result {
	return Result{
		"ok":           true,
		"source":       runtimeAPISource,
		"recent_calls": observability.ProjectDiagnostics(r.observer.RecentCalls()),
	}
}

// RuntimeOverview 是 Nexus/NexusDock Cloud 的轻量控制面投影。
// 这里直接统计本地索引，不能调用 RuntimeSkills 等明细接口，否则一个概览请求
// 会重新触发 Skill 包校验、digest 和文件遍历，远程链路仍会被本地扫描拖慢。
func (r *Runtime) RuntimeOverview() (Result, error) {
	tasks, err := r.taskTools.RuntimeOverview()
	if err != nil {
		return nil, err
	}
	skillCount, err := r.skills.RuntimeCount()
	if err != nil {
		return nil, err
	}
	pluginCount, pluginErr := r.plugins.RuntimeCount()
	return Result{
		"ok":      true,
		"source":  runtimeAPISource,
		"tasks":   tasks,
		"skills":  map[string]any{"count": skillCount},
		"plugins": map[string]any{"count": pluginCount, "available": pluginErr == nil},
		"mcp":     map[string]any{"count": r.dynamicMCP.RuntimeCount()},
	}, nil
}

func (r *Runtime) RuntimeSkills() (Result, error) {
	return r.skills.RuntimeSkills()
}

func (r *Runtime) RuntimeSkill(skill string) (Result, error) {
	return r.skills.RuntimeSkill(skill)
}

func (r *Runtime) RuntimeSkillFiles(skill string) (Result, error) {
	return r.skills.RuntimeSkillFiles(skill)
}

func (r *Runtime) RuntimeSkillFile(skill, relativePath string) (Result, error) {
	return r.skills.RuntimeSkillFile(skill, relativePath)
}

func (r *Runtime) RuntimePlugins(ctx context.Context) (Result, error) {
	result, err := r.plugins.RuntimeList()
	if err != nil {
		return nil, err
	}
	result["ok"] = true
	result["source"] = runtimeAPISource
	return result, nil
}

func (r *Runtime) RuntimePlugin(ctx context.Context, name string) (Result, error) {
	return r.runtimePluginManage(ctx, map[string]any{"action": "inspect", "name": name})
}

// Runtime Plugin API 只暴露只读索引与 inspect；安装、更新、启停和删除仍由
// plugin_manage 的确认与事务语义负责，避免面向 UI 的接口形成第二套生命周期入口。
func (r *Runtime) runtimePluginManage(ctx context.Context, args map[string]any) (Result, error) {
	if err := r.validateToolArguments(toolplugin.ToolManage, args); err != nil {
		return nil, err
	}
	var request toolplugin.ManageRequest
	if err := decodeToolInput(toolplugin.ToolManage, args, &request); err != nil {
		return nil, err
	}
	result, err := r.plugins.Manage(ctx, request)
	if err != nil {
		return nil, err
	}
	result["ok"] = true
	result["source"] = runtimeAPISource
	return result, nil
}

func (r *Runtime) RuntimeTasks(status string, limit int) (Result, error) {
	return r.taskTools.RuntimeTasks(status, limit)
}

func (r *Runtime) RuntimeTask(id string) (Result, error) {
	return r.taskTools.RuntimeTask(id)
}

func (r *Runtime) RuntimeTaskDelete(id string) (Result, error) {
	return r.taskTools.RuntimeTaskDelete(id)
}

func (r *Runtime) RuntimeCapabilities(ctx context.Context, refresh bool) (Result, error) {
	result, err := r.AgentDockContext(ctx)
	if err != nil {
		return nil, err
	}
	result["source"] = runtimeAPISource
	return result, nil
}

func (r *Runtime) RuntimeMCPServers(ctx context.Context) (Result, error) {
	return r.runtimeMCPManage(ctx, map[string]any{"action": "list"})
}

func (r *Runtime) RuntimeMCPServer(ctx context.Context, name string) (Result, error) {
	return r.runtimeMCPManage(ctx, map[string]any{"action": "inspect", "name": name})
}

func (r *Runtime) RuntimeMCPManage(ctx context.Context, args map[string]any) (Result, error) {
	return r.runtimeMCPManage(ctx, args)
}

func (r *Runtime) runtimeMCPManage(ctx context.Context, args map[string]any) (Result, error) {
	if err := r.validateToolArguments(toolmcp.ToolManage, args); err != nil {
		return nil, err
	}
	var request toolmcp.ManageRequest
	if err := decodeToolInput("mcp_manage", args, &request); err != nil {
		return nil, err
	}
	result, err := r.dynamicMCP.Manage(ctx, request)
	if err != nil {
		return nil, err
	}
	result["ok"] = true
	result["source"] = runtimeAPISource
	return result, nil
}

package app

import (
	"context"
	"strings"

	"github.com/uvwt/agentdock/internal/config"
)

const runtimeAPISource = "agentdock-api"

func (r *Runtime) RuntimeStatus() Result {
	tools := r.ToolNames()
	return Result{
		"ok":                    true,
		"source":                runtimeAPISource,
		"service":               config.ServerName,
		"version":               config.Version,
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

func (r *Runtime) RuntimeSkills() (Result, error) {
	return r.skills.RuntimeSkills()
}

func (r *Runtime) RuntimeSkill(skill string) (Result, error) {
	return r.skills.RuntimeSkill(skill)
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
	result, err := r.dynamicMCP.Manage(ctx, args)
	if err != nil {
		return nil, err
	}
	result["ok"] = true
	result["source"] = runtimeAPISource
	return result, nil
}

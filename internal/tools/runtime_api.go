package tools

import (
	"context"
	"strings"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/skills"
	"github.com/uvwt/agentdock/internal/taskstate"
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
	result, err := r.skillList()
	if err != nil {
		return nil, err
	}
	items, _ := result["skills"].([]map[string]any)
	for _, item := range items {
		skill, _ := item["skill"].(string)
		version, _ := item["active_version"].(string)
		if strings.TrimSpace(skill) == "" || strings.TrimSpace(version) == "" {
			continue
		}
		packageDir, err := r.skills.state.InstalledPath(skill, version)
		if err != nil {
			return nil, skillToolError(err)
		}
		document, err := skills.LoadSkillDocument(packageDir)
		if err != nil {
			return nil, skillToolError(err)
		}
		files, err := collectRuntimeSkillFiles(packageDir)
		if err != nil {
			return nil, err
		}
		item["name"] = document.Name
		item["description"] = document.Description
		item["file_count"] = len(files)
	}
	result["source"] = runtimeAPISource
	return result, nil
}

func (r *Runtime) RuntimeSkill(skill string) (Result, error) {
	result, err := r.skillInspect(map[string]any{"skill": skill})
	if err != nil {
		return nil, err
	}
	result["source"] = runtimeAPISource
	result["files"] = []runtimeSkillFile{}
	result["file_count"] = 0

	version, _ := result["version"].(string)
	if strings.TrimSpace(version) == "" {
		return result, nil
	}
	packageDir, err := r.skills.state.InstalledPath(skill, version)
	if err != nil {
		return nil, skillToolError(err)
	}
	files, err := collectRuntimeSkillFiles(packageDir)
	if err != nil {
		return nil, err
	}
	result["files"] = files
	result["file_count"] = len(files)
	return result, nil
}

func (r *Runtime) RuntimeTasks(status string, limit int) (Result, error) {
	statusFilter := taskstate.Status(strings.ToLower(strings.TrimSpace(status)))
	if statusFilter != "" && statusFilter != taskstate.StatusActive && statusFilter != taskstate.StatusBlocked && statusFilter != taskstate.StatusCompleted {
		return nil, toolErrorDetails("INVALID_STATUS", "unsupported task status filter", "validation", map[string]any{"status": statusFilter, "allowed": []string{"active", "blocked", "completed"}})
	}
	if limit <= 0 {
		limit = 50
	}
	tasks, err := r.tasks.List(statusFilter, limit)
	if err != nil {
		return nil, taskToolError(err)
	}
	items := make([]map[string]any, 0, len(tasks))
	for _, task := range tasks {
		item := compactTaskListItem(task)
		item["created_at"] = task.CreatedAt
		item["event_count"] = len(task.Events)
		if task.CompletedAt != nil {
			item["completed_at"] = *task.CompletedAt
		}
		if len(task.SourceTemplates) > 0 {
			item["source_templates"] = task.SourceTemplates
		} else if task.Template != nil {
			// 旧任务仍可通过 Runtime API 查看原模板来源。
			item["source_templates"] = []taskstate.TemplateReference{{ID: task.Template.ID, Version: task.Template.Version, Hash: task.Template.Hash}}
		}
		items = append(items, item)
	}
	return Result{"ok": true, "source": runtimeAPISource, "action": "list", "tasks": items, "count": len(items)}, nil
}

func (r *Runtime) RuntimeTask(id string) (Result, error) {
	task, err := r.tasks.Get(strings.TrimSpace(id))
	if err != nil {
		return nil, taskToolError(err)
	}
	return Result{"ok": true, "source": runtimeAPISource, "action": "get", "task": task}, nil
}

func (r *Runtime) RuntimeTaskDelete(id string) (Result, error) {
	task, err := r.tasks.Delete(strings.TrimSpace(id))
	if err != nil {
		return nil, taskToolError(err)
	}
	return Result{
		"ok": true, "source": runtimeAPISource, "action": "delete",
		"task_id": task.ID, "deleted_task": compactTaskSummary(task),
	}, nil
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
	result, err := r.mcpManage(ctx, args)
	if err != nil {
		return nil, err
	}
	result["ok"] = true
	result["source"] = runtimeAPISource
	return result, nil
}

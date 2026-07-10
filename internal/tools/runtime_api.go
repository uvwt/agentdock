package tools

import (
	"context"
	"strings"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/taskstate"
)

const runtimeAPISource = "agentdock-runtime-api"

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
	result["source"] = runtimeAPISource
	return result, nil
}

func (r *Runtime) RuntimeSkill(skill string) (Result, error) {
	result, err := r.skillInspect(map[string]any{"skill": skill})
	if err != nil {
		return nil, err
	}
	result["source"] = runtimeAPISource
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
		item := map[string]any{
			"id":              task.ID,
			"title":           task.Title,
			"goal":            task.Goal,
			"status":          task.Status,
			"phase":           task.Phase,
			"blocker":         task.Blocker,
			"condition_count": len(task.Conditions),
			"step_count":      len(task.Steps),
			"event_count":     len(task.Events),
			"review_status":   reviewStatus(task),
			"created_at":      task.CreatedAt,
			"updated_at":      task.UpdatedAt,
		}
		if task.CompletedAt != nil {
			item["completed_at"] = *task.CompletedAt
		}
		if task.Template != nil {
			item["template_id"] = task.Template.ID
			item["template_version"] = task.Template.Version
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

func (r *Runtime) RuntimeEnv() (Result, error) {
	items, err := r.skills.env.List()
	if err != nil {
		return nil, toolErrorDetails("ENV_REGISTRY_FAILED", err.Error(), "runtime", nil)
	}
	return Result{"ok": true, "source": runtimeAPISource, "action": "list", "skills": items, "count": len(items)}, nil
}

func (r *Runtime) RuntimeCapabilities(ctx context.Context, refresh bool) (Result, error) {
	result, err := r.AgentDockContext(ctx)
	if err != nil {
		return nil, err
	}
	result["source"] = runtimeAPISource
	return result, nil
}

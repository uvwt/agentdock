package task

import (
	"strings"

	"github.com/uvwt/agentdock/internal/taskstate"
)

func (s *Service) RuntimeTasks(status string, limit int) (Result, error) {
	statusFilter := taskstate.Status(strings.ToLower(strings.TrimSpace(status)))
	if statusFilter != "" && statusFilter != taskstate.StatusActive && statusFilter != taskstate.StatusBlocked && statusFilter != taskstate.StatusCompleted {
		return nil, toolErrorDetails("INVALID_STATUS", "unsupported task status filter", "validation", map[string]any{"status": statusFilter, "allowed": []string{"active", "blocked", "completed"}})
	}
	if limit <= 0 {
		limit = 50
	}
	tasks, err := s.tasks.List(statusFilter, limit)
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
	return Result{"ok": true, "source": "agentdock-api", "action": "list", "tasks": items, "count": len(items)}, nil
}

func (s *Service) RuntimeTask(id string) (Result, error) {
	task, err := s.tasks.Get(strings.TrimSpace(id))
	if err != nil {
		return nil, taskToolError(err)
	}
	return Result{"ok": true, "source": "agentdock-api", "action": "get", "task": task}, nil
}

func (s *Service) RuntimeTaskDelete(id string) (Result, error) {
	task, err := s.tasks.Delete(strings.TrimSpace(id))
	if err != nil {
		return nil, taskToolError(err)
	}
	return Result{
		"ok": true, "source": "agentdock-api", "action": "delete",
		"task_id": task.ID, "deleted_task": compactTaskSummary(task),
	}, nil
}

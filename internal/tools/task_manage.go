package tools

import (
	"strings"

	"github.com/uvwt/agentdock/internal/taskstate"
)

var taskActions = []string{
	"create", "list", "get", "add_condition", "add_evidence", "advance",
	"record_attempt", "block", "resume", "complete",
}

func (r *Runtime) taskManage(args map[string]any) (Result, error) {
	action := strings.ToLower(strings.TrimSpace(stringArg(args, "action", "")))
	var (
		task taskstate.Task
		err  error
	)
	switch action {
	case "create":
		task, err = r.tasks.Create(
			stringArg(args, "title", ""),
			stringArg(args, "goal", ""),
			stringSliceArg(args, "completion_conditions"),
		)
	case "list":
		status := taskstate.Status(strings.ToLower(strings.TrimSpace(stringArg(args, "status", ""))))
		if status != "" && status != taskstate.StatusActive && status != taskstate.StatusBlocked && status != taskstate.StatusCompleted {
			return nil, toolErrorDetails("INVALID_STATUS", "unsupported task status filter", "validation", map[string]any{
				"status": status, "allowed": []string{"active", "blocked", "completed"},
			})
		}
		tasks, listErr := r.tasks.List(status, intArg(args, "limit", 50))
		if listErr != nil {
			return nil, taskToolError(listErr)
		}
		items := make([]map[string]any, 0, len(tasks))
		for _, item := range tasks {
			verified := 0
			for _, condition := range item.Conditions {
				if len(condition.Evidence) > 0 {
					verified++
				}
			}
			items = append(items, map[string]any{
				"id": item.ID, "title": item.Title, "goal": item.Goal,
				"status": item.Status, "phase": item.Phase, "blocker": item.Blocker,
				"condition_count": len(item.Conditions), "verified_condition_count": verified,
				"attempt_count": len(item.Attempts), "updated_at": item.UpdatedAt,
			})
		}
		return Result{"ok": true, "action": action, "tasks": items, "count": len(items), "state_dir": r.tasks.Root()}, nil
	case "get":
		task, err = r.tasks.Get(stringArg(args, "task_id", ""))
	case "add_condition":
		task, err = r.tasks.AddCondition(stringArg(args, "task_id", ""), stringArg(args, "condition", ""))
	case "add_evidence":
		task, err = r.tasks.AddEvidence(
			stringArg(args, "task_id", ""),
			stringArg(args, "condition_id", ""),
			stringArg(args, "summary", ""),
			stringArg(args, "source", ""),
		)
	case "advance":
		task, err = r.tasks.Advance(stringArg(args, "task_id", ""))
	case "record_attempt":
		task, err = r.tasks.RecordAttempt(
			stringArg(args, "task_id", ""),
			stringArg(args, "strategy", ""),
			stringArg(args, "outcome", ""),
			stringArg(args, "diagnosis", ""),
			stringArg(args, "evidence", ""),
		)
	case "block":
		task, err = r.tasks.Block(
			stringArg(args, "task_id", ""),
			stringArg(args, "blocker", ""),
			stringArg(args, "evidence", ""),
		)
	case "resume":
		task, err = r.tasks.Resume(stringArg(args, "task_id", ""), stringArg(args, "summary", ""))
	case "complete":
		task, err = r.tasks.Complete(stringArg(args, "task_id", ""), stringArg(args, "summary", ""))
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported task_manage action", "validation", map[string]any{
			"action": action, "allowed": taskActions,
		})
	}
	if err != nil {
		return nil, taskToolError(err)
	}
	return Result{"ok": true, "action": action, "task": task, "state_dir": r.tasks.Root()}, nil
}

func taskToolError(err error) error {
	return toolErrorDetails("TASK_STATE_ERROR", err.Error(), "validation", map[string]any{"retryable": false})
}

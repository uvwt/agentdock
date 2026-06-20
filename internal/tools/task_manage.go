package tools

import (
	"encoding/json"
	"strings"

	"github.com/uvwt/agentdock/internal/taskstate"
)

var taskActions = []string{
	"create", "list", "get", "add_condition", "add_evidence", "advance", "phase_checkpoint", "complete_step", "skip_step",
	"record_attempt", "block", "resume", "complete", "template_save", "template_validate", "template_publish",
	"template_retire", "template_list", "template_get", "template_match",
}

func (r *Runtime) taskManage(args map[string]any) (Result, error) {
	action := strings.ToLower(strings.TrimSpace(stringArg(args, "action", "")))
	var (
		task taskstate.Task
		err  error
	)
	switch action {
	case "create":
		var candidates []taskstate.TemplateCandidate
		if raw := args["template_candidates"]; raw != nil {
			_ = remarshal(raw, &candidates)
		}
		task, err = r.tasks.CreateWithTemplate(stringArg(args, "title", ""), stringArg(args, "goal", ""), stringSliceArg(args, "completion_conditions"), stringArg(args, "template_id", ""), stringArg(args, "template_version", ""), stringArg(args, "selected_reason", ""), candidates)
		if err != nil {
			return nil, taskToolError(err)
		}
		return Result{
			"ok": true, "action": action, "task_id": task.ID, "task_summary": compactTaskSummary(task), "state_dir": r.tasks.Root(),
			"next_required_action": "Use phase_checkpoint after real work, or call get only when the full task snapshot is needed",
		}, nil
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
	case "phase_checkpoint":
		input := taskstate.PhaseCheckpointInput{
			AdvancePhase: boolArg(args, "advance_phase", false),
			CompleteTask: boolArg(args, "complete_task", false),
			Summary:      stringArg(args, "summary", ""),
		}
		if raw := args["step_completions"]; raw != nil {
			if err := remarshal(raw, &input.StepCompletions); err != nil {
				return nil, taskToolError(err)
			}
		}
		if raw := args["condition_evidence"]; raw != nil {
			if err := remarshal(raw, &input.ConditionEvidence); err != nil {
				return nil, taskToolError(err)
			}
		}
		task, err = r.tasks.PhaseCheckpoint(stringArg(args, "task_id", ""), input)
		if err != nil {
			return nil, taskToolError(err)
		}
		return Result{"ok": true, "action": action, "task_summary": compactTaskSummary(task), "state_dir": r.tasks.Root()}, nil
	case "complete_step":
		var evidence taskstate.StepEvidence
		if raw := args["step_evidence"]; raw != nil {
			if err := remarshal(raw, &evidence); err != nil {
				return nil, taskToolError(err)
			}
		}
		if strings.TrimSpace(evidence.Summary) == "" {
			evidence.Summary = stringArg(args, "summary", "")
		}
		task, err = r.tasks.CompleteStep(stringArg(args, "task_id", ""), stringArg(args, "step_id", ""), evidence, boolArg(args, "substituted", false), stringArg(args, "substitution_reason", ""))
	case "skip_step":
		task, err = r.tasks.SkipStep(stringArg(args, "task_id", ""), stringArg(args, "step_id", ""), stringArg(args, "summary", ""))
	case "record_attempt":
		task, err = r.tasks.RecordAttempt(
			stringArg(args, "task_id", ""),
			stringArg(args, "strategy", ""),
			stringArg(args, "outcome", ""),
			stringArg(args, "diagnosis", ""),
			stringArg(args, "evidence", ""),
		)
		if err != nil {
			return nil, taskToolError(err)
		}
		return Result{
			"ok": true, "action": action, "task_summary": compactTaskSummary(task), "state_dir": r.tasks.Root(),
			"warning":              "record_attempt only records an attempt; it does not execute commands, change configuration, or advance the task",
			"next_required_action": "Call a non-task tool for a real environment action, then record a concise checkpoint or final verification summary",
		}, nil
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
	case "template_save":
		var template taskstate.Template
		if err := remarshal(mapArg(args, "template"), &template); err != nil {
			return nil, taskToolError(err)
		}
		applyTemplateGuardrailArgs(args, &template)
		template, err = r.tasks.SaveTemplateDraft(template)
		if err == nil {
			return Result{"ok": true, "action": action, "template": template, "workflow_dir": r.tasks.WorkflowRoot()}, nil
		}
	case "template_validate":
		template, validateErr := r.tasks.ValidateTemplate(stringArg(args, "template_id", ""), stringArg(args, "template_version", ""))
		if validateErr != nil {
			return nil, taskToolError(validateErr)
		}
		return Result{"ok": true, "action": action, "template": template, "valid": true, "workflow_dir": r.tasks.WorkflowRoot()}, nil
	case "template_publish":
		template, publishErr := r.tasks.PublishTemplate(stringArg(args, "template_id", ""), stringArg(args, "template_version", ""))
		if publishErr != nil {
			return nil, taskToolError(publishErr)
		}
		return Result{"ok": true, "action": action, "template": template, "workflow_dir": r.tasks.WorkflowRoot()}, nil
	case "template_retire":
		template, retireErr := r.tasks.RetireTemplate(stringArg(args, "template_id", ""), stringArg(args, "template_version", ""))
		if retireErr != nil {
			return nil, taskToolError(retireErr)
		}
		return Result{"ok": true, "action": action, "template": template, "workflow_dir": r.tasks.WorkflowRoot()}, nil
	case "template_get":
		template, getErr := r.tasks.GetTemplate(stringArg(args, "template_id", ""), stringArg(args, "template_version", ""))
		if getErr != nil {
			return nil, taskToolError(getErr)
		}
		return Result{"ok": true, "action": action, "template": template, "workflow_dir": r.tasks.WorkflowRoot()}, nil
	case "template_list":
		templates, listErr := r.tasks.ListTemplates(taskstate.TemplateStatus(stringArg(args, "template_status", "")))
		if listErr != nil {
			return nil, taskToolError(listErr)
		}
		return Result{"ok": true, "action": action, "templates": templates, "count": len(templates), "workflow_dir": r.tasks.WorkflowRoot()}, nil
	case "template_match":
		candidates, matchErr := r.tasks.MatchTemplates(stringArg(args, "goal", ""), stringArg(args, "device", ""), stringArg(args, "task_type", ""))
		if matchErr != nil {
			return nil, taskToolError(matchErr)
		}
		return Result{"ok": true, "action": action, "candidates": candidates, "count": len(candidates), "workflow_dir": r.tasks.WorkflowRoot()}, nil
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

func compactTaskSummary(task taskstate.Task) map[string]any {
	completedSteps := 0
	for _, step := range task.Steps {
		if step.Status == "completed" || step.Status == "skipped" {
			completedSteps++
		}
	}
	verifiedConditions := 0
	for _, condition := range task.Conditions {
		if len(condition.Evidence) > 0 {
			verifiedConditions++
		}
	}
	summary := map[string]any{
		"id": task.ID, "title": task.Title, "status": task.Status, "phase": task.Phase,
		"completed_step_count": completedSteps, "step_count": len(task.Steps),
		"verified_condition_count": verifiedConditions, "condition_count": len(task.Conditions),
		"updated_at": task.UpdatedAt,
	}
	if task.CompletedAt != nil {
		summary["completed_at"] = *task.CompletedAt
	}
	return summary
}

func taskToolError(err error) error {
	return toolErrorDetails("TASK_STATE_ERROR", err.Error(), "validation", map[string]any{"retryable": false})
}

func remarshal(input any, out any) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func applyTemplateGuardrailArgs(args map[string]any, template *taskstate.Template) {
	if _, ok := args["allow_long_template"]; ok {
		template.AllowLongTemplate = boolArg(args, "allow_long_template", false)
	}
	if reason := strings.TrimSpace(stringArg(args, "long_template_reason", "")); reason != "" {
		template.LongTemplateReason = reason
	}
}

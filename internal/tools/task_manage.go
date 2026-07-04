package tools

import (
	"encoding/json"
	"strings"

	"github.com/uvwt/agentdock/internal/taskstate"
)

var taskActions = []string{
	"create", "list", "get", "add_condition", "add_evidence", "advance", "phase_checkpoint", "complete_step", "skip_step",
	"record_attempt", "block", "resume", "complete", "final_review", "complete_after_review", "template_save", "template_validate", "template_publish",
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
			"next_required_action": "Do real work with non-task tools. Use block or record_attempt only for failures. When work appears complete, call final_review once, then complete_after_review if it passes.",
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
			items = append(items, map[string]any{
				"id": item.ID, "title": item.Title, "goal": item.Goal,
				"status": item.Status, "phase": item.Phase, "blocker": item.Blocker,
				"condition_count": len(item.Conditions), "review_status": reviewStatus(item),
				"attempt_count": len(item.Attempts), "updated_at": item.UpdatedAt,
			})
		}
		return Result{"ok": true, "action": action, "tasks": items, "count": len(items), "state_dir": r.tasks.Root()}, nil
	case "get":
		task, err = r.tasks.Get(stringArg(args, "task_id", ""))
		if err != nil {
			return nil, taskToolError(err)
		}
		return Result{"ok": true, "action": action, "task": task, "state_dir": r.tasks.Root()}, nil
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
			"warning":              "record_attempt is for failed attempts only; it does not execute commands, change configuration, or advance the task",
			"next_required_action": "Call a non-task tool for a real environment action. When the work is complete, use final_review instead of step evidence.",
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
	case "final_review":
		input := taskstate.FinalReviewInput{
			Status:        stringArg(args, "review_status", stringArg(args, "status", "pass")),
			Summary:       stringArg(args, "summary", ""),
			VerifiedFacts: stringSliceArg(args, "verified_facts"),
			OpenRisks:     stringSliceArg(args, "open_risks"),
			MissingChecks: stringSliceArg(args, "missing_checks"),
		}
		task, err = r.tasks.FinalReview(stringArg(args, "task_id", ""), input)
	case "complete_after_review":
		task, err = r.tasks.CompleteAfterReview(stringArg(args, "task_id", ""), stringArg(args, "summary", ""))
	case "template_save":
		var template taskstate.Template
		if err := remarshal(mapArg(args, "template"), &template); err != nil {
			return nil, taskToolError(err)
		}
		applyTemplateGuardrailArgs(args, &template)
		template, err = r.tasks.SaveTemplateDraft(template)
		if err == nil {
			return Result{"ok": true, "action": action, "template_id": template.ID, "template_summary": compactTemplateSummary(template), "workflow_dir": r.tasks.WorkflowRoot()}, nil
		}
	case "template_validate":
		template, validateErr := r.tasks.ValidateTemplate(stringArg(args, "template_id", ""), stringArg(args, "template_version", ""))
		if validateErr != nil {
			return nil, taskToolError(validateErr)
		}
		return Result{"ok": true, "action": action, "template_id": template.ID, "template_summary": compactTemplateSummary(template), "valid": true, "workflow_dir": r.tasks.WorkflowRoot()}, nil
	case "template_publish":
		template, publishErr := r.tasks.PublishTemplate(stringArg(args, "template_id", ""), stringArg(args, "template_version", ""))
		if publishErr != nil {
			return nil, taskToolError(publishErr)
		}
		return Result{"ok": true, "action": action, "template_id": template.ID, "template_summary": compactTemplateSummary(template), "workflow_dir": r.tasks.WorkflowRoot()}, nil
	case "template_retire":
		template, retireErr := r.tasks.RetireTemplate(stringArg(args, "template_id", ""), stringArg(args, "template_version", ""))
		if retireErr != nil {
			return nil, taskToolError(retireErr)
		}
		return Result{"ok": true, "action": action, "template_id": template.ID, "template_summary": compactTemplateSummary(template), "workflow_dir": r.tasks.WorkflowRoot()}, nil
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
		items := make([]map[string]any, 0, len(templates))
		for _, template := range templates {
			items = append(items, compactTemplateSummary(template))
		}
		return Result{"ok": true, "action": action, "templates": items, "count": len(items), "workflow_dir": r.tasks.WorkflowRoot()}, nil
	case "template_match":
		candidates, matchErr := r.tasks.MatchTemplates(stringArg(args, "goal", ""), stringArg(args, "device", ""), stringArg(args, "task_type", ""))
		if matchErr != nil {
			return nil, taskToolError(matchErr)
		}
		vectorIndexStatus, vectorIndexItems, embeddingModel := r.tasks.VectorIndexInfo()
		return Result{
			"ok":                    true,
			"action":                action,
			"candidates":            candidates,
			"count":                 len(candidates),
			"workflow_dir":          r.tasks.WorkflowRoot(),
			"vector_search_enabled": r.tasks.VectorSearchEnabled(),
			"vector_index_status":   vectorIndexStatus,
			"vector_index_items":    vectorIndexItems,
			"embedding_model":       embeddingModel,
		}, nil
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported task_manage action", "validation", map[string]any{
			"action": action, "allowed": taskActions,
		})
	}
	if err != nil {
		return nil, taskToolError(err)
	}
	return Result{"ok": true, "action": action, "task_id": task.ID, "task_summary": compactTaskSummary(task), "state_dir": r.tasks.Root()}, nil
}

func compactTaskSummary(task taskstate.Task) map[string]any {
	completedSteps := 0
	currentPhaseSteps := []map[string]any{}
	for _, step := range task.Steps {
		if step.Status == "completed" || step.Status == "skipped" {
			completedSteps++
		}
		if step.Phase == task.Phase && step.Status == "pending" {
			currentPhaseSteps = append(currentPhaseSteps, map[string]any{
				"id":       step.ID,
				"title":    truncateString(step.Title, 120),
				"required": step.Required,
			})
		}
	}
	conditionRefs := make([]map[string]any, 0, len(task.Conditions))
	for _, condition := range task.Conditions {
		conditionRefs = append(conditionRefs, map[string]any{
			"id":   condition.ID,
			"text": truncateString(condition.Text, 160),
		})
	}
	summary := map[string]any{
		"id": task.ID, "title": task.Title, "status": task.Status, "phase": task.Phase,
		"completed_step_count": completedSteps, "step_count": len(task.Steps),
		"condition_count": len(task.Conditions), "condition_refs": conditionRefs, "review_status": reviewStatus(task),
		"current_phase_steps": currentPhaseSteps, "updated_at": task.UpdatedAt,
	}
	if task.FinalReview != nil {
		summary["final_review"] = map[string]any{
			"status":              task.FinalReview.Status,
			"summary":             truncateString(task.FinalReview.Summary, 200),
			"verified_fact_count": len(task.FinalReview.VerifiedFacts),
			"open_risk_count":     len(task.FinalReview.OpenRisks),
			"missing_check_count": len(task.FinalReview.MissingChecks),
			"reviewed_at":         task.FinalReview.ReviewedAt,
		}
	}
	if task.CompletedAt != nil {
		summary["completed_at"] = *task.CompletedAt
	}
	return summary
}

func reviewStatus(task taskstate.Task) string {
	if task.FinalReview == nil {
		return "not_started"
	}
	return task.FinalReview.Status
}

func compactTemplateSummary(template taskstate.Template) map[string]any {
	return map[string]any{
		"id":                   template.ID,
		"version":              template.Version,
		"title":                truncateString(template.Title, 120),
		"status":               template.Status,
		"keyword_count":        len(template.Match.Keywords),
		"device_count":         len(template.Match.Devices),
		"task_type_count":      len(template.Match.TaskTypes),
		"priority":             template.Match.Priority,
		"condition_count":      len(template.CompletionConditions),
		"step_count":           len(template.Steps),
		"allow_long_template":  template.AllowLongTemplate,
		"long_template_reason": truncateString(template.LongTemplateReason, 160),
		"hash":                 template.Hash,
		"published_at":         template.PublishedAt,
		"retired_at":           template.RetiredAt,
	}
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

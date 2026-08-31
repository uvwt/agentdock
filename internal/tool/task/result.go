package task

import (
	"encoding/json"
	"errors"

	"github.com/uvwt/agentdock/internal/taskstate"
)

func normalizeJSONToolValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			typed[key] = normalizeJSONToolValue(child)
		}
		return typed
	case []any:
		for i, child := range typed {
			typed[i] = normalizeJSONToolValue(child)
		}
		return typed
	case float64:
		if typed == float64(int(typed)) {
			return int(typed)
		}
		return typed
	default:
		return value
	}
}

func compactTaskSummary(task taskstate.Task) map[string]any {
	completedSteps := 0
	steps := make([]map[string]any, 0, len(task.Steps))
	var currentStep map[string]any
	for _, step := range task.Steps {
		if step.Status == taskstate.StepCompleted {
			completedSteps++
		}
		item := map[string]any{"id": step.ID, "title": truncateString(step.Title, 120), "status": step.Status}
		steps = append(steps, item)
		if currentStep == nil && step.Status == taskstate.StepInProgress {
			currentStep = item
		}
	}
	if currentStep == nil {
		for _, item := range steps {
			if item["status"] == taskstate.StepPending {
				currentStep = item
				break
			}
		}
	}
	conditionRefs := make([]map[string]any, 0, len(task.Conditions))
	for _, condition := range task.Conditions {
		conditionRefs = append(conditionRefs, map[string]any{"id": condition.ID, "text": truncateString(condition.Text, 160)})
	}
	summary := map[string]any{
		"id": task.ID, "title": task.Title, "status": task.Status, "phase": task.Phase,
		"completed_step_count": completedSteps, "step_count": len(task.Steps), "steps": steps,
		"condition_count": len(task.Conditions), "condition_refs": conditionRefs, "review_status": reviewStatus(task),
		"updated_at": task.UpdatedAt,
	}
	if currentStep != nil {
		summary["current_step"] = currentStep
	}
	if task.Summary != "" {
		summary["summary"] = truncateString(task.Summary, 240)
	}
	if len(task.SourceTemplates) > 0 {
		summary["source_templates"] = task.SourceTemplates
	}
	if task.Blocker != "" {
		summary["blocker"] = truncateString(task.Blocker, 240)
	}
	if task.FinalReview != nil {
		summary["final_review"] = map[string]any{
			"status": task.FinalReview.Status, "summary": truncateString(task.FinalReview.Summary, 200),
			"verified_count": len(task.FinalReview.VerifiedFacts), "risk_count": len(task.FinalReview.OpenRisks), "reviewed_at": task.FinalReview.ReviewedAt,
		}
	}
	if task.CompletedAt != nil {
		summary["completed_at"] = *task.CompletedAt
	}
	return summary
}

func compactTaskListItem(task taskstate.Task) map[string]any {
	summary := compactTaskSummary(task)
	item := map[string]any{
		"id": summary["id"], "title": summary["title"], "goal": task.Goal, "status": summary["status"], "phase": summary["phase"],
		"completed_step_count": summary["completed_step_count"], "step_count": summary["step_count"],
		"review_status": summary["review_status"], "updated_at": summary["updated_at"],
	}
	for _, key := range []string{"current_step", "summary", "blocker"} {
		if value, ok := summary[key]; ok {
			item[key] = value
		}
	}
	return item
}

func reviewStatus(task taskstate.Task) string {
	if task.FinalReview == nil {
		return "not_started"
	}
	return task.FinalReview.Status
}

func templateMatchRecommendation(candidates []taskstate.TemplateCandidate) map[string]any {
	bestScore := 0
	if len(candidates) > 0 {
		bestScore = candidates[0].Score
	}
	recommended := "plain_task"
	reason := "no active template is specific enough; create a plain recoverable task"
	if bestScore >= 85 {
		recommended = "use_template"
		reason = "top candidate score is strong enough to select by default"
	} else if bestScore >= 60 {
		recommended = "consider_template"
		reason = "top candidate is plausible but should be checked against the user goal"
	}
	return map[string]any{
		"recommended":           recommended,
		"recommendation_reason": reason,
		"best_candidate_score":  bestScore,
		"score_thresholds": map[string]any{
			"use_template":      85,
			"consider_template": 60,
			"plain_task_below":  60,
		},
	}
}
func compactTemplateSummary(template taskstate.Template) map[string]any {
	return map[string]any{
		"id":                   template.ID,
		"version":              template.Version,
		"title":                truncateString(template.Title, 120),
		"status":               template.Status,
		"keyword_count":        len(template.Match.Keywords),
		"device_count":         len(template.Match.Devices),
		"type":                 template.Match.Type,
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
	if errors.Is(err, taskstate.ErrTaskNotFound) {
		return toolErrorDetails("TASK_NOT_FOUND", err.Error(), "not_found", map[string]any{"retryable": false})
	}
	return toolErrorDetails("TASK_STATE_ERROR", err.Error(), "validation", map[string]any{"retryable": false})
}

func remarshal(input any, out any) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

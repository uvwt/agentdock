package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/taskstate"
)

var taskActions = []string{"create", "list", "get", "block", "resume", "final_review", "complete_after_review"}

var workflowTemplateActions = []string{"save", "validate", "publish", "retire", "list", "get", "match", "vector_index"}

type taskManageInput struct {
	Action               string
	Title                string
	Goal                 string
	CompletionConditions []string
	TemplateID           string
	TemplateVersion      string
	SelectedReason       string
	TemplateCandidates   []taskstate.TemplateCandidate
	Status               taskstate.Status
	Limit                int
	TaskID               string
	Blocker              string
	Evidence             string
	Summary              string
	ReviewStatus         string
	VerifiedFacts        []string
	OpenRisks            []string
	MissingChecks        []string
}

type workflowTemplateInput struct {
	Action               string
	TemplateID           string
	TemplateVersion      string
	TemplateStatus       string
	Goal                 string
	Device               string
	Type                 string
	Template             taskstate.Template
	AllowLongTemplateSet bool
	AllowLongTemplate    bool
	LongTemplateReason   string
}

type workflowTemplateDraftRequest struct {
	Template taskstate.Template `json:"template"`
}

type workflowTemplateMatchRequest struct {
	Goal   string `json:"goal"`
	Device string `json:"device"`
	Type   string `json:"type"`
}

func parseTaskManageInput(args map[string]any) taskManageInput {
	input := taskManageInput{
		Action:               strings.ToLower(strings.TrimSpace(stringArg(args, "action", ""))),
		Title:                stringArg(args, "title", ""),
		Goal:                 stringArg(args, "goal", ""),
		CompletionConditions: stringSliceArg(args, "completion_conditions"),
		TemplateID:           strings.TrimSpace(stringArg(args, "template_id", "")),
		TemplateVersion:      strings.TrimSpace(stringArg(args, "template_version", "")),
		SelectedReason:       stringArg(args, "selected_reason", ""),
		Status:               taskstate.Status(strings.ToLower(strings.TrimSpace(stringArg(args, "status", "")))),
		Limit:                intArg(args, "limit", 50),
		TaskID:               stringArg(args, "task_id", ""),
		Blocker:              stringArg(args, "blocker", ""),
		Evidence:             stringArg(args, "evidence", ""),
		Summary:              stringArg(args, "summary", ""),
		ReviewStatus:         stringArg(args, "review_status", stringArg(args, "status", "pass")),
		VerifiedFacts:        stringSliceArg(args, "verified_facts"),
		OpenRisks:            stringSliceArg(args, "open_risks"),
		MissingChecks:        stringSliceArg(args, "missing_checks"),
	}
	if raw := args["template_candidates"]; raw != nil {
		_ = remarshal(raw, &input.TemplateCandidates)
	}
	return input
}

func parseWorkflowTemplateInput(args map[string]any) (workflowTemplateInput, error) {
	input := workflowTemplateInput{
		Action:             strings.ToLower(strings.TrimSpace(stringArg(args, "action", ""))),
		TemplateID:         strings.TrimSpace(stringArg(args, "template_id", "")),
		TemplateVersion:    strings.TrimSpace(stringArg(args, "template_version", "")),
		TemplateStatus:     strings.TrimSpace(stringArg(args, "template_status", "")),
		Goal:               stringArg(args, "goal", ""),
		Device:             stringArg(args, "device", ""),
		Type:               stringArg(args, "type", ""),
		LongTemplateReason: strings.TrimSpace(stringArg(args, "long_template_reason", "")),
	}
	if _, ok := args["allow_long_template"]; ok {
		input.AllowLongTemplateSet = true
		input.AllowLongTemplate = boolArg(args, "allow_long_template", false)
	}
	if input.Action == "save" {
		if err := remarshal(mapArg(args, "template"), &input.Template); err != nil {
			return input, taskToolError(err)
		}
		input.applyTemplateGuardrails()
	}
	return input, nil
}

func (input workflowTemplateInput) escapedTemplatePath(action string) string {
	id := url.PathEscape(input.TemplateID)
	version := url.PathEscape(input.TemplateVersion)
	if action == "" {
		return fmt.Sprintf("/v1/workflow-templates/%s/%s", id, version)
	}
	return fmt.Sprintf("/v1/workflow-templates/%s/%s/%s", id, version, action)
}

func (input *workflowTemplateInput) applyTemplateGuardrails() {
	if input.AllowLongTemplateSet {
		input.Template.AllowLongTemplate = input.AllowLongTemplate
	}
	if input.LongTemplateReason != "" {
		input.Template.LongTemplateReason = input.LongTemplateReason
	}
}

func (r *Runtime) taskManage(ctx context.Context, args map[string]any) (Result, error) {
	input := parseTaskManageInput(args)
	var (
		task taskstate.Task
		err  error
	)
	switch input.Action {
	case "create":
		if input.TemplateID != "" {
			template, fetchErr := r.nexusWorkflowTemplate(ctx, input.TemplateID, input.TemplateVersion)
			if fetchErr != nil {
				return nil, fetchErr
			}
			task, err = r.tasks.CreateFromTemplate(input.Title, input.Goal, input.CompletionConditions, template, input.SelectedReason, input.TemplateCandidates)
		} else {
			task, err = r.tasks.Create(input.Title, input.Goal, input.CompletionConditions)
		}
		if err != nil {
			return nil, taskToolError(err)
		}
		return Result{
			"ok": true, "action": input.Action, "task_id": task.ID, "task_summary": compactTaskSummary(task), "state_dir": r.tasks.Root(),
			"next_required_action": "Do real work with non-task tools. Use block only for blockers. When work appears complete, call final_review once, then complete_after_review if it passes.",
		}, nil
	case "list":
		if input.Status != "" && input.Status != taskstate.StatusActive && input.Status != taskstate.StatusBlocked && input.Status != taskstate.StatusCompleted {
			return nil, toolErrorDetails("INVALID_STATUS", "unsupported task status filter", "validation", map[string]any{
				"status": input.Status, "allowed": []string{"active", "blocked", "completed"},
			})
		}
		tasks, listErr := r.tasks.List(input.Status, input.Limit)
		if listErr != nil {
			return nil, taskToolError(listErr)
		}
		items := make([]map[string]any, 0, len(tasks))
		for _, item := range tasks {
			items = append(items, map[string]any{
				"id": item.ID, "title": item.Title, "goal": item.Goal,
				"status": item.Status, "phase": item.Phase, "blocker": item.Blocker,
				"condition_count": len(item.Conditions), "review_status": reviewStatus(item),
			})
		}
		return Result{"ok": true, "action": input.Action, "tasks": items, "count": len(items), "state_dir": r.tasks.Root()}, nil
	case "get":
		task, err = r.tasks.Get(input.TaskID)
		if err != nil {
			return nil, taskToolError(err)
		}
		return Result{"ok": true, "action": input.Action, "task": task, "state_dir": r.tasks.Root()}, nil
	case "block":
		task, err = r.tasks.Block(input.TaskID, input.Blocker, input.Evidence)
	case "resume":
		task, err = r.tasks.Resume(input.TaskID, input.Summary)
	case "final_review":
		review := taskstate.FinalReviewInput{
			Status:        input.ReviewStatus,
			Summary:       input.Summary,
			VerifiedFacts: input.VerifiedFacts,
			OpenRisks:     input.OpenRisks,
			MissingChecks: input.MissingChecks,
		}
		task, err = r.tasks.FinalReview(input.TaskID, review)
	case "complete_after_review":
		task, err = r.tasks.CompleteAfterReview(input.TaskID, input.Summary)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported task_manage action", "validation", map[string]any{
			"action": input.Action, "allowed": taskActions,
		})
	}
	if err != nil {
		return nil, taskToolError(err)
	}
	return Result{"ok": true, "action": input.Action, "task_id": task.ID, "task_summary": compactTaskSummary(task), "state_dir": r.tasks.Root()}, nil
}

func (r *Runtime) workflowTemplateManage(ctx context.Context, args map[string]any) (Result, error) {
	input, err := parseWorkflowTemplateInput(args)
	if err != nil {
		return nil, err
	}
	switch input.Action {
	case "save":
		request := workflowTemplateDraftRequest{Template: input.Template}
		return compactNexusTemplateMutationResult(r.nexusWorkflowJSON(ctx, "POST", "/v1/workflow-templates/drafts", request))
	case "validate", "publish", "retire":
		return compactNexusTemplateMutationResult(r.nexusWorkflowJSON(ctx, "POST", input.escapedTemplatePath(input.Action), struct{}{}))
	case "get":
		result, err := r.nexusWorkflowJSON(ctx, "GET", input.escapedTemplatePath(""), nil)
		if err != nil {
			return nil, err
		}
		var template taskstate.Template
		if err := remarshal(result["template"], &template); err == nil {
			result["template"] = template
		}
		return result, nil
	case "list":
		path := "/v1/workflow-templates"
		if input.TemplateStatus != "" {
			path += "?status=" + url.QueryEscape(input.TemplateStatus)
		}
		result, err := r.nexusWorkflowJSON(ctx, "GET", path, nil)
		if err != nil {
			return nil, err
		}
		if items, ok := result["items"]; ok {
			var summaries []map[string]any
			if err := remarshal(items, &summaries); err == nil {
				for i := range summaries {
					for key, value := range summaries[i] {
						summaries[i][key] = normalizeJSONToolValue(value)
					}
				}
				result["templates"] = summaries
			} else {
				result["templates"] = items
			}
		}
		return result, nil
	case "match":
		return r.matchWorkflowTemplates(ctx, input)
	case "vector_index":
		return r.nexusWorkflowJSON(ctx, "GET", "/v1/workflow-templates/vector-index", nil)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported workflow_template_manage action", "validation", map[string]any{"action": input.Action, "allowed": workflowTemplateActions})
	}
}

func compactNexusTemplateMutationResult(result Result, err error) (Result, error) {
	if err != nil {
		return nil, err
	}
	delete(result, "template")
	return result, nil
}

func (r *Runtime) matchWorkflowTemplates(ctx context.Context, input workflowTemplateInput) (Result, error) {
	request := workflowTemplateMatchRequest{Goal: input.Goal, Device: input.Device, Type: input.Type}
	return r.nexusWorkflowJSON(ctx, "POST", "/v1/workflow-templates/match", request)
}

func (r *Runtime) nexusWorkflowTemplate(ctx context.Context, id, version string) (taskstate.Template, error) {
	if strings.TrimSpace(version) == "" {
		return taskstate.Template{}, taskToolError(fmt.Errorf("template_version is required when template_id is set"))
	}
	result, err := r.nexusWorkflowJSON(ctx, "GET", fmt.Sprintf("/v1/workflow-templates/%s/%s", url.PathEscape(id), url.PathEscape(version)), nil)
	if err != nil {
		return taskstate.Template{}, err
	}
	var template taskstate.Template
	if err := remarshal(result["template"], &template); err != nil {
		return taskstate.Template{}, taskToolError(fmt.Errorf("decode NexusDock workflow template: %w", err))
	}
	return template, nil
}

func (r *Runtime) nexusWorkflowJSON(ctx context.Context, method, path string, payload any) (Result, error) {
	base := strings.TrimRight(strings.TrimSpace(r.cfg.NexusEndpoint), "/")
	if base == "" {
		return nil, toolErrorDetails("NEXUS_NOT_CONFIGURED", "AGENTDOCK_NEXUS_ENDPOINT is required for workflow_template_manage", "configuration", map[string]any{"retryable": false})
	}
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, taskToolError(err)
		}
		body = bytes.NewReader(data)
	}
	client := http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return nil, taskToolError(err)
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token := strings.TrimSpace(r.cfg.NexusToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, toolErrorCause("NEXUS_REQUEST_FAILED", err.Error(), "network", map[string]any{"retryable": true}, err)
	}
	defer resp.Body.Close()
	data, err := readBoundedBody(resp.Body, 2*1024*1024)
	if err != nil {
		return nil, toolErrorCause("NEXUS_RESPONSE_BODY_INVALID", err.Error(), "response", map[string]any{"status": resp.StatusCode}, err)
	}
	var result Result
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, toolErrorCause("NEXUS_INVALID_RESPONSE", err.Error(), "response", map[string]any{"status": resp.StatusCode, "response_bytes": len(data)}, err)
		}
	} else {
		result = Result{}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := resp.Status
		if errMap, ok := result["error"].(map[string]any); ok {
			if msg, ok := errMap["message"].(string); ok && msg != "" {
				message = msg
			}
		} else if msg, ok := result["message"].(string); ok && msg != "" {
			message = msg
		}
		return nil, toolErrorDetails("NEXUS_WORKFLOW_ERROR", message, "nexus", map[string]any{"status": resp.StatusCode})
	}
	if result == nil {
		result = Result{}
	}
	for key, value := range result {
		result[key] = normalizeJSONToolValue(value)
	}
	result["nexus_endpoint"] = base
	return result, nil
}

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
	currentPhaseSteps := []map[string]any{}
	for _, step := range task.Steps {
		if step.Status == "completed" || step.Status == "skipped" {
			completedSteps++
		}
		if step.Phase == task.Phase && step.Status == "pending" {
			currentPhaseSteps = append(currentPhaseSteps, map[string]any{
				"id":    step.ID,
				"title": truncateString(step.Title, 120),
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
	return toolErrorDetails("TASK_STATE_ERROR", err.Error(), "validation", map[string]any{"retryable": false})
}

func remarshal(input any, out any) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

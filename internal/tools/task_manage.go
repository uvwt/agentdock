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

var taskActions = []string{"create", "list", "get", "checkpoint", "block", "resume", "final_review", "complete"}

var workflowTemplateActions = []string{"save", "validate", "publish", "retire", "list", "get", "get_many", "match", "vector_index"}

type taskManageInput struct {
	Action               string
	Title                string
	Goal                 string
	CompletionConditions []string
	Steps                []taskstate.TaskStepInput
	TemplateID           string
	SourceTemplateIDs    []string
	Status               string
	Limit                int
	TaskID               string
	StepID               string
	Summary              string
	Verified             []string
	Risks                []string
}

type workflowTemplateInput struct {
	Action               string
	TemplateID           string
	TemplateIDs          []string
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

func parseTaskManageInput(args map[string]any) (taskManageInput, error) {
	input := taskManageInput{
		Action:               strings.ToLower(strings.TrimSpace(stringArg(args, "action", ""))),
		Title:                stringArg(args, "title", ""),
		Goal:                 stringArg(args, "goal", ""),
		CompletionConditions: stringSliceArg(args, "completion_conditions"),
		TemplateID:           strings.TrimSpace(stringArg(args, "template_id", "")),
		SourceTemplateIDs:    stringSliceArg(args, "source_template_ids"),
		Status:               strings.ToLower(strings.TrimSpace(stringArg(args, "status", ""))),
		Limit:                intArg(args, "limit", 50),
		TaskID:               stringArg(args, "task_id", ""),
		StepID:               stringArg(args, "step_id", ""),
		Summary:              stringArg(args, "summary", ""),
		Verified:             stringSliceArg(args, "verified"),
		Risks:                stringSliceArg(args, "risks"),
	}
	if raw := args["steps"]; raw != nil {
		if err := remarshal(raw, &input.Steps); err != nil {
			return input, toolErrorDetails("VALIDATION_ERROR", "steps must be an array of task steps", "validation", map[string]any{"field": "steps", "reason": err.Error()})
		}
	}
	return input, nil
}

func parseWorkflowTemplateInput(args map[string]any) (workflowTemplateInput, error) {
	input := workflowTemplateInput{
		Action:             strings.ToLower(strings.TrimSpace(stringArg(args, "action", ""))),
		TemplateID:         strings.TrimSpace(stringArg(args, "template_id", "")),
		TemplateIDs:        stringSliceArg(args, "template_ids"),
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
	input, err := parseTaskManageInput(args)
	if err != nil {
		return nil, err
	}
	var task taskstate.Task
	switch input.Action {
	case "create":
		steps := append([]taskstate.TaskStepInput(nil), input.Steps...)
		conditions := append([]string(nil), input.CompletionConditions...)
		var sourceTemplates []taskstate.TemplateReference
		if input.TemplateID != "" && len(input.SourceTemplateIDs) > 0 {
			return nil, taskToolError(fmt.Errorf("template_id and source_template_ids cannot be used together"))
		}
		if input.TemplateID != "" {
			template, fetchErr := r.nexusActiveWorkflowTemplate(ctx, input.TemplateID)
			if fetchErr != nil {
				return nil, fetchErr
			}
			if len(steps) == 0 {
				steps = taskStepInputsFromTemplate(template)
			}
			conditions = append(append([]string{}, template.CompletionConditions...), conditions...)
			sourceTemplates = []taskstate.TemplateReference{{ID: template.ID, Version: template.Version, Hash: template.Hash}}
		}
		if len(input.SourceTemplateIDs) > 0 {
			if len(input.SourceTemplateIDs) < 2 || len(input.SourceTemplateIDs) > 3 {
				return nil, taskToolError(fmt.Errorf("source_template_ids must contain 2 or 3 template ids"))
			}
			if len(steps) == 0 || len(conditions) == 0 {
				return nil, toolErrorDetails("TEMPLATE_COMPOSITION_REQUIRED", "multiple source templates require composed steps and completion_conditions", "validation", map[string]any{"source_template_ids": input.SourceTemplateIDs})
			}
			templates, fetchErr := r.nexusActiveWorkflowTemplates(ctx, input.SourceTemplateIDs)
			if fetchErr != nil {
				return nil, fetchErr
			}
			sourceTemplates = templateReferences(templates)
		}
		task, err = r.tasks.Create(input.Title, input.Goal, conditions, steps, sourceTemplates)
		if err != nil {
			return nil, taskToolError(err)
		}
		return Result{
			"ok": true, "action": input.Action, "task_id": task.ID, "task_summary": compactTaskSummary(task), "state_dir": r.tasks.Root(),
			"next_required_action": "Use checkpoint when a step starts or completes. Use block only for a real blocker. After all steps and real verification are complete, call final_review, then complete.",
		}, nil
	case "list":
		status := taskstate.Status(input.Status)
		if status != "" && status != taskstate.StatusActive && status != taskstate.StatusBlocked && status != taskstate.StatusCompleted {
			return nil, toolErrorDetails("INVALID_STATUS", "unsupported task status filter", "validation", map[string]any{"status": status, "allowed": []string{"active", "blocked", "completed"}})
		}
		tasks, listErr := r.tasks.List(status, input.Limit)
		if listErr != nil {
			return nil, taskToolError(listErr)
		}
		items := make([]map[string]any, 0, len(tasks))
		for _, item := range tasks {
			items = append(items, compactTaskListItem(item))
		}
		return Result{"ok": true, "action": input.Action, "tasks": items, "count": len(items), "state_dir": r.tasks.Root()}, nil
	case "get":
		task, err = r.tasks.Get(input.TaskID)
		if err != nil {
			return nil, taskToolError(err)
		}
		return Result{"ok": true, "action": input.Action, "task": task, "state_dir": r.tasks.Root()}, nil
	case "checkpoint":
		task, err = r.tasks.Checkpoint(input.TaskID, input.StepID, input.Status, input.Summary)
	case "block":
		task, err = r.tasks.Block(input.TaskID, input.Summary)
	case "resume":
		task, err = r.tasks.Resume(input.TaskID, input.Summary)
	case "final_review":
		review := taskstate.FinalReviewInput{Status: input.Status, Summary: input.Summary, VerifiedFacts: input.Verified, OpenRisks: input.Risks}
		task, err = r.tasks.FinalReview(input.TaskID, review)
	case "complete":
		task, err = r.tasks.Complete(input.TaskID)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported task_manage action", "validation", map[string]any{"action": input.Action, "allowed": taskActions})
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
	case "get_many":
		templates, err := r.nexusActiveWorkflowTemplates(ctx, input.TemplateIDs)
		if err != nil {
			return nil, err
		}
		return Result{
			"ok": true, "action": input.Action, "templates": templates, "count": len(templates), "composition_required": true,
			"next_required_action": "Combine these templates for the current user goal: prune irrelevant steps, deduplicate, order the remaining steps, and merge completion conditions. Then call task_manage create with source_template_ids, composed steps, and completion_conditions.",
		}, nil
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

func (r *Runtime) nexusActiveWorkflowTemplates(ctx context.Context, ids []string) ([]taskstate.Template, error) {
	ids = normalizeTemplateIDs(ids)
	if len(ids) < 2 || len(ids) > 3 {
		return nil, taskToolError(fmt.Errorf("template_ids must contain 2 or 3 distinct ids"))
	}
	templates := make([]taskstate.Template, 0, len(ids))
	for _, id := range ids {
		template, err := r.nexusActiveWorkflowTemplate(ctx, id)
		if err != nil {
			return nil, err
		}
		templates = append(templates, template)
	}
	return templates, nil
}

func (r *Runtime) nexusActiveWorkflowTemplate(ctx context.Context, id string) (taskstate.Template, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return taskstate.Template{}, taskToolError(fmt.Errorf("template_id is required"))
	}
	result, err := r.nexusWorkflowJSON(ctx, "GET", "/v1/workflow-templates?status=active", nil)
	if err != nil {
		return taskstate.Template{}, err
	}
	raw := result["templates"]
	if raw == nil {
		raw = result["items"]
	}
	var summaries []struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	if err := remarshal(raw, &summaries); err != nil {
		return taskstate.Template{}, taskToolError(fmt.Errorf("decode active workflow templates: %w", err))
	}
	for _, summary := range summaries {
		if summary.ID == id {
			return r.nexusWorkflowTemplate(ctx, summary.ID, summary.Version)
		}
	}
	return taskstate.Template{}, taskToolError(fmt.Errorf("active workflow template %s not found", id))
}

func (r *Runtime) nexusWorkflowTemplate(ctx context.Context, id, version string) (taskstate.Template, error) {
	result, err := r.nexusWorkflowJSON(ctx, "GET", fmt.Sprintf("/v1/workflow-templates/%s/%s", url.PathEscape(id), url.PathEscape(version)), nil)
	if err != nil {
		return taskstate.Template{}, err
	}
	var template taskstate.Template
	if err := remarshal(result["template"], &template); err != nil {
		return taskstate.Template{}, taskToolError(fmt.Errorf("decode NexusDock workflow template: %w", err))
	}
	if template.Status != taskstate.TemplateActive {
		return taskstate.Template{}, taskToolError(fmt.Errorf("workflow template %s is not active", id))
	}
	return template, nil
}

func normalizeTemplateIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func taskStepInputsFromTemplate(template taskstate.Template) []taskstate.TaskStepInput {
	steps := make([]taskstate.TaskStepInput, 0, len(template.Steps))
	for _, step := range template.Steps {
		steps = append(steps, taskstate.TaskStepInput{ID: step.ID, Title: step.Title, Phase: step.Phase})
	}
	return steps
}

func templateReferences(templates []taskstate.Template) []taskstate.TemplateReference {
	refs := make([]taskstate.TemplateReference, 0, len(templates))
	for _, template := range templates {
		refs = append(refs, taskstate.TemplateReference{ID: template.ID, Version: template.Version, Hash: template.Hash})
	}
	return refs
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
	return toolErrorDetails("TASK_STATE_ERROR", err.Error(), "validation", map[string]any{"retryable": false})
}

func remarshal(input any, out any) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

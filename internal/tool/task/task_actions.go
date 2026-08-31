package task

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/uvwt/agentdock/internal/taskstate"
)

var taskActions = []string{"create", "list", "get", "checkpoint", "block", "resume", "final_review", "complete"}

var workflowTemplateActions = []string{"publish", "retire", "list", "get", "get_many", "match", "vector_index"}

type taskManageInput struct {
	Action               string
	Title                string
	Goal                 string
	Project              string
	Device               string
	CompletionConditions []string
	Steps                []taskstate.TaskStepInput
	TemplateID           string
	SourceTemplateIDs    []string
	LearningChecks       []taskstate.EvolutionBinding
	Status               string
	Limit                int
	TaskID               string
	StepID               string
	CompletedStepIDs     []string
	CompletedStepIDsSet  bool
	CurrentStepID        string
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

type workflowTemplatePublishRequest struct {
	Template taskstate.Template `json:"template"`
}

type workflowTemplateMatchRequest struct {
	Goal   string `json:"goal"`
	Device string `json:"device"`
	Type   string `json:"type"`
}

func normalizeTaskManageRequest(request ManageRequest) (taskManageInput, error) {
	steps := make([]taskstate.TaskStepInput, len(request.Steps))
	for i, step := range request.Steps {
		steps[i] = taskstate.TaskStepInput{ID: step.ID, Title: step.Title}
	}
	learningChecks := make([]taskstate.EvolutionBinding, len(request.LearningChecks))
	for i, check := range request.LearningChecks {
		learningChecks[i] = taskstate.EvolutionBinding{
			EvolutionID: check.EvolutionID,
			OnSuccess:   check.OnSuccess,
			OnFailure:   check.OnFailure,
		}
	}

	input := taskManageInput{
		Action:               strings.ToLower(strings.TrimSpace(request.Action)),
		Title:                request.Title,
		Goal:                 request.Goal,
		Project:              strings.ToLower(strings.TrimSpace(request.Project)),
		Device:               strings.TrimSpace(request.Device),
		CompletionConditions: append([]string(nil), request.CompletionConditions...),
		Steps:                steps,
		TemplateID:           strings.TrimSpace(request.TemplateID),
		SourceTemplateIDs:    append([]string(nil), request.SourceTemplateIDs...),
		LearningChecks:       learningChecks,
		Status:               strings.ToLower(strings.TrimSpace(request.Status)),
		Limit:                intValue(request.Limit, 50),
		TaskID:               request.TaskID,
		StepID:               request.StepID,
		CurrentStepID:        request.CurrentStepID,
		Summary:              request.Summary,
		Verified:             append([]string(nil), request.Verified...),
		Risks:                append([]string(nil), request.Risks...),
	}
	if request.CompletedStepIDs != nil {
		input.CompletedStepIDsSet = true
		input.CompletedStepIDs = append([]string(nil), (*request.CompletedStepIDs)...)
	}
	if len(input.LearningChecks) > 3 {
		return input, toolErrorDetails("VALIDATION_ERROR", "learning_checks cannot exceed 3", "validation", map[string]any{"field": "learning_checks"})
	}
	return input, nil
}

func normalizeWorkflowRequest(request WorkflowRequest) workflowTemplateInput {
	input := workflowTemplateInput{
		Action:             strings.ToLower(strings.TrimSpace(request.Action)),
		TemplateID:         strings.TrimSpace(request.TemplateID),
		TemplateIDs:        append([]string(nil), request.TemplateIDs...),
		TemplateVersion:    strings.TrimSpace(request.TemplateVersion),
		TemplateStatus:     strings.TrimSpace(request.TemplateStatus),
		Goal:               request.Goal,
		Device:             request.Device,
		Type:               request.Type,
		LongTemplateReason: strings.TrimSpace(request.LongTemplateReason),
	}
	if request.AllowLongTemplate != nil {
		input.AllowLongTemplateSet = true
		input.AllowLongTemplate = *request.AllowLongTemplate
	}
	if request.Template != nil {
		input.Template = *request.Template
	}
	if input.Action == "publish" {
		input.applyTemplateGuardrails()
	}
	return input
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

func (s *Service) Manage(ctx context.Context, request ManageRequest) (Result, error) {
	input, err := normalizeTaskManageRequest(request)
	if err != nil {
		return nil, err
	}
	var task taskstate.Task
	var evolutionWarning string
	switch input.Action {
	case "create":
		steps := append([]taskstate.TaskStepInput(nil), input.Steps...)
		conditions := append([]string(nil), input.CompletionConditions...)
		var sourceTemplates []taskstate.TemplateReference
		if input.TemplateID != "" && len(input.SourceTemplateIDs) > 0 {
			return nil, taskToolError(fmt.Errorf("template_id and source_template_ids cannot be used together"))
		}
		if input.TemplateID != "" {
			template, fetchErr := s.nexusActiveWorkflowTemplate(ctx, input.TemplateID)
			if fetchErr != nil {
				return nil, fetchErr
			}
			if len(steps) == 0 {
				steps = taskStepInputsFromTemplate(template)
			}
			conditions = append(append([]string{}, template.CompletionConditions...), conditions...)
			sourceTemplates = []taskstate.TemplateReference{{ID: template.ID, Version: template.Version, Hash: template.Hash, SourceEvolutionID: template.SourceEvolutionID}}
		}
		if len(input.SourceTemplateIDs) > 0 {
			if len(input.SourceTemplateIDs) < 2 || len(input.SourceTemplateIDs) > 3 {
				return nil, taskToolError(fmt.Errorf("source_template_ids must contain 2 or 3 template ids"))
			}
			if len(steps) == 0 || len(conditions) == 0 {
				return nil, toolErrorDetails("TEMPLATE_COMPOSITION_REQUIRED", "multiple source templates require composed steps and completion_conditions", "validation", map[string]any{"source_template_ids": input.SourceTemplateIDs})
			}
			templates, fetchErr := s.nexusActiveWorkflowTemplates(ctx, input.SourceTemplateIDs)
			if fetchErr != nil {
				return nil, fetchErr
			}
			sourceTemplates = templateReferences(templates)
		}
		bindings := append([]taskstate.EvolutionBinding(nil), input.LearningChecks...)
		if len(bindings) > 0 {
			if s.evolution == nil || s.config().NexusEndpoint == "" {
				return nil, taskToolError(fmt.Errorf("learning_checks require Evolution with Nexus configured"))
			}
			preflightTask := taskstate.Task{Project: input.Project, Device: input.Device, SourceTemplates: sourceTemplates}
			bindings, err = s.evolution.ValidateBindings(ctx, preflightTask, bindings)
			if err != nil {
				return nil, taskToolError(err)
			}
		}
		task, err = s.tasks.CreateWithContext(input.Title, input.Goal, input.Project, input.Device, conditions, steps, sourceTemplates, bindings...)
		if err != nil {
			return nil, taskToolError(err)
		}
		task, evolutionWarning = s.refreshGuidanceBestEffort(ctx, task)
		result := Result{
			"action": input.Action, "task_id": task.ID, "task_summary": compactTaskSummary(task), "state_dir": s.tasks.Root(),
			"next_required_action": "Use checkpoint at meaningful recovery points; completed_step_ids/current_step_id can update several steps atomically. Use block only for a real blocker. After all steps and real verification are complete, call final_review, then complete.",
		}
		if len(task.GuidanceContext) > 0 {
			result["guidance_context"] = task.GuidanceContext
		}
		if evolutionWarning != "" {
			result["evolution_warning"] = evolutionWarning
		}
		return result, nil
	case "list":
		status := taskstate.Status(input.Status)
		if status != "" && status != taskstate.StatusActive && status != taskstate.StatusBlocked && status != taskstate.StatusCompleted {
			return nil, toolErrorDetails("INVALID_STATUS", "unsupported task status filter", "validation", map[string]any{"status": status, "allowed": []string{"active", "blocked", "completed"}})
		}
		tasks, listErr := s.tasks.List(status, input.Limit)
		if listErr != nil {
			return nil, taskToolError(listErr)
		}
		items := make([]map[string]any, 0, len(tasks))
		for _, item := range tasks {
			items = append(items, compactTaskListItem(item))
		}
		return Result{"action": input.Action, "tasks": items, "count": len(items), "state_dir": s.tasks.Root()}, nil
	case "get":
		task, err = s.tasks.Get(input.TaskID)
		if err != nil {
			return nil, taskToolError(err)
		}
		return Result{"action": input.Action, "task": task, "state_dir": s.tasks.Root()}, nil
	case "checkpoint":
		singleStepMode := strings.TrimSpace(input.StepID) != "" || input.Status != ""
		batchMode := input.CompletedStepIDsSet || strings.TrimSpace(input.CurrentStepID) != ""
		if singleStepMode && batchMode {
			return nil, toolErrorDetails("VALIDATION_ERROR", "single-step and batch checkpoint fields cannot be combined", "validation", map[string]any{
				"single_step_fields": []string{"step_id", "status"},
				"batch_fields":       []string{"completed_step_ids", "current_step_id"},
			})
		}
		if batchMode {
			task, err = s.tasks.BatchCheckpoint(input.TaskID, input.CompletedStepIDs, input.CurrentStepID, input.Summary)
		} else {
			task, err = s.tasks.Checkpoint(input.TaskID, input.StepID, input.Status, input.Summary)
		}
	case "block":
		task, err = s.tasks.Block(input.TaskID, input.Summary)
	case "resume":
		task, err = s.tasks.Resume(input.TaskID, input.Summary)
		if err == nil {
			task, evolutionWarning = s.refreshGuidanceBestEffort(ctx, task)
		}
	case "final_review":
		review := taskstate.FinalReviewInput{Status: input.Status, Summary: input.Summary, VerifiedFacts: input.Verified, OpenRisks: input.Risks}
		task, err = s.tasks.FinalReview(input.TaskID, review)
		if err == nil {
			warnings := make([]string, 0, 2)
			if warning := s.resolveBindingsBestEffort(ctx, task); warning != "" {
				warnings = append(warnings, warning)
			}
			var warning string
			task, warning = s.refreshCandidatesBestEffort(ctx, task)
			if warning != "" {
				warnings = append(warnings, warning)
			}
			evolutionWarning = strings.Join(warnings, "; ")
		}
	case "complete":
		task, err = s.tasks.Complete(input.TaskID)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported task_manage action", "validation", map[string]any{"action": input.Action, "allowed": taskActions})
	}
	if err != nil {
		return nil, taskToolError(err)
	}
	result := Result{"action": input.Action, "task_id": task.ID, "task_summary": compactTaskSummary(task), "state_dir": s.tasks.Root()}
	if input.Action == "resume" && len(task.GuidanceContext) > 0 {
		result["guidance_context"] = task.GuidanceContext
	}
	if input.Action == "final_review" && task.FinalReview != nil {
		result["review_revision"] = task.FinalReview.ReviewRevision
		if len(task.EvolutionCandidates) > 0 {
			result["evolution_candidates"] = task.EvolutionCandidates
		}
	}
	if evolutionWarning != "" {
		result["evolution_warning"] = evolutionWarning
	}
	return result, nil
}

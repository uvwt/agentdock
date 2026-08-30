package task

import "github.com/uvwt/agentdock/internal/taskstate"

type StepRequest struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type LearningCheckRequest struct {
	EvolutionID string `json:"evolution_id"`
	OnSuccess   string `json:"on_success"`
	OnFailure   string `json:"on_failure"`
}

// ManageRequest 只表达 task_manage 对模型公开的输入字段。
// 持久化状态中的 phase、bound_at 等内部字段在规范化阶段由 Task 域模型补齐，不能从边界注入。
type ManageRequest struct {
	Action               string                 `json:"action"`
	Title                string                 `json:"title,omitempty"`
	Goal                 string                 `json:"goal,omitempty"`
	Project              string                 `json:"project,omitempty"`
	Device               string                 `json:"device,omitempty"`
	CompletionConditions []string               `json:"completion_conditions,omitempty"`
	Steps                []StepRequest          `json:"steps,omitempty"`
	TemplateID           string                 `json:"template_id,omitempty"`
	SourceTemplateIDs    []string               `json:"source_template_ids,omitempty"`
	LearningChecks       []LearningCheckRequest `json:"learning_checks,omitempty"`
	Status               string                 `json:"status,omitempty"`
	Limit                *int                   `json:"limit,omitempty"`
	TaskID               string                 `json:"task_id,omitempty"`
	StepID               string                 `json:"step_id,omitempty"`
	CompletedStepIDs     *[]string              `json:"completed_step_ids,omitempty"`
	CurrentStepID        string                 `json:"current_step_id,omitempty"`
	Summary              string                 `json:"summary,omitempty"`
	Verified             []string               `json:"verified,omitempty"`
	Risks                []string               `json:"risks,omitempty"`
}

// WorkflowRequest 是 workflow_template_manage 的强类型输入。
type WorkflowRequest struct {
	Action             string              `json:"action"`
	TemplateID         string              `json:"template_id,omitempty"`
	TemplateIDs        []string            `json:"template_ids,omitempty"`
	TemplateVersion    string              `json:"template_version,omitempty"`
	TemplateStatus     string              `json:"template_status,omitempty"`
	Goal               string              `json:"goal,omitempty"`
	Device             string              `json:"device,omitempty"`
	Type               string              `json:"type,omitempty"`
	Template           *taskstate.Template `json:"template,omitempty"`
	AllowLongTemplate  *bool               `json:"allow_long_template,omitempty"`
	LongTemplateReason string              `json:"long_template_reason,omitempty"`
}

func intValue(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

package taskstate

import (
	"errors"
	"time"
)

const (
	SchemaVersion     = 1
	FinalReviewPass   = "pass"
	FinalReviewFailed = "failed"

	maxTaskTitleBytes        = 512
	maxTaskGoalBytes         = 16 << 10
	maxTaskConditionBytes    = 4 << 10
	maxTaskConditions        = 64
	maxTaskStepTitleBytes    = 512
	maxTaskSummaryBytes      = 8 << 10
	maxTaskReviewItemBytes   = 4 << 10
	maxTaskReviewItems       = 64
	maxTaskEvents            = 256
	maxTaskEventSummaryBytes = 4 << 10
	maxTaskStateFileBytes    = 8 << 20
)

var ErrTaskNotFound = errors.New("task not found")

type Phase string

const (
	PhaseCheck    Phase = "check"
	PhaseExecute  Phase = "execute"
	PhaseVerify   Phase = "verify"
	PhaseCloseout Phase = "closeout"
)

type Status string

const (
	StatusActive    Status = "active"
	StatusBlocked   Status = "blocked"
	StatusCompleted Status = "completed"
)

type Condition struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

type FinalReviewInput struct {
	Status        string   `json:"status"`
	Summary       string   `json:"summary"`
	VerifiedFacts []string `json:"verified_facts,omitempty"`
	OpenRisks     []string `json:"open_risks,omitempty"`
	MissingChecks []string `json:"missing_checks,omitempty"`
}

type FinalReview struct {
	Status         string    `json:"status"`
	Summary        string    `json:"summary"`
	VerifiedFacts  []string  `json:"verified_facts,omitempty"`
	OpenRisks      []string  `json:"open_risks,omitempty"`
	MissingChecks  []string  `json:"missing_checks,omitempty"`
	ReviewRevision string    `json:"review_revision"`
	ReviewedAt     time.Time `json:"reviewed_at"`
}

type Event struct {
	Type      string    `json:"type"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

type Task struct {
	SchemaVersion         int                    `json:"schema_version"`
	ID                    string                 `json:"id"`
	Title                 string                 `json:"title"`
	Goal                  string                 `json:"goal"`
	Project               string                 `json:"project,omitempty"`
	Device                string                 `json:"device,omitempty"`
	Status                Status                 `json:"status"`
	Phase                 Phase                  `json:"phase"`
	Conditions            []Condition            `json:"conditions"`
	Template              *TemplateSelection     `json:"template,omitempty"` // 仅用于读取旧任务状态。
	SourceTemplates       []TemplateReference    `json:"source_templates,omitempty"`
	Steps                 []TaskStep             `json:"steps,omitempty"`
	Events                []Event                `json:"events"`
	Blocker               string                 `json:"blocker,omitempty"`
	Summary               string                 `json:"summary,omitempty"`
	GuidanceContext       []EvolutionContextItem `json:"guidance_context,omitempty"`
	EvolutionGuidanceSeen []string               `json:"evolution_guidance_seen,omitempty"`
	EvolutionCandidates   []EvolutionContextItem `json:"evolution_candidates,omitempty"`
	EvolutionBindings     []EvolutionBinding     `json:"evolution_bindings,omitempty"`
	FinalReview           *FinalReview           `json:"final_review,omitempty"`
	CreatedAt             time.Time              `json:"created_at"`
	UpdatedAt             time.Time              `json:"updated_at"`
	CompletedAt           *time.Time             `json:"completed_at,omitempty"`
}

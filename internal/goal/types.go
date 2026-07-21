package goal

import "time"

const (
	SchemaVersion = 1

	maxTitleBytes           = 512
	maxObjectiveBytes       = 16 << 10
	maxSummaryBytes         = 8 << 10
	maxBlockerBytes         = 8 << 10
	maxConstraintValueBytes = 4 << 10
	maxCriterionExprBytes   = 4 << 10
	maxStepSummaryBytes     = 4 << 10
	maxEvidenceSummaryBytes = 4 << 10
	maxWorkerIDBytes        = 256
	maxWorkspaceIDBytes     = 256
	maxMilestoneTitleBytes  = 512
	maxEventsEmbedded       = 128
	maxGoalStateFileBytes   = 8 << 20
	maxCriteria             = 32
	maxConstraints          = 32
	maxMilestones           = 24
	maxSteps                = 48
	maxCommitSteps          = 24
	maxEvidence             = 64
	maxPendingApprovals     = 32
	defaultLeaseTTL         = 30 * time.Minute
	defaultListLimit        = 50
	maxListLimit            = 200
)

// Status is the Goal state machine value.
type Status string

const (
	StatusDraft                Status = "draft"
	StatusPlanning             Status = "planning"
	StatusAwaitingPlanApproval Status = "awaiting_plan_approval"
	StatusExecuting            Status = "executing"
	StatusVerifying            Status = "verifying"
	StatusCompleted            Status = "completed"
	StatusRegressed            Status = "regressed"
	StatusReplanning           Status = "replanning"
	StatusBlocked              Status = "blocked"
	StatusAwaitingReasoning    Status = "awaiting_reasoning"
	StatusPaused               Status = "paused"
	StatusCancelled            Status = "cancelled"
	StatusFailed               Status = "failed"
	StatusAwaitingUser         Status = "awaiting_user"
	StatusAwaitingCredentials  Status = "awaiting_credentials"
	StatusAwaitingApproval     Status = "awaiting_approval"
)

// Mode controls default policy strictness.
type Mode string

const (
	ModeGuarded   Mode = "guarded"
	ModeAutopilot Mode = "autopilot" // reserved; P1 still enforces guarded defaults
	ModeReadonly  Mode = "readonly"
)

// CriterionType is a machine-checkable success condition kind.
type CriterionType string

const (
	CriterionCommand CriterionType = "command"
	CriterionMetric  CriterionType = "metric"
	CriterionBrowser CriterionType = "browser"
	CriterionManual  CriterionType = "manual"
)

// CriterionStatus tracks verification progress for one criterion.
type CriterionStatus string

const (
	CriterionPending   CriterionStatus = "pending"
	CriterionSatisfied CriterionStatus = "satisfied"
	CriterionFailed    CriterionStatus = "failed"
	CriterionSkipped   CriterionStatus = "skipped"
)

// ConstraintType classifies a goal constraint.
type ConstraintType string

const (
	ConstraintProhibition ConstraintType = "prohibition"
	ConstraintQuality     ConstraintType = "quality"
	ConstraintApproval    ConstraintType = "approval"
	ConstraintBudget      ConstraintType = "budget"
)

// StepAction is the whitelist of deterministic / planning actions a commit may propose.
// Arbitrary shell strings are never accepted as actions.
type StepAction string

const (
	ActionInspectFiles     StepAction = "inspect_files"
	ActionPreparePatch     StepAction = "prepare_patch"
	ActionApplyPatch       StepAction = "apply_patch"
	ActionRunTests         StepAction = "run_tests"
	ActionRunCommand       StepAction = "run_command"
	ActionStartProcess     StepAction = "start_process"
	ActionBrowserNavigate  StepAction = "browser_navigate"
	ActionBrowserAct       StepAction = "browser_act"
	ActionBrowserVerify    StepAction = "browser_verify"
	ActionCollectLogs      StepAction = "collect_logs"
	ActionCollectMetrics   StepAction = "collect_metrics"
	ActionCreateCheckpoint StepAction = "create_checkpoint"
	ActionRequestApproval  StepAction = "request_approval"
	ActionMarkBlocked      StepAction = "mark_blocked"
	ActionEnterVerify      StepAction = "enter_verify"
	ActionReplan           StepAction = "replan"
)

// KnownStepActions is the commit_turn whitelist.
var KnownStepActions = map[StepAction]struct{}{
	ActionInspectFiles:     {},
	ActionPreparePatch:     {},
	ActionApplyPatch:       {},
	ActionRunTests:         {},
	ActionRunCommand:       {},
	ActionStartProcess:     {},
	ActionBrowserNavigate:  {},
	ActionBrowserAct:       {},
	ActionBrowserVerify:    {},
	ActionCollectLogs:      {},
	ActionCollectMetrics:   {},
	ActionCreateCheckpoint: {},
	ActionRequestApproval:  {},
	ActionMarkBlocked:      {},
	ActionEnterVerify:      {},
	ActionReplan:           {},
}

// Decision is the high-level outcome of a reasoning turn.
type Decision string

const (
	DecisionContinue Decision = "continue"
	DecisionBlock    Decision = "block"
	DecisionComplete Decision = "complete"
	DecisionReplan   Decision = "replan"
	DecisionPause    Decision = "pause"
	DecisionVerify   Decision = "verify"
)

// StepStatus tracks planned step progress.
type StepStatus string

const (
	StepPending    StepStatus = "pending"
	StepInProgress StepStatus = "in_progress"
	StepCompleted  StepStatus = "completed"
	StepSkipped    StepStatus = "skipped"
	StepFailed     StepStatus = "failed"
)

// MilestoneStatus tracks milestone progress.
type MilestoneStatus string

const (
	MilestonePending   MilestoneStatus = "pending"
	MilestoneActive    MilestoneStatus = "active"
	MilestoneCompleted MilestoneStatus = "completed"
	MilestoneSkipped   MilestoneStatus = "skipped"
)

// SuccessCriterion is a machine-oriented completion condition.
type SuccessCriterion struct {
	ID         string          `json:"id"`
	Type       CriterionType   `json:"type"`
	Expression string          `json:"expression"`
	Status     CriterionStatus `json:"status"`
	EvidenceID string          `json:"evidence_id,omitempty"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// Constraint limits what the runtime or worker may do.
type Constraint struct {
	Type  ConstraintType `json:"type"`
	Value string         `json:"value"`
}

// Budget bounds loops and resource use.
type Budget struct {
	MaxReasoningTurns         int `json:"max_reasoning_turns"`
	MaxReplans                int `json:"max_replans"`
	MaxConversationRotations  int `json:"max_conversation_rotations"`
	MaxRuntimeMinutes         int `json:"max_runtime_minutes"`
	MaxIdenticalFailures      int `json:"max_identical_failures"`
	MaxBrowserRetries         int `json:"max_browser_retries"`
	MaxChangedFiles           int `json:"max_changed_files"`
	ReasoningTurnsUsed        int `json:"reasoning_turns_used"`
	ReplansUsed               int `json:"replans_used"`
	ConversationRotationsUsed int `json:"conversation_rotations_used"`
}

// Milestone is a named phase of the goal.
type Milestone struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Status    MilestoneStatus `json:"status"`
	Summary   string          `json:"summary,omitempty"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// Step is a concrete planned or running unit of work.
type Step struct {
	ID          string     `json:"id"`
	MilestoneID string     `json:"milestone_id,omitempty"`
	Action      StepAction `json:"action"`
	Targets     []string   `json:"targets,omitempty"`
	Summary     string     `json:"summary,omitempty"`
	Status      StepStatus `json:"status"`
	Idempotency string     `json:"idempotency_key,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Lease grants exclusive reasoning rights for one capsule version.
type Lease struct {
	LeaseID        string    `json:"lease_id"`
	GoalID         string    `json:"goal_id"`
	WorkerID       string    `json:"worker_id"`
	CapsuleVersion int       `json:"capsule_version"`
	ExpiresAt      time.Time `json:"expires_at"`
	AcquiredAt     time.Time `json:"acquired_at"`
}

// Approval records a pending high-risk action.
type Approval struct {
	ID        string    `json:"id"`
	Action    string    `json:"action"`
	Summary   string    `json:"summary"`
	Risk      string    `json:"risk,omitempty"`
	Status    string    `json:"status"` // pending | approved | rejected
	CreatedAt time.Time `json:"created_at"`
}

// EvidenceRef points at stored proof without embedding full payloads.
// Data holds compact machine-readable fields for the verifier (exit codes, urls, metrics).
type EvidenceRef struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Summary   string         `json:"summary"`
	URI       string         `json:"uri,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// Event is an embedded recent event; full history lives in JSONL.
type Event struct {
	Type      string    `json:"type"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

// Goal is the durable source of truth for Goal Mode.
type Goal struct {
	SchemaVersion    int                `json:"schema_version"`
	ID               string             `json:"goal_id"`
	Title            string             `json:"title"`
	Objective        string             `json:"objective"`
	Status           Status             `json:"status"`
	Mode             Mode               `json:"mode"`
	WorkspaceID      string             `json:"workspace_id,omitempty"`
	DeviceID         string             `json:"device_id,omitempty"`
	BaseGitSHA       string             `json:"base_git_sha,omitempty"`
	CurrentGitSHA    string             `json:"current_git_sha,omitempty"`
	CapsuleVersion   int                `json:"capsule_version"`
	Milestones       []Milestone        `json:"milestones,omitempty"`
	Steps            []Step             `json:"steps,omitempty"`
	SuccessCriteria  []SuccessCriterion `json:"success_criteria"`
	Constraints      []Constraint       `json:"constraints,omitempty"`
	Budget           Budget             `json:"budget"`
	PendingApprovals []Approval         `json:"pending_approvals,omitempty"`
	Evidence         []EvidenceRef      `json:"evidence,omitempty"`
	ActiveLease      *Lease             `json:"active_lease,omitempty"`
	CurrentProblem   string             `json:"current_problem,omitempty"`
	CurrentRequest   string             `json:"current_request,omitempty"`
	CompletedNotes   []string           `json:"completed,omitempty"`
	Blocker          string             `json:"blocker,omitempty"`
	Summary          string             `json:"summary,omitempty"`
	// WorkerConversationURL is the durable ChatGPT (or other web worker) thread to resume.
	// Example: https://chatgpt.com/c/<id>. Empty means no bound thread yet.
	WorkerConversationURL string `json:"worker_conversation_url,omitempty"`
	// WorkerConversationID is an optional opaque id derived from the URL.
	WorkerConversationID string `json:"worker_conversation_id,omitempty"`
	// Progress tracking for infinite-loop protection.
	NoProgressStreak        int        `json:"no_progress_streak,omitempty"`
	LastProgressFingerprint string     `json:"last_progress_fingerprint,omitempty"`
	Events                  []Event    `json:"events"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
	CompletedAt             *time.Time `json:"completed_at,omitempty"`
}

// CreateInput is the create API.
type CreateInput struct {
	Title           string
	Objective       string
	WorkspaceID     string
	DeviceID        string
	Mode            Mode
	SuccessCriteria []SuccessCriterionInput
	Constraints     []Constraint
	Budget          *Budget
	Milestones      []MilestoneInput
	BaseGitSHA      string
}

// SuccessCriterionInput is create-time criterion data.
type SuccessCriterionInput struct {
	ID         string        `json:"id,omitempty"`
	Type       CriterionType `json:"type,omitempty"`
	Expression string        `json:"expression"`
}

// MilestoneInput is create-time milestone data.
type MilestoneInput struct {
	ID    string `json:"id,omitempty"`
	Title string `json:"title"`
}

// CommitTurnInput is a structured reasoning commit.
type CommitTurnInput struct {
	GoalID                 string
	ReasoningLeaseID       string
	ExpectedCapsuleVersion int
	Decision               Decision
	Summary                string
	NextMilestone          string
	CurrentProblem         string
	CurrentRequest         string
	Steps                  []CommitStepInput
	CompletedNotes         []string
}

// CommitStepInput is one proposed next step.
type CommitStepInput struct {
	Action      StepAction `json:"action"`
	Targets     []string   `json:"targets,omitempty"`
	Summary     string     `json:"summary,omitempty"`
	MilestoneID string     `json:"milestone_id,omitempty"`
	Idempotency string     `json:"idempotency_key,omitempty"`
}

// MarkBlockedInput records a blocker.
type MarkBlockedInput struct {
	GoalID   string
	Reason   string
	Tried    string
	Evidence string
	NeedUser string
}

// RequestApprovalInput registers a pending approval.
type RequestApprovalInput struct {
	GoalID  string
	Action  string
	Summary string
	Risk    string
}

// DefaultBudget returns P1 defaults.
func DefaultBudget() Budget {
	return Budget{
		MaxReasoningTurns:        20,
		MaxReplans:               4,
		MaxConversationRotations: 5,
		MaxRuntimeMinutes:        180,
		MaxIdenticalFailures:     2,
		MaxBrowserRetries:        3,
		MaxChangedFiles:          20,
	}
}

package taskstate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	SchemaVersion                = 1
	MaxStrategyAttempts          = 2
	MaxConsecutiveAttemptRecords = 2
	AttemptRecordedEventType     = "attempt_recorded"
	FinalReviewPass              = "pass"
	FinalReviewFailed            = "failed"
)

type Phase string

const (
	PhaseCheck    Phase = "check"
	PhaseExecute  Phase = "execute"
	PhaseVerify   Phase = "verify"
	PhaseCloseout Phase = "closeout"
)

var phaseOrder = []Phase{PhaseCheck, PhaseExecute, PhaseVerify, PhaseCloseout}

type Status string

const (
	StatusActive    Status = "active"
	StatusBlocked   Status = "blocked"
	StatusCompleted Status = "completed"
)

type Condition struct {
	ID        string     `json:"id"`
	Text      string     `json:"text"`
	CreatedAt time.Time  `json:"created_at"`
	Evidence  []Evidence `json:"evidence"`
}

type Evidence struct {
	Summary   string    `json:"summary"`
	Source    string    `json:"source,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type ConditionEvidenceUpdate struct {
	ConditionID string `json:"condition_id"`
	Summary     string `json:"summary"`
	Source      string `json:"source,omitempty"`
}

type StepCompletionUpdate struct {
	StepID             string       `json:"step_id"`
	Evidence           StepEvidence `json:"evidence,omitempty"`
	Summary            string       `json:"summary,omitempty"`
	Substituted        bool         `json:"substituted,omitempty"`
	SubstitutionReason string       `json:"substitution_reason,omitempty"`
}

type PhaseCheckpointInput struct {
	StepCompletions   []StepCompletionUpdate    `json:"step_completions,omitempty"`
	ConditionEvidence []ConditionEvidenceUpdate `json:"condition_evidence,omitempty"`
	AdvancePhase      bool                      `json:"advance_phase,omitempty"`
	CompleteTask      bool                      `json:"complete_task,omitempty"`
	Summary           string                    `json:"summary"`
}

type FinalReviewInput struct {
	Status        string   `json:"status"`
	Summary       string   `json:"summary"`
	VerifiedFacts []string `json:"verified_facts,omitempty"`
	OpenRisks     []string `json:"open_risks,omitempty"`
	MissingChecks []string `json:"missing_checks,omitempty"`
}

type FinalReview struct {
	Status        string    `json:"status"`
	Summary       string    `json:"summary"`
	VerifiedFacts []string  `json:"verified_facts,omitempty"`
	OpenRisks     []string  `json:"open_risks,omitempty"`
	MissingChecks []string  `json:"missing_checks,omitempty"`
	ReviewedAt    time.Time `json:"reviewed_at"`
}

type Attempt struct {
	Phase     Phase     `json:"phase"`
	Strategy  string    `json:"strategy"`
	Outcome   string    `json:"outcome"`
	Diagnosis string    `json:"diagnosis,omitempty"`
	Evidence  string    `json:"evidence,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Event struct {
	Type      string    `json:"type"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

type Task struct {
	SchemaVersion int                `json:"schema_version"`
	ID            string             `json:"id"`
	Title         string             `json:"title"`
	Goal          string             `json:"goal"`
	Status        Status             `json:"status"`
	Phase         Phase              `json:"phase"`
	Conditions    []Condition        `json:"conditions"`
	Template      *TemplateSelection `json:"template,omitempty"`
	Steps         []TaskStep         `json:"steps,omitempty"`
	Attempts      []Attempt          `json:"attempts"`
	Events        []Event            `json:"events"`
	Blocker       string             `json:"blocker,omitempty"`
	Summary       string             `json:"summary,omitempty"`
	FinalReview   *FinalReview       `json:"final_review,omitempty"`
	CreatedAt     time.Time          `json:"created_at"`
	UpdatedAt     time.Time          `json:"updated_at"`
	CompletedAt   *time.Time         `json:"completed_at,omitempty"`
}

type Store struct {
	root string
	mu   sync.Mutex

	vectorMu       sync.Mutex
	vectorProvider EmbeddingProvider
	vectorModel    string
	vectorMinScore float64
	vectorIndex    *templateVectorIndex
}

func New(root string) (*Store, error) {
	return NewWithOptions(root, StoreOptions{})
}

func NewWithOptions(root string, opts StoreOptions) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("task state root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve task state root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("create task state root: %w", err)
	}
	if err := os.Chmod(abs, 0o700); err != nil {
		return nil, fmt.Errorf("secure task state root: %w", err)
	}
	return newStore(abs, opts), nil
}

func (s *Store) Root() string { return s.root }

func (s *Store) VectorSearchEnabled() bool { return s.vectorProvider != nil }

func (s *Store) Create(title, goal string, conditionTexts []string) (Task, error) {
	return s.CreateWithTemplate(title, goal, conditionTexts, "", "", "", nil)
}

func (s *Store) CreateWithTemplate(title, goal string, conditionTexts []string, templateID, templateVersion, selectedReason string, candidates []TemplateCandidate) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	title = strings.TrimSpace(title)
	goal = strings.TrimSpace(goal)
	if title == "" || goal == "" {
		return Task{}, errors.New("task title and goal are required")
	}
	now := time.Now().UTC()
	var selection *TemplateSelection
	var steps []TaskStep
	if strings.TrimSpace(templateID) != "" {
		if strings.TrimSpace(templateVersion) == "" {
			return Task{}, errors.New("template_version is required when template_id is set")
		}
		template, err := s.loadTemplateLocked("published", templateID, templateVersion)
		if err != nil {
			return Task{}, fmt.Errorf("load active template: %w", err)
		}
		if template.Status != TemplateActive {
			return Task{}, errors.New("only active templates can create tasks")
		}
		conditionTexts = append(append([]string{}, template.CompletionConditions...), conditionTexts...)
		selection = &TemplateSelection{ID: template.ID, Version: template.Version, Hash: template.Hash, SelectedReason: strings.TrimSpace(selectedReason), Candidates: candidates, Snapshot: template}
		steps = make([]TaskStep, 0, len(template.Steps))
		for _, step := range template.Steps {
			steps = append(steps, TaskStep{ID: step.ID, Title: step.Title, Phase: step.Phase, Required: step.Required, DependsOn: append([]string{}, step.DependsOn...), SuggestedCommands: append([]string{}, step.SuggestedCommands...), Substitution: step.Substitution, Status: "pending", Evidence: []StepEvidence{}, UpdatedAt: now})
		}
	}
	conditionTexts = normalizeTexts(conditionTexts)
	if len(conditionTexts) == 0 {
		return Task{}, errors.New("at least one completion condition is required")
	}
	id, err := newID()
	if err != nil {
		return Task{}, err
	}
	task := Task{SchemaVersion: SchemaVersion, ID: id, Title: title, Goal: goal, Status: StatusActive, Phase: PhaseCheck, Conditions: make([]Condition, 0, len(conditionTexts)), Template: selection, Steps: steps, Attempts: []Attempt{}, Events: []Event{{Type: "created", Summary: "task created", CreatedAt: now}}, CreatedAt: now, UpdatedAt: now}
	if selection != nil {
		task.Events = append(task.Events, Event{Type: "template_selected", Summary: selection.ID + "@" + selection.Version + ": " + selection.SelectedReason, CreatedAt: now})
	}
	for i, text := range conditionTexts {
		task.Conditions = append(task.Conditions, Condition{ID: fmt.Sprintf("cond_%02d", i+1), Text: text, CreatedAt: now, Evidence: []Evidence{}})
	}
	if err := s.saveLocked(task); err != nil {
		return Task{}, err
	}
	return task, nil
}

func (s *Store) Get(id string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(id)
}

func (s *Store) List(status Status, limit int) ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	tasks := make([]Task, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "tsk_") || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.root, entry.Name()))
		if err != nil {
			return nil, err
		}
		var task Task
		if err := json.Unmarshal(data, &task); err != nil {
			return nil, fmt.Errorf("decode task %s: %w", entry.Name(), err)
		}
		if status == "" || task.Status == status {
			tasks = append(tasks, task)
		}
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].UpdatedAt.After(tasks[j].UpdatedAt) })
	if len(tasks) > limit {
		tasks = tasks[:limit]
	}
	return tasks, nil
}

func (s *Store) AddCondition(id, text string) (Task, error) {
	return s.mutate(id, func(task *Task, now time.Time) error {
		if err := requireMutable(task); err != nil {
			return err
		}
		text = strings.TrimSpace(text)
		if text == "" {
			return errors.New("condition text is required")
		}
		for _, condition := range task.Conditions {
			if strings.EqualFold(condition.Text, text) {
				return errors.New("completion condition already exists")
			}
		}
		conditionID := fmt.Sprintf("cond_%02d", len(task.Conditions)+1)
		task.Conditions = append(task.Conditions, Condition{ID: conditionID, Text: text, CreatedAt: now, Evidence: []Evidence{}})
		task.Events = append(task.Events, Event{Type: "condition_added", Summary: conditionID + ": " + text, CreatedAt: now})
		return nil
	})
}

func (s *Store) AddEvidence(id, conditionID, summary, source string) (Task, error) {
	return s.mutate(id, func(task *Task, now time.Time) error {
		return applyConditionEvidence(task, conditionID, summary, source, now, true)
	})
}

func (s *Store) Advance(id string) (Task, error) {
	return s.mutate(id, func(task *Task, now time.Time) error {
		return advancePhase(task, now, true)
	})
}

func (s *Store) CompleteStep(id, stepID string, evidence StepEvidence, substituted bool, substitutionReason string) (Task, error) {
	return s.mutate(id, func(task *Task, now time.Time) error {
		return applyStepCompletion(task, StepCompletionUpdate{
			StepID: stepID, Evidence: evidence, Substituted: substituted, SubstitutionReason: substitutionReason,
		}, now, true)
	})
}

func (s *Store) SkipStep(id, stepID, reason string) (Task, error) {
	return s.mutate(id, func(task *Task, now time.Time) error {
		if err := requireActive(task); err != nil {
			return err
		}
		reason = strings.TrimSpace(reason)
		if reason == "" {
			return errors.New("skip reason is required")
		}
		for i := range task.Steps {
			step := &task.Steps[i]
			if step.ID != strings.TrimSpace(stepID) {
				continue
			}
			if step.Required {
				return errors.New("required steps cannot be skipped")
			}
			if step.Phase != task.Phase {
				return fmt.Errorf("step %s belongs to phase %s, current phase is %s", step.ID, step.Phase, task.Phase)
			}
			step.Status = "skipped"
			step.SkipReason = reason
			step.UpdatedAt = now
			task.Events = append(task.Events, Event{Type: "step_skipped", Summary: step.ID + ": " + reason, CreatedAt: now})
			return nil
		}
		return fmt.Errorf("task step %q not found", stepID)
	})
}

func (s *Store) RecordAttempt(id, strategy, outcome, diagnosis, evidence string) (Task, error) {
	return s.mutate(id, func(task *Task, now time.Time) error {
		if err := requireActive(task); err != nil {
			return err
		}
		strategy = strings.TrimSpace(strategy)
		outcome = strings.ToLower(strings.TrimSpace(outcome))
		diagnosis = strings.TrimSpace(diagnosis)
		evidence = strings.TrimSpace(evidence)
		if strategy == "" {
			return errors.New("strategy is required")
		}
		if outcome != "success" && outcome != "failure" {
			return errors.New("outcome must be success or failure")
		}
		if outcome == "failure" && (diagnosis == "" || evidence == "") {
			return errors.New("failed attempts require diagnosis and new evidence")
		}
		attempts := 0
		for _, attempt := range task.Attempts {
			if strings.EqualFold(attempt.Strategy, strategy) {
				attempts++
			}
			if outcome == "failure" && attempt.Outcome == "failure" && strings.EqualFold(strings.TrimSpace(attempt.Evidence), evidence) {
				return errors.New("failed attempt evidence must be new")
			}
		}
		if attempts >= MaxStrategyAttempts {
			return fmt.Errorf("strategy %q reached the maximum of %d attempts; choose a different strategy", strategy, MaxStrategyAttempts)
		}

		// record_attempt 只是失败/尝试记录，不是真实执行动作。
		// 如果连续记录多次，说明 Agent 正在把“写日志”误当成“推进任务”，这里直接打断。
		consecutiveAttempts := 0
		for i := len(task.Events) - 1; i >= 0; i-- {
			if task.Events[i].Type != AttemptRecordedEventType {
				break
			}
			consecutiveAttempts++
		}
		if consecutiveAttempts >= MaxConsecutiveAttemptRecords {
			return errors.New("Stop recording attempts. Execute a real environment action next, then use final_review when the work is actually complete")
		}

		task.Attempts = append(task.Attempts, Attempt{Phase: task.Phase, Strategy: strategy, Outcome: outcome, Diagnosis: diagnosis, Evidence: evidence, CreatedAt: now})
		task.Events = append(task.Events, Event{Type: AttemptRecordedEventType, Summary: strategy + ": " + outcome, CreatedAt: now})
		return nil
	})
}

func (s *Store) Block(id, blocker, evidence string) (Task, error) {
	return s.mutate(id, func(task *Task, now time.Time) error {
		if err := requireMutable(task); err != nil {
			return err
		}
		blocker = strings.TrimSpace(blocker)
		evidence = strings.TrimSpace(evidence)
		if blocker == "" || evidence == "" {
			return errors.New("blocker and evidence are required")
		}
		task.Status = StatusBlocked
		task.Blocker = blocker
		task.Events = append(task.Events, Event{Type: "blocked", Summary: blocker + "; evidence: " + evidence, CreatedAt: now})
		return nil
	})
}

func (s *Store) Resume(id, summary string) (Task, error) {
	return s.mutate(id, func(task *Task, now time.Time) error {
		if task.Status != StatusBlocked {
			return errors.New("only blocked tasks can be resumed")
		}
		summary = strings.TrimSpace(summary)
		if summary == "" {
			return errors.New("resume summary is required")
		}
		task.Status = StatusActive
		task.Blocker = ""
		task.Events = append(task.Events, Event{Type: "resumed", Summary: summary, CreatedAt: now})
		return nil
	})
}

func (s *Store) Complete(id, summary string) (Task, error) {
	return s.mutate(id, func(task *Task, now time.Time) error {
		return completeTask(task, summary, now, true)
	})
}

func (s *Store) FinalReview(id string, input FinalReviewInput) (Task, error) {
	return s.mutate(id, func(task *Task, now time.Time) error {
		return applyFinalReview(task, input, now)
	})
}

func (s *Store) CompleteAfterReview(id, summary string) (Task, error) {
	return s.mutate(id, func(task *Task, now time.Time) error {
		if task.FinalReview == nil || task.FinalReview.Status != FinalReviewPass {
			return errors.New("final_review must pass before complete_after_review")
		}
		if len(task.FinalReview.MissingChecks) > 0 {
			return errors.New("final_review still has missing checks")
		}
		if strings.TrimSpace(summary) == "" {
			summary = task.FinalReview.Summary
		}
		return completeTask(task, summary, now, true)
	})
}

func (s *Store) PhaseCheckpoint(id string, input PhaseCheckpointInput) (Task, error) {
	return s.mutate(id, func(task *Task, now time.Time) error {
		if err := requireActive(task); err != nil {
			return err
		}
		input.Summary = strings.TrimSpace(input.Summary)
		if input.Summary == "" {
			return errors.New("checkpoint summary is required")
		}
		if input.AdvancePhase && input.CompleteTask {
			return errors.New("advance_phase and complete_task are mutually exclusive")
		}
		if len(input.StepCompletions) == 0 && len(input.ConditionEvidence) == 0 && !input.AdvancePhase && !input.CompleteTask {
			return errors.New("checkpoint requires at least one update, phase advance, or task completion")
		}

		phaseBefore := task.Phase
		for _, update := range input.StepCompletions {
			if err := applyStepCompletion(task, update, now, false); err != nil {
				return err
			}
		}
		for _, update := range input.ConditionEvidence {
			if err := applyConditionEvidence(task, update.ConditionID, update.Summary, update.Source, now, false); err != nil {
				return err
			}
		}
		if input.AdvancePhase {
			if err := advancePhase(task, now, false); err != nil {
				return err
			}
		}
		if input.CompleteTask {
			if err := completeTask(task, input.Summary, now, false); err != nil {
				return err
			}
		}

		eventType := "phase_checkpoint"
		eventSummary := fmt.Sprintf("%s: %s; steps=%d; conditions=%d", phaseBefore, input.Summary, len(input.StepCompletions), len(input.ConditionEvidence))
		if input.AdvancePhase {
			eventSummary += "; next=" + string(task.Phase)
		}
		if input.CompleteTask {
			eventType = "completed"
			eventSummary += "; status=completed"
		}
		task.Events = append(task.Events, Event{Type: eventType, Summary: eventSummary, CreatedAt: now})
		return nil
	})
}

func applyConditionEvidence(task *Task, conditionID, summary, source string, now time.Time, emitEvent bool) error {
	if err := requireMutable(task); err != nil {
		return err
	}
	conditionID = strings.TrimSpace(conditionID)
	summary = strings.TrimSpace(summary)
	source = strings.TrimSpace(source)
	if conditionID == "" || summary == "" {
		return errors.New("condition_id and evidence summary are required")
	}
	for i := range task.Conditions {
		if task.Conditions[i].ID != conditionID {
			continue
		}
		task.Conditions[i].Evidence = append(task.Conditions[i].Evidence, Evidence{Summary: summary, Source: source, CreatedAt: now})
		if emitEvent {
			task.Events = append(task.Events, Event{Type: "evidence_added", Summary: conditionID + ": " + summary, CreatedAt: now})
		}
		return nil
	}
	return fmt.Errorf("completion condition %q not found", conditionID)
}

func applyStepCompletion(task *Task, update StepCompletionUpdate, now time.Time, emitEvent bool) error {
	if err := requireActive(task); err != nil {
		return err
	}
	stepID := strings.TrimSpace(update.StepID)
	for i := range task.Steps {
		step := &task.Steps[i]
		if step.ID != stepID {
			continue
		}
		if step.Status == "completed" {
			return errors.New("step is already completed")
		}
		if step.Phase != task.Phase {
			return fmt.Errorf("step %s belongs to phase %s, current phase is %s", step.ID, step.Phase, task.Phase)
		}
		for _, dep := range step.DependsOn {
			if !stepCompleted(task.Steps, dep) {
				return fmt.Errorf("step %s dependency %s is not completed", step.ID, dep)
			}
		}

		evidence := update.Evidence
		evidence.Type = strings.TrimSpace(evidence.Type)
		evidence.Source = strings.TrimSpace(evidence.Source)
		evidence.Result = strings.TrimSpace(evidence.Result)
		evidence.Summary = strings.TrimSpace(evidence.Summary)
		completionSummary := strings.TrimSpace(update.Summary)
		if completionSummary == "" {
			completionSummary = evidence.Summary
		}
		if completionSummary == "" {
			return errors.New("step completion summary is required")
		}
		if phrase := incompleteEvidencePhrase(completionSummary, evidence.Summary, evidence.Result); phrase != "" {
			return fmt.Errorf("step completion still describes incomplete work: %q", phrase)
		}

		evidenceProvided := evidence.Type != "" || evidence.Source != "" || evidence.Result != "" || evidence.ArtifactRef != "" || evidence.SHA256 != ""
		if evidenceProvided {
			// 普通步骤只需要一句完成摘要；但一旦传入结构化 evidence，仍要求字段完整，避免保存半截证据误导恢复上下文。
			if evidence.Type == "" || evidence.Source == "" || evidence.Result == "" || evidence.Summary == "" {
				return errors.New("step evidence requires type, source, result, and summary when provided")
			}
			evidence.CreatedAt = now
		}

		if update.Substituted {
			if step.Substitution == "forbidden" {
				return errors.New("step substitution is forbidden")
			}
			if strings.TrimSpace(update.SubstitutionReason) == "" {
				return errors.New("substitution reason is required")
			}
		}
		step.Status = "completed"
		step.Substituted = update.Substituted
		step.SubstitutionReason = strings.TrimSpace(update.SubstitutionReason)
		if evidenceProvided {
			step.Evidence = append(step.Evidence, evidence)
		}
		step.UpdatedAt = now
		if emitEvent {
			task.Events = append(task.Events, Event{Type: "step_completed", Summary: step.ID + ": " + completionSummary, CreatedAt: now})
		}
		return nil
	}
	return fmt.Errorf("task step %q not found", stepID)
}

func advancePhase(task *Task, now time.Time, emitEvent bool) error {
	if err := requireActive(task); err != nil {
		return err
	}
	if missing := incompleteRequiredSteps(*task, task.Phase); len(missing) > 0 {
		return fmt.Errorf("required steps incomplete for phase %s: %s", task.Phase, strings.Join(missing, ", "))
	}
	for i, phase := range phaseOrder {
		if phase != task.Phase {
			continue
		}
		if i == len(phaseOrder)-1 {
			return errors.New("task is already in closeout; use final_review then complete_after_review")
		}
		task.Phase = phaseOrder[i+1]
		if emitEvent {
			task.Events = append(task.Events, Event{Type: "phase_advanced", Summary: string(task.Phase), CreatedAt: now})
		}
		return nil
	}
	return fmt.Errorf("invalid task phase %q", task.Phase)
}

func applyFinalReview(task *Task, input FinalReviewInput, now time.Time) error {
	if err := requireActive(task); err != nil {
		return err
	}
	status := strings.ToLower(strings.TrimSpace(input.Status))
	if status == "" {
		status = FinalReviewPass
	}
	if status != FinalReviewPass && status != FinalReviewFailed {
		return errors.New("final review status must be pass or failed")
	}
	summary := strings.TrimSpace(input.Summary)
	if summary == "" {
		return errors.New("final review summary is required")
	}
	verifiedFacts := normalizeReviewItems(input.VerifiedFacts)
	openRisks := normalizeReviewItems(input.OpenRisks)
	missingChecks := normalizeReviewItems(input.MissingChecks)
	if status == FinalReviewPass {
		if len(missingChecks) > 0 {
			return errors.New("passing final review cannot have missing checks")
		}
		if len(verifiedFacts) == 0 {
			return errors.New("passing final review requires at least one verified fact")
		}
	} else if len(openRisks) == 0 && len(missingChecks) == 0 {
		return errors.New("failed final review requires open risks or missing checks")
	}

	task.FinalReview = &FinalReview{Status: status, Summary: summary, VerifiedFacts: verifiedFacts, OpenRisks: openRisks, MissingChecks: missingChecks, ReviewedAt: now}
	if status == FinalReviewPass {
		// final_review 是新的常规收尾入口：模板步骤仍保留为恢复线索，
		// 但不再要求模型逐步填 step evidence。最终复查通过后，用复查结果覆盖剩余必填步骤并进入 closeout。
		markPendingStepsReviewed(task, now)
		task.Phase = PhaseCloseout
	}
	task.Events = append(task.Events, Event{Type: "final_review", Summary: status + ": " + summary, CreatedAt: now})
	return nil
}

func markPendingStepsReviewed(task *Task, now time.Time) {
	for i := range task.Steps {
		step := &task.Steps[i]
		if step.Status != "pending" {
			continue
		}
		step.Status = "completed"
		step.Substituted = true
		step.SubstitutionReason = "covered by final_review"
		step.UpdatedAt = now
	}
}

func completeTask(task *Task, summary string, now time.Time, emitEvent bool) error {
	if err := requireActive(task); err != nil {
		return err
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return errors.New("final verification summary is required")
	}
	if task.Phase != PhaseCloseout {
		return errors.New("task must reach closeout before completion")
	}
	if missingSteps := incompleteRequiredSteps(*task, ""); len(missingSteps) > 0 {
		return fmt.Errorf("required template steps incomplete: %s", strings.Join(missingSteps, ", "))
	}
	task.Status = StatusCompleted
	task.Summary = summary
	task.CompletedAt = &now
	if emitEvent {
		task.Events = append(task.Events, Event{Type: "completed", Summary: summary, CreatedAt: now})
	}
	return nil
}

func (s *Store) mutate(id string, fn func(*Task, time.Time) error) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, err := s.loadLocked(id)
	if err != nil {
		return Task{}, err
	}
	now := time.Now().UTC()
	if err := fn(&task, now); err != nil {
		return Task{}, err
	}
	task.UpdatedAt = now
	if err := s.saveLocked(task); err != nil {
		return Task{}, err
	}
	return task, nil
}

func (s *Store) loadLocked(id string) (Task, error) {
	if err := validateID(id); err != nil {
		return Task{}, err
	}
	data, err := os.ReadFile(filepath.Join(s.root, id+".json"))
	if os.IsNotExist(err) {
		return Task{}, fmt.Errorf("task %s not found", id)
	}
	if err != nil {
		return Task{}, err
	}
	var task Task
	if err := json.Unmarshal(data, &task); err != nil {
		return Task{}, fmt.Errorf("decode task %s: %w", id, err)
	}
	if task.SchemaVersion != SchemaVersion {
		return Task{}, fmt.Errorf("unsupported task schema version %d", task.SchemaVersion)
	}
	return task, nil
}

func (s *Store) saveLocked(task Task) error {
	if err := validateID(task.ID); err != nil {
		return err
	}
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(s.root, ".task-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	target := filepath.Join(s.root, task.ID+".json")
	if err := os.Rename(tmpName, target); err != nil {
		return err
	}
	return os.Chmod(target, 0o600)
}

func normalizeTexts(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		duplicate := false
		for _, existing := range out {
			if similarConditionText(existing, value) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func similarConditionText(a, b string) bool {
	a = conditionCompareText(a)
	b = conditionCompareText(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	aRuneLen := len([]rune(a))
	bRuneLen := len([]rune(b))
	if aRuneLen >= 8 && bRuneLen >= 8 && (strings.Contains(a, b) || strings.Contains(b, a)) {
		return true
	}
	if aRuneLen < 8 || bRuneLen < 8 {
		return false
	}
	aPairs := conditionBigramSet(a)
	bPairs := conditionBigramSet(b)
	if len(aPairs) == 0 || len(bPairs) == 0 {
		return false
	}
	shared := 0
	for pair := range aPairs {
		if _, ok := bPairs[pair]; ok {
			shared++
		}
	}
	return float64(shared*2)/float64(len(aPairs)+len(bPairs)) >= 0.62
}

func conditionCompareText(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		switch r {
		case '，', '。', '！', '？', '、', '；', '：', '（', '）', '(', ')', '[', ']', '【', '】', '{', '}', '《', '》', '“', '”', '‘', '’', '"', '\'', '`':
			continue
		}
		if r > ' ' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func conditionBigramSet(value string) map[string]struct{} {
	runes := []rune(value)
	out := map[string]struct{}{}
	if len(runes) < 2 {
		if value != "" {
			out[value] = struct{}{}
		}
		return out
	}
	for i := 0; i < len(runes)-1; i++ {
		out[string(runes[i:i+2])] = struct{}{}
	}
	return out
}

func requireMutable(task *Task) error {
	if task.Status == StatusCompleted {
		return errors.New("completed tasks are immutable")
	}
	return nil
}

func requireActive(task *Task) error {
	if task.Status != StatusActive {
		return fmt.Errorf("task status must be active, got %s", task.Status)
	}
	return nil
}

func incompleteEvidencePhrase(values ...string) string {
	// step completion 代表“这一步完成了”，证据里如果还写着待检查/未验证，
	// 大概率是把部分检查误标为完成，必须拒绝。
	markers := []string{
		"pending", "still required", "still needed", "not yet", "not checked", "not verified", "not executed", "remaining",
		"待检查", "待验证", "待确认", "待处理", "待执行", "待完成",
		"仍待", "仍需", "还需", "还没", "还未",
		"尚未", "未完成", "未验证", "未检查", "未执行", "未确认",
	}
	for _, value := range values {
		lower := strings.ToLower(strings.TrimSpace(value))
		if lower == "" {
			continue
		}
		for _, marker := range markers {
			if strings.Contains(lower, marker) {
				return marker
			}
		}
	}
	return ""
}

func incompleteRequiredSteps(task Task, phase Phase) []string {
	var out []string
	for _, step := range task.Steps {
		if step.Required && (phase == "" || step.Phase == phase) && step.Status != "completed" {
			out = append(out, step.ID)
		}
	}
	return out
}

func normalizeReviewItems(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func stepCompleted(steps []TaskStep, id string) bool {
	for _, step := range steps {
		if step.ID == id {
			return step.Status == "completed"
		}
	}
	return false
}

func validateID(id string) error {
	if !strings.HasPrefix(id, "tsk_") || len(id) != 20 {
		return errors.New("invalid task id")
	}
	for _, ch := range id[4:] {
		if !strings.ContainsRune("0123456789abcdef", ch) {
			return errors.New("invalid task id")
		}
	}
	return nil
}

func newID() (string, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate task id: %w", err)
	}
	return "tsk_" + hex.EncodeToString(raw), nil
}

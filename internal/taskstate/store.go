package taskstate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/uvwt/agentdock/internal/atomicfile"
	"github.com/uvwt/agentdock/internal/filelock"
	"github.com/uvwt/agentdock/internal/textutil"
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
	Status        string    `json:"status"`
	Summary       string    `json:"summary"`
	VerifiedFacts []string  `json:"verified_facts,omitempty"`
	OpenRisks     []string  `json:"open_risks,omitempty"`
	MissingChecks []string  `json:"missing_checks,omitempty"`
	ReviewedAt    time.Time `json:"reviewed_at"`
}

type Event struct {
	Type      string    `json:"type"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

type Task struct {
	SchemaVersion   int                 `json:"schema_version"`
	ID              string              `json:"id"`
	Title           string              `json:"title"`
	Goal            string              `json:"goal"`
	Status          Status              `json:"status"`
	Phase           Phase               `json:"phase"`
	Conditions      []Condition         `json:"conditions"`
	Template        *TemplateSelection  `json:"template,omitempty"` // 仅用于读取旧任务状态。
	SourceTemplates []TemplateReference `json:"source_templates,omitempty"`
	Steps           []TaskStep          `json:"steps,omitempty"`
	Events          []Event             `json:"events"`
	Blocker         string              `json:"blocker,omitempty"`
	Summary         string              `json:"summary,omitempty"`
	FinalReview     *FinalReview        `json:"final_review,omitempty"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
	CompletedAt     *time.Time          `json:"completed_at,omitempty"`
}

type Store struct {
	root string
	mu   sync.Mutex
}

func New(root string) (*Store, error) {
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
	return &Store{root: abs}, nil
}

func (s *Store) acquireStoreLock() (func(), error) {
	s.mu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	releaseFileLock, err := filelock.Acquire(ctx, filepath.Join(s.root, ".store.lock"))
	cancel()
	if err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("lock task state: %w", err)
	}
	return func() {
		releaseFileLock()
		s.mu.Unlock()
	}, nil
}

func (s *Store) Root() string { return s.root }

func (s *Store) Create(title, goal string, conditionTexts []string, steps []TaskStepInput, sourceTemplates []TemplateReference) (Task, error) {
	release, err := s.acquireStoreLock()
	if err != nil {
		return Task{}, err
	}
	defer release()

	title = strings.TrimSpace(title)
	goal = strings.TrimSpace(goal)
	if title == "" || goal == "" {
		return Task{}, errors.New("task title and goal are required")
	}
	if err := validateTextLimit("task title", title, maxTaskTitleBytes); err != nil {
		return Task{}, err
	}
	if err := validateTextLimit("task goal", goal, maxTaskGoalBytes); err != nil {
		return Task{}, err
	}
	conditionTexts = normalizeTexts(conditionTexts)
	if len(conditionTexts) == 0 {
		return Task{}, errors.New("at least one completion condition is required")
	}
	if len(conditionTexts) > maxTaskConditions {
		return Task{}, fmt.Errorf("task completion conditions cannot exceed %d", maxTaskConditions)
	}
	for _, condition := range conditionTexts {
		if err := validateTextLimit("task completion condition", condition, maxTaskConditionBytes); err != nil {
			return Task{}, err
		}
	}

	now := time.Now().UTC()
	taskSteps, err := normalizeTaskSteps(steps, now)
	if err != nil {
		return Task{}, err
	}
	templateRefs, err := normalizeTemplateReferences(sourceTemplates)
	if err != nil {
		return Task{}, err
	}
	id, err := newID()
	if err != nil {
		return Task{}, err
	}
	phase := PhaseCheck
	if len(taskSteps) > 0 {
		phase = taskSteps[0].Phase
	}
	task := Task{
		SchemaVersion:   SchemaVersion,
		ID:              id,
		Title:           title,
		Goal:            goal,
		Status:          StatusActive,
		Phase:           phase,
		Conditions:      make([]Condition, 0, len(conditionTexts)),
		SourceTemplates: templateRefs,
		Steps:           taskSteps,
		Events:          []Event{{Type: "created", Summary: "task created", CreatedAt: now}},
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if len(templateRefs) > 0 {
		ids := make([]string, 0, len(templateRefs))
		for _, ref := range templateRefs {
			ids = append(ids, ref.ID+"@"+ref.Version)
		}
		appendTaskEvent(&task, Event{Type: "templates_selected", Summary: strings.Join(ids, ", "), CreatedAt: now})
	}
	for i, text := range conditionTexts {
		task.Conditions = append(task.Conditions, Condition{ID: fmt.Sprintf("cond_%02d", i+1), Text: text, CreatedAt: now})
	}
	if err := s.saveLocked(task); err != nil {
		return Task{}, err
	}
	return task, nil
}

func (s *Store) Get(id string) (Task, error) {
	release, err := s.acquireStoreLock()
	if err != nil {
		return Task{}, err
	}
	defer release()
	return s.loadLocked(id)
}

func (s *Store) Delete(id string) (Task, error) {
	release, err := s.acquireStoreLock()
	if err != nil {
		return Task{}, err
	}
	defer release()

	task, err := s.loadLocked(id)
	if err != nil {
		return Task{}, err
	}
	if err := os.Remove(filepath.Join(s.root, id+".json")); err != nil {
		return Task{}, fmt.Errorf("delete task %s: %w", id, err)
	}
	return task, nil
}

func (s *Store) List(status Status, limit int) ([]Task, error) {
	release, err := s.acquireStoreLock()
	if err != nil {
		return nil, err
	}
	defer release()
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
		data, err := readTaskStateFile(filepath.Join(s.root, entry.Name()))
		if err != nil {
			slog.Warn("skip unreadable task state", "file", entry.Name(), "error", err)
			continue
		}
		task, err := decodeTask(data, entry.Name())
		if err != nil {
			slog.Warn("skip invalid task state", "file", entry.Name(), "error", err)
			continue
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

func (s *Store) Checkpoint(id, stepID, status, summary string) (Task, error) {
	return s.mutate(id, func(task *Task, now time.Time) error {
		if err := requireActive(task); err != nil {
			return err
		}
		if err := requireFinalReviewOpen(task); err != nil {
			return err
		}
		stepID = strings.TrimSpace(stepID)
		status = strings.ToLower(strings.TrimSpace(status))
		summary = strings.TrimSpace(summary)
		if stepID == "" || summary == "" {
			return errors.New("step_id and summary are required")
		}
		if err := validateTextLimit("checkpoint summary", summary, maxTaskSummaryBytes); err != nil {
			return err
		}
		if status != StepPending && status != StepInProgress && status != StepCompleted {
			return errors.New("step status must be pending, in_progress, or completed")
		}

		stepIndex := -1
		for i := range task.Steps {
			if task.Steps[i].ID == stepID {
				stepIndex = i
				break
			}
		}
		if stepIndex < 0 {
			return fmt.Errorf("task step %s not found", stepID)
		}
		step := &task.Steps[stepIndex]
		if step.Status == status && task.Summary == summary {
			return nil
		}
		if !validStepTransition(step.Status, status) {
			return fmt.Errorf("cannot move step %s from %s to %s", step.ID, step.Status, status)
		}
		if status == StepInProgress {
			for i := range task.Steps {
				if i != stepIndex && task.Steps[i].Status == StepInProgress {
					return fmt.Errorf("step %s is already in progress", task.Steps[i].ID)
				}
			}
		}
		step.Status = status
		step.UpdatedAt = now
		if task.FinalReview != nil && task.FinalReview.Status == FinalReviewFailed {
			task.FinalReview = nil
		}
		task.Phase = step.Phase
		task.Summary = summary
		appendTaskEvent(task, Event{Type: "checkpoint", Summary: step.ID + "=" + status + ": " + summary, CreatedAt: now})
		return nil
	})
}

func (s *Store) BatchCheckpoint(id string, completedStepIDs []string, currentStepID, summary string) (Task, error) {
	return s.mutate(id, func(task *Task, now time.Time) error {
		if err := requireActive(task); err != nil {
			return err
		}
		if err := requireFinalReviewOpen(task); err != nil {
			return err
		}

		completedStepIDs = normalizeStepIDs(completedStepIDs)
		currentStepID = strings.TrimSpace(currentStepID)
		summary = strings.TrimSpace(summary)
		if len(completedStepIDs) == 0 && currentStepID == "" {
			return errors.New("completed_step_ids or current_step_id is required")
		}
		if summary == "" {
			return errors.New("checkpoint summary is required")
		}
		if err := validateTextLimit("checkpoint summary", summary, maxTaskSummaryBytes); err != nil {
			return err
		}

		// 批量 checkpoint 必须先验证所有目标步骤，再统一写入，避免无效请求留下半更新状态。
		stepIndexes := make(map[string]int, len(task.Steps))
		for i := range task.Steps {
			stepIndexes[task.Steps[i].ID] = i
		}

		completedSet := make(map[string]struct{}, len(completedStepIDs))
		for _, stepID := range completedStepIDs {
			stepIndex, ok := stepIndexes[stepID]
			if !ok {
				return fmt.Errorf("task step %s not found", stepID)
			}
			if !validStepTransition(task.Steps[stepIndex].Status, StepCompleted) {
				return fmt.Errorf("cannot move step %s from %s to %s", stepID, task.Steps[stepIndex].Status, StepCompleted)
			}
			completedSet[stepID] = struct{}{}
		}

		currentStepIndex := -1
		if currentStepID != "" {
			if _, overlaps := completedSet[currentStepID]; overlaps {
				return fmt.Errorf("step %s cannot be both completed and current", currentStepID)
			}
			stepIndex, ok := stepIndexes[currentStepID]
			if !ok {
				return fmt.Errorf("task step %s not found", currentStepID)
			}
			if !validStepTransition(task.Steps[stepIndex].Status, StepInProgress) {
				return fmt.Errorf("cannot move step %s from %s to %s", currentStepID, task.Steps[stepIndex].Status, StepInProgress)
			}
			for i := range task.Steps {
				if task.Steps[i].Status != StepInProgress || i == stepIndex {
					continue
				}
				if _, completing := completedSet[task.Steps[i].ID]; !completing {
					return fmt.Errorf("step %s is already in progress", task.Steps[i].ID)
				}
			}
			currentStepIndex = stepIndex
		}

		for _, stepID := range completedStepIDs {
			step := &task.Steps[stepIndexes[stepID]]
			step.Status = StepCompleted
			step.UpdatedAt = now
		}
		if currentStepIndex >= 0 {
			step := &task.Steps[currentStepIndex]
			step.Status = StepInProgress
			step.UpdatedAt = now
			task.Phase = step.Phase
		} else if len(completedStepIDs) > 0 {
			task.Phase = task.Steps[stepIndexes[completedStepIDs[len(completedStepIDs)-1]]].Phase
		}
		if task.FinalReview != nil && task.FinalReview.Status == FinalReviewFailed {
			task.FinalReview = nil
		}
		task.Summary = summary
		eventSummary := "completed=[" + strings.Join(completedStepIDs, ",") + "]"
		if currentStepID != "" {
			eventSummary += ", current=" + currentStepID
		}
		appendTaskEvent(task, Event{Type: "checkpoint", Summary: eventSummary + ": " + summary, CreatedAt: now})
		return nil
	})
}

func (s *Store) Block(id, summary string) (Task, error) {
	return s.mutate(id, func(task *Task, now time.Time) error {
		if err := requireMutable(task); err != nil {
			return err
		}
		if err := requireFinalReviewOpen(task); err != nil {
			return err
		}
		summary = strings.TrimSpace(summary)
		if summary == "" {
			return errors.New("block summary is required")
		}
		if err := validateTextLimit("block summary", summary, maxTaskSummaryBytes); err != nil {
			return err
		}
		task.Status = StatusBlocked
		task.Blocker = summary
		task.Summary = summary
		appendTaskEvent(task, Event{Type: "blocked", Summary: summary, CreatedAt: now})
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
		if err := validateTextLimit("resume summary", summary, maxTaskSummaryBytes); err != nil {
			return err
		}
		task.Status = StatusActive
		task.Blocker = ""
		task.Summary = summary
		appendTaskEvent(task, Event{Type: "resumed", Summary: summary, CreatedAt: now})
		return nil
	})
}

func (s *Store) FinalReview(id string, input FinalReviewInput) (Task, error) {
	return s.mutate(id, func(task *Task, now time.Time) error {
		return applyFinalReview(task, input, now)
	})
}

func (s *Store) Complete(id string) (Task, error) {
	return s.mutate(id, func(task *Task, now time.Time) error {
		if task.FinalReview == nil || task.FinalReview.Status != FinalReviewPass {
			return errors.New("final_review must pass before complete")
		}
		return completeTask(task, task.FinalReview.Summary, now, true)
	})
}

func applyFinalReview(task *Task, input FinalReviewInput, now time.Time) error {
	if err := requireActive(task); err != nil {
		return err
	}
	if err := requireFinalReviewOpen(task); err != nil {
		return err
	}
	status := strings.ToLower(strings.TrimSpace(input.Status))
	if status != FinalReviewPass && status != FinalReviewFailed {
		return errors.New("final review status must be pass or failed")
	}
	summary := strings.TrimSpace(input.Summary)
	if summary == "" {
		return errors.New("final review summary is required")
	}
	if err := validateTextLimit("final review summary", summary, maxTaskSummaryBytes); err != nil {
		return err
	}
	verifiedFacts := normalizeReviewItems(input.VerifiedFacts)
	openRisks := normalizeReviewItems(input.OpenRisks)
	missingChecks := normalizeReviewItems(input.MissingChecks)
	for label, items := range map[string][]string{"verified fact": verifiedFacts, "open risk": openRisks, "missing check": missingChecks} {
		if len(items) > maxTaskReviewItems {
			return fmt.Errorf("final review %s items cannot exceed %d", label, maxTaskReviewItems)
		}
		for _, item := range items {
			if err := validateTextLimit("final review "+label, item, maxTaskReviewItemBytes); err != nil {
				return err
			}
		}
	}
	if status == FinalReviewPass {
		if len(verifiedFacts) == 0 {
			return errors.New("passing final review requires at least one verified fact")
		}
		if pending := incompleteStepIDs(task.Steps); len(pending) > 0 {
			return fmt.Errorf("passing final review requires all task steps completed: %s", strings.Join(pending, ", "))
		}
	} else if len(openRisks) == 0 && len(missingChecks) == 0 {
		return errors.New("failed final review requires at least one risk")
	}

	task.FinalReview = &FinalReview{Status: status, Summary: summary, VerifiedFacts: verifiedFacts, OpenRisks: openRisks, MissingChecks: missingChecks, ReviewedAt: now}
	if status == FinalReviewPass {
		task.Phase = PhaseCloseout
	}
	task.Summary = summary
	appendTaskEvent(task, Event{Type: "final_review", Summary: status + ": " + summary, CreatedAt: now})
	return nil
}

func completeTask(task *Task, summary string, now time.Time, emitEvent bool) error {
	if err := requireActive(task); err != nil {
		return err
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return errors.New("final verification summary is required")
	}
	if err := validateTextLimit("final verification summary", summary, maxTaskSummaryBytes); err != nil {
		return err
	}
	if task.Phase != PhaseCloseout {
		return errors.New("task must reach closeout before completion")
	}
	task.Status = StatusCompleted
	task.Summary = summary
	task.CompletedAt = &now
	if emitEvent {
		appendTaskEvent(task, Event{Type: "completed", Summary: summary, CreatedAt: now})
	}
	return nil
}

func (s *Store) mutate(id string, fn func(*Task, time.Time) error) (Task, error) {
	release, err := s.acquireStoreLock()
	if err != nil {
		return Task{}, err
	}
	defer release()
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
	data, err := readTaskStateFile(filepath.Join(s.root, id+".json"))
	if os.IsNotExist(err) {
		return Task{}, fmt.Errorf("%w: %s", ErrTaskNotFound, id)
	}
	if err != nil {
		return Task{}, err
	}
	task, err := decodeTask(data, id)
	if err != nil {
		return Task{}, err
	}
	return task, nil
}

func (s *Store) saveLocked(task Task) error {
	if err := validateID(task.ID); err != nil {
		return err
	}
	if len(task.Events) > maxTaskEvents {
		task.Events = append([]Event(nil), task.Events[len(task.Events)-maxTaskEvents:]...)
	}
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > maxTaskStateFileBytes {
		return fmt.Errorf("task state exceeds %d bytes", maxTaskStateFileBytes)
	}
	target := filepath.Join(s.root, task.ID+".json")
	return atomicfile.Write(target, data, 0o600)
}

func normalizeTaskSteps(values []TaskStepInput, now time.Time) ([]TaskStep, error) {
	if len(values) > 12 {
		return nil, errors.New("task steps cannot exceed 12")
	}
	seen := make(map[string]struct{}, len(values))
	steps := make([]TaskStep, 0, len(values))
	for _, value := range values {
		id := strings.TrimSpace(value.ID)
		title := strings.TrimSpace(value.Title)
		if id == "" || title == "" {
			return nil, errors.New("each task step requires id and title")
		}
		if err := validateTextLimit("task step title", title, maxTaskStepTitleBytes); err != nil {
			return nil, err
		}
		if !validStepID(id) {
			return nil, fmt.Errorf("invalid task step id %q", id)
		}
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("duplicate task step id %q", id)
		}
		phase := value.Phase
		if phase == "" {
			phase = PhaseExecute
		}
		if phase != PhaseCheck && phase != PhaseExecute && phase != PhaseVerify && phase != PhaseCloseout {
			return nil, fmt.Errorf("invalid task step phase %q", phase)
		}
		seen[id] = struct{}{}
		steps = append(steps, TaskStep{ID: id, Title: title, Phase: phase, Status: StepPending, UpdatedAt: now})
	}
	return steps, nil
}

func normalizeTemplateReferences(values []TemplateReference) ([]TemplateReference, error) {
	if len(values) > 3 {
		return nil, errors.New("source templates cannot exceed 3")
	}
	seen := make(map[string]struct{}, len(values))
	refs := make([]TemplateReference, 0, len(values))
	for _, value := range values {
		value.ID = strings.TrimSpace(value.ID)
		value.Version = strings.TrimSpace(value.Version)
		value.Hash = strings.TrimSpace(value.Hash)
		if value.ID == "" || value.Version == "" {
			return nil, errors.New("source template id and version are required")
		}
		if _, exists := seen[value.ID]; exists {
			return nil, fmt.Errorf("duplicate source template %q", value.ID)
		}
		seen[value.ID] = struct{}{}
		refs = append(refs, value)
	}
	return refs, nil
}

func validStepID(id string) bool {
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func validStepTransition(from, to string) bool {
	if from == to {
		return true
	}
	switch from {
	case StepPending:
		return to == StepInProgress || to == StepCompleted
	case StepInProgress:
		return to == StepCompleted
	default:
		return false
	}
}

func incompleteStepIDs(steps []TaskStep) []string {
	ids := make([]string, 0)
	for _, step := range steps {
		if step.Status != StepCompleted {
			ids = append(ids, step.ID)
		}
	}
	return ids
}

func normalizeStepIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
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

func requireFinalReviewOpen(task *Task) error {
	if task.FinalReview != nil && task.FinalReview.Status == FinalReviewPass {
		return errors.New("task already passed final review")
	}
	return nil
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

func readTaskStateFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxTaskStateFileBytes {
		return nil, fmt.Errorf("task state exceeds %d bytes", maxTaskStateFileBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxTaskStateFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxTaskStateFileBytes {
		return nil, fmt.Errorf("task state exceeds %d bytes", maxTaskStateFileBytes)
	}
	return data, nil
}

func decodeTask(data []byte, label string) (Task, error) {
	var task Task
	if err := json.Unmarshal(data, &task); err != nil {
		return Task{}, fmt.Errorf("decode task %s: %w", label, err)
	}
	if task.SchemaVersion != SchemaVersion {
		return Task{}, fmt.Errorf("unsupported task schema version %d", task.SchemaVersion)
	}
	if len(task.Events) > maxTaskEvents {
		task.Events = append([]Event(nil), task.Events[len(task.Events)-maxTaskEvents:]...)
	}
	return task, nil
}

func appendTaskEvent(task *Task, event Event) {
	event.Summary = textutil.SafeTruncateString(strings.TrimSpace(event.Summary), maxTaskEventSummaryBytes).Text
	task.Events = append(task.Events, event)
	if len(task.Events) > maxTaskEvents {
		task.Events = append([]Event(nil), task.Events[len(task.Events)-maxTaskEvents:]...)
	}
}

func validateTextLimit(label, value string, maxBytes int) error {
	if len([]byte(value)) > maxBytes {
		return fmt.Errorf("%s exceeds %d bytes", label, maxBytes)
	}
	return nil
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

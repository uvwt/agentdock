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

	"github.com/uvwt/agentdock/internal/atomicfile"
)

const (
	SchemaVersion     = 1
	FinalReviewPass   = "pass"
	FinalReviewFailed = "failed"
)

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

func (s *Store) Root() string { return s.root }

func (s *Store) Create(title, goal string, conditionTexts []string, steps []TaskStepInput, sourceTemplates []TemplateReference) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	title = strings.TrimSpace(title)
	goal = strings.TrimSpace(goal)
	if title == "" || goal == "" {
		return Task{}, errors.New("task title and goal are required")
	}
	conditionTexts = normalizeTexts(conditionTexts)
	if len(conditionTexts) == 0 {
		return Task{}, errors.New("at least one completion condition is required")
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
		task.Events = append(task.Events, Event{Type: "templates_selected", Summary: strings.Join(ids, ", "), CreatedAt: now})
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

func (s *Store) Checkpoint(id, stepID, status, summary string) (Task, error) {
	return s.mutate(id, func(task *Task, now time.Time) error {
		if err := requireActive(task); err != nil {
			return err
		}
		if task.FinalReview != nil && task.FinalReview.Status == FinalReviewPass {
			return errors.New("task already passed final review")
		}
		stepID = strings.TrimSpace(stepID)
		status = strings.ToLower(strings.TrimSpace(status))
		summary = strings.TrimSpace(summary)
		if stepID == "" || summary == "" {
			return errors.New("step_id and summary are required")
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
		task.Events = append(task.Events, Event{Type: "checkpoint", Summary: step.ID + "=" + status + ": " + summary, CreatedAt: now})
		return nil
	})
}

func (s *Store) Block(id, summary string) (Task, error) {
	return s.mutate(id, func(task *Task, now time.Time) error {
		if err := requireMutable(task); err != nil {
			return err
		}
		summary = strings.TrimSpace(summary)
		if summary == "" {
			return errors.New("block summary is required")
		}
		task.Status = StatusBlocked
		task.Blocker = summary
		task.Summary = summary
		task.Events = append(task.Events, Event{Type: "blocked", Summary: summary, CreatedAt: now})
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
	status := strings.ToLower(strings.TrimSpace(input.Status))
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
	task.Events = append(task.Events, Event{Type: "final_review", Summary: status + ": " + summary, CreatedAt: now})
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
	if task.Phase != PhaseCloseout {
		return errors.New("task must reach closeout before completion")
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

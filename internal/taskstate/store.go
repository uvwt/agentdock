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
	SchemaVersion int                `json:"schema_version"`
	ID            string             `json:"id"`
	Title         string             `json:"title"`
	Goal          string             `json:"goal"`
	Status        Status             `json:"status"`
	Phase         Phase              `json:"phase"`
	Conditions    []Condition        `json:"conditions"`
	Template      *TemplateSelection `json:"template,omitempty"`
	Steps         []TaskStep         `json:"steps,omitempty"`
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

func (s *Store) Create(title, goal string, conditionTexts []string) (Task, error) {
	return s.createTask(title, goal, conditionTexts, nil, nil)
}

func (s *Store) CreateFromTemplate(title, goal string, conditionTexts []string, template Template, selectedReason string, candidates []TemplateCandidate) (Task, error) {
	if template.Status != TemplateActive {
		return Task{}, errors.New("only active templates can create tasks")
	}
	selection := &candidatesWithReason{reason: selectedReason, candidates: candidates}
	return s.createTask(title, goal, conditionTexts, &template, selection)
}

type candidatesWithReason struct {
	reason     string
	candidates []TemplateCandidate
}

func (s *Store) createTask(title, goal string, conditionTexts []string, template *Template, selectionInfo *candidatesWithReason) (Task, error) {
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
	if template != nil {
		conditionTexts = append(append([]string{}, template.CompletionConditions...), conditionTexts...)
		if selectionInfo == nil {
			return Task{}, errors.New("template selection metadata is required")
		}
		selection = &TemplateSelection{
			ID:             template.ID,
			Version:        template.Version,
			Hash:           template.Hash,
			SelectedReason: strings.TrimSpace(selectionInfo.reason),
			Candidates:     append([]TemplateCandidate(nil), selectionInfo.candidates...),
		}
		steps = make([]TaskStep, 0, len(template.Steps))
		for _, step := range template.Steps {
			steps = append(steps, TaskStep{ID: step.ID, Title: step.Title, Phase: step.Phase, Status: "pending", UpdatedAt: now})
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
	task := Task{SchemaVersion: SchemaVersion, ID: id, Title: title, Goal: goal, Status: StatusActive, Phase: PhaseCheck, Conditions: make([]Condition, 0, len(conditionTexts)), Template: selection, Steps: steps, Events: []Event{{Type: "created", Summary: "task created", CreatedAt: now}}, CreatedAt: now, UpdatedAt: now}
	if selection != nil {
		task.Events = append(task.Events, Event{Type: "template_selected", Summary: selection.ID + "@" + selection.Version + ": " + selection.SelectedReason, CreatedAt: now})
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
		// final_review 是常规收尾入口：模板步骤只作为流程锚点和恢复线索，
		// 不再承担必填步骤、依赖或替代规则等工作流引擎职责。
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

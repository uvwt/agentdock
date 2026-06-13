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
	SchemaVersion       = 1
	MaxStrategyAttempts = 2
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
	SchemaVersion int         `json:"schema_version"`
	ID            string      `json:"id"`
	Title         string      `json:"title"`
	Goal          string      `json:"goal"`
	Status        Status      `json:"status"`
	Phase         Phase       `json:"phase"`
	Conditions    []Condition `json:"conditions"`
	Attempts      []Attempt   `json:"attempts"`
	Events        []Event     `json:"events"`
	Blocker       string      `json:"blocker,omitempty"`
	Summary       string      `json:"summary,omitempty"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
	CompletedAt   *time.Time  `json:"completed_at,omitempty"`
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
	id, err := newID()
	if err != nil {
		return Task{}, err
	}
	task := Task{
		SchemaVersion: SchemaVersion,
		ID:            id,
		Title:         title,
		Goal:          goal,
		Status:        StatusActive,
		Phase:         PhaseCheck,
		Conditions:    make([]Condition, 0, len(conditionTexts)),
		Attempts:      []Attempt{},
		Events:        []Event{{Type: "created", Summary: "task created", CreatedAt: now}},
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	for i, text := range conditionTexts {
		task.Conditions = append(task.Conditions, Condition{
			ID: fmt.Sprintf("cond_%02d", i+1), Text: text, CreatedAt: now, Evidence: []Evidence{},
		})
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
			if task.Conditions[i].ID == conditionID {
				task.Conditions[i].Evidence = append(task.Conditions[i].Evidence, Evidence{Summary: summary, Source: source, CreatedAt: now})
				task.Events = append(task.Events, Event{Type: "evidence_added", Summary: conditionID + ": " + summary, CreatedAt: now})
				return nil
			}
		}
		return fmt.Errorf("completion condition %q not found", conditionID)
	})
}

func (s *Store) Advance(id string) (Task, error) {
	return s.mutate(id, func(task *Task, now time.Time) error {
		if err := requireActive(task); err != nil {
			return err
		}
		for i, phase := range phaseOrder {
			if phase != task.Phase {
				continue
			}
			if i == len(phaseOrder)-1 {
				return errors.New("task is already in closeout; use complete after all conditions have evidence")
			}
			task.Phase = phaseOrder[i+1]
			task.Events = append(task.Events, Event{Type: "phase_advanced", Summary: string(task.Phase), CreatedAt: now})
			return nil
		}
		return fmt.Errorf("invalid task phase %q", task.Phase)
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
		task.Attempts = append(task.Attempts, Attempt{Phase: task.Phase, Strategy: strategy, Outcome: outcome, Diagnosis: diagnosis, Evidence: evidence, CreatedAt: now})
		task.Events = append(task.Events, Event{Type: "attempt_recorded", Summary: strategy + ": " + outcome, CreatedAt: now})
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
		if err := requireActive(task); err != nil {
			return err
		}
		if task.Phase != PhaseCloseout {
			return errors.New("task must reach closeout before completion")
		}
		missing := make([]string, 0)
		for _, condition := range task.Conditions {
			if len(condition.Evidence) == 0 {
				missing = append(missing, condition.ID)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("completion conditions missing evidence: %s", strings.Join(missing, ", "))
		}
		summary = strings.TrimSpace(summary)
		if summary == "" {
			return errors.New("completion summary is required")
		}
		task.Status = StatusCompleted
		task.Summary = summary
		task.CompletedAt = &now
		task.Events = append(task.Events, Event{Type: "completed", Summary: summary, CreatedAt: now})
		return nil
	})
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
		seen[key] = struct{}{}
		out = append(out, value)
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

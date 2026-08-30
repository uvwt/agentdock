package taskstate

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *Store) Create(title, goal string, conditionTexts []string, steps []TaskStepInput, sourceTemplates []TemplateReference) (Task, error) {
	return s.CreateWithContext(title, goal, "", "", conditionTexts, steps, sourceTemplates)
}

func (s *Store) CreateWithContext(title, goal, project, device string, conditionTexts []string, steps []TaskStepInput, sourceTemplates []TemplateReference, initialBindings ...EvolutionBinding) (Task, error) {
	release, err := s.acquireStoreLock()
	if err != nil {
		return Task{}, err
	}
	defer release()

	title = strings.TrimSpace(title)
	goal = strings.TrimSpace(goal)
	project = strings.ToLower(strings.TrimSpace(project))
	device = strings.TrimSpace(device)
	if title == "" || goal == "" {
		return Task{}, errors.New("task title and goal are required")
	}
	if err := validateTextLimit("task title", title, maxTaskTitleBytes); err != nil {
		return Task{}, err
	}
	if err := validateTextLimit("task goal", goal, maxTaskGoalBytes); err != nil {
		return Task{}, err
	}
	if len(project) > 256 || len(device) > 256 {
		return Task{}, errors.New("task project/device context is too long")
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
	bindings := make([]EvolutionBinding, 0, len(initialBindings))
	seenBindings := make(map[string]EvolutionBinding, len(initialBindings))
	for _, binding := range initialBindings {
		binding, err = normalizeEvolutionBinding(binding)
		if err != nil {
			return Task{}, err
		}
		if existing, ok := seenBindings[binding.EvolutionID]; ok {
			if existing.OnSuccess == binding.OnSuccess && existing.OnFailure == binding.OnFailure {
				continue
			}
			return Task{}, errors.New("evolution is already bound with different learning check semantics")
		}
		binding.BoundAt = now
		seenBindings[binding.EvolutionID] = binding
		bindings = append(bindings, binding)
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
		SchemaVersion:     SchemaVersion,
		ID:                id,
		Title:             title,
		Goal:              goal,
		Project:           project,
		Device:            device,
		Status:            StatusActive,
		Phase:             phase,
		Conditions:        make([]Condition, 0, len(conditionTexts)),
		SourceTemplates:   templateRefs,
		Steps:             taskSteps,
		EvolutionBindings: bindings,
		Events:            []Event{{Type: "created", Summary: "task created", CreatedAt: now}},
		CreatedAt:         now,
		UpdatedAt:         now,
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

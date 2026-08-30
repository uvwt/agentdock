package taskstate

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

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

	reviewRevision, err := newOpaqueID("rev_")
	if err != nil {
		return err
	}
	task.FinalReview = &FinalReview{Status: status, Summary: summary, VerifiedFacts: verifiedFacts, OpenRisks: openRisks, MissingChecks: missingChecks, ReviewRevision: reviewRevision, ReviewedAt: now}
	task.EvolutionCandidates = nil
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

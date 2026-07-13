package taskstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTaskLifecyclePersistsLiveProgress(t *testing.T) {
	root := t.TempDir()
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Create(
		"Deploy AgentDock",
		"deploy and verify",
		[]string{"health is 200", "tool call succeeds"},
		[]TaskStepInput{
			{ID: "check", Title: "Check current state", Phase: PhaseCheck},
			{ID: "verify", Title: "Verify service", Phase: PhaseVerify},
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if task.Phase != PhaseCheck || task.Status != StatusActive || task.Steps[0].Status != StepPending {
		t.Fatalf("unexpected initial state: %#v", task)
	}
	info, err := os.Stat(filepath.Join(root, task.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("task file mode = %o", info.Mode().Perm())
	}
	if _, err := store.Complete(task.ID); err == nil {
		t.Fatal("complete succeeded before final_review")
	}

	task, err = store.Checkpoint(task.ID, "check", StepInProgress, "checking current state")
	if err != nil {
		t.Fatal(err)
	}
	if task.Steps[0].Status != StepInProgress || task.Summary != "checking current state" {
		t.Fatalf("checkpoint did not persist progress: %#v", task)
	}
	if _, err := store.Checkpoint(task.ID, "check", StepCompleted, "current state checked"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Checkpoint(task.ID, "verify", StepCompleted, "service verified"); err != nil {
		t.Fatal(err)
	}
	task, err = store.FinalReview(task.ID, FinalReviewInput{
		Status:        FinalReviewPass,
		Summary:       "all checks passed",
		VerifiedFacts: []string{"health endpoint returned 200"},
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err = store.Complete(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusCompleted || task.CompletedAt == nil || task.Summary != "all checks passed" {
		t.Fatalf("unexpected completed state: %#v", task)
	}

	reopened, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reopened.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != StatusCompleted || loaded.Steps[1].Status != StepCompleted {
		t.Fatalf("task did not survive restart: %#v", loaded)
	}
}

func TestFinalReviewRequiresCompletedStepsAndVerifiedFacts(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Create(
		"Repair",
		"repair service",
		[]string{"service responds"},
		[]TaskStepInput{{ID: "repair", Title: "Repair service"}},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.FinalReview(task.ID, FinalReviewInput{Status: FinalReviewPass, Summary: "checked", VerifiedFacts: []string{"unit test passed"}})
	if err == nil || !strings.Contains(err.Error(), "all task steps completed") {
		t.Fatalf("expected incomplete-step error, got %v", err)
	}
	if _, err := store.FinalReview(task.ID, FinalReviewInput{Status: FinalReviewFailed, Summary: "not ready"}); err == nil {
		t.Fatal("failed final review accepted no risks")
	}
	if _, err := store.Checkpoint(task.ID, "repair", StepCompleted, "repair complete"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinalReview(task.ID, FinalReviewInput{Status: FinalReviewPass, Summary: "checked"}); err == nil {
		t.Fatal("passing final review accepted no verified facts")
	}
}

func TestCheckpointAllowsOnlyForwardProgress(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Create(
		"Implement",
		"implement change",
		[]string{"tests pass"},
		[]TaskStepInput{{ID: "code", Title: "Write code"}, {ID: "test", Title: "Run tests"}},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Checkpoint(task.ID, "code", StepInProgress, "coding"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Checkpoint(task.ID, "test", StepInProgress, "testing"); err == nil {
		t.Fatal("accepted a second in-progress step")
	}
	if _, err := store.Checkpoint(task.ID, "code", StepCompleted, "code complete"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Checkpoint(task.ID, "code", StepPending, "regress"); err == nil {
		t.Fatal("accepted backward step transition")
	}
	if _, err := store.Checkpoint(task.ID, "missing", StepCompleted, "missing"); err == nil {
		t.Fatal("accepted unknown step")
	}
}

func TestBatchCheckpointUpdatesStepsAtomicallyAndSupportsRetry(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Create(
		"Implement",
		"implement and document change",
		[]string{"tests pass"},
		[]TaskStepInput{
			{ID: "inspect", Title: "Inspect", Phase: PhaseCheck},
			{ID: "test", Title: "Test", Phase: PhaseVerify},
			{ID: "docs", Title: "Document", Phase: PhaseExecute},
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Checkpoint(task.ID, "inspect", StepInProgress, "inspecting"); err != nil {
		t.Fatal(err)
	}
	task, err = store.BatchCheckpoint(task.ID, []string{"inspect", "test"}, "docs", "tests passed; writing docs")
	if err != nil {
		t.Fatal(err)
	}
	if task.Steps[0].Status != StepCompleted || task.Steps[1].Status != StepCompleted || task.Steps[2].Status != StepInProgress {
		t.Fatalf("unexpected batch progress: %#v", task.Steps)
	}
	if task.Phase != PhaseExecute || len(task.Events) != 3 {
		t.Fatalf("unexpected batch task state: %#v", task)
	}
	if _, err := store.BatchCheckpoint(task.ID, []string{"inspect", "test"}, "docs", "retry same checkpoint"); err != nil {
		t.Fatalf("idempotent batch retry failed: %v", err)
	}
	task, err = store.FinalReview(task.ID, FinalReviewInput{Status: FinalReviewFailed, Summary: "docs pending", OpenRisks: []string{"documentation incomplete"}})
	if err != nil {
		t.Fatal(err)
	}
	task, err = store.BatchCheckpoint(task.ID, []string{"docs"}, "", "documentation complete")
	if err != nil {
		t.Fatal(err)
	}
	if task.FinalReview != nil || task.Steps[2].Status != StepCompleted {
		t.Fatalf("batch checkpoint did not clear failed review: %#v", task)
	}

	atomicTask, err := store.Create(
		"Atomic",
		"reject partial progress",
		[]string{"invalid batch rejected"},
		[]TaskStepInput{{ID: "code", Title: "Code"}, {ID: "docs", Title: "Docs"}},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.BatchCheckpoint(atomicTask.ID, []string{"code", "missing"}, "docs", "invalid batch"); err == nil {
		t.Fatal("accepted batch with unknown step")
	}
	loaded, err := store.Get(atomicTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Steps[0].Status != StepPending || loaded.Steps[1].Status != StepPending {
		t.Fatalf("invalid batch partially changed task: %#v", loaded.Steps)
	}
	if _, err := store.BatchCheckpoint(atomicTask.ID, []string{"docs"}, "docs", "overlap"); err == nil {
		t.Fatal("accepted step as both completed and current")
	}
	if _, err := store.Checkpoint(atomicTask.ID, "code", StepInProgress, "coding"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BatchCheckpoint(atomicTask.ID, nil, "docs", "switch current step"); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("accepted a second in-progress step: %v", err)
	}
	loaded, err = store.Get(atomicTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Steps[0].Status != StepInProgress || loaded.Steps[1].Status != StepPending {
		t.Fatalf("rejected current-step switch changed task: %#v", loaded.Steps)
	}
}

func TestBlockAndResumeUsesOneSummary(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Create("Repair", "repair service", []string{"service works"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	task, err = store.Block(task.ID, "SSH connection timed out three times")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusBlocked || task.Blocker == "" {
		t.Fatalf("unexpected blocked state: %#v", task)
	}
	task, err = store.Resume(task.ID, "network restored")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusActive || task.Blocker != "" || task.Summary != "network restored" {
		t.Fatalf("unexpected resumed state: %#v", task)
	}
	task, err = store.FinalReview(task.ID, FinalReviewInput{Status: FinalReviewPass, Summary: "ready to close", VerifiedFacts: []string{"network restored"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Block(task.ID, "late blocker"); err == nil || !strings.Contains(err.Error(), "passed final review") {
		t.Fatalf("block should reject a passed final review: %v", err)
	}
	if _, err := store.FinalReview(task.ID, FinalReviewInput{Status: FinalReviewFailed, Summary: "reopen", OpenRisks: []string{"late risk"}}); err == nil || !strings.Contains(err.Error(), "passed final review") {
		t.Fatalf("final review should not overwrite a passed review: %v", err)
	}
}

func TestCreateStoresComposedTemplateSources(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	refs := []TemplateReference{
		{ID: "development", Version: "1.2.0", Hash: "sha256:development"},
		{ID: "deployment", Version: "1.1.0", Hash: "sha256:deployment"},
	}
	task, err := store.Create(
		"Develop and deploy",
		"implement, test, and deploy",
		[]string{"tests pass", "tests pass", "production is healthy"},
		[]TaskStepInput{{ID: "implement", Title: "Implement"}, {ID: "deploy", Title: "Deploy", Phase: PhaseVerify}},
		refs,
	)
	if err != nil {
		t.Fatal(err)
	}
	refs[0].ID = "mutated"
	if len(task.SourceTemplates) != 2 || task.SourceTemplates[0].ID != "development" {
		t.Fatalf("source templates were not copied: %#v", task.SourceTemplates)
	}
	if len(task.Conditions) != 2 || task.Steps[0].Phase != PhaseExecute || task.Steps[1].Phase != PhaseVerify {
		t.Fatalf("task input was not normalized: %#v", task)
	}
}

func TestCreateRejectsInvalidSteps(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Create(
		"Invalid",
		"reject duplicate steps",
		[]string{"rejected"},
		[]TaskStepInput{{ID: "same", Title: "One"}, {ID: "same", Title: "Two"}},
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "duplicate task step") {
		t.Fatalf("expected duplicate-step error, got %v", err)
	}
}

func TestDeleteRemovesOnlySelectedTask(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Create("First", "delete first task", []string{"first removed"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Create("Second", "keep second task", []string{"second remains"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := store.Delete(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.ID != first.ID {
		t.Fatalf("deleted task id = %q, want %q", deleted.ID, first.ID)
	}
	if _, err := store.Get(first.ID); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("deleted task still exists or returned wrong error: %v", err)
	}
	kept, err := store.Get(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if kept.ID != second.ID {
		t.Fatalf("kept task id = %q, want %q", kept.ID, second.ID)
	}
}

func TestIndependentStoresDoNotLoseConcurrentCheckpoints(t *testing.T) {
	root := t.TempDir()
	first, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	task, err := first.Create(
		"Concurrent task",
		"keep both updates",
		[]string{"both steps complete"},
		[]TaskStepInput{
			{ID: "one", Title: "First step", Phase: PhaseExecute},
			{ID: "two", Title: "Second step", Phase: PhaseVerify},
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, update := range []struct {
		store  *Store
		stepID string
	}{
		{store: first, stepID: "one"},
		{store: second, stepID: "two"},
	} {
		wg.Add(1)
		go func(update struct {
			store  *Store
			stepID string
		}) {
			defer wg.Done()
			_, err := update.store.Checkpoint(task.ID, update.stepID, StepCompleted, "completed "+update.stepID)
			errs <- err
		}(update)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Checkpoint() error = %v", err)
		}
	}

	got, err := first.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range got.Steps {
		if step.Status != StepCompleted {
			t.Fatalf("step %s status = %s, want completed", step.ID, step.Status)
		}
	}
}

func TestListSkipsCorruptTaskState(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	valid, err := store.Create("Valid", "keep listing", []string{"listed"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	corruptID := "tsk_0123456789abcdef"
	if err := os.WriteFile(filepath.Join(store.Root(), corruptID+".json"), []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	listed, err := store.List("", 50)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != valid.ID {
		t.Fatalf("List() = %#v, want only valid task", listed)
	}
}

func TestTaskEventsAreBoundedAndDuplicateCheckpointIsIdempotent(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Create(
		"Bounded events",
		"keep task state bounded",
		[]string{"event count bounded"},
		[]TaskStepInput{{ID: "work", Title: "Work", Phase: PhaseExecute}},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Checkpoint(task.ID, "work", StepInProgress, "started")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Checkpoint(task.ID, "work", StepInProgress, "started")
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Events) != len(first.Events) {
		t.Fatalf("duplicate checkpoint events = %d, want %d", len(second.Events), len(first.Events))
	}
	for index := 0; index < maxTaskEvents+25; index++ {
		appendTaskEvent(&second, Event{Type: "test", Summary: strings.Repeat("x", maxTaskEventSummaryBytes+100), CreatedAt: time.Now()})
	}
	if len(second.Events) != maxTaskEvents {
		t.Fatalf("event count = %d, want %d", len(second.Events), maxTaskEvents)
	}
	for _, event := range second.Events {
		if len([]byte(event.Summary)) > maxTaskEventSummaryBytes {
			t.Fatalf("event summary bytes = %d, want <= %d", len([]byte(event.Summary)), maxTaskEventSummaryBytes)
		}
	}
}

func TestTaskStateRejectsOversizedText(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Create(strings.Repeat("x", maxTaskTitleBytes+1), "goal", []string{"done"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "task title exceeds") {
		t.Fatalf("Create() error = %v, want title limit", err)
	}
}

func TestTaskStateFileSizeLimitDoesNotBreakList(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	valid, err := store.Create("Valid", "keep listing", []string{"listed"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	oversizedID := "tsk_0123456789abcdee"
	file, err := os.Create(filepath.Join(store.Root(), oversizedID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxTaskStateFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	listed, err := store.List("", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != valid.ID {
		t.Fatalf("List() = %#v, want only valid task", listed)
	}
	if _, err := store.Get(oversizedID); err == nil || !strings.Contains(err.Error(), "task state exceeds") {
		t.Fatalf("Get() error = %v, want task state size limit", err)
	}
}

func TestGetTrimsLegacyTaskEventHistory(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Create("Legacy events", "trim old events", []string{"bounded"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < maxTaskEvents+40; index++ {
		task.Events = append(task.Events, Event{Type: "legacy", Summary: fmt.Sprintf("event-%03d", index), CreatedAt: time.Now()})
	}
	data, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Root(), task.ID+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Events) != maxTaskEvents {
		t.Fatalf("event count = %d, want %d", len(loaded.Events), maxTaskEvents)
	}
	if loaded.Events[0].Summary != "event-040" {
		t.Fatalf("first retained event = %q, want event-040", loaded.Events[0].Summary)
	}
}

func TestBatchCheckpointUsesTaskStateLimits(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Create(
		"Batch limits",
		"apply task governance to batch updates",
		[]string{"bounded"},
		[]TaskStepInput{{ID: "work", Title: "Work", Phase: PhaseExecute}},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.BatchCheckpoint(task.ID, []string{"work"}, "", strings.Repeat("x", maxTaskSummaryBytes+1)); err == nil || !strings.Contains(err.Error(), "checkpoint summary exceeds") {
		t.Fatalf("BatchCheckpoint() error = %v, want summary limit", err)
	}
	loaded, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Steps[0].Status != StepPending || len(loaded.Events) != len(task.Events) {
		t.Fatalf("oversized batch changed task: %#v", loaded)
	}
}

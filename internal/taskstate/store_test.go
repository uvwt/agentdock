package taskstate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
	if task.Status != StatusActive || task.Blocker != "" {
		t.Fatalf("unexpected resumed state: %#v", task)
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

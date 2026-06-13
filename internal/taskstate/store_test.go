package taskstate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTaskLifecyclePersistsAndRequiresEvidence(t *testing.T) {
	root := t.TempDir()
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Create("Deploy AgentDock", "deploy and verify", []string{"health is 200", "tool call succeeds"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Phase != PhaseCheck || task.Status != StatusActive {
		t.Fatalf("unexpected initial state: %#v", task)
	}
	info, err := os.Stat(filepath.Join(root, task.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("task file mode = %o", info.Mode().Perm())
	}

	for range 3 {
		task, err = store.Advance(task.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if task.Phase != PhaseCloseout {
		t.Fatalf("phase = %s", task.Phase)
	}
	if _, err := store.Complete(task.ID, "done"); err == nil {
		t.Fatal("completion without evidence succeeded")
	}
	for _, condition := range task.Conditions {
		if _, err := store.AddEvidence(task.ID, condition.ID, "verified", "test"); err != nil {
			t.Fatal(err)
		}
	}
	task, err = store.Complete(task.ID, "all checks passed")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusCompleted || task.CompletedAt == nil {
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
	if loaded.Summary != "all checks passed" {
		t.Fatalf("summary = %q", loaded.Summary)
	}
}

func TestAttemptLimitAndFailureEvidence(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Create("Repair", "repair service", []string{"service works"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordAttempt(task.ID, "restart", "failure", "", ""); err == nil {
		t.Fatal("failure without diagnosis and evidence succeeded")
	}
	for i := 0; i < MaxStrategyAttempts; i++ {
		if _, err := store.RecordAttempt(task.ID, "restart", "failure", "new diagnosis", "new log evidence"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.RecordAttempt(task.ID, "restart", "success", "", ""); err == nil {
		t.Fatal("third attempt with same strategy succeeded")
	}
}

func TestBlockAndResume(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Create("Repair", "repair service", []string{"service works"})
	if err != nil {
		t.Fatal(err)
	}
	task, err = store.Block(task.ID, "missing prerequisite", "authorization failed")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusBlocked {
		t.Fatalf("status = %s", task.Status)
	}
	if _, err := store.Advance(task.ID); err == nil {
		t.Fatal("blocked task advanced")
	}
	task, err = store.Resume(task.ID, "prerequisite restored")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusActive || task.Blocker != "" {
		t.Fatalf("unexpected resumed state: %#v", task)
	}
}

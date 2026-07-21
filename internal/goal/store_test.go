package goal

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestGoalLifecycleLeaseAndCommit(t *testing.T) {
	root := t.TempDir()
	store, err := New(filepath.Join(root, "goals"))
	if err != nil {
		t.Fatal(err)
	}

	g, err := store.Create(CreateInput{
		Title:     "Fix login",
		Objective: "Login button should navigate to dashboard",
		Mode:      ModeGuarded,
		SuccessCriteria: []SuccessCriterionInput{
			{ID: "tests", Type: CriterionCommand, Expression: "test_exit_code == 0"},
			{ID: "browser", Type: CriterionBrowser, Expression: "url_contains:/dashboard"},
		},
		Constraints: []Constraint{
			{Type: ConstraintProhibition, Value: "no_git_push"},
		},
		Milestones: []MilestoneInput{
			{ID: "repro", Title: "Reproduce"},
			{ID: "fix", Title: "Apply fix"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if g.Status != StatusPlanning || g.CapsuleVersion != 1 {
		t.Fatalf("unexpected create state: %#v", g)
	}
	info, err := os.Stat(filepath.Join(store.Root(), g.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("goal file mode = %o", info.Mode().Perm())
	}

	g, lease, err := store.AcquireLease(g.ID, "chatgpt-web-1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if g.Status != StatusAwaitingReasoning || lease.CapsuleVersion != 1 {
		t.Fatalf("unexpected lease state goal=%#v lease=%#v", g, lease)
	}

	// Version conflict: wrong expected version.
	if _, err := store.CommitTurn(CommitTurnInput{
		GoalID:                 g.ID,
		ReasoningLeaseID:       lease.LeaseID,
		ExpectedCapsuleVersion: 99,
		Decision:               DecisionContinue,
		Summary:                "should fail",
	}); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("expected STATE_CONFLICT, got %v", err)
	}

	// Wrong lease id.
	if _, err := store.CommitTurn(CommitTurnInput{
		GoalID:                 g.ID,
		ReasoningLeaseID:       "lease_deadbeef",
		ExpectedCapsuleVersion: 1,
		Decision:               DecisionContinue,
		Summary:                "should fail",
	}); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("expected lease mismatch conflict, got %v", err)
	}

	g, err = store.CommitTurn(CommitTurnInput{
		GoalID:                 g.ID,
		ReasoningLeaseID:       lease.LeaseID,
		ExpectedCapsuleVersion: 1,
		Decision:               DecisionContinue,
		Summary:                "Login handler returns 500; inspect auth route",
		NextMilestone:          "repro",
		CurrentProblem:         "POST /api/login → 500",
		CurrentRequest:         "Inspect login handler and propose patch plan",
		Steps: []CommitStepInput{
			{Action: ActionInspectFiles, Targets: []string{"src/login.ts"}, Summary: "read login handler"},
			{Action: ActionRunTests, Summary: "run unit tests"},
		},
		CompletedNotes: []string{"reproduced 500 on login"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if g.Status != StatusExecuting || g.CapsuleVersion != 2 {
		t.Fatalf("unexpected after commit: status=%s version=%d", g.Status, g.CapsuleVersion)
	}
	if g.ActiveLease != nil {
		t.Fatal("lease should be released after commit")
	}
	if g.Budget.ReasoningTurnsUsed != 1 {
		t.Fatalf("reasoning turns = %d", g.Budget.ReasoningTurnsUsed)
	}
	if len(g.Steps) != 2 {
		t.Fatalf("steps = %d", len(g.Steps))
	}

	// Capsule should resume cleanly after process restart.
	reopened, err := New(filepath.Join(root, "goals"))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reopened.Get(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	cap := BuildCapsule(loaded)
	if cap.GoalID != g.ID || cap.CapsuleVersion != 2 {
		t.Fatalf("capsule mismatch: %#v", cap)
	}
	if cap.ResumePrompt == "" || cap.CurrentProblem == "" {
		t.Fatalf("capsule incomplete: %#v", cap)
	}

	// Second worker can acquire after previous lease consumed.
	_, lease2, err := reopened.AcquireLease(g.ID, "chatgpt-web-2", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.CommitTurn(CommitTurnInput{
		GoalID:                 g.ID,
		ReasoningLeaseID:       lease2.LeaseID,
		ExpectedCapsuleVersion: 2,
		Decision:               DecisionVerify,
		Summary:                "patch applied; enter verification",
		Steps: []CommitStepInput{
			{Action: ActionBrowserVerify, Summary: "verify dashboard redirect"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err = reopened.Get(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != StatusVerifying || loaded.CapsuleVersion != 3 {
		t.Fatalf("unexpected verify state: %#v", loaded)
	}

	// Complete requires evidence that satisfies criteria.
	if _, err := reopened.MarkCompleted(g.ID, "done", nil); err == nil {
		t.Fatal("expected complete without evidence to fail")
	}
	if _, err := reopened.AddEvidence(g.ID, EvidenceRef{
		Kind:    "tests",
		Summary: "unit tests passed",
		URI:     "artifact://logs/tests-01",
		Data:    map[string]any{"exit_code": 0, "test_exit_code": 0, "criterion_id": "tests"},
	}); err != nil {
		t.Fatal(err)
	}
	// Still missing browser criterion.
	if _, err := reopened.MarkCompleted(g.ID, "partial", []string{"evd"}); err == nil {
		t.Fatal("expected complete with unmet browser criterion to fail")
	}
	if _, err := reopened.AddEvidence(g.ID, EvidenceRef{
		Kind:    "browser",
		Summary: "landed on dashboard",
		Data:    map[string]any{"url": "http://127.0.0.1:3000/dashboard", "console_errors": 0, "criterion_id": "browser"},
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err = reopened.MarkCompleted(g.ID, "all criteria satisfied", []string{"evd"})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != StatusCompleted || loaded.CompletedAt == nil {
		t.Fatalf("expected completed: %#v", loaded)
	}
}

func TestRejectUnknownStepAction(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(CreateInput{
		Title:     "x",
		Objective: "y",
		SuccessCriteria: []SuccessCriterionInput{
			{Expression: "manual ok", Type: CriterionManual},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	g, lease, err := store.AcquireLease(g.ID, "w1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CommitTurn(CommitTurnInput{
		GoalID:                 g.ID,
		ReasoningLeaseID:       lease.LeaseID,
		ExpectedCapsuleVersion: g.CapsuleVersion,
		Decision:               DecisionContinue,
		Summary:                "try shell",
		Steps: []CommitStepInput{
			{Action: StepAction("rm -rf /"), Summary: "bad"},
		},
	})
	if err == nil {
		t.Fatal("expected unknown action rejection")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("want invalid input, got %v", err)
	}
}

func TestConcurrentLeaseConflict(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(CreateInput{
		Title:     "lease race",
		Objective: "only one worker",
		SuccessCriteria: []SuccessCriterionInput{
			{Expression: "ok", Type: CriterionManual},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AcquireLease(g.ID, "worker-a", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AcquireLease(g.ID, "worker-b", time.Minute); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("expected conflict for second worker, got %v", err)
	}
	// Same worker may refresh.
	if _, _, err := store.AcquireLease(g.ID, "worker-a", time.Minute); err != nil {
		t.Fatal(err)
	}
}

func TestIdempotentStepMerge(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(CreateInput{
		Title:     "idem",
		Objective: "no duplicate steps",
		SuccessCriteria: []SuccessCriterionInput{
			{Expression: "ok", Type: CriterionManual},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	g, lease, err := store.AcquireLease(g.ID, "w", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	key := "goal_step_run_tests_v1"
	g, err = store.CommitTurn(CommitTurnInput{
		GoalID: g.ID, ReasoningLeaseID: lease.LeaseID, ExpectedCapsuleVersion: g.CapsuleVersion,
		Decision: DecisionContinue, Summary: "first",
		Steps: []CommitStepInput{{Action: ActionRunTests, Idempotency: key, Summary: "run"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	g, lease, err = store.AcquireLease(g.ID, "w", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	g, err = store.CommitTurn(CommitTurnInput{
		GoalID: g.ID, ReasoningLeaseID: lease.LeaseID, ExpectedCapsuleVersion: g.CapsuleVersion,
		Decision: DecisionContinue, Summary: "second",
		Steps: []CommitStepInput{{Action: ActionRunTests, Idempotency: key, Summary: "run again"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, step := range g.Steps {
		if step.ID == key {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected single idempotent step, got %d in %#v", count, g.Steps)
	}
}

func TestBudgetBlocksExcessReasoning(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	maxTurns := 1
	g, err := store.Create(CreateInput{
		Title:     "budget",
		Objective: "stop loops",
		Budget:    &Budget{MaxReasoningTurns: maxTurns},
		SuccessCriteria: []SuccessCriterionInput{
			{Expression: "ok", Type: CriterionManual},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	g, lease, err := store.AcquireLease(g.ID, "w", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	g, err = store.CommitTurn(CommitTurnInput{
		GoalID: g.ID, ReasoningLeaseID: lease.LeaseID, ExpectedCapsuleVersion: g.CapsuleVersion,
		Decision: DecisionContinue, Summary: "turn 1",
	})
	if err != nil {
		t.Fatal(err)
	}
	g, lease, err = store.AcquireLease(g.ID, "w", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CommitTurn(CommitTurnInput{
		GoalID: g.ID, ReasoningLeaseID: lease.LeaseID, ExpectedCapsuleVersion: g.CapsuleVersion,
		Decision: DecisionContinue, Summary: "turn 2",
	})
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("expected budget exceeded, got %v", err)
	}
}

func TestListAndPauseCancel(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(CreateInput{
		Title:     "list me",
		Objective: "lifecycle",
		SuccessCriteria: []SuccessCriterionInput{
			{Expression: "ok", Type: CriterionManual},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Pause(g.ID, "user paused"); err != nil {
		t.Fatal(err)
	}
	listed, err := store.List(StatusPaused, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != g.ID {
		t.Fatalf("list paused = %#v", listed)
	}
	if _, err := store.Resume(g.ID, "continue"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Cancel(g.ID, "abandoned"); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Get(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != StatusCancelled {
		t.Fatalf("status = %s", loaded.Status)
	}
}


func TestBindWorkerConversation(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(CreateInput{
		Title: "bind", Objective: "thread",
		SuccessCriteria: []SuccessCriterionInput{{Expression: "ok", Type: CriterionManual}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ver := g.CapsuleVersion
	g, err = store.BindWorkerConversation(g.ID, "https://chatgpt.com/c/helloThread?utm=1", "")
	if err != nil {
		t.Fatal(err)
	}
	if g.WorkerConversationURL != "https://chatgpt.com/c/helloThread" {
		t.Fatalf("url=%q", g.WorkerConversationURL)
	}
	if g.WorkerConversationID != "helloThread" {
		t.Fatalf("id=%q", g.WorkerConversationID)
	}
	if g.CapsuleVersion <= ver {
		t.Fatalf("capsule not bumped: %d -> %d", ver, g.CapsuleVersion)
	}
	// idempotent same binding should not error
	if _, err := store.BindWorkerConversation(g.ID, "https://chatgpt.com/c/helloThread", "helloThread"); err != nil {
		t.Fatal(err)
	}
}


func TestBindWorkerConversationAcceptsWebPrefixedThread(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(CreateInput{
		Title: "web", Objective: "accept WEB: thread ids",
		SuccessCriteria: []SuccessCriterionInput{{Expression: "ok", Type: CriterionManual}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Live ChatGPT currently uses /c/WEB:<uuid>.
	g, err = store.BindWorkerConversation(g.ID, "https://chatgpt.com/c/WEB:ca643b7e-69e3-4df7-aafa-a5bc3e44f314", "")
	if err != nil {
		t.Fatal(err)
	}
	if g.WorkerConversationID != "WEB:ca643b7e-69e3-4df7-aafa-a5bc3e44f314" {
		t.Fatalf("id=%q", g.WorkerConversationID)
	}
	if _, err := store.BindWorkerConversation(g.ID, "https://chatgpt.com/", ""); err == nil {
		t.Fatal("expected reject non-thread home url")
	}
}


func TestCommitTurnSoftRejectsBlockWhenPartsExist(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	parts := filepath.Join(dir, "parts")
	if err := os.MkdirAll(parts, 0o755); err != nil {
		t.Fatal(err)
	}
	partPath := filepath.Join(parts, "letters_01.md")
	if err := os.WriteFile(partPath, []byte(strings.Repeat("x", 100)), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(CreateInput{
		Title: "soft block", Objective: "book",
		SuccessCriteria: []SuccessCriterionInput{
			{ID: "p01_bytes", Type: CriterionCommand, Expression: "file_min_bytes:" + partPath + ":50"},
		},
		Budget: &Budget{MaxReasoningTurns: 10, MaxIdenticalFailures: 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	gg, lease, err := store.AcquireLease(g.ID, "model", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	out, err := store.CommitTurn(CommitTurnInput{
		GoalID: g.ID, ReasoningLeaseID: lease.LeaseID,
		ExpectedCapsuleVersion: gg.CapsuleVersion,
		Decision:               DecisionBlock,
		Summary:                "安全層阻止，無原文可讀",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status == StatusBlocked {
		t.Fatalf("expected soft-reject to continue, got blocked: %+v", out)
	}
	if out.Status != StatusExecuting {
		t.Fatalf("status=%s", out.Status)
	}
	saw := false
	for _, e := range out.Events {
		if e.Type == "block_soft_rejected" {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("expected block_soft_rejected event, events=%+v", out.Events)
	}
}

func TestCommitTurnBlockStillWorksWithoutProgress(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(CreateInput{
		Title: "hard block", Objective: "x",
		SuccessCriteria: []SuccessCriterionInput{
			{ID: "m", Type: CriterionManual, Expression: "ok"},
		},
		Budget: &Budget{MaxReasoningTurns: 10, MaxIdenticalFailures: 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	gg, lease, err := store.AcquireLease(g.ID, "model", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	out, err := store.CommitTurn(CommitTurnInput{
		GoalID: g.ID, ReasoningLeaseID: lease.LeaseID,
		ExpectedCapsuleVersion: gg.CapsuleVersion,
		Decision:               DecisionBlock,
		Summary:                "need user password",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != StatusBlocked {
		t.Fatalf("expected blocked, got %s", out.Status)
	}
}

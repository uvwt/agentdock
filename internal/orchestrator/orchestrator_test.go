package orchestrator

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/goal"
)

type fakeWaker struct {
	wakeCount int32
	wakeFn    func(ctx context.Context, goalID string) (map[string]any, error)
	rotated   int32
}

func (f *fakeWaker) Wake(ctx context.Context, goalID string) (map[string]any, error) {
	atomic.AddInt32(&f.wakeCount, 1)
	if f.wakeFn != nil {
		return f.wakeFn(ctx, goalID)
	}
	return map[string]any{"ok": true}, nil
}
func (f *fakeWaker) ForceRotate() { atomic.AddInt32(&f.rotated, 1) }
func (f *fakeWaker) ClearWakeCooldown(string) {}
func (f *fakeWaker) Status() map[string]any {
	return map[string]any{"wake_count": atomic.LoadInt32(&f.wakeCount)}
}

type fakeExec struct {
	calls int32
}

func (f *fakeExec) ExecutePending(ctx context.Context, g goal.Goal) (goal.RunResult, goal.Goal, error) {
	atomic.AddInt32(&f.calls, 1)
	return goal.RunResult{OK: true, Summary: "noop"}, g, nil
}

func TestOrchestratorCompletesAfterCommitAndEvidence(t *testing.T) {
	store, err := goal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(goal.CreateInput{
		Title: "L3", Objective: "unattended complete",
		SuccessCriteria: []goal.SuccessCriterionInput{
			{ID: "m", Type: goal.CriterionManual, Expression: "ok"},
		},
		Budget: &goal.Budget{MaxReasoningTurns: 10, MaxIdenticalFailures: 5},
	})
	if err != nil {
		t.Fatal(err)
	}

	// On wake: simulate model commit_turn shortly after.
	waker := &fakeWaker{}
	waker.wakeFn = func(ctx context.Context, goalID string) (map[string]any, error) {
		go func() {
			time.Sleep(50 * time.Millisecond)
			// acquire + commit as model would
			gg, lease, err := store.AcquireLease(goalID, "fake-model", time.Minute)
			if err != nil {
				return
			}
			_, _ = store.CommitTurn(goal.CommitTurnInput{
				GoalID: goalID, ReasoningLeaseID: lease.LeaseID,
				ExpectedCapsuleVersion: gg.CapsuleVersion,
				Decision:               goal.DecisionContinue,
				Summary:                "plan: add evidence and finish",
				Steps:                  nil,
			})
			// add evidence for manual criterion
			_, _ = store.AddEvidence(goalID, goal.EvidenceRef{
				Kind: "manual", Summary: "done",
				Data: map[string]any{"criterion_id": "m", "satisfied": true},
			})
		}()
		return map[string]any{"ok": true}, nil
	}

	orch := New(store, waker, &fakeExec{}, Config{
		CommitWait: 2 * time.Second, PollInterval: 20 * time.Millisecond,
		MaxNoCommit: 2, MaxTicks: 30, TickPause: 20 * time.Millisecond,
		RotateOnNoCommit: true,
		// Commit arrives in ~50ms; keep watchdog loose so it does not race.
		ToolActivityWait: 2 * time.Second,
	})
	st, err := orch.Start(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Running {
		t.Fatalf("%#v", st)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st = orch.Status(g.ID)
		if st.Phase == "completed" || st.Phase == "blocked" || st.Phase == "error" {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	st = orch.Status(g.ID)
	if st.Phase != "completed" {
		// mark_completed may need evidence path — check goal status
		loaded, _ := store.Get(g.ID)
		if loaded.Status != goal.StatusCompleted && st.Phase != "completed" {
			t.Fatalf("expected completed, status=%#v goal=%s msg=%s err=%s", st, loaded.Status, st.LastMessage, st.LastError)
		}
	}
	if atomic.LoadInt32(&waker.wakeCount) < 1 {
		t.Fatal("expected at least one wake")
	}
}

func TestOrchestratorBlocksOnRepeatedNoCommit(t *testing.T) {
	store, err := goal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(goal.CreateInput{
		Title: "no commit", Objective: "should block",
		SuccessCriteria: []goal.SuccessCriterionInput{
			{ID: "m", Type: goal.CriterionManual, Expression: "ok"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	waker := &fakeWaker{wakeFn: func(ctx context.Context, goalID string) (map[string]any, error) {
		// wake ok but never commit
		return map[string]any{"ok": true}, nil
	}}
	orch := New(store, waker, &fakeExec{}, Config{
		CommitWait: 80 * time.Millisecond, PollInterval: 15 * time.Millisecond,
		MaxNoCommit: 2, MaxTicks: 20, TickPause: 10 * time.Millisecond,
		// Rotation is optional last-resort; default product config keeps it off.
		RotateOnNoCommit: true,
		// Disable MCP watchdog so this test exercises no-commit rotation path.
		ToolActivityWait: -1,
	})
	if _, err := orch.Start(g.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st := orch.Status(g.ID)
		if st.Phase == "blocked" {
			if atomic.LoadInt32(&waker.rotated) < 1 {
				t.Fatalf("expected rotation attempts, rotated=%d", waker.rotated)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected blocked, got %#v", orch.Status(g.ID))
}

func TestOrchestratorWaitsOnBusyWakeWithoutRotate(t *testing.T) {
	store, err := goal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(goal.CreateInput{
		Title: "busy page", Objective: "wait not re-paste storm",
		SuccessCriteria: []goal.SuccessCriterionInput{
			{ID: "m", Type: goal.CriterionManual, Expression: "ok"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	waker := &fakeWaker{wakeFn: func(ctx context.Context, goalID string) (map[string]any, error) {
		return nil, errors.New("page not idle before paste: page_stuck: CDP method timed out")
	}}
	orch := New(store, waker, &fakeExec{}, Config{
		CommitWait: 50 * time.Millisecond, PollInterval: 10 * time.Millisecond,
		MaxNoCommit: 2, MaxTicks: 6, TickPause: 15 * time.Millisecond,
		RotateOnNoCommit: true,
	})
	if _, err := orch.Start(g.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	sawWaitIdle := false
	for time.Now().Before(deadline) {
		st := orch.Status(g.ID)
		if st.Phase == "wait_idle" || strings.Contains(st.LastMessage, "without re-paste") {
			sawWaitIdle = true
		}
		if st.Phase == "blocked" || st.Phase == "stopped" || !st.Running {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !sawWaitIdle {
		t.Fatalf("expected wait_idle handling, status=%#v", orch.Status(g.ID))
	}
	if atomic.LoadInt32(&waker.rotated) != 0 {
		t.Fatalf("busy wake must not rotate, rotated=%d", waker.rotated)
	}
	// Busy waits do not burn no_commit streak toward MaxNoCommit block.
	if orch.Status(g.ID).NoCommitStreak != 0 {
		t.Fatalf("busy should not increment no_commit, %#v", orch.Status(g.ID))
	}
}

func TestIsPageBusyWakeErr(t *testing.T) {
	if !isPageBusyWakeErr(errors.New("page not idle before paste: x")) {
		t.Fatal("expected busy")
	}
	// Soft tool_permission (page blocked) is busy; hard unresolved is not (separate path).
	if !isPageBusyWakeErr(errors.New("page blocked before paste: tool_permission")) {
		t.Fatal("expected tool permission busy")
	}
	if isPageBusyWakeErr(errors.New("tool_permission_unresolved: permission dialog still present")) {
		t.Fatal("hard permission fail must not be classified as busy")
	}
	if isPageBusyWakeErr(errors.New("resume prompt is empty")) {
		t.Fatal("empty prompt is not busy")
	}
}

func TestIsPermissionHardFail(t *testing.T) {
	if !isPermissionHardFail(errors.New("tool_permission_unresolved: permission dialog still present after 90s")) {
		t.Fatal("expected hard fail")
	}
	if isPermissionHardFail(errors.New("page not idle")) {
		t.Fatal("idle is not permission hard fail")
	}
}

func TestOrchestratorBlocksOnPermissionHardFail(t *testing.T) {
	store, err := goal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(goal.CreateInput{
		Title: "perm", Objective: "auto approve fail",
		SuccessCriteria: []goal.SuccessCriterionInput{
			{ID: "m", Type: goal.CriterionManual, Expression: "ok"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	waker := &fakeWaker{wakeFn: func(ctx context.Context, goalID string) (map[string]any, error) {
		return nil, errors.New("tool_permission_unresolved: permission dialog still present after 50ms (auto-approve failed or disabled)")
	}}
	orch := New(store, waker, &fakeExec{}, Config{
		CommitWait: 100 * time.Millisecond, PollInterval: 10 * time.Millisecond,
		MaxNoCommit: 3, MaxTicks: 10, TickPause: 10 * time.Millisecond,
		ToolActivityWait: -1,
	})
	if _, err := orch.Start(g.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		st := orch.Status(g.ID)
		if st.Phase == "blocked" {
			loaded, _ := store.Get(g.ID)
			if loaded.Status != goal.StatusBlocked {
				t.Fatalf("goal status=%s want blocked", loaded.Status)
			}
			if atomic.LoadInt32(&waker.rotated) != 0 {
				t.Fatalf("must not rotate on permission hard fail")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected blocked, got %#v", orch.Status(g.ID))
}

func TestOrchestratorBlocksWhenNoMCPActivity(t *testing.T) {
	store, err := goal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(goal.CreateInput{
		Title: "no mcp", Objective: "watchdog",
		SuccessCriteria: []goal.SuccessCriterionInput{
			{ID: "m", Type: goal.CriterionManual, Expression: "ok"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Wake succeeds (paste delivered) but model never calls MCP.
	waker := &fakeWaker{wakeFn: func(ctx context.Context, goalID string) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	}}
	orch := New(store, waker, &fakeExec{}, Config{
		CommitWait: 2 * time.Second, PollInterval: 20 * time.Millisecond,
		MaxNoCommit: 5, MaxTicks: 20, TickPause: 20 * time.Millisecond,
		ToolActivityWait: 80 * time.Millisecond,
	})
	if _, err := orch.Start(g.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		st := orch.Status(g.ID)
		if st.Phase == "blocked" {
			if !strings.Contains(st.LastMessage, "no MCP") && !strings.Contains(st.LastMessage, "tool activity") {
				// accept any blocked from watchdog path
				loaded, _ := store.Get(g.ID)
				if loaded.Status != goal.StatusBlocked {
					t.Fatalf("msg=%q status=%s", st.LastMessage, loaded.Status)
				}
			}
			// Should fail on first delivered wake, not after MaxNoCommit re-pastes.
			if atomic.LoadInt32(&waker.wakeCount) > 2 {
				t.Fatalf("too many wakes before MCP watchdog: %d", waker.wakeCount)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected blocked on no MCP activity, got %#v", orch.Status(g.ID))
}

func TestHasMCPActivity(t *testing.T) {
	base := progressBaseline{events: 1, evidence: 0, outBytes: 0, steps: 0}
	g := goal.Goal{
		CapsuleVersion: 1,
		Events: []goal.Event{
			{Type: "created"},
			{Type: "lease_acquired"},
			{Type: "worker_conversation_bound"},
			{Type: "verified"},
			{Type: "execution_applied"},
		},
	}
	if hasMCPActivity(g, 1, base, false) {
		t.Fatal("wake plumbing / local verify must not count as MCP activity")
	}
	g.Events = append(g.Events, goal.Event{Type: "reasoning_committed"})
	if !hasMCPActivity(g, 1, base, false) {
		t.Fatal("reasoning_committed should count")
	}
	// Capsule bump alone is not enough (local orch can bump).
	g2 := goal.Goal{CapsuleVersion: 9}
	if hasMCPActivity(g2, 1, base, false) {
		t.Fatal("capsule bump alone must not count")
	}
	// Live MCP tool call does count.
	if !hasMCPActivity(g2, 1, base, true) {
		t.Fatal("mcpLive should count")
	}
}



func TestOrchestratorStop(t *testing.T) {
	store, err := goal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(goal.CreateInput{
		Title: "stop", Objective: "stop me",
		SuccessCriteria: []goal.SuccessCriterionInput{{Expression: "ok", Type: goal.CriterionManual}},
	})
	if err != nil {
		t.Fatal(err)
	}
	waker := &fakeWaker{wakeFn: func(ctx context.Context, goalID string) (map[string]any, error) {
		<-ctx.Done()
		return nil, errors.New("cancelled")
	}}
	orch := New(store, waker, nil, Config{
		CommitWait: time.Minute, PollInterval: 50 * time.Millisecond,
		MaxNoCommit: 5, MaxTicks: 100, TickPause: 50 * time.Millisecond,
	})
	if _, err := orch.Start(g.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	st := orch.Stop(g.ID)
	if st.Running {
		t.Fatalf("should not be running: %#v", st)
	}
}

func TestWakeResultErrorBusyAndOkFalse(t *testing.T) {
	if err := wakeResultError(map[string]any{"busy": true}); err == nil {
		t.Fatal("expected busy error")
	}
	if err := wakeResultError(map[string]any{"ok": false, "error": "nope"}); err == nil || err.Error() != "nope" {
		t.Fatalf("got %v", err)
	}
	if err := wakeResultError(map[string]any{"ok": true}); err != nil {
		t.Fatal(err)
	}
}

func TestWakeFailureBackoff(t *testing.T) {
	d := wakeFailureBackoff(1, time.Second)
	if d < 2*time.Second {
		t.Fatalf("%s", d)
	}
	d = wakeFailureBackoff(10, time.Second)
	if d != 30*time.Second {
		t.Fatalf("%s", d)
	}
}

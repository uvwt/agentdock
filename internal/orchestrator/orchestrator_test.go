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

func TestIsLiveMCPBusySummary(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"none", false},
		{"file_edit:atomic_write", true},
		{"atomic_write", true},
		{"search_text x3", true},
		{"read_file /tmp/x", true},
		{"run_command go test", true},
		{"exec_command ls", true},
		{"browser_act evaluate", true},
		{"goal_manage:get", false},
		{"goal_manage:commit_turn", false},
		{"goal_manage:acquire_lease", false},
		{"goal_manage:get,goal_manage:commit_turn", false},
		{"file_edit then goal_manage:commit", true},
	}
	for _, tc := range cases {
		if got := isLiveMCPBusySummary(tc.in); got != tc.want {
			t.Fatalf("isLiveMCPBusySummary(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

func TestIsProductiveMCPSummary(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"none", false},
		{"search_text", false},
		{"read_file", false},
		{"file_edit", true},
		{"atomic_write", true},
		{"commit_turn", true},
		{"goal_manage:commit", true},
		{"goal_manage:acquire_lease", true},
		{"add_evidence", true},
		{"run_command", true},
		{"exec_command", true},
		{"search_text,read_file", false},
		{"search_text,file_edit", true},
	}
	for _, tc := range cases {
		if got := isProductiveMCPSummary(tc.in); got != tc.want {
			t.Fatalf("isProductiveMCPSummary(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

// scriptedActivity returns a fixed summary for MCP activity probes.
type scriptedActivity struct {
	summary string
	live    bool
}

func (s scriptedActivity) MCPToolActivitySince(since time.Time) bool {
	if s.live {
		return true
	}
	return s.summary != "" && s.summary != "none"
}
func (s scriptedActivity) MCPToolActivitySummary(since time.Time) string {
	if s.summary == "" {
		return "none"
	}
	return s.summary
}

func TestOrchestratorMCPBusyGateDelaysWake(t *testing.T) {
	store, err := goal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(goal.CreateInput{
		Title: "busy gate", Objective: "do not wake while file_edit live",
		SuccessCriteria: []goal.SuccessCriterionInput{
			{ID: "m", Type: goal.CriterionManual, Expression: "ok"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RequestReasoning(g.ID, "work", ""); err != nil {
		t.Fatal(err)
	}
	waker := &fakeWaker{}
	orch := New(store, waker, &fakeExec{}, Config{
		CommitWait: 50 * time.Millisecond, PollInterval: 10 * time.Millisecond,
		MaxNoCommit: 2, MaxTicks: 8, TickPause: 15 * time.Millisecond,
		ToolActivityWait: -1,
	})
	orch.SetActivitySource(scriptedActivity{summary: "file_edit:atomic_write", live: true})
	if _, err := orch.Start(g.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(400 * time.Millisecond)
	sawWaitIdle := false
	for time.Now().Before(deadline) {
		st := orch.Status(g.ID)
		if st.Phase == "wait_idle" || strings.Contains(st.LastMessage, "MCP tools still active") {
			sawWaitIdle = true
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	orch.Stop(g.ID)
	if !sawWaitIdle {
		t.Fatalf("expected wait_idle MCP busy gate, status=%#v wakes=%d", orch.Status(g.ID), atomic.LoadInt32(&waker.wakeCount))
	}
	if atomic.LoadInt32(&waker.wakeCount) != 0 {
		t.Fatalf("wake should be delayed while live tools, count=%d", atomic.LoadInt32(&waker.wakeCount))
	}
}

func TestWaitCommitHandsOffStagnantAfterThrash(t *testing.T) {
	store, err := goal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(goal.CreateInput{
		Title: "thrash", Objective: "stagnant re-wake",
		SuccessCriteria: []goal.SuccessCriterionInput{
			{ID: "m", Type: goal.CriterionManual, Expression: "ok"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Activity always reports search_text only (non-productive).
	orch := New(store, &fakeWaker{}, &fakeExec{}, Config{
		CommitWait: 2 * time.Second, PollInterval: 20 * time.Millisecond,
		// toolWait must be >= StagnantAfter or production bumps stagnant to toolWait+90s
		ToolActivityWait: -1,
		StagnantAfter:    80 * time.Millisecond,
	})
	orch.SetActivitySource(scriptedActivity{summary: "search_text", live: true})
	base := progressBaseline{wakeAt: time.Now(), events: len(g.Events), evidence: len(g.Evidence), outBytes: 0, steps: len(g.Steps)}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	committed, _, activity, err := orch.waitCommitHandsOff(ctx, g.ID, g.CapsuleVersion, g.Status, 2*time.Second, 20*time.Millisecond, -1, base)
	if err != nil {
		t.Fatal(err)
	}
	if committed {
		t.Fatal("expected no commit")
	}
	if !activity {
		t.Fatal("expected activity=true from thrash tools")
	}
}

func TestWaitCommitHandsOffStagnantAfterProductiveQuiet(t *testing.T) {
	store, err := goal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(goal.CreateInput{
		Title: "quiet after write", Objective: "stagnant after productive",
		SuccessCriteria: []goal.SuccessCriterionInput{
			{ID: "m", Type: goal.CriterionManual, Expression: "ok"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// First summaries productive, then thrash-only via time-based switch.
	act := &switchingActivity{productiveUntil: time.Now().Add(60 * time.Millisecond)}
	orch := New(store, &fakeWaker{}, &fakeExec{}, Config{
		CommitWait: 2 * time.Second, PollInterval: 15 * time.Millisecond,
		ToolActivityWait: -1,
		StagnantAfter:    70 * time.Millisecond,
	})
	orch.SetActivitySource(act)
	base := progressBaseline{wakeAt: time.Now(), events: len(g.Events), evidence: len(g.Evidence), outBytes: 0, steps: len(g.Steps)}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	committed, _, activity, err := orch.waitCommitHandsOff(ctx, g.ID, g.CapsuleVersion, g.Status, 2*time.Second, 15*time.Millisecond, -1, base)
	if err != nil {
		t.Fatal(err)
	}
	if committed {
		t.Fatal("expected no commit")
	}
	if !activity {
		t.Fatal("expected activity")
	}
}

type switchingActivity struct {
	productiveUntil time.Time
}

func (s *switchingActivity) MCPToolActivitySince(since time.Time) bool { return true }
func (s *switchingActivity) MCPToolActivitySummary(since time.Time) string {
	if time.Now().Before(s.productiveUntil) {
		return "file_edit"
	}
	// After productive window: only search. Summary since lastProductive should be search-only.
	return "search_text"
}

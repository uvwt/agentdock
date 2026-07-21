package goal

import (
	"testing"
	"time"
)

func TestProgressDetectorBlocksIdenticalTurns(t *testing.T) {
	g := Goal{
		Status: StatusExecuting,
		Budget: Budget{MaxIdenticalFailures: 2, MaxReasoningTurns: 20},
		SuccessCriteria: []SuccessCriterion{
			{ID: "t", Type: CriterionManual, Expression: "ok", Status: CriterionPending},
		},
		CurrentProblem: "same problem",
	}
	fp1 := ProgressFingerprint(g)
	rep := EvaluateProgress("", g, 0)
	if !rep.Advanced || rep.Streak != 0 {
		t.Fatalf("first: %#v", rep)
	}
	// no change
	rep = EvaluateProgress(fp1, g, 0)
	if rep.Advanced || rep.Streak != 1 || rep.ShouldBlock {
		t.Fatalf("second identical: %#v", rep)
	}
	rep = EvaluateProgress(fp1, g, 1)
	if !rep.ShouldBlock || rep.Streak != 2 {
		t.Fatalf("should block: %#v", rep)
	}
	// progress via evidence
	g.Evidence = append(g.Evidence, EvidenceRef{ID: "e1", Kind: "tests", Summary: "pass"})
	rep = EvaluateProgress(fp1, g, 2)
	if !rep.Advanced || rep.Streak != 0 {
		t.Fatalf("evidence should advance: %#v", rep)
	}
}

func TestCommitTurnNoProgressBlocks(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(CreateInput{
		Title: "loop", Objective: "detect loops",
		Budget: &Budget{MaxIdenticalFailures: 2, MaxReasoningTurns: 20},
		SuccessCriteria: []SuccessCriterionInput{
			{ID: "m", Type: CriterionManual, Expression: "ok"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// three continues with same problem and no new signals
	for i := 0; i < 3; i++ {
		var lease Lease
		g, lease, err = store.AcquireLease(g.ID, "w", time.Minute)
		if err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
		g, err = store.CommitTurn(CommitTurnInput{
			GoalID: g.ID, ReasoningLeaseID: lease.LeaseID, ExpectedCapsuleVersion: g.CapsuleVersion,
			Decision: DecisionContinue, Summary: "still looking",
			CurrentProblem: "unchanged root cause guess",
		})
		if err != nil {
			t.Fatalf("commit %d: %v status=%s", i, err, g.Status)
		}
	}
	g, err = store.Get(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if g.Status != StatusBlocked {
		t.Fatalf("expected blocked after no-progress streak, got %s streak=%d", g.Status, g.NoProgressStreak)
	}
	if g.NoProgressStreak < 2 {
		t.Fatalf("streak=%d", g.NoProgressStreak)
	}
}

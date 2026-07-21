package goal

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// ProgressReport describes whether a turn produced meaningful forward motion.
type ProgressReport struct {
	Advanced    bool     `json:"advanced"`
	Fingerprint string   `json:"fingerprint"`
	Signals     []string `json:"signals,omitempty"`
	Streak      int      `json:"no_progress_streak"`
	ShouldBlock bool     `json:"should_block"`
	Reason      string   `json:"reason,omitempty"`
}

// ProgressFingerprint captures durable signals that count as progress.
func ProgressFingerprint(g Goal) string {
	parts := []string{
		fmt.Sprintf("status=%s", g.Status),
		fmt.Sprintf("evidence=%d", len(g.Evidence)),
		fmt.Sprintf("completed_notes=%d", len(g.CompletedNotes)),
		fmt.Sprintf("problem=%s", strings.TrimSpace(g.CurrentProblem)),
		fmt.Sprintf("git=%s", strings.TrimSpace(g.CurrentGitSHA)),
	}
	satisfied := 0
	for _, c := range g.SuccessCriteria {
		if c.Status == CriterionSatisfied {
			satisfied++
		}
		parts = append(parts, "crit:"+c.ID+"="+string(c.Status))
	}
	parts = append(parts, fmt.Sprintf("satisfied=%d", satisfied))
	stepDone := 0
	for _, s := range g.Steps {
		if s.Status == StepCompleted {
			stepDone++
		}
		parts = append(parts, "step:"+s.ID+"="+string(s.Status))
	}
	parts = append(parts, fmt.Sprintf("steps_done=%d", stepDone))
	for _, m := range g.Milestones {
		parts = append(parts, "ms:"+m.ID+"="+string(m.Status))
	}
	// evidence ids (newest last already) — include last few summaries
	start := 0
	if len(g.Evidence) > 5 {
		start = len(g.Evidence) - 5
	}
	for _, e := range g.Evidence[start:] {
		parts = append(parts, "ev:"+e.ID+":"+e.Kind+":"+e.Summary)
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:8])
}

// EvaluateProgress compares pre-turn fingerprint to the post-turn goal.
// streak is the previous NoProgressStreak on the goal.
func EvaluateProgress(beforeFingerprint string, after Goal, previousStreak int) ProgressReport {
	fp := ProgressFingerprint(after)
	rep := ProgressReport{Fingerprint: fp, Streak: previousStreak}
	if beforeFingerprint == "" || beforeFingerprint != fp {
		rep.Advanced = true
		rep.Streak = 0
		rep.Signals = progressSignals(beforeFingerprint, after)
		return rep
	}
	rep.Advanced = false
	rep.Streak = previousStreak + 1
	rep.Reason = "no new evidence, criterion change, step completion, milestone change, or problem statement"
	maxFail := after.Budget.MaxIdenticalFailures
	if maxFail <= 0 {
		maxFail = 2
	}
	if rep.Streak >= maxFail {
		rep.ShouldBlock = true
		rep.Reason = fmt.Sprintf("no_progress for %d consecutive reasoning turns (max_identical_failures=%d)", rep.Streak, maxFail)
	}
	return rep
}

func progressSignals(_ string, after Goal) []string {
	var out []string
	if len(after.Evidence) > 0 {
		out = append(out, "evidence")
	}
	for _, c := range after.SuccessCriteria {
		if c.Status == CriterionSatisfied {
			out = append(out, "criterion_satisfied")
			break
		}
	}
	for _, s := range after.Steps {
		if s.Status == StepCompleted {
			out = append(out, "step_completed")
			break
		}
	}
	for _, m := range after.Milestones {
		if m.Status == MilestoneCompleted || m.Status == MilestoneActive {
			out = append(out, "milestone_movement")
			break
		}
	}
	if strings.TrimSpace(after.CurrentProblem) != "" {
		out = append(out, "problem_updated")
	}
	if len(out) == 0 {
		out = append(out, "state_changed")
	}
	return out
}

package goal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/uvwt/agentdock/internal/atomicfile"
	"github.com/uvwt/agentdock/internal/filelock"
)

// Store persists goals as one JSON file each under root.
type Store struct {
	root   string
	events *EventLog
	mu     sync.Mutex
}

// New creates a goal store rooted at root (typically ~/.agentdock/goals).
// Event log is stored in sibling ../events relative to root when eventsRoot is empty.
func New(root string) (*Store, error) {
	return NewWithEvents(root, "")
}

// NewWithEvents allows explicit event log directory.
func NewWithEvents(root, eventsRoot string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("goal state root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve goal state root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("create goal state root: %w", err)
	}
	if err := os.Chmod(abs, 0o700); err != nil {
		return nil, fmt.Errorf("secure goal state root: %w", err)
	}
	if eventsRoot == "" {
		eventsRoot = filepath.Join(filepath.Dir(abs), "events")
	}
	log, err := newEventLog(eventsRoot)
	if err != nil {
		return nil, err
	}
	return &Store{root: abs, events: log}, nil
}

func (s *Store) Root() string { return s.root }

func (s *Store) acquireStoreLock() (func(), error) {
	s.mu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	releaseFileLock, err := filelock.Acquire(ctx, filepath.Join(s.root, ".store.lock"))
	cancel()
	if err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("lock goal state: %w", err)
	}
	return func() {
		releaseFileLock()
		s.mu.Unlock()
	}, nil
}

// Create persists a new goal in planning status.
func (s *Store) Create(input CreateInput) (Goal, error) {
	release, err := s.acquireStoreLock()
	if err != nil {
		return Goal{}, err
	}
	defer release()

	title := strings.TrimSpace(input.Title)
	objective := strings.TrimSpace(input.Objective)
	if title == "" {
		title = truncateForTitle(objective)
	}
	if title == "" || objective == "" {
		return Goal{}, invalidInput("title and objective are required")
	}
	if err := validateTextLimit("goal title", title, maxTitleBytes); err != nil {
		return Goal{}, err
	}
	if err := validateTextLimit("goal objective", objective, maxObjectiveBytes); err != nil {
		return Goal{}, err
	}

	criteria, err := normalizeCriteria(input.SuccessCriteria, time.Now().UTC())
	if err != nil {
		return Goal{}, err
	}
	constraints, err := normalizeConstraints(input.Constraints)
	if err != nil {
		return Goal{}, err
	}
	milestones, err := normalizeMilestones(input.Milestones, time.Now().UTC())
	if err != nil {
		return Goal{}, err
	}

	mode := input.Mode
	if mode == "" {
		mode = ModeGuarded
	}
	if mode != ModeGuarded && mode != ModeAutopilot && mode != ModeReadonly {
		return Goal{}, invalidInput(fmt.Sprintf("unsupported mode %q", mode))
	}

	budget := DefaultBudget()
	if input.Budget != nil {
		budget = mergeBudget(budget, *input.Budget)
	}

	now := time.Now().UTC()
	id, err := newPrefixedID("goal")
	if err != nil {
		return Goal{}, err
	}

	g := Goal{
		SchemaVersion:   SchemaVersion,
		ID:              id,
		Title:           title,
		Objective:       objective,
		Status:          StatusPlanning,
		Mode:            mode,
		WorkspaceID:     strings.TrimSpace(input.WorkspaceID),
		DeviceID:        strings.TrimSpace(input.DeviceID),
		BaseGitSHA:      strings.TrimSpace(input.BaseGitSHA),
		CurrentGitSHA:   strings.TrimSpace(input.BaseGitSHA),
		CapsuleVersion:  1,
		Milestones:      milestones,
		SuccessCriteria: criteria,
		Constraints:     constraints,
		Budget:          budget,
		Events:          []Event{{Type: "created", Summary: "goal created", CreatedAt: now}},
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := validateTextLimit("workspace_id", g.WorkspaceID, maxWorkspaceIDBytes); err != nil {
		return Goal{}, err
	}
	if err := s.saveLocked(g); err != nil {
		return Goal{}, err
	}
	_ = s.events.Append(g.ID, "created", "goal created", map[string]any{
		"status":          g.Status,
		"capsule_version": g.CapsuleVersion,
	})
	return g, nil
}

// Get returns a goal by id.
func (s *Store) Get(id string) (Goal, error) {
	release, err := s.acquireStoreLock()
	if err != nil {
		return Goal{}, err
	}
	defer release()
	return s.loadLocked(id)
}

// List returns goals ordered by updated_at desc.
func (s *Store) List(status Status, limit int) ([]Goal, error) {
	release, err := s.acquireStoreLock()
	if err != nil {
		return nil, err
	}
	defer release()
	if limit <= 0 || limit > maxListLimit {
		limit = defaultListLimit
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	goals := make([]Goal, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "goal_") || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := readGoalStateFile(filepath.Join(s.root, entry.Name()))
		if err != nil {
			slog.Warn("skip unreadable goal state", "file", entry.Name(), "error", err)
			continue
		}
		g, err := decodeGoal(data, entry.Name())
		if err != nil {
			slog.Warn("skip invalid goal state", "file", entry.Name(), "error", err)
			continue
		}
		if status == "" || g.Status == status {
			goals = append(goals, g)
		}
	}
	sort.Slice(goals, func(i, j int) bool { return goals[i].UpdatedAt.After(goals[j].UpdatedAt) })
	if len(goals) > limit {
		goals = goals[:limit]
	}
	return goals, nil
}

// RequestReasoning marks the goal as awaiting a reasoning worker without acquiring a lease.
// Product ChatGPT Browser Worker auto-wakes on this status.
func (s *Store) RequestReasoning(goalID, request, problem string) (Goal, error) {
	g, err := s.mutate(goalID, func(g *Goal, now time.Time) error {
		if isTerminal(g.Status) {
			return invalidInput(fmt.Sprintf("cannot request reasoning on terminal status %s", g.Status))
		}
		if r := strings.TrimSpace(request); r != "" {
			if err := validateTextLimit("current_request", r, maxSummaryBytes); err != nil {
				return err
			}
			g.CurrentRequest = r
		}
		if p := strings.TrimSpace(problem); p != "" {
			if err := validateTextLimit("current_problem", p, maxSummaryBytes); err != nil {
				return err
			}
			g.CurrentProblem = p
		}
		g.Status = StatusAwaitingReasoning
		g.ActiveLease = nil
		g.CapsuleVersion++
		appendGoalEvent(g, Event{Type: "awaiting_reasoning", Summary: firstNonEmptyStr(request, "reasoning requested"), CreatedAt: now})
		return nil
	})
	if err != nil {
		return Goal{}, err
	}
	_ = s.events.Append(g.ID, "awaiting_reasoning", g.CurrentRequest, nil)
	return g, nil
}

// AcquireLease grants exclusive reasoning rights for the current capsule version.
func (s *Store) AcquireLease(goalID, workerID string, ttl time.Duration) (Goal, Lease, error) {
	var lease Lease
	g, err := s.mutate(goalID, func(g *Goal, now time.Time) error {
		if isTerminal(g.Status) {
			return invalidInput(fmt.Sprintf("cannot acquire lease on terminal status %s", g.Status))
		}
		workerID = strings.TrimSpace(workerID)
		if workerID == "" {
			return invalidInput("worker_id is required")
		}
		if err := validateTextLimit("worker_id", workerID, maxWorkerIDBytes); err != nil {
			return err
		}
		if ttl <= 0 {
			ttl = defaultLeaseTTL
		}
		if g.ActiveLease != nil && g.ActiveLease.ExpiresAt.After(now) && g.ActiveLease.WorkerID != workerID {
			return stateConflict("goal already has an active lease from another worker", g, g.CapsuleVersion, g.ActiveLease.LeaseID)
		}
		id, err := newPrefixedID("lease")
		if err != nil {
			return err
		}
		lease = Lease{
			LeaseID:        id,
			GoalID:         g.ID,
			WorkerID:       workerID,
			CapsuleVersion: g.CapsuleVersion,
			ExpiresAt:      now.Add(ttl),
			AcquiredAt:     now,
		}
		cp := lease
		g.ActiveLease = &cp
		if g.Status == StatusPlanning || g.Status == StatusReplanning || g.Status == StatusRegressed || g.Status == StatusVerifying {
			g.Status = StatusAwaitingReasoning
		}
		appendGoalEvent(g, Event{Type: "lease_acquired", Summary: workerID, CreatedAt: now})
		return nil
	})
	if err != nil {
		return Goal{}, Lease{}, err
	}
	_ = s.events.Append(g.ID, "lease_acquired", workerID, map[string]any{
		"lease_id":        lease.LeaseID,
		"capsule_version": lease.CapsuleVersion,
		"worker_id":       workerID,
	})
	return g, lease, nil
}

// ReleaseLease clears the active lease when ids match.
func (s *Store) ReleaseLease(goalID, leaseID string) (Goal, error) {
	g, err := s.mutate(goalID, func(g *Goal, now time.Time) error {
		if g.ActiveLease == nil {
			return nil
		}
		if leaseID != "" && g.ActiveLease.LeaseID != leaseID {
			return stateConflict("lease_id does not match active lease", g, g.CapsuleVersion, leaseID)
		}
		appendGoalEvent(g, Event{Type: "lease_released", Summary: g.ActiveLease.LeaseID, CreatedAt: now})
		g.ActiveLease = nil
		return nil
	})
	if err != nil {
		return Goal{}, err
	}
	_ = s.events.Append(g.ID, "lease_released", leaseID, nil)
	return g, nil
}

// CommitTurn applies a structured reasoning decision under lease + version checks.
func (s *Store) CommitTurn(input CommitTurnInput) (Goal, error) {
	g, err := s.mutate(input.GoalID, func(g *Goal, now time.Time) error {
		if err := requireActiveLease(g, input.ReasoningLeaseID, now); err != nil {
			return err
		}
		if input.ExpectedCapsuleVersion != g.CapsuleVersion {
			return stateConflict(
				fmt.Sprintf("expected capsule_version %d but current is %d", input.ExpectedCapsuleVersion, g.CapsuleVersion),
				g, input.ExpectedCapsuleVersion, input.ReasoningLeaseID,
			)
		}
		if g.Budget.MaxReasoningTurns > 0 && g.Budget.ReasoningTurnsUsed >= g.Budget.MaxReasoningTurns {
			return fmt.Errorf("%w: max_reasoning_turns=%d", ErrBudgetExceeded, g.Budget.MaxReasoningTurns)
		}
		if isTerminal(g.Status) {
			return invalidInput(fmt.Sprintf("cannot commit turn on terminal status %s", g.Status))
		}

		// Snapshot progress fingerprint before this turn mutates goal state.
		startFP := ProgressFingerprint(*g)
		startStreak := g.NoProgressStreak

		decision := input.Decision
		if decision == "" {
			return invalidInput("decision is required")
		}
		if !validDecision(decision) {
			return invalidInput(fmt.Sprintf("unsupported decision %q", decision))
		}
		summary := strings.TrimSpace(input.Summary)
		if summary == "" {
			return invalidInput("summary is required")
		}
		if err := validateTextLimit("commit summary", summary, maxSummaryBytes); err != nil {
			return err
		}

		steps, err := normalizeCommitSteps(input.Steps, now)
		if err != nil {
			return err
		}

		g.Budget.ReasoningTurnsUsed++
		g.Summary = summary
		if p := strings.TrimSpace(input.CurrentProblem); p != "" {
			if err := validateTextLimit("current_problem", p, maxSummaryBytes); err != nil {
				return err
			}
			g.CurrentProblem = p
		}
		if r := strings.TrimSpace(input.CurrentRequest); r != "" {
			if err := validateTextLimit("current_request", r, maxSummaryBytes); err != nil {
				return err
			}
			g.CurrentRequest = r
		}
		for _, note := range input.CompletedNotes {
			note = strings.TrimSpace(note)
			if note == "" {
				continue
			}
			if err := validateTextLimit("completed note", note, maxSummaryBytes); err != nil {
				return err
			}
			g.CompletedNotes = append(g.CompletedNotes, note)
		}
		if len(steps) > 0 {
			g.Steps = mergeSteps(g.Steps, steps, now)
		}
		if ms := strings.TrimSpace(input.NextMilestone); ms != "" {
			activateMilestone(g, ms, now)
		}

		// Soft-reject false decision=block when progressive book parts / evidence already
		// exist or a staged source path is readable. Model often claims "no source" /
		// safety while files are on disk (Spiritual Letters r9). Keep real blocks when
		// there is no durable progress.
		if decision == DecisionBlock && shouldSoftRejectBlock(*g, summary) {
			decision = DecisionContinue
			if strings.TrimSpace(input.CurrentProblem) == "" {
				g.CurrentProblem = trimRunes("soft-rejected block: "+summary, 800)
			}
			if strings.TrimSpace(input.CurrentRequest) == "" && strings.TrimSpace(g.CurrentRequest) == "" {
				g.CurrentRequest = "continue: source/parts already on disk; do not decision=block; write next part and commit_turn"
			}
			appendGoalEvent(g, Event{
				Type:      "block_soft_rejected",
				Summary:   trimRunes(summary, 400),
				CreatedAt: now,
			})
		}

		switch decision {
		case DecisionContinue:
			g.Status = StatusExecuting
			g.Blocker = ""
		case DecisionVerify:
			g.Status = StatusVerifying
			g.Blocker = ""
		case DecisionBlock:
			g.Status = StatusBlocked
			g.Blocker = summary
		case DecisionComplete:
			// P1 records the claim; P2 verifier will require evidence for every criterion.
			g.Status = StatusCompleted
			g.Blocker = ""
			completed := now
			g.CompletedAt = &completed
		case DecisionReplan:
			if g.Budget.MaxReplans > 0 && g.Budget.ReplansUsed >= g.Budget.MaxReplans {
				return fmt.Errorf("%w: max_replans=%d", ErrBudgetExceeded, g.Budget.MaxReplans)
			}
			g.Budget.ReplansUsed++
			g.Status = StatusReplanning
			g.Blocker = ""
		case DecisionPause:
			g.Status = StatusPaused
		}

		// Progress detector: block loops that restate the same durable state.
		afterFP := ProgressFingerprint(*g)
		rep := ProgressReport{Fingerprint: afterFP}
		if g.LastProgressFingerprint == "" {
			// First measured turn after create: always record baseline.
			rep.Advanced = afterFP != startFP
			if !rep.Advanced {
				// even first commit may not change fingerprint if empty mutations; still count
				rep.Advanced = true
			}
			rep.Streak = 0
			rep.Signals = progressSignals(startFP, *g)
		} else if afterFP == g.LastProgressFingerprint || afterFP == startFP {
			rep.Advanced = false
			rep.Streak = startStreak + 1
			rep.Reason = "no new evidence, criterion change, step completion, milestone change, or problem statement"
			maxFail := g.Budget.MaxIdenticalFailures
			if maxFail <= 0 {
				maxFail = 2
			}
			if rep.Streak >= maxFail {
				rep.ShouldBlock = true
				rep.Reason = fmt.Sprintf("no_progress for %d consecutive reasoning turns (max_identical_failures=%d)", rep.Streak, maxFail)
			}
		} else {
			rep.Advanced = true
			rep.Streak = 0
			rep.Signals = progressSignals(startFP, *g)
		}
		g.LastProgressFingerprint = afterFP
		g.NoProgressStreak = rep.Streak
		if rep.ShouldBlock && decision != DecisionBlock && decision != DecisionComplete && decision != DecisionPause {
			g.Status = StatusBlocked
			g.Blocker = rep.Reason
			appendGoalEvent(g, Event{Type: "no_progress", Summary: rep.Reason, CreatedAt: now})
		} else if rep.Advanced {
			appendGoalEvent(g, Event{Type: "progress", Summary: strings.Join(rep.Signals, ","), CreatedAt: now})
		}

		// Successful commit consumes the lease and bumps capsule version.
		g.ActiveLease = nil
		g.CapsuleVersion++
		appendGoalEvent(g, Event{
			Type:      "reasoning_committed",
			Summary:   fmt.Sprintf("%s: %s", decision, summary),
			CreatedAt: now,
		})
		return nil
	})
	if err != nil {
		return Goal{}, err
	}
	_ = s.events.Append(g.ID, "reasoning_committed", string(input.Decision), map[string]any{
		"capsule_version": g.CapsuleVersion,
		"status":          g.Status,
		"summary":         g.Summary,
	})
	return g, nil
}

// Pause freezes a non-terminal goal.
func (s *Store) Pause(goalID, summary string) (Goal, error) {
	return s.lifecycle(goalID, StatusPaused, "paused", summary, nil)
}

// Resume returns a paused/blocked/awaiting_* goal to executing or planning.
func (s *Store) Resume(goalID, summary string) (Goal, error) {
	g, err := s.mutate(goalID, func(g *Goal, now time.Time) error {
		switch g.Status {
		case StatusPaused, StatusBlocked, StatusAwaitingUser, StatusAwaitingCredentials, StatusAwaitingApproval:
		case StatusAwaitingReasoning, StatusExecuting, StatusPlanning, StatusReplanning:
			// already runnable
		default:
			if isTerminal(g.Status) {
				return invalidInput(fmt.Sprintf("cannot resume terminal status %s", g.Status))
			}
		}
		summary = strings.TrimSpace(summary)
		if summary != "" {
			if err := validateTextLimit("resume summary", summary, maxSummaryBytes); err != nil {
				return err
			}
			g.Summary = summary
		}
		if g.Status == StatusPlanning || g.Status == StatusReplanning {
			// keep
		} else {
			g.Status = StatusExecuting
		}
		g.Blocker = ""
		g.CapsuleVersion++
		appendGoalEvent(g, Event{Type: "resumed", Summary: summary, CreatedAt: now})
		return nil
	})
	if err != nil {
		return Goal{}, err
	}
	_ = s.events.Append(g.ID, "resumed", summary, map[string]any{"status": g.Status})
	return g, nil
}

// Cancel marks a goal cancelled.
func (s *Store) Cancel(goalID, summary string) (Goal, error) {
	return s.lifecycle(goalID, StatusCancelled, "cancelled", summary, func(g *Goal, now time.Time) {
		g.ActiveLease = nil
		completed := now
		g.CompletedAt = &completed
	})
}

// MarkBlocked records a blocker with required context.
func (s *Store) MarkBlocked(input MarkBlockedInput) (Goal, error) {
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		return Goal{}, invalidInput("blocker reason is required")
	}
	parts := []string{"reason: " + reason}
	if t := strings.TrimSpace(input.Tried); t != "" {
		parts = append(parts, "tried: "+t)
	}
	if e := strings.TrimSpace(input.Evidence); e != "" {
		parts = append(parts, "evidence: "+e)
	}
	if n := strings.TrimSpace(input.NeedUser); n != "" {
		parts = append(parts, "need_user: "+n)
	}
	blocker := strings.Join(parts, " | ")
	if err := validateTextLimit("blocker", blocker, maxBlockerBytes); err != nil {
		return Goal{}, err
	}
	g, err := s.mutate(input.GoalID, func(g *Goal, now time.Time) error {
		if isTerminal(g.Status) {
			return invalidInput(fmt.Sprintf("cannot block terminal status %s", g.Status))
		}
		g.Status = StatusBlocked
		g.Blocker = blocker
		g.Summary = reason
		g.ActiveLease = nil
		g.CapsuleVersion++
		appendGoalEvent(g, Event{Type: "blocked", Summary: reason, CreatedAt: now})
		return nil
	})
	if err != nil {
		return Goal{}, err
	}
	_ = s.events.Append(g.ID, "blocked", reason, map[string]any{"blocker": blocker})
	return g, nil
}

// MarkCompleted completes a goal only when the verifier reports all criteria satisfied.
func (s *Store) MarkCompleted(goalID, summary string, evidenceIDs []string) (Goal, error) {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return Goal{}, invalidInput("summary is required to mark completed")
	}
	if err := validateTextLimit("completion summary", summary, maxSummaryBytes); err != nil {
		return Goal{}, err
	}
	g, err := s.mutate(goalID, func(g *Goal, now time.Time) error {
		if g.Status == StatusCancelled || g.Status == StatusFailed {
			return invalidInput(fmt.Sprintf("cannot complete from status %s", g.Status))
		}
		if len(g.SuccessCriteria) > 0 && len(evidenceIDs) == 0 && len(g.Evidence) == 0 {
			return invalidInput("at least one evidence reference is required to mark completed when success criteria exist")
		}
		// Apply verifier: update criterion statuses from current evidence.
		report := Verify(*g)
		applyVerifyReport(g, report, now)
		if !report.OK {
			return fmt.Errorf("%w: %s (unmet=%v)", ErrVerifyFailed, report.Summary, report.UnmetIDs)
		}
		g.Status = StatusCompleted
		g.Summary = summary
		g.Blocker = ""
		g.ActiveLease = nil
		completed := now
		g.CompletedAt = &completed
		g.CapsuleVersion++
		appendGoalEvent(g, Event{Type: "completed", Summary: summary, CreatedAt: now})
		return nil
	})
	if err != nil {
		return Goal{}, err
	}
	_ = s.events.Append(g.ID, "completed", summary, map[string]any{"evidence_ids": evidenceIDs})
	return g, nil
}

// ResolveApproval approves or rejects a pending approval by id or action key.
func (s *Store) ResolveApproval(goalID, approvalID, decision, note string) (Goal, error) {
	decision = strings.ToLower(strings.TrimSpace(decision))
	if decision != "approved" && decision != "rejected" {
		return Goal{}, invalidInput("decision must be approved or rejected")
	}
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		return Goal{}, invalidInput("approval_id is required")
	}
	note = strings.TrimSpace(note)
	if err := validateTextLimit("approval note", note, maxSummaryBytes); err != nil {
		return Goal{}, err
	}
	g, err := s.mutate(goalID, func(g *Goal, now time.Time) error {
		if isTerminal(g.Status) {
			return invalidInput(fmt.Sprintf("cannot resolve approval on terminal status %s", g.Status))
		}
		found := false
		pendingLeft := 0
		for i := range g.PendingApprovals {
			a := &g.PendingApprovals[i]
			match := a.ID == approvalID || strings.EqualFold(a.Action, approvalID)
			if !match {
				if a.Status == "" || a.Status == "pending" {
					pendingLeft++
				}
				continue
			}
			if a.Status != "" && a.Status != "pending" {
				return invalidInput(fmt.Sprintf("approval %s already %s", a.ID, a.Status))
			}
			a.Status = decision
			if note != "" {
				a.Summary = a.Summary + " | " + note
			}
			found = true
		}
		// recount pending after update
		pendingLeft = 0
		for _, a := range g.PendingApprovals {
			if a.Status == "" || a.Status == "pending" {
				pendingLeft++
			}
		}
		if !found {
			return invalidInput("approval not found: " + approvalID)
		}
		if decision == "approved" && pendingLeft == 0 && g.Status == StatusAwaitingApproval {
			g.Status = StatusExecuting
		}
		if decision == "rejected" {
			g.Status = StatusBlocked
			g.Blocker = "approval rejected: " + approvalID
		}
		g.CapsuleVersion++
		appendGoalEvent(g, Event{Type: "approval_" + decision, Summary: approvalID + " " + note, CreatedAt: now})
		return nil
	})
	if err != nil {
		return Goal{}, err
	}
	_ = s.events.Append(g.ID, "approval_"+decision, approvalID, map[string]any{"note": note})
	return g, nil
}

// ApplyExecution persists step outcomes and evidence produced by the workflow runner.
func (s *Store) ApplyExecution(goalID string, result RunResult) (Goal, error) {
	g, err := s.mutate(goalID, func(g *Goal, now time.Time) error {
		if isTerminal(g.Status) {
			return invalidInput(fmt.Sprintf("cannot apply execution on terminal status %s", g.Status))
		}
		for _, step := range result.Steps {
			if step.Evidence != nil {
				ev := *step.Evidence
				if ev.ID == "" {
					id, err := newPrefixedID("evd")
					if err != nil {
						return err
					}
					ev.ID = id
				}
				if ev.CreatedAt.IsZero() {
					ev.CreatedAt = now
				}
				if len(g.Evidence) >= maxEvidence {
					g.Evidence = g.Evidence[len(g.Evidence)-maxEvidence+1:]
				}
				g.Evidence = append(g.Evidence, ev)
			}
			// mark matching goal steps
			for i := range g.Steps {
				if g.Steps[i].ID != step.Name && g.Steps[i].Idempotency != step.Name {
					continue
				}
				if step.Skipped {
					// Local runner cannot execute this step (e.g. tool-name list, browser/patch).
					// Mark skipped so the orchestrator stops re-trying the same non-command forever.
					g.Steps[i].Status = StepSkipped
					g.Steps[i].UpdatedAt = now
					if step.Detail != "" {
						g.Steps[i].Summary = step.Detail
					} else if step.Error != "" {
						g.Steps[i].Summary = step.Error
					}
					continue
				}
				if step.OK {
					g.Steps[i].Status = StepCompleted
				} else {
					g.Steps[i].Status = StepFailed
				}
				g.Steps[i].UpdatedAt = now
				if step.Detail != "" {
					g.Steps[i].Summary = step.Detail
				}
			}
		}
		// re-verify after new evidence
		report := Verify(*g)
		applyVerifyReport(g, report, now)
		if !result.OK {
			// keep executing/blocked rather than completing
			if g.Status == StatusVerifying {
				g.Status = StatusRegressed
			}
			g.CurrentProblem = result.Summary
			if detail := formatExecutionFailureDetail(result); detail != "" {
				req := "local step failed; fix using stderr and re-commit executable steps.\n" + detail
				g.CurrentRequest = trimRunes(req, 1800)
				if strings.TrimSpace(g.CurrentProblem) == "" {
					g.CurrentProblem = trimRunes(detail, 800)
				}
			}
		} else if g.Status == StatusExecuting || g.Status == StatusPlanning || g.Status == StatusReplanning {
			// stay executing unless all criteria already satisfied
			if report.OK {
				g.Status = StatusVerifying
			}
		}
		if regs := detectEmptyArtifactRegressions(*g); len(regs) > 0 {
			msg := "artifact_regressed: " + strings.Join(regs, "; ")
			g.Status = StatusRegressed
			g.CurrentProblem = firstNonEmptyStr(msg, g.CurrentProblem)
			req := "CRITICAL: output file became empty after being non-empty. Restore from backup/.tmp if available, then file_edit action=atomic_write full content. Paths: " + strings.Join(regs, ", ")
			g.CurrentRequest = trimRunes(req, 1800)
			g.Blocker = firstNonEmptyStr(g.Blocker, msg)
			appendGoalEvent(g, Event{Type: "artifact_regressed", Summary: msg, CreatedAt: now})
			ev := EvidenceRef{Kind: "artifact_regression", Summary: msg, Data: map[string]any{"paths": regs, "ok": false}, CreatedAt: now}
			if id, err := newPrefixedID("evd"); err == nil {
				ev.ID = id
				g.Evidence = append(g.Evidence, ev)
			}
		}
		g.CapsuleVersion++
		appendGoalEvent(g, Event{Type: "execution_applied", Summary: result.Summary, CreatedAt: now})
		return nil
	})
	if err != nil {
		return Goal{}, err
	}
	_ = s.events.Append(g.ID, "execution_applied", result.Summary, map[string]any{"ok": result.OK, "steps": len(result.Steps)})
	return g, nil
}

// VerifyGoal re-evaluates criteria and persists criterion statuses without completing.
func (s *Store) VerifyGoal(goalID string) (Goal, VerifyReport, error) {
	var report VerifyReport
	g, err := s.mutate(goalID, func(g *Goal, now time.Time) error {
		report = Verify(*g)
		applyVerifyReport(g, report, now)
		if report.OK && (g.Status == StatusExecuting || g.Status == StatusVerifying) {
			g.Status = StatusVerifying
		}
		if !report.OK && g.Status == StatusVerifying && report.Failed > 0 {
			g.Status = StatusRegressed
		}
		g.CapsuleVersion++
		appendGoalEvent(g, Event{Type: "verified", Summary: report.Summary, CreatedAt: now})
		return nil
	})
	if err != nil {
		return Goal{}, VerifyReport{}, err
	}
	_ = s.events.Append(g.ID, "verified", report.Summary, map[string]any{"ok": report.OK})
	return g, report, nil
}

func applyVerifyReport(g *Goal, report VerifyReport, now time.Time) {
	byID := map[string]CriterionVerifyResult{}
	for _, r := range report.Results {
		byID[r.ID] = r
	}
	for i := range g.SuccessCriteria {
		if r, ok := byID[g.SuccessCriteria[i].ID]; ok {
			g.SuccessCriteria[i].Status = r.Status
			g.SuccessCriteria[i].EvidenceID = r.EvidenceID
			g.SuccessCriteria[i].UpdatedAt = now
		}
	}
}

// RequestApproval appends a pending approval and may move status.
func (s *Store) RequestApproval(input RequestApprovalInput) (Goal, error) {
	action := strings.TrimSpace(input.Action)
	summary := strings.TrimSpace(input.Summary)
	if action == "" || summary == "" {
		return Goal{}, invalidInput("action and summary are required")
	}
	if err := validateTextLimit("approval action", action, maxSummaryBytes); err != nil {
		return Goal{}, err
	}
	if err := validateTextLimit("approval summary", summary, maxSummaryBytes); err != nil {
		return Goal{}, err
	}
	g, err := s.mutate(input.GoalID, func(g *Goal, now time.Time) error {
		if isTerminal(g.Status) {
			return invalidInput(fmt.Sprintf("cannot request approval on terminal status %s", g.Status))
		}
		if len(g.PendingApprovals) >= maxPendingApprovals {
			return invalidInput("pending approvals limit reached")
		}
		id, err := newPrefixedID("apr")
		if err != nil {
			return err
		}
		g.PendingApprovals = append(g.PendingApprovals, Approval{
			ID:        id,
			Action:    action,
			Summary:   summary,
			Risk:      strings.TrimSpace(input.Risk),
			Status:    "pending",
			CreatedAt: now,
		})
		g.Status = StatusAwaitingApproval
		g.CapsuleVersion++
		appendGoalEvent(g, Event{Type: "approval_requested", Summary: action + ": " + summary, CreatedAt: now})
		return nil
	})
	if err != nil {
		return Goal{}, err
	}
	_ = s.events.Append(g.ID, "approval_requested", action, map[string]any{"summary": summary})
	return g, nil
}

// UpdateConstraints replaces constraints and bumps capsule version.
func (s *Store) UpdateConstraints(goalID string, constraints []Constraint) (Goal, error) {
	normalized, err := normalizeConstraints(constraints)
	if err != nil {
		return Goal{}, err
	}
	g, err := s.mutate(goalID, func(g *Goal, now time.Time) error {
		if isTerminal(g.Status) {
			return invalidInput(fmt.Sprintf("cannot update constraints on terminal status %s", g.Status))
		}
		g.Constraints = normalized
		g.CapsuleVersion++
		appendGoalEvent(g, Event{Type: "constraints_updated", Summary: fmt.Sprintf("%d constraints", len(normalized)), CreatedAt: now})
		return nil
	})
	if err != nil {
		return Goal{}, err
	}
	_ = s.events.Append(g.ID, "constraints_updated", fmt.Sprintf("%d constraints", len(normalized)), nil)
	return g, nil
}

// AddEvidence attaches an evidence reference.
func (s *Store) AddEvidence(goalID string, ref EvidenceRef) (Goal, error) {
	ref.ID = strings.TrimSpace(ref.ID)
	ref.Kind = strings.TrimSpace(ref.Kind)
	ref.Summary = strings.TrimSpace(ref.Summary)
	if ref.Kind == "" || ref.Summary == "" {
		return Goal{}, invalidInput("evidence kind and summary are required")
	}
	if err := validateTextLimit("evidence summary", ref.Summary, maxEvidenceSummaryBytes); err != nil {
		return Goal{}, err
	}
	g, err := s.mutate(goalID, func(g *Goal, now time.Time) error {
		if ref.ID == "" {
			id, err := newPrefixedID("evd")
			if err != nil {
				return err
			}
			ref.ID = id
		}
		if ref.CreatedAt.IsZero() {
			ref.CreatedAt = now
		}
		if ref.Data != nil {
			ref.Data = cloneMap(ref.Data)
		}
		if len(g.Evidence) >= maxEvidence {
			g.Evidence = g.Evidence[len(g.Evidence)-maxEvidence+1:]
		}
		g.Evidence = append(g.Evidence, ref)
		// refresh criterion statuses when structured evidence arrives
		report := Verify(*g)
		applyVerifyReport(g, report, now)
		g.CapsuleVersion++
		appendGoalEvent(g, Event{Type: "evidence_added", Summary: ref.Kind + ": " + ref.Summary, CreatedAt: now})
		return nil
	})
	if err != nil {
		return Goal{}, err
	}
	_ = s.events.Append(g.ID, "evidence_added", ref.Summary, map[string]any{"id": ref.ID, "kind": ref.Kind})
	return g, nil
}


// BindWorkerConversation records the durable ChatGPT (or other web worker) thread URL
// so the next Wake can navigate back to the same conversation instead of home/new chat.
func (s *Store) BindWorkerConversation(goalID, conversationURL, conversationID string) (Goal, error) {
	goalID = strings.TrimSpace(goalID)
	conversationURL = strings.TrimSpace(conversationURL)
	conversationID = strings.TrimSpace(conversationID)
	if goalID == "" {
		return Goal{}, invalidInput("goal_id is required")
	}
	if conversationURL == "" && conversationID == "" {
		return Goal{}, invalidInput("conversation url or id is required")
	}
	if conversationURL != "" {
		// Normalize common ChatGPT forms.
		if strings.HasPrefix(conversationURL, "http://") {
			conversationURL = "https://" + strings.TrimPrefix(conversationURL, "http://")
		}
		if i := strings.IndexAny(conversationURL, "?#"); i >= 0 {
			conversationURL = conversationURL[:i]
		}
		conversationURL = strings.TrimRight(conversationURL, "/")
		if conversationID == "" {
			conversationID = conversationIDFromURL(conversationURL)
		}
		// Reject non-thread URLs / CDP ids. Better no binding than a bad one.
		if conversationIDFromURL(conversationURL) == "" {
			return Goal{}, invalidInput("conversation url is not a ChatGPT thread URL")
		}
	}
	if conversationID != "" && !isChatGPTConversationSlug(conversationID) {
		return Goal{}, invalidInput("conversation id is not a ChatGPT thread slug")
	}
	g, err := s.mutate(goalID, func(g *Goal, now time.Time) error {
		changed := false
		if conversationURL != "" && g.WorkerConversationURL != conversationURL {
			g.WorkerConversationURL = conversationURL
			changed = true
		}
		if conversationID != "" && g.WorkerConversationID != conversationID {
			g.WorkerConversationID = conversationID
			changed = true
		}
		if !changed {
			return nil
		}
		// Binding a worker thread is operational metadata; bump capsule so resume sees it.
		g.CapsuleVersion++
		appendGoalEvent(g, Event{
			Type: "worker_conversation_bound",
			Summary: firstNonEmptyStr(conversationID, conversationURL),
			CreatedAt: now,
		})
		return nil
	})
	if err != nil {
		return Goal{}, err
	}
	_ = s.events.Append(g.ID, "worker_conversation_bound", firstNonEmptyStr(g.WorkerConversationID, g.WorkerConversationURL), map[string]any{
		"url": g.WorkerConversationURL,
		"id":  g.WorkerConversationID,
	})
	return g, nil
}

func conversationIDFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// https://chatgpt.com/c/<id> or /g/.../c/<id>
	for _, marker := range []string{"/c/", "/chat/"} {
		if i := strings.Index(raw, marker); i >= 0 {
			id := raw[i+len(marker):]
			if j := strings.IndexAny(id, "?#/"); j >= 0 {
				id = id[:j]
			}
			id = strings.TrimSpace(id)
			if !isChatGPTConversationSlug(id) {
				return ""
			}
			return id
		}
	}
	return ""
}

// isChatGPTConversationSlug accepts current ChatGPT thread slugs, including
// "WEB:<uuid>" which appears in live chatgpt.com/c/WEB:... URLs.
func isChatGPTConversationSlug(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || len(id) < 8 || len(id) > 160 {
		return false
	}
	if strings.Contains(id, " ") || strings.Contains(id, "/") {
		return false
	}
	// Allow one optional "WEB:" / "g-" style prefix used by ChatGPT web.
	body := id
	if i := strings.Index(id, ":"); i >= 0 {
		prefix := id[:i]
		body = id[i+1:]
		if prefix == "" || body == "" {
			return false
		}
		for _, r := range prefix {
			ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
			if !ok {
				return false
			}
		}
		if strings.Contains(body, ":") {
			return false
		}
	}
	for _, r := range body {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
		if !ok {
			return false
		}
	}
	return len(body) >= 8
}


func (s *Store) lifecycle(goalID string, status Status, eventType, summary string, extra func(*Goal, time.Time)) (Goal, error) {
	summary = strings.TrimSpace(summary)
	if summary != "" {
		if err := validateTextLimit(eventType+" summary", summary, maxSummaryBytes); err != nil {
			return Goal{}, err
		}
	}
	g, err := s.mutate(goalID, func(g *Goal, now time.Time) error {
		if isTerminal(g.Status) && g.Status != status {
			return invalidInput(fmt.Sprintf("cannot transition from terminal status %s", g.Status))
		}
		g.Status = status
		if summary != "" {
			g.Summary = summary
		}
		if extra != nil {
			extra(g, now)
		}
		g.CapsuleVersion++
		appendGoalEvent(g, Event{Type: eventType, Summary: summary, CreatedAt: now})
		return nil
	})
	if err != nil {
		return Goal{}, err
	}
	_ = s.events.Append(g.ID, eventType, summary, map[string]any{"status": status})
	return g, nil
}

func (s *Store) mutate(id string, fn func(*Goal, time.Time) error) (Goal, error) {
	release, err := s.acquireStoreLock()
	if err != nil {
		return Goal{}, err
	}
	defer release()
	g, err := s.loadLocked(id)
	if err != nil {
		return Goal{}, err
	}
	now := time.Now().UTC()
	if err := fn(&g, now); err != nil {
		return Goal{}, err
	}
	g.UpdatedAt = now
	if err := s.saveLocked(g); err != nil {
		return Goal{}, err
	}
	return g, nil
}

func (s *Store) loadLocked(id string) (Goal, error) {
	if err := validateGoalID(id); err != nil {
		return Goal{}, err
	}
	data, err := readGoalStateFile(filepath.Join(s.root, id+".json"))
	if os.IsNotExist(err) {
		return Goal{}, fmt.Errorf("%w: %s", ErrGoalNotFound, id)
	}
	if err != nil {
		return Goal{}, err
	}
	return decodeGoal(data, id)
}

func (s *Store) saveLocked(g Goal) error {
	if err := validateGoalID(g.ID); err != nil {
		return err
	}
	if len(g.Events) > maxEventsEmbedded {
		g.Events = append([]Event(nil), g.Events[len(g.Events)-maxEventsEmbedded:]...)
	}
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > maxGoalStateFileBytes {
		return fmt.Errorf("goal state exceeds %d bytes", maxGoalStateFileBytes)
	}
	return atomicfile.Write(filepath.Join(s.root, g.ID+".json"), data, 0o600)
}

func readGoalStateFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxGoalStateFileBytes+1))
}

func decodeGoal(data []byte, label string) (Goal, error) {
	if len(data) > maxGoalStateFileBytes {
		return Goal{}, fmt.Errorf("goal state file too large: %s", label)
	}
	var g Goal
	if err := json.Unmarshal(data, &g); err != nil {
		return Goal{}, fmt.Errorf("decode goal %s: %w", label, err)
	}
	if g.ID == "" {
		return Goal{}, fmt.Errorf("goal id missing in %s", label)
	}
	return g, nil
}

func appendGoalEvent(g *Goal, event Event) {
	g.Events = append(g.Events, event)
	if len(g.Events) > maxEventsEmbedded {
		g.Events = g.Events[len(g.Events)-maxEventsEmbedded:]
	}
}

func requireActiveLease(g *Goal, leaseID string, now time.Time) error {
	leaseID = strings.TrimSpace(leaseID)
	if leaseID == "" {
		return ErrLeaseRequired
	}
	if g.ActiveLease == nil {
		return ErrLeaseRequired
	}
	if g.ActiveLease.LeaseID != leaseID {
		return stateConflict("reasoning_lease_id does not match active lease", g, g.CapsuleVersion, leaseID)
	}
	if !g.ActiveLease.ExpiresAt.After(now) {
		return ErrLeaseExpired
	}
	return nil
}

func isTerminal(status Status) bool {
	switch status {
	case StatusCompleted, StatusCancelled, StatusFailed:
		return true
	default:
		return false
	}
}

// shouldSoftRejectBlock is true when the model issued decision=block but durable
// goal progress (parts on disk, evidence, or staged source paths) already exists.
// Prevents false "no source" / safety blocks from killing multi-batch book jobs.
func shouldSoftRejectBlock(g Goal, summary string) bool {
	// Always soft-reject if progressive part files or evidence exist.
	if len(g.Evidence) > 0 {
		return true
	}
	for _, c := range g.SuccessCriteria {
		expr := strings.TrimSpace(c.Expression)
		if !strings.HasPrefix(expr, "file_min_bytes:") {
			continue
		}
		rest := strings.TrimPrefix(expr, "file_min_bytes:")
		i := strings.LastIndex(rest, ":")
		if i <= 0 {
			continue
		}
		path := rest[:i]
		base := filepath.Base(path)
		if !(strings.HasPrefix(base, "letters_") || strings.HasPrefix(base, "chapter_")) {
			continue
		}
		if st, err := os.Stat(path); err == nil && !st.IsDir() && st.Size() > 0 {
			return true
		}
	}
	// Source path mentioned in request/problem is readable → model should not block.
	blob := g.CurrentRequest + "\n" + g.CurrentProblem + "\n" + summary
	for _, tok := range strings.FieldsFunc(blob, func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\t' || r == '"' || r == '\'' || r == '，' || r == '。' || r == '、' || r == ')' || r == '('
	}) {
		tok = strings.Trim(tok, "()[]{},:;")
		if !strings.HasSuffix(tok, ".txt") && !strings.HasSuffix(tok, ".md") && !strings.Contains(tok, "batch_") {
			continue
		}
		if st, err := os.Stat(tok); err == nil && !st.IsDir() && st.Size() > 100 {
			return true
		}
	}
	return false
}

func validDecision(d Decision) bool {
	switch d {
	case DecisionContinue, DecisionBlock, DecisionComplete, DecisionReplan, DecisionPause, DecisionVerify:
		return true
	default:
		return false
	}
}

func normalizeCriteria(values []SuccessCriterionInput, now time.Time) ([]SuccessCriterion, error) {
	if len(values) == 0 {
		return nil, invalidInput("at least one success criterion is required")
	}
	if len(values) > maxCriteria {
		return nil, invalidInput(fmt.Sprintf("success criteria cannot exceed %d", maxCriteria))
	}
	out := make([]SuccessCriterion, 0, len(values))
	seen := map[string]struct{}{}
	for i, v := range values {
		id := strings.TrimSpace(v.ID)
		if id == "" {
			id = fmt.Sprintf("crit_%02d", i+1)
		}
		if !validTokenID(id) {
			return nil, invalidInput(fmt.Sprintf("invalid criterion id %q", id))
		}
		if _, ok := seen[id]; ok {
			return nil, invalidInput(fmt.Sprintf("duplicate criterion id %q", id))
		}
		typ := v.Type
		if typ == "" {
			typ = CriterionManual
		}
		if typ != CriterionCommand && typ != CriterionMetric && typ != CriterionBrowser && typ != CriterionManual {
			return nil, invalidInput(fmt.Sprintf("unsupported criterion type %q", typ))
		}
		expr := strings.TrimSpace(v.Expression)
		if expr == "" {
			return nil, invalidInput("criterion expression is required")
		}
		if err := validateTextLimit("criterion expression", expr, maxCriterionExprBytes); err != nil {
			return nil, err
		}
		seen[id] = struct{}{}
		out = append(out, SuccessCriterion{
			ID:         id,
			Type:       typ,
			Expression: expr,
			Status:     CriterionPending,
			UpdatedAt:  now,
		})
	}
	return out, nil
}

func normalizeConstraints(values []Constraint) ([]Constraint, error) {
	if len(values) > maxConstraints {
		return nil, invalidInput(fmt.Sprintf("constraints cannot exceed %d", maxConstraints))
	}
	out := make([]Constraint, 0, len(values))
	for _, v := range values {
		typ := v.Type
		val := strings.TrimSpace(v.Value)
		if typ == "" || val == "" {
			return nil, invalidInput("constraint type and value are required")
		}
		if typ != ConstraintProhibition && typ != ConstraintQuality && typ != ConstraintApproval && typ != ConstraintBudget {
			return nil, invalidInput(fmt.Sprintf("unsupported constraint type %q", typ))
		}
		if err := validateTextLimit("constraint value", val, maxConstraintValueBytes); err != nil {
			return nil, err
		}
		out = append(out, Constraint{Type: typ, Value: val})
	}
	return out, nil
}

func normalizeMilestones(values []MilestoneInput, now time.Time) ([]Milestone, error) {
	if len(values) > maxMilestones {
		return nil, invalidInput(fmt.Sprintf("milestones cannot exceed %d", maxMilestones))
	}
	out := make([]Milestone, 0, len(values))
	seen := map[string]struct{}{}
	for i, v := range values {
		id := strings.TrimSpace(v.ID)
		title := strings.TrimSpace(v.Title)
		if id == "" {
			id = fmt.Sprintf("ms_%02d", i+1)
		}
		if title == "" {
			return nil, invalidInput("milestone title is required")
		}
		if !validTokenID(id) {
			return nil, invalidInput(fmt.Sprintf("invalid milestone id %q", id))
		}
		if _, ok := seen[id]; ok {
			return nil, invalidInput(fmt.Sprintf("duplicate milestone id %q", id))
		}
		if err := validateTextLimit("milestone title", title, maxMilestoneTitleBytes); err != nil {
			return nil, err
		}
		seen[id] = struct{}{}
		out = append(out, Milestone{ID: id, Title: title, Status: MilestonePending, UpdatedAt: now})
	}
	return out, nil
}

func normalizeCommitSteps(values []CommitStepInput, now time.Time) ([]Step, error) {
	if len(values) > maxCommitSteps {
		return nil, invalidInput(fmt.Sprintf("commit steps cannot exceed %d", maxCommitSteps))
	}
	out := make([]Step, 0, len(values))
	for i, v := range values {
		action := v.Action
		if _, ok := KnownStepActions[action]; !ok {
			return nil, invalidInput(fmt.Sprintf("unknown step action %q", action))
		}
		summary := strings.TrimSpace(v.Summary)
		if err := validateTextLimit("step summary", summary, maxStepSummaryBytes); err != nil {
			return nil, err
		}
		targets := make([]string, 0, len(v.Targets))
		for _, t := range v.Targets {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			if err := validateTextLimit("step target", t, maxSummaryBytes); err != nil {
				return nil, err
			}
			targets = append(targets, t)
		}
		id := fmt.Sprintf("step_%02d_%s", i+1, action)
		if key := strings.TrimSpace(v.Idempotency); key != "" {
			if !validTokenID(key) {
				return nil, invalidInput(fmt.Sprintf("invalid idempotency key %q", key))
			}
			id = key
		}
		out = append(out, Step{
			ID:          id,
			MilestoneID: strings.TrimSpace(v.MilestoneID),
			Action:      action,
			Targets:     targets,
			Summary:     summary,
			Status:      StepPending,
			Idempotency: strings.TrimSpace(v.Idempotency),
			UpdatedAt:   now,
		})
	}
	return out, nil
}

func mergeSteps(existing, incoming []Step, now time.Time) []Step {
	byID := map[string]int{}
	out := append([]Step(nil), existing...)
	for i, step := range out {
		byID[step.ID] = i
	}
	for _, step := range incoming {
		if idx, ok := byID[step.ID]; ok {
			// Idempotent replay: keep completed, refresh pending/failed.
			if out[idx].Status == StepCompleted {
				continue
			}
			out[idx] = step
			out[idx].UpdatedAt = now
			continue
		}
		out = append(out, step)
	}
	if len(out) > maxSteps {
		out = out[len(out)-maxSteps:]
	}
	return out
}

func activateMilestone(g *Goal, id string, now time.Time) {
	for i := range g.Milestones {
		if g.Milestones[i].ID == id {
			if g.Milestones[i].Status == MilestonePending {
				g.Milestones[i].Status = MilestoneActive
			}
			g.Milestones[i].UpdatedAt = now
			return
		}
		// Auto-complete previous active when switching.
		if g.Milestones[i].Status == MilestoneActive {
			g.Milestones[i].Status = MilestoneCompleted
			g.Milestones[i].UpdatedAt = now
		}
	}
	// Unknown milestone id: append lightly.
	if validTokenID(id) && len(g.Milestones) < maxMilestones {
		g.Milestones = append(g.Milestones, Milestone{
			ID: id, Title: id, Status: MilestoneActive, UpdatedAt: now,
		})
	}
}

func mergeBudget(base, override Budget) Budget {
	if override.MaxReasoningTurns > 0 {
		base.MaxReasoningTurns = override.MaxReasoningTurns
	}
	if override.MaxReplans > 0 {
		base.MaxReplans = override.MaxReplans
	}
	if override.MaxConversationRotations > 0 {
		base.MaxConversationRotations = override.MaxConversationRotations
	}
	if override.MaxRuntimeMinutes > 0 {
		base.MaxRuntimeMinutes = override.MaxRuntimeMinutes
	}
	if override.MaxIdenticalFailures > 0 {
		base.MaxIdenticalFailures = override.MaxIdenticalFailures
	}
	if override.MaxBrowserRetries > 0 {
		base.MaxBrowserRetries = override.MaxBrowserRetries
	}
	if override.MaxChangedFiles > 0 {
		base.MaxChangedFiles = override.MaxChangedFiles
	}
	return base
}

func validateGoalID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" || !strings.HasPrefix(id, "goal_") {
		return invalidInput("invalid goal id")
	}
	if !validTokenID(id) {
		return invalidInput("invalid goal id characters")
	}
	return nil
}

func validTokenID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func validateTextLimit(label, value string, maxBytes int) error {
	if value == "" {
		return nil
	}
	if len(value) > maxBytes {
		return invalidInput(fmt.Sprintf("%s exceeds %d bytes", label, maxBytes))
	}
	if !utf8.ValidString(value) {
		return invalidInput(fmt.Sprintf("%s is not valid UTF-8", label))
	}
	return nil
}

func newPrefixedID(prefix string) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(b[:]), nil
}

func truncateForTitle(objective string) string {
	objective = strings.TrimSpace(objective)
	if objective == "" {
		return ""
	}
	runes := []rune(objective)
	if len(runes) <= 80 {
		return objective
	}
	return string(runes[:80])
}


func formatExecutionFailureDetail(result RunResult) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(result.Summary))
	for _, step := range result.Steps {
		if step.OK || step.Skipped {
			continue
		}
		b.WriteString("\n- step ")
		b.WriteString(firstNonEmptyStr(step.Name, step.Type))
		if step.ExitCode != 0 {
			b.WriteString(fmt.Sprintf(" exit=%d", step.ExitCode))
		}
		if step.Error != "" {
			b.WriteString(": ")
			b.WriteString(step.Error)
		}
		if step.Stderr != "" {
			b.WriteString("\n  stderr: ")
			b.WriteString(trimRunes(step.Stderr, 500))
		} else if step.Evidence != nil && step.Evidence.Data != nil {
			if s, _ := step.Evidence.Data["stderr_tail"].(string); strings.TrimSpace(s) != "" {
				b.WriteString("\n  stderr: ")
				b.WriteString(trimRunes(s, 500))
			}
		}
		if step.Stdout != "" {
			b.WriteString("\n  stdout: ")
			b.WriteString(trimRunes(step.Stdout, 300))
		}
	}
	return strings.TrimSpace(b.String())
}

// detectEmptyArtifactRegressions finds tracked output paths that are currently empty/missing
// after we previously observed non-empty content for them.
func detectEmptyArtifactRegressions(g Goal) []string {
	paths := map[string]int64{}
	for _, c := range g.SuccessCriteria {
		expr := strings.TrimSpace(c.Expression)
		switch {
		case strings.HasPrefix(expr, "file_min_bytes:"):
			path, n, err := splitPathInt(strings.TrimPrefix(expr, "file_min_bytes:"))
			if err == nil && path != "" {
				if cur, ok := paths[path]; !ok || n > cur {
					paths[path] = n
				}
			}
		case strings.HasPrefix(expr, "file_min_lines:"):
			path, _, err := splitPathInt(strings.TrimPrefix(expr, "file_min_lines:"))
			if err == nil && path != "" {
				if _, ok := paths[path]; !ok {
					paths[path] = 1
				}
			}
		case strings.HasPrefix(expr, "test -s "):
			p := strings.TrimSpace(strings.TrimPrefix(expr, "test -s "))
			p = strings.Trim(p, `"'`)
			if p != "" {
				if _, ok := paths[p]; !ok {
					paths[p] = 1
				}
			}
		}
	}
	// seed from prior evidence sizes
	for _, ev := range g.Evidence {
		if ev.Data == nil {
			continue
		}
		for _, key := range []string{"path", "absolute_path"} {
			p, _ := ev.Data[key].(string)
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if b, ok := asInt64(ev.Data["bytes"]); ok && b > 0 {
				if _, exists := paths[p]; !exists {
					paths[p] = 1
				}
			}
			if b, ok := asInt64(ev.Data["previous_bytes"]); ok && b > 0 {
				if _, exists := paths[p]; !exists {
					paths[p] = 1
				}
			}
			if b, ok := asInt64(ev.Data["size_bytes"]); ok && b > 0 {
				if _, exists := paths[p]; !exists {
					paths[p] = 1
				}
			}
		}
	}
	var regs []string
	for path, minBytes := range paths {
		if minBytes <= 0 {
			minBytes = 1
		}
		// only if we previously saw non-empty content for this path
		hadNonEmpty := false
		for _, ev := range g.Evidence {
			if ev.Data == nil {
				continue
			}
			p1, _ := ev.Data["path"].(string)
			p2, _ := ev.Data["absolute_path"].(string)
			if p1 != path && p2 != path {
				continue
			}
			if b, ok := asInt64(ev.Data["bytes"]); ok && b > 0 {
				hadNonEmpty = true
				break
			}
			if b, ok := asInt64(ev.Data["previous_bytes"]); ok && b > 0 {
				hadNonEmpty = true
				break
			}
			if b, ok := asInt64(ev.Data["size_bytes"]); ok && b > 0 {
				hadNonEmpty = true
				break
			}
		}
		if !hadNonEmpty {
			continue
		}
		st, err := os.Stat(path)
		size := int64(0)
		if err == nil && !st.IsDir() {
			size = st.Size()
		}
		if err != nil || st.IsDir() || size < minBytes {
			regs = append(regs, fmt.Sprintf("%s (size=%d, need>=%d)", path, size, minBytes))
		}
	}
	sort.Strings(regs)
	return regs
}

func asInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case int:
		return int64(t), true
	case int32:
		return int64(t), true
	case int64:
		return t, true
	case float64:
		return int64(t), true
	case float32:
		return int64(t), true
	default:
		return 0, false
	}
}


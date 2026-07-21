package orchestrator

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/uvwt/agentdock/internal/goal"
)

// Waker wakes a ChatGPT (or other) reasoning worker for a goal.
type Waker interface {
	Wake(ctx context.Context, goalID string) (map[string]any, error)
	ForceRotate()
	// ClearWakeCooldown allows the next Wake to re-paste after a stagnant wait
	// (book jobs need multi-batch continues; cooldown otherwise blocks re-prompt).
	ClearWakeCooldown(goalID string)
	Status() map[string]any
}

// Executor runs deterministic pending steps for a goal and persists results.
type Executor interface {
	ExecutePending(ctx context.Context, g goal.Goal) (goal.RunResult, goal.Goal, error)
}

// ActivitySource reports real MCP tool invocations (not browser worker probes).
// Optional on Orchestrator — when set, wait_commit treats tool calls as progress.
type ActivitySource interface {
	// MCPToolActivitySince returns true if a model-facing MCP tool ran at/after since.
	MCPToolActivitySince(since time.Time) bool
	// MCPToolActivitySummary is a short operator string of recent tools.
	MCPToolActivitySummary(since time.Time) string
}

// Config tunes the L3 unattended loop.
type Config struct {
	// How long to wait for commit_turn after a successful Wake.
	CommitWait time.Duration
	// Poll interval while waiting for capsule version / status change.
	// During wait_commit this is store-only (no CDP). Keep it gentle.
	PollInterval time.Duration
	// Max consecutive wake attempts that produce no commit.
	MaxNoCommit int
	// Max orchestrator ticks before forced stop (safety).
	MaxTicks int
	// Pause between ticks.
	TickPause time.Duration
	// If true, rotate worker conversation after no-commit timeout.
	RotateOnNoCommit bool
	// ToolActivityWait is how long after a delivered resume paste we require
	// evidence of MCP/tool use (goal event, evidence, steps, output growth, commit).
	// Zero uses default. Negative disables the watchdog.
	ToolActivityWait time.Duration
	// StagnantAfter is how long wait_commit tolerates thrash-only or stalled
	// productive tools before ending early so orch can re-wake. Zero → 4m.
	StagnantAfter time.Duration
}

func DefaultConfig() Config {
	return Config{
		// Book-scale MCP turns routinely exceed a few minutes; keep waiting rather
		// than re-pasting resume prompts into a live ChatGPT tab.
		CommitWait:       12 * time.Minute,
		PollInterval:     5 * time.Second,
		MaxNoCommit:      4,
		MaxTicks:         200,
		TickPause:        5 * time.Second,
		// Opening new chats on no-commit abandons in-flight tool use and freezes Chrome.
		RotateOnNoCommit: false,
		// If the model never touches MCP after paste, fail fast (connector/permission).
		ToolActivityWait: 3 * time.Minute,
		StagnantAfter:    4 * time.Minute,
	}
}

// Status is operator-facing orchestrator state for one goal run.
type Status struct {
	GoalID         string    `json:"goal_id"`
	Running        bool      `json:"running"`
	Phase          string    `json:"phase"` // idle|waking|wait_commit|executing|verifying|rotating|completed|blocked|stopped|error
	Ticks          int       `json:"ticks"`
	NoCommitStreak int       `json:"no_commit_streak"`
	LastError      string    `json:"last_error,omitempty"`
	LastMessage    string    `json:"last_message,omitempty"`
	StartedAt      time.Time `json:"started_at,omitempty"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
	GoalStatus     string    `json:"goal_status,omitempty"`
	CapsuleVersion int       `json:"capsule_version,omitempty"`
}

// Orchestrator is the L3 director: AgentDock drives until goal terminal.
// ChatGPT is only woken when reasoning is needed; long work runs locally.
type Orchestrator struct {
	store    *goal.Store
	waker    Waker
	executor Executor
	activity ActivitySource
	cfg      Config

	mu   sync.Mutex
	runs map[string]*runState // goalID -> run
}

type runState struct {
	cancel       context.CancelFunc
	status       Status
	softUnblocks int // auto-resume of false model blocks this run
}

func New(store *goal.Store, waker Waker, executor Executor, cfg Config) *Orchestrator {
	if cfg.CommitWait <= 0 || cfg.PollInterval <= 0 {
		cfg = DefaultConfig()
	}
	// ToolActivityWait: 0 → product default; negative → disabled (tests / manual).
	if cfg.ToolActivityWait == 0 {
		cfg.ToolActivityWait = 3 * time.Minute
	}
	if cfg.StagnantAfter <= 0 {
		cfg.StagnantAfter = 4 * time.Minute
	}
	return &Orchestrator{
		store:    store,
		waker:    waker,
		executor: executor,
		cfg:      cfg,
		runs:     map[string]*runState{},
	}
}

// SetActivitySource wires real MCP tool-call detection into wait_commit.
func (o *Orchestrator) SetActivitySource(src ActivitySource) {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.activity = src
	o.mu.Unlock()
}

// Start begins unattended orchestration for goalID (idempotent if already running).
func (o *Orchestrator) Start(goalID string) (Status, error) {
	goalID = strings.TrimSpace(goalID)
	if goalID == "" {
		return Status{}, fmt.Errorf("goal_id is required")
	}
	if o.store == nil {
		return Status{}, fmt.Errorf("goal store is required")
	}
	g, err := o.store.Get(goalID)
	if err != nil {
		return Status{}, err
	}
	if isTerminal(g.Status) {
		return Status{GoalID: goalID, Phase: string(g.Status), GoalStatus: string(g.Status)}, fmt.Errorf("goal is already terminal: %s", g.Status)
	}

	o.mu.Lock()
	if st, ok := o.runs[goalID]; ok && st.status.Running {
		s := st.status
		o.mu.Unlock()
		return s, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	rs := &runState{
		cancel: cancel,
		status: Status{
			GoalID: goalID, Running: true, Phase: "starting",
			StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
			GoalStatus: string(g.Status), CapsuleVersion: g.CapsuleVersion,
		},
	}
	o.runs[goalID] = rs
	o.mu.Unlock()

	go o.loop(ctx, goalID)
	return o.Status(goalID), nil
}

// Stop cancels orchestration for goalID.
func (o *Orchestrator) Stop(goalID string) Status {
	o.mu.Lock()
	rs, ok := o.runs[goalID]
	if ok && rs.cancel != nil {
		rs.cancel()
		rs.status.Running = false
		rs.status.Phase = "stopped"
		rs.status.UpdatedAt = time.Now().UTC()
		rs.status.LastMessage = "orchestrator stopped by operator"
	}
	o.mu.Unlock()
	return o.Status(goalID)
}

// Status returns current orchestration status.
func (o *Orchestrator) Status(goalID string) Status {
	o.mu.Lock()
	defer o.mu.Unlock()
	if rs, ok := o.runs[goalID]; ok {
		return rs.status
	}
	return Status{GoalID: goalID, Phase: "idle", Running: false}
}

// ListRunning returns statuses for active runs.
func (o *Orchestrator) ListRunning() []Status {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]Status, 0, len(o.runs))
	for _, rs := range o.runs {
		if rs.status.Running {
			out = append(out, rs.status)
		}
	}
	return out
}

func (o *Orchestrator) setStatus(goalID string, mut func(*Status)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	rs, ok := o.runs[goalID]
	if !ok {
		return
	}
	mut(&rs.status)
	rs.status.UpdatedAt = time.Now().UTC()
}

func (o *Orchestrator) canSoftUnblock(goalID string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	rs, ok := o.runs[goalID]
	if !ok {
		return false
	}
	return rs.softUnblocks < maxSoftUnblocks
}

func (o *Orchestrator) incSoftUnblock(goalID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if rs, ok := o.runs[goalID]; ok {
		rs.softUnblocks++
	}
}

func (o *Orchestrator) loop(ctx context.Context, goalID string) {
	defer func() {
		o.mu.Lock()
		if rs, ok := o.runs[goalID]; ok {
			rs.status.Running = false
			if rs.status.Phase == "starting" || rs.status.Phase == "waking" || rs.status.Phase == "wait_commit" || rs.status.Phase == "executing" || rs.status.Phase == "verifying" {
				rs.status.Phase = "stopped"
			}
			rs.status.UpdatedAt = time.Now().UTC()
		}
		o.mu.Unlock()
	}()

	cfg := o.cfg
	noCommit := 0

	for tick := 0; tick < cfg.MaxTicks; tick++ {
		if ctx.Err() != nil {
			o.setStatus(goalID, func(s *Status) {
				s.Phase = "stopped"
				s.LastMessage = "context cancelled"
			})
			return
		}

		g, err := o.store.Get(goalID)
		if err != nil {
			o.setStatus(goalID, func(s *Status) {
				s.Phase = "error"
				s.LastError = err.Error()
			})
			return
		}
		o.setStatus(goalID, func(s *Status) {
			s.Ticks = tick + 1
			s.GoalStatus = string(g.Status)
			s.CapsuleVersion = g.CapsuleVersion
			s.NoCommitStreak = noCommit
		})

		// Terminal?
		if isTerminal(g.Status) {
			o.setStatus(goalID, func(s *Status) {
				s.Phase = string(g.Status)
				s.LastMessage = "goal terminal"
			})
			return
		}
		// Human gates — do not spin.
		if g.Status == goal.StatusAwaitingApproval || g.Status == goal.StatusAwaitingUser ||
			g.Status == goal.StatusAwaitingCredentials || g.Status == goal.StatusPaused {
			o.setStatus(goalID, func(s *Status) {
				s.Phase = "blocked"
				s.LastMessage = "waiting for human: " + string(g.Status)
			})
			// Stay running but sleep longer; operator may approve/resume.
			if !sleep(ctx, 5*time.Second) {
				return
			}
			continue
		}
		if g.Status == goal.StatusBlocked {
			// Soft-recover false model blocks when durable parts/source exist.
			// Hard blocks (permission, wake fail, no MCP, max ticks) still stop.
			if o.canSoftUnblock(goalID) && isSoftRecoverableBlock(g.Blocker) && hasDurableBookProgress(g) {
				o.incSoftUnblock(goalID)
				req, prob, ok := buildNextBatchRequest(g, true)
				if !ok {
					req = firstNonEmpty(g.CurrentRequest, "continue toward unmet criteria; do not decision=block")
					prob = firstNonEmpty(g.CurrentProblem, "soft-unblocked false model block")
				}
				if _, err := o.store.Resume(goalID, "orchestrator soft-unblock: "+firstNonEmpty(g.Blocker, "false block")); err != nil {
					o.setStatus(goalID, func(s *Status) {
						s.Phase = "blocked"
						s.LastMessage = firstNonEmpty(g.Blocker, "goal blocked")
						s.LastError = err.Error()
					})
					return
				}
				if g2, err := o.store.RequestReasoning(goalID, req, prob); err == nil {
					g = g2
				}
				if o.waker != nil {
					o.waker.ClearWakeCooldown(goalID)
				}
				o.setStatus(goalID, func(s *Status) {
					s.Phase = "waking"
					s.LastMessage = "soft-unblocked false model block; re-requesting next batch"
					s.GoalStatus = string(g.Status)
				})
				if !sleep(ctx, cfg.TickPause) {
					return
				}
				continue
			}
			o.setStatus(goalID, func(s *Status) {
				s.Phase = "blocked"
				s.LastMessage = firstNonEmpty(g.Blocker, "goal blocked")
			})
			return
		}

		// 1) Need reasoning?
		if needsReasoning(g) {
			if o.waker == nil {
				o.setStatus(goalID, func(s *Status) {
					s.Phase = "error"
					s.LastError = "no waker configured"
				})
				return
			}
			// Ensure status is awaiting_reasoning for clear capsule.
			if g.Status != goal.StatusAwaitingReasoning {
				if g2, err := o.store.RequestReasoning(goalID, g.CurrentRequest, g.CurrentProblem); err == nil {
					g = g2
				}
			}
			beforeVer := g.CapsuleVersion
			beforeStatus := g.Status

			// MCP busy gate: never CDP-wake while model tools are still running.
			// Re-waking into a busy ChatGPT tab pins Chrome (r9 page_stuck).
			// Use a short window; ignore commit/get-only tails that linger in the ring
			// after a successful turn (micro demo over-delayed wakes).
			if o.activity != nil {
				sum := o.activity.MCPToolActivitySummary(time.Now().Add(-20 * time.Second))
				if isLiveMCPBusySummary(sum) {
					o.setStatus(goalID, func(s *Status) {
						s.Phase = "wait_idle"
						s.LastMessage = "MCP tools still active (" + sum + "); delaying wake (no CDP)"
					})
					if !sleep(ctx, busyWakeBackoff(cfg.TickPause)) {
						return
					}
					continue
				}
			}

			o.setStatus(goalID, func(s *Status) {
				s.Phase = "waking"
				s.LastMessage = "waking ChatGPT worker with resume prompt"
			})
			wakeCtx, cancel := context.WithTimeout(ctx, cfg.CommitWait)
			wakeRes, wakeErr := o.waker.Wake(wakeCtx, goalID)
			cancel()
			if wakeErr == nil {
				wakeErr = wakeResultError(wakeRes)
			}
		if wakeErr != nil {
				// Permission dialog could not be auto-approved → hard block (operator rule).
				if isPermissionHardFail(wakeErr) {
					_, _ = o.store.MarkBlocked(goal.MarkBlockedInput{
						GoalID:   goalID,
						Reason:   "orchestrator: tool permission auto-approve failed",
						Tried:    wakeErr.Error(),
						NeedUser: "manually click Allow on the ChatGPT tool/connector dialog (or disable the tool), then goal_manage resume + orchestrate_start",
					})
					o.setStatus(goalID, func(s *Status) {
						s.Phase = "blocked"
						s.LastError = wakeErr.Error()
						s.LastMessage = "blocked: tool permission unresolved"
					})
					return
				}
				// Page still busy / stuck CDP: do NOT count as a failed paste cycle.
				// Wait without re-pasting or SoftRebind (CDP thrash freezes Chrome).
				if isPageBusyWakeErr(wakeErr) {
					o.setStatus(goalID, func(s *Status) {
						s.Phase = "wait_idle"
						s.LastError = wakeErr.Error()
						s.LastMessage = "page busy or blocked; waiting without re-paste"
					})
					if !sleep(ctx, busyWakeBackoff(cfg.TickPause)) {
						return
					}
					continue
				}
				noCommit++
				o.setStatus(goalID, func(s *Status) {
					s.LastError = wakeErr.Error()
					s.LastMessage = "wake failed"
					s.NoCommitStreak = noCommit
				})
				// Only hard-rotate on login/quota class errors.
				if o.waker != nil && needsHardRotate(wakeErr) {
					o.setStatus(goalID, func(s *Status) { s.Phase = "rotating"; s.LastMessage = "hard-rotating conversation after fatal wake failure" })
					o.waker.ForceRotate()
				}
				if noCommit >= cfg.MaxNoCommit {
					_, _ = o.store.MarkBlocked(goal.MarkBlockedInput{
						GoalID:   goalID,
						Reason:   "orchestrator: wake/commit failed repeatedly",
						Tried:    fmt.Sprintf("no_commit_streak=%d last=%v", noCommit, wakeErr),
						NeedUser: "check browser login, MCP connection, and ChatGPT quota; then goal_manage resume + orchestrate_start",
					})
					o.setStatus(goalID, func(s *Status) { s.Phase = "blocked"; s.LastMessage = "blocked after repeated wake failures" })
					return
				}
				backoff := wakeFailureBackoff(noCommit, cfg.TickPause)
				if !sleep(ctx, backoff) {
					return
				}
				continue
			}
			// Successful wake (including cooldown skip). wait_commit is store-only:
			// no CDP snapshot/evaluate (hands-off after paste — r5 freeze root).
			_ = wakeRes
			// Cooldown skip still counts as "paste already delivered" for watchdog.
			delivered := !wakeSkipped(wakeRes)
			if wakeSkipped(wakeRes) {
				// already_delivered / cooldown: treat as delivered for MCP watchdog.
				delivered = true
			}

			// Snapshot progress baselines AFTER wake so lease/bind events from paste
			// are not counted as model MCP activity.
			gBase, _ := o.store.Get(goalID)
			if gBase.ID == "" {
				gBase = g
			}
			beforeEvents := len(gBase.Events)
			beforeEvidence := len(gBase.Evidence)
			beforeOut := trackedOutputBytes(gBase)
			beforeSteps := len(gBase.Steps)
			wakeAt := time.Now()

			// 2) Wait for commit_turn (capsule version bump or leave awaiting_reasoning)
			o.setStatus(goalID, func(s *Status) {
				s.Phase = "wait_commit"
				if delivered {
					s.LastMessage = "waiting for goal_manage commit_turn via MCP (hands-off, no CDP)"
				} else {
					s.LastMessage = "resume already delivered recently; waiting for commit_turn (hands-off)"
				}
			})
			// If output files are already growing, give the model a longer commit window.
			commitWait := cfg.CommitWait
			if beforeOut > 0 {
				if commitWait < 20*time.Minute {
					commitWait = 20 * time.Minute
				}
			}
			// Tool-activity watchdog: keep armed even on cooldown skip.
			// r6 bug: manual wake + orch re-Wake returned cooldown → toolWait=-1
			// and never blocked on "no MCP activity". Cooldown means paste already
			// happened recently; still require MCP progress from that paste.
			toolWait := cfg.ToolActivityWait
			if toolWait == 0 {
				toolWait = 3 * time.Minute
			}
			// Negative still disables (tests). Cooldown skip keeps watchdog ON.
			committed, gAfter, activity, err := o.waitCommitHandsOff(ctx, goalID, beforeVer, beforeStatus, commitWait, cfg.PollInterval, toolWait, progressBaseline{
				events:   beforeEvents,
				evidence: beforeEvidence,
				outBytes: beforeOut,
				steps:    beforeSteps,
				wakeAt:   wakeAt,
			})
			if err != nil {
				o.setStatus(goalID, func(s *Status) { s.Phase = "error"; s.LastError = err.Error() })
				return
			}
			if !committed {
				outBytes := trackedOutputBytes(gAfter)
				// No MCP/tool activity after a delivered paste → hard block.
				// Only when ToolActivityWait is enabled (toolWait >= 0).
				// Hard-block only with zero durable progress. r9 had letters_01.md + evidence
				// then got blocked on a later quiet wake — aborting a working book job.
				if delivered && !activity && toolWait >= 0 && outBytes == 0 && len(gAfter.Evidence) == 0 {
					mcpSummary := "none"
					if o.activity != nil {
						mcpSummary = o.activity.MCPToolActivitySummary(wakeAt)
						if mcpSummary == "" {
							mcpSummary = "none"
						}
					}
					_, _ = o.store.MarkBlocked(goal.MarkBlockedInput{
						GoalID:   goalID,
						Reason:   "orchestrator: no MCP/tool activity after resume paste",
						Tried:    fmt.Sprintf("tool_activity_wait=%s capsule=%d events=%d evidence=%d out_bytes=%d mcp_calls=%s", toolWait, gAfter.CapsuleVersion, len(gAfter.Events), len(gAfter.Evidence), outBytes, mcpSummary),
						NeedUser: "open the bound ChatGPT conversation, confirm AgentDock/Svananda MCP is connected in that chat, approve tool permission if shown, ensure model calls goal_manage get then commit_turn; then resume + orchestrate_start",
					})
					o.setStatus(goalID, func(s *Status) {
						s.Phase = "blocked"
						s.LastMessage = "blocked: no MCP tool activity after wake"
					})
					return
				}
				// Output grew *during this wait* without commit_turn: model is still writing.
				// Hold one short backoff, then loop (will re-wake only if still no commit later).
				// r9: "outBytes > 0" forever-hold after letters_01 prevented letters_02+ re-paste.
				if outBytes > beforeOut {
					o.setStatus(goalID, func(s *Status) {
						s.Phase = "wait_commit"
						s.LastMessage = fmt.Sprintf("output grew this cycle (%d bytes); brief hold then continue", outBytes)
					})
					_, _ = o.store.AddEvidence(goalID, goal.EvidenceRef{
						Kind:    "orchestrator",
						Summary: "output grew without commit_turn; short hold before next wake",
						Data:    map[string]any{"reason": "output_progress", "tracked_output_bytes": outBytes, "before_out_bytes": beforeOut},
					})
					if !sleep(ctx, busyWakeBackoff(cfg.TickPause)) {
						return
					}
					// Allow a follow-up resume paste for the next book batch.
					if o.waker != nil {
						o.waker.ClearWakeCooldown(goalID)
					}
					continue
				}
				// Only count a no-commit streak when we actually delivered a paste this cycle.
				if delivered {
					noCommit++
				}
				// Stagnant / thrash: rewrite current_request to next missing part + anti-search.
				// Without this, re-wake re-pastes the same stale batch (r9 search_text thrash).
				if req, prob, ok := buildNextBatchRequest(gAfter, true); ok {
					if g2, err := o.store.RequestReasoning(goalID, req, prob); err == nil {
						gAfter = g2
					}
				} else if activity {
					req := firstNonEmpty(gAfter.CurrentRequest, "continue toward unmet success criteria")
					req = "【停止 thrash】禁止 search_text 空轉；立刻 write/commit。\n" + req
					if g2, err := o.store.RequestReasoning(goalID, req, firstNonEmpty(gAfter.CurrentProblem, "thrash: non-productive tools only")); err == nil {
						gAfter = g2
					}
				}
				// Stagnant progress (parts exist but no commit / no new bytes this cycle):
				// clear cooldown so the next tick can re-paste a continue prompt (book batches).
				if o.waker != nil && delivered {
					o.waker.ClearWakeCooldown(goalID)
				}
				o.setStatus(goalID, func(s *Status) {
					s.NoCommitStreak = noCommit
					s.LastMessage = fmt.Sprintf("no commit_turn within timeout (out=%d activity=%v); will re-wake", outBytes, activity)
				})
				_, _ = o.store.AddEvidence(goalID, goal.EvidenceRef{
					Kind:    "orchestrator",
					Summary: "still waiting for commit_turn; keeping same conversation",
					Data: map[string]any{
						"reason":               "no_commit_timeout",
						"streak":               noCommit,
						"tracked_output_bytes": outBytes,
						"delivered":            delivered,
						"mcp_activity":         activity,
					},
				})
				if outBytes == 0 && noCommit >= 2 {
					_, _ = o.store.MarkBlocked(goal.MarkBlockedInput{
						GoalID:   goalID,
						Reason:   "orchestrator: no commit_turn and no output growth",
						Tried:    fmt.Sprintf("no_commit_streak=%d tracked_output_bytes=%d", noCommit, outBytes),
						NeedUser: "check ChatGPT tab received resume prompt, approve tool permissions, and ensure model writes parts via atomic_write",
					})
					o.setStatus(goalID, func(s *Status) { s.Phase = "blocked"; s.LastMessage = "blocked: no content progress" })
					return
				}
				if cfg.RotateOnNoCommit && delivered && noCommit >= cfg.MaxNoCommit-1 && o.waker != nil {
					o.setStatus(goalID, func(s *Status) { s.Phase = "rotating"; s.LastMessage = "last-resort conversation rotate after repeated no-commit" })
					o.waker.ForceRotate()
				}
				if noCommit >= cfg.MaxNoCommit {
					_, _ = o.store.MarkBlocked(goal.MarkBlockedInput{
						GoalID:   goalID,
						Reason:   "orchestrator: model did not call commit_turn",
						Tried:    fmt.Sprintf("%d wake attempts without structured commit", noCommit),
						NeedUser: "ensure ChatGPT is connected to this AgentDock MCP and follows resume prompt",
					})
					o.setStatus(goalID, func(s *Status) { s.Phase = "blocked" })
					return
				}
				if !sleep(ctx, cfg.TickPause) {
					return
				}
				continue
			}
			noCommit = 0
			g = gAfter
			// Keep wake cooldown after commit. ChatGPT often continues tool work in the
			// same turn; clearing cooldown here caused immediate re-paste mid-reply.
			o.setStatus(goalID, func(s *Status) {
				s.LastMessage = "commit_turn received"
				s.CapsuleVersion = g.CapsuleVersion
				s.GoalStatus = string(g.Status)
				s.NoCommitStreak = 0
			})
		}

		// Reload after possible commit.
		g, err = o.store.Get(goalID)
		if err != nil {
			o.setStatus(goalID, func(s *Status) { s.Phase = "error"; s.LastError = err.Error() })
			return
		}
		if isTerminal(g.Status) {
			o.setStatus(goalID, func(s *Status) { s.Phase = string(g.Status); s.LastMessage = "goal terminal after commit" })
			return
		}

		// 3) Execute deterministic pending steps locally (no model).
		if hasExecutablePending(g) && o.executor != nil &&
			(g.Status == goal.StatusExecuting || g.Status == goal.StatusVerifying || g.Status == goal.StatusPlanning) {
			o.setStatus(goalID, func(s *Status) {
				s.Phase = "executing"
				s.LastMessage = "running deterministic pending steps"
			})
			runRes, g2, err := o.executor.ExecutePending(ctx, g)
			if err != nil {
				o.setStatus(goalID, func(s *Status) {
					s.LastError = err.Error()
					s.LastMessage = "execute failed"
				})
				// Request reasoning about the failure rather than dying immediately.
				_, _ = o.store.RequestReasoning(goalID, "execution failed: "+err.Error(), g.CurrentProblem)
				if !sleep(ctx, cfg.TickPause) {
					return
				}
				continue
			}
			g = g2
			o.setStatus(goalID, func(s *Status) {
				s.LastMessage = runRes.Summary
				s.GoalStatus = string(g.Status)
				s.CapsuleVersion = g.CapsuleVersion
			})
			if !runRes.OK {
				// Local failure → ask model to replan using stderr-enriched request from ApplyExecution.
				req := g.CurrentRequest
				if strings.TrimSpace(req) == "" {
					req = "local steps failed: " + runRes.Summary
				}
				_, _ = o.store.RequestReasoning(goalID, req, firstNonEmpty(g.CurrentProblem, runRes.Summary))
				if !sleep(ctx, cfg.TickPause) {
					return
				}
				continue
			}
		}

		// 4) Verify / complete when possible.
		o.setStatus(goalID, func(s *Status) { s.Phase = "verifying"; s.LastMessage = "verifying success criteria" })
		g, report, err := o.store.VerifyGoal(goalID)
		if err != nil {
			o.setStatus(goalID, func(s *Status) { s.LastError = err.Error() })
		} else if report.OK {
			// Attempt mark completed if evidence satisfies criteria.
			g2, completeErr := o.store.MarkCompleted(goalID, firstNonEmpty(g.Summary, "orchestrator: all criteria satisfied"), nil)
			if completeErr == nil {
				o.setStatus(goalID, func(s *Status) {
					s.Phase = "completed"
					s.GoalStatus = string(g2.Status)
					s.LastMessage = "goal completed"
				})
				return
			}
			// Verify ok but complete rejected — continue loop (may need explicit evidence ids path).
			o.setStatus(goalID, func(s *Status) {
				s.LastMessage = "criteria satisfied but mark_completed deferred: " + errString(completeErr)
			})
		} else {
			// Not done: advance current_request to next missing part when possible.
			g, _ = o.store.Get(goalID)
			if !hasExecutablePending(g) && g.Status != goal.StatusAwaitingReasoning &&
				g.Status != goal.StatusAwaitingApproval && !isTerminal(g.Status) {
				req, prob, ok := buildNextBatchRequest(g, false)
				if !ok {
					req = firstNonEmpty(g.CurrentRequest, "continue toward unmet success criteria: "+report.Summary)
					prob = g.CurrentProblem
				}
				_, _ = o.store.RequestReasoning(goalID, req, prob)
			}
			o.setStatus(goalID, func(s *Status) {
				s.LastMessage = "criteria unmet: " + report.Summary
			})
		}

		if !sleep(ctx, cfg.TickPause) {
			return
		}
	}

	_, _ = o.store.MarkBlocked(goal.MarkBlockedInput{
		GoalID:   goalID,
		Reason:   "orchestrator: max ticks reached",
		Tried:    fmt.Sprintf("max_ticks=%d", cfg.MaxTicks),
		NeedUser: "inspect goal timeline and raise limits or continue manually",
	})
	o.setStatus(goalID, func(s *Status) {
		s.Phase = "blocked"
		s.LastMessage = "max ticks reached"
	})
}

func (o *Orchestrator) waitCommit(ctx context.Context, goalID string, beforeVer int, beforeStatus goal.Status, wait, poll time.Duration) (bool, goal.Goal, error) {
	committed, g, _, err := o.waitCommitHandsOff(ctx, goalID, beforeVer, beforeStatus, wait, poll, -1, progressBaseline{})
	return committed, g, err
}

type progressBaseline struct {
	events   int
	evidence int
	outBytes int64
	steps    int
	wakeAt   time.Time
}

// waitCommitHandsOff polls the goal store only — never touches the browser/CDP.
// toolWait >= 0 enables the MCP activity watchdog: if the deadline for toolWait
// passes with no model-side MCP/file progress, returns (false, g, false, nil)
// so the caller can MarkBlocked. toolWait < 0 disables the watchdog.
func (o *Orchestrator) waitCommitHandsOff(ctx context.Context, goalID string, beforeVer int, beforeStatus goal.Status, wait, poll, toolWait time.Duration, base progressBaseline) (committed bool, g goal.Goal, activity bool, err error) {
	if poll <= 0 {
		poll = 5 * time.Second
	}
	deadline := time.Now().Add(wait)
	var toolDeadline time.Time
	if toolWait >= 0 {
		toolDeadline = time.Now().Add(toolWait)
	}
	sawActivity := false
	sawProductive := false
	lastProductive := time.Time{}
	lastOutSeen := base.outBytes
	// r9: search_text thrash kept wait_commit alive for full CommitWait with no
	// file_edit/commit. If connectivity was proven but no productive tool for this
	// window, end wait early so orch re-pastes a continue prompt.
	stagnantAfter := o.cfg.StagnantAfter
	if stagnantAfter <= 0 {
		stagnantAfter = 4 * time.Minute
	}
	if toolWait > 0 && toolWait < stagnantAfter {
		stagnantAfter = toolWait + 90*time.Second
	}
	for {
		if ctx.Err() != nil {
			return false, goal.Goal{}, sawActivity, ctx.Err()
		}
		g, err = o.store.Get(goalID)
		if err != nil {
			return false, goal.Goal{}, sawActivity, err
		}
		mcpLive := false
		mcpSummary := ""
		if o.activity != nil && !base.wakeAt.IsZero() {
			mcpLive = o.activity.MCPToolActivitySince(base.wakeAt)
			mcpSummary = o.activity.MCPToolActivitySummary(base.wakeAt)
		}
		storeLive := hasStoreModelActivity(g, beforeVer, base)
		if !sawActivity && (mcpLive || storeLive) {
			sawActivity = true
			detail := "store"
			if mcpLive {
				detail = mcpSummary
				if detail == "" {
					detail = "mcp tools"
				}
			}
			o.setStatus(goalID, func(s *Status) {
				s.LastMessage = "MCP/tool activity detected (" + detail + "); waiting for commit_turn (hands-off)"
			})
		}
		// Productive = writes/commits/evidence/output growth — not search_text spam.
		// Only refresh lastProductive on *new* productive activity after the last mark.
		// Full-since-wake MCP summary forever-refreshed lastProductive after one file_edit
		// and blocked thrash/stagnant re-wake (micro demo 2026-07-22).
		outNow := trackedOutputBytes(g)
		outputGrew := outNow > lastOutSeen
		productiveNow := storeLive || outputGrew
		if !productiveNow && o.activity != nil {
			since := base.wakeAt
			if !lastProductive.IsZero() {
				since = lastProductive
			}
			if !since.IsZero() {
				since = since.Add(-500 * time.Millisecond)
			}
			productiveNow = isProductiveMCPSummary(o.activity.MCPToolActivitySummary(since))
		} else if !productiveNow {
			// no activity source: count full-window productive only once
			productiveNow = isProductiveMCPSummary(mcpSummary) && lastProductive.IsZero()
		}
		if productiveNow {
			if !sawProductive {
				sawProductive = true
				o.setStatus(goalID, func(s *Status) {
					s.LastMessage = "productive progress detected; waiting for commit_turn (hands-off)"
				})
			}
			lastProductive = time.Now()
			if outputGrew {
				lastOutSeen = outNow
			}
		}
		if isTerminal(g.Status) {
			return true, g, true, nil
		}
		// Real commit_turn only: evidence_added/verify also bump capsule and must NOT
		// count as commits (r9 false-commit → re-wake → no-MCP hard-block with parts on disk).
		if hasReasoningCommittedSince(g, base) {
			return true, g, true, nil
		}
		if g.Status != beforeStatus && g.Status != goal.StatusAwaitingReasoning {
			// e.g. moved to executing/blocked via other path
			return true, g, true, nil
		}
		_ = beforeVer
		now := time.Now()
		// Tool-activity watchdog: no MCP/file progress soon after paste.
		if toolWait >= 0 && !sawActivity && !toolDeadline.IsZero() && now.After(toolDeadline) {
			return false, g, false, nil
		}
		// Stagnation: connected but only thrashing non-productive tools.
		if sawActivity && !sawProductive && now.After(base.wakeAt.Add(stagnantAfter)) {
			o.setStatus(goalID, func(s *Status) {
				s.LastMessage = fmt.Sprintf("no productive tools for %s (thrash/search only); re-waking", stagnantAfter)
			})
			return false, g, true, nil
		}
		// Was productive earlier but went quiet — re-wake sooner than full CommitWait.
		if sawProductive && !lastProductive.IsZero() && now.After(lastProductive.Add(stagnantAfter)) {
			o.setStatus(goalID, func(s *Status) {
				s.LastMessage = fmt.Sprintf("productive tools stalled for %s; re-waking for next batch", stagnantAfter)
			})
			return false, g, true, nil
		}
		if now.After(deadline) {
			return false, g, sawActivity, nil
		}
		// Sleep until next poll, but not past tool/commit deadlines.
		next := poll
		if toolWait >= 0 && !sawActivity && !toolDeadline.IsZero() {
			if d := time.Until(toolDeadline); d > 0 && d < next {
				next = d
			}
		}
		if d := time.Until(deadline); d > 0 && d < next {
			next = d
		}
		if next < 50*time.Millisecond {
			next = 50 * time.Millisecond
		}
		if !sleep(ctx, next) {
			return false, g, sawActivity, ctx.Err()
		}
	}
}

// hasStoreModelActivity reports goal-store signals that imply the *model* advanced
// the goal (commit_turn, new steps, evidence, output files). Local orch verify /
// empty execution_applied must NOT count — r7 false-negative root.
func hasStoreModelActivity(g goal.Goal, beforeVer int, base progressBaseline) bool {
	if len(g.Steps) > base.steps {
		return true
	}
	if len(g.Evidence) > base.evidence {
		return true
	}
	if trackedOutputBytes(g) > base.outBytes {
		return true
	}
	for i := base.events; i < len(g.Events); i++ {
		t := strings.ToLower(strings.TrimSpace(g.Events[i].Type))
		switch t {
		case "reasoning_committed", "evidence_added", "resumed", "completed":
			return true
		case "lease_acquired", "worker_conversation_bound", "awaiting_reasoning", "created",
			"verified", "execution_applied", "blocked", "constraints_updated", "":
			// local orch / plumbing — not model MCP progress
			continue
		default:
			// unknown event types: be conservative and count
			return true
		}
	}
	// Capsule bump alone is insufficient (local verify can bump without model).
	_ = beforeVer
	return false
}

// hasMCPActivity is the combined signal: real MCP tool calls OR store model activity.
func hasMCPActivity(g goal.Goal, beforeVer int, base progressBaseline, mcpLive bool) bool {
	if mcpLive {
		return true
	}
	return hasStoreModelActivity(g, beforeVer, base)
}

// hasReasoningCommittedSince is true only for a real goal_manage commit_turn
// after the wake baseline (not evidence_added / verified capsule bumps).
func hasReasoningCommittedSince(g goal.Goal, base progressBaseline) bool {
	for i := base.events; i < len(g.Events); i++ {
		if strings.EqualFold(strings.TrimSpace(g.Events[i].Type), "reasoning_committed") {
			return true
		}
	}
	return false
}

// isProductiveMCPSummary reports tools that advance the goal (writes/commits),
// not pure search/read thrash that would otherwise keep wait_commit alive (r9).
func isLiveMCPBusySummary(summary string) bool {
	s := strings.ToLower(strings.TrimSpace(summary))
	if s == "" || s == "none" {
		return false
	}
	// Still writing / searching / executing — do not CDP-wake.
	live := []string{"file_edit", "atomic_write", "search_text", "read_file", "run_command", "exec_command", "browser_"}
	for _, p := range live {
		if strings.Contains(s, p) {
			return true
		}
	}
	// goal_manage get/commit/lease alone is not "page busy"; allow re-wake.
	return false
}

func isProductiveMCPSummary(summary string) bool {
	s := strings.ToLower(summary)
	if s == "" || s == "none" {
		return false
	}
	for _, p := range []string{
		"file_edit", "atomic_write", "commit_turn", "add_evidence",
		"run_command", "exec_command", "goal_manage:commit",
		"goal_manage:add_evidence", "goal_manage:acquire_lease",
	} {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func needsReasoning(g goal.Goal) bool {
	switch g.Status {
	case goal.StatusAwaitingReasoning, goal.StatusPlanning, goal.StatusReplanning, goal.StatusRegressed:
		return true
	case goal.StatusExecuting, goal.StatusVerifying:
		// Need model if nothing left to execute deterministically.
		return !hasExecutablePending(g)
	default:
		return false
	}
}

func hasExecutablePending(g goal.Goal) bool {
	for _, s := range g.Steps {
		if s.Status != goal.StepPending && s.Status != goal.StepFailed {
			continue
		}
		switch s.Action {
		case goal.ActionRunTests, goal.ActionRunCommand, goal.ActionCollectLogs,
			goal.ActionCreateCheckpoint, goal.ActionInspectFiles, goal.ActionEnterVerify:
			return true
		}
	}
	return false
}

func isTerminal(s goal.Status) bool {
	return s == goal.StatusCompleted || s == goal.StatusCancelled || s == goal.StatusFailed
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}



func needsHardRotate(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// Hard errors where the current tab/session is unusable.
	hard := []string{
		"login_required", "sign in", "log in",
		"usage limit", "rate limit", "quota",
		"cloudflare challenge", "captcha",
		"browser tools disabled",
	}
	for _, h := range hard {
		if strings.Contains(msg, h) {
			return true
		}
	}
	// Transient CDP/page glitches: soft rebind only.
	return false
}

func wakeResultError(res map[string]any) error {
	if res == nil {
		return nil
	}
	// Cooldown / already-delivered is success for the orchestrator loop.
	if wakeSkipped(res) {
		return nil
	}
	if busy, _ := res["busy"].(bool); busy {
		// Concurrent wake in progress — treat as page-busy, not a failed paste.
		return fmt.Errorf("page_busy: worker is already waking a goal")
	}
	if ok, exists := res["ok"].(bool); exists && !ok {
		if msg, _ := res["error"].(string); strings.TrimSpace(msg) != "" {
			return fmt.Errorf("%s", msg)
		}
		return fmt.Errorf("wake returned ok=false")
	}
	return nil
}

// isPermissionHardFail is true when the tool/connector permission dialog could
// not be auto-approved. Orchestrator must MarkBlocked immediately (operator rule).
func isPermissionHardFail(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "tool_permission_unresolved") ||
		strings.Contains(msg, "permission dialog still present") ||
		(strings.Contains(msg, "tool_permission") && strings.Contains(msg, "auto-approve failed"))
}

// isPageBusyWakeErr is true when re-pasting would interrupt live ChatGPT work or
// hammer an unresponsive renderer. Orchestrator must wait, not SoftRebind/ForceRotate.
// Permission hard-fails are handled separately by isPermissionHardFail.
func isPageBusyWakeErr(err error) bool {
	if err == nil || isPermissionHardFail(err) {
		return false
	}
	msg := strings.ToLower(err.Error())
	needles := []string{
		"page not idle",
		"page_busy",
		"page_stuck",
		"page blocked",
		"tool_permission",
		"already waking",
		"cdp method timed out",
		"runtime.evaluate",
		"browser runner returned invalid json",
	}
	for _, n := range needles {
		if strings.Contains(msg, n) {
			return true
		}
	}
	return false
}

func busyWakeBackoff(base time.Duration) time.Duration {
	if base < 15*time.Second {
		return 30 * time.Second
	}
	if base > 2*time.Minute {
		return 2 * time.Minute
	}
	return base * 2
}

func wakeSkipped(res map[string]any) bool {
	if res == nil {
		return false
	}
	if skipped, _ := res["skipped"].(bool); skipped {
		return true
	}
	if cooldown, _ := res["cooldown"].(bool); cooldown {
		return true
	}
	return false
}

func wakeFailureBackoff(streak int, base time.Duration) time.Duration {
	if base <= 0 {
		base = time.Second
	}
	// 2s, 4s, 8s, 16s... capped at 30s
	mult := 1 << streak
	if mult < 2 {
		mult = 2
	}
	d := base * time.Duration(mult)
	if d > 30*time.Second {
		return 30 * time.Second
	}
	if d < 2*time.Second {
		return 2 * time.Second
	}
	return d
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}


// trackedOutputBytes sums sizes of files referenced by success criteria path gates.
// Used as a coarse progress signal while waiting for commit_turn.
func trackedOutputBytes(g goal.Goal) int64 {
	paths := map[string]struct{}{}
	// Evidence URIs (file://...) often point at parts written this run.
	for _, ev := range g.Evidence {
		u := strings.TrimSpace(ev.URI)
		if strings.HasPrefix(u, "file://") {
			paths[strings.TrimPrefix(u, "file://")] = struct{}{}
		}
		if d, ok := ev.Data["path"].(string); ok {
			if strings.TrimSpace(d) != "" {
				paths[strings.TrimSpace(d)] = struct{}{}
			}
		}
	}
	for _, c := range g.SuccessCriteria {
		expr := strings.TrimSpace(c.Expression)
		switch {
		case strings.HasPrefix(expr, "file_min_bytes:"):
			// file_min_bytes:/path:123
			rest := strings.TrimPrefix(expr, "file_min_bytes:")
			if i := strings.LastIndex(rest, ":"); i > 0 {
				paths[rest[:i]] = struct{}{}
			}
		case strings.HasPrefix(expr, "file_min_lines:"):
			rest := strings.TrimPrefix(expr, "file_min_lines:")
			if i := strings.LastIndex(rest, ":"); i > 0 {
				paths[rest[:i]] = struct{}{}
			}
		case strings.HasPrefix(expr, "file_not_contains:"):
			rest := strings.TrimPrefix(expr, "file_not_contains:")
			if i := strings.LastIndex(rest, ":"); i > 0 {
				paths[rest[:i]] = struct{}{}
			}
		case strings.HasPrefix(expr, "test -s "):
			p := strings.TrimSpace(strings.TrimPrefix(expr, "test -s "))
			p = strings.Trim(p, "'\"")
			if p != "" {
				paths[p] = struct{}{}
			}
		case strings.Contains(expr, "grep -q") && strings.Contains(expr, ".md"):
			// grep -q '^#' '/path/file.md'
			if i := strings.Index(expr, "'"); i >= 0 {
				rest := expr[i+1:]
				if j := strings.Index(rest, "'"); j > 0 {
					paths[rest[:j]] = struct{}{}
				}
			}
		}
	}
	// Expand parts directories mentioned in objective/request (book jobs).
	blob := g.Objective + "\n" + g.CurrentRequest
	for _, token := range strings.FieldsFunc(blob, func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\t' || r == '"' || r == '\'' || r == '，' || r == '。'
	}) {
		if strings.Contains(token, "parts_r") || strings.HasSuffix(token, "/parts") {
			token = strings.Trim(token, "()[]{},")
			if st, err := os.Stat(token); err == nil && st.IsDir() {
				entries, _ := os.ReadDir(token)
				for _, e := range entries {
					if e.IsDir() {
						continue
					}
					name := e.Name()
					if strings.HasPrefix(name, "letters_") || strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".md.tmp") {
						paths[token+"/"+name] = struct{}{}
					}
				}
			}
		}
	}
	var sum int64
	for p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// ignore relative bare names like letters_01.md without directory; still try
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			sum += st.Size()
		}
	}
	return sum
}

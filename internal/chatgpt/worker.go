package chatgpt

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/uvwt/agentdock/internal/goal"
)

// Worker is the product-level ChatGPT Browser Worker.
// Fixed chatgpt profile, headful by default, conversation rotation via Loop.
type Worker struct {
	mu sync.Mutex

	loop    *Loop
	browser *RuntimeBrowser

	browserEnabled    bool
	autoWake          bool
	autoApproveTools  bool
	last           ResumeResult
	lastErr        string
	waking         bool
	enabled        bool
	wakeCooldown   time.Duration

	// suppressAutoWake[goalID] is set while an L3 orchestrator owns that goal.
	// Prevents request_reasoning → MaybeAutoWake from double-firing with orch.Wake.
	suppressAutoWake map[string]bool
	// lastSuccessfulWake tracks successful paste times so concurrent orch+manual
	// wake paths cannot spam ChatGPT with the same resume prompt.
	lastSuccessfulWake map[string]time.Time
	lastWakeCapsule    map[string]int
}

// WorkerOptions configures the product worker.
type WorkerOptions struct {
	ProfileID               string
	Headless                bool
	AutoWake                bool
	// AutoApproveTools clicks ChatGPT permission modals for tool/connector use.
	AutoApproveTools        bool
	BrowserEnabled          bool
	MaxTurnsPerConversation int
	WaitIdleTimeout         time.Duration
	LeaseTTL                time.Duration
	// WakeCooldown skips a successful re-wake of the same capsule within this window.
	// Zero uses DefaultWakeCooldown. Negative disables cooldown (tests only).
	WakeCooldown time.Duration
}

// DefaultWakeCooldown is the minimum gap between successful resume pastes for one goal.
// Must be long enough that book-scale MCP turns are not re-prompted mid-tool-use.
const DefaultWakeCooldown = 8 * time.Minute

// DefaultWorkerOptions returns product defaults: headful chatgpt profile.
// Auto-wake is off by default: L3 orchestrator owns waking. Enable auto-wake only
// when running without orchestrator (single-shot resume helper).
func DefaultWorkerOptions() WorkerOptions {
	return WorkerOptions{
		ProfileID:               "chatgpt",
		Headless:                false,
		AutoWake:                false,
		AutoApproveTools:        false,
		MaxTurnsPerConversation: 50,
		WaitIdleTimeout:         5 * time.Minute,
		LeaseTTL:                30 * time.Minute,
		WakeCooldown:             DefaultWakeCooldown,
	}
}

// NewWorker wires Loop + RuntimeBrowser.
// goals is the Goal store; caller is used for browser_* tools.
func NewWorker(goals *goal.Store, caller ToolCaller, opts WorkerOptions) *Worker {
	if opts.ProfileID == "" {
		opts.ProfileID = "chatgpt"
	}
	cfg := DefaultConfig()
	cfg.ProfileID = opts.ProfileID
	if opts.MaxTurnsPerConversation > 0 {
		cfg.MaxTurnsPerConversation = opts.MaxTurnsPerConversation
	}
	if opts.WaitIdleTimeout > 0 {
		cfg.WaitIdleTimeout = opts.WaitIdleTimeout
		// Unit tests pass sub-second WaitIdleTimeout; do not burn 90s on post-paste
		// Svananda permission polling in those paths.
		if opts.WaitIdleTimeout < 5*time.Second {
			cfg.PostPastePermissionWait = -1
		}
	}
	if opts.LeaseTTL > 0 {
		cfg.LeaseTTL = opts.LeaseTTL
	}
	cooldown := opts.WakeCooldown
	if cooldown == 0 {
		cooldown = DefaultWakeCooldown
	}
	browser := &RuntimeBrowser{
		Caller:           caller,
		ProfileID:        opts.ProfileID,
		Headless:         opts.Headless,
		AutoApproveTools: opts.AutoApproveTools,
	}
	return &Worker{
		browser:            browser,
		autoWake:           opts.AutoWake,
		autoApproveTools:   opts.AutoApproveTools,
		browserEnabled:     opts.BrowserEnabled,
		enabled:            true,
		wakeCooldown:       cooldown,
		suppressAutoWake:   map[string]bool{},
		lastSuccessfulWake: map[string]time.Time{},
		lastWakeCapsule:    map[string]int{},
		loop: &Loop{
			Browser: browser,
			Goals:   goals,
			Config:  cfg,
		},
	}
}

// Wake starts/continues the ChatGPT web worker for a goal.
func (w *Worker) Wake(ctx context.Context, goalID string) (map[string]any, error) {
	if w == nil || !w.enabled {
		return nil, fmt.Errorf("ChatGPT browser worker is not enabled")
	}
	if !w.browserEnabled {
		return map[string]any{
			"ok": false, "error": "browser tools disabled; start with browser enabled (agentdock-desktop or AGENTDOCK_BROWSER_ENABLED=true)",
		}, fmt.Errorf("browser tools disabled")
	}
	goalID = trimGoalID(goalID)
	if goalID == "" {
		return nil, fmt.Errorf("goal_id is required")
	}

	w.mu.Lock()
	if w.waking {
		last := w.lastSnapshot()
		w.mu.Unlock()
		// Return a real error so orchestrator does not treat busy as a successful wake
		// and wait CommitWait for a commit that was never prompted.
		return map[string]any{"ok": false, "busy": true, "message": "worker is already waking a goal", "last": last},
			fmt.Errorf("worker is already waking a goal")
	}
	// Cooldown: if we already pasted a resume for this capsule recently, do not paste again.
	// Returning ok/skipped lets orchestrator keep waiting for commit_turn instead of re-spamming.
	if w.wakeCooldown > 0 {
		if at, ok := w.lastSuccessfulWake[goalID]; ok {
			if time.Since(at) < w.wakeCooldown {
				last := w.lastSnapshot()
				capVer := w.lastWakeCapsule[goalID]
				status := w.statusLocked()
				w.mu.Unlock()
				return map[string]any{
					"ok": true, "skipped": true, "cooldown": true,
					// r6: cooldown means paste already happened — orch must still arm MCP watchdog.
					"already_delivered": true,
					"delivered":         true,
					"goal_id":           goalID, "capsule_version": capVer,
					"message":           "recent resume prompt already delivered; waiting for commit_turn instead of re-pasting",
					"retry_after_sec":   int((w.wakeCooldown - time.Since(at)).Seconds()) + 1,
					"last":              last,
					"worker":            status,
				}, nil
			}
		}
	}
	w.waking = true
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		w.waking = false
		w.mu.Unlock()
	}()

	res, err := w.loop.Wake(ctx, goalID)
	w.mu.Lock()
	w.last = res
	if err != nil {
		w.lastErr = err.Error()
	} else {
		w.lastErr = ""
		w.lastSuccessfulWake[goalID] = time.Now()
		w.lastWakeCapsule[goalID] = res.CapsuleVersion
	}
	status := w.statusLocked()
	w.mu.Unlock()

	if err != nil {
		return map[string]any{"ok": false, "error": err.Error(), "goal_id": goalID, "worker": status}, err
	}
	return map[string]any{
		"ok":                true,
		"delivered":         true,
		"already_delivered": false,
		"goal_id":           res.GoalID,
		"worker_id":         res.WorkerID,
		"lease_id":          res.LeaseID,
		"capsule_version":   res.CapsuleVersion,
		"conversation_id":   res.ConversationID,
		"rotated":           res.Rotated,
		"rotation_reason":   res.RotationReason,
		"resume_prompt":     res.ResumePrompt,
		"blockers":          res.Blockers,
		"profile_id":        w.loop.Config.ProfileID,
		"headless":          w.browser.Headless,
		"worker":            status,
		"message":           "resume prompt delivered to ChatGPT; model must call goal_manage commit_turn via MCP",
	}, nil
}

// WakeAsync fires Wake in background (auto-wake).
func (w *Worker) WakeAsync(goalID string) {
	if w == nil || !w.AutoWakeEnabled() {
		return
	}
	goalID = trimGoalID(goalID)
	if goalID == "" || w.AutoWakeSuppressed(goalID) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		_, _ = w.Wake(ctx, goalID)
	}()
}

// OpenSession ensures the dedicated chatgpt profile browser is open on chatgpt.com.
// Used at desktop startup so the only user-facing surface is ChatGPT web.
func (w *Worker) OpenSession(ctx context.Context) (map[string]any, error) {
	if w == nil || !w.enabled {
		return nil, fmt.Errorf("ChatGPT browser worker is not enabled")
	}
	if !w.browserEnabled {
		return map[string]any{
			"ok": false, "error": "browser tools disabled; start with browser enabled (agentdock --browser-enabled) and install browser-runner",
		}, fmt.Errorf("browser tools disabled")
	}
	if w.browser == nil {
		return nil, fmt.Errorf("browser adapter is nil")
	}
	// Product open always wants a visible window so the user can login.
	w.browser.Headless = false
	profile := w.loop.Config.ProfileID
	if profile == "" {
		profile = "chatgpt"
	}
	w.browser.ProfileID = profile
	if err := w.browser.EnsureSession(ctx, profile); err != nil {
		w.mu.Lock()
		w.lastErr = err.Error()
		w.mu.Unlock()
		return map[string]any{"ok": false, "error": err.Error(), "profile_id": profile, "worker": w.Status()}, err
	}
	if err := w.browser.OpenChatGPT(ctx); err != nil {
		w.mu.Lock()
		w.lastErr = err.Error()
		w.mu.Unlock()
		return map[string]any{"ok": false, "error": err.Error(), "profile_id": profile, "session_id": w.browser.sessionID, "worker": w.Status()}, err
	}
	blockers, _ := w.browser.DetectBlockers(ctx)
	w.mu.Lock()
	w.lastErr = ""
	w.mu.Unlock()
	return map[string]any{
		"ok":         true,
		"profile_id": profile,
		"session_id": w.browser.sessionID,
		"page_id":    w.browser.pageID,
		"headless":   false,
		"url":        "https://chatgpt.com/",
		"blockers":   blockers,
		"message":    "ChatGPT opened in dedicated Chrome/Chromium profile (keep_open visible). First time: complete login/CAPTCHA in that window.",
		"worker":     w.Status(),
	}, nil
}

// Status returns operator-facing worker state.
func (w *Worker) Status() map[string]any {
	if w == nil {
		return map[string]any{"enabled": false}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.statusLocked()
}

func (w *Worker) statusLocked() map[string]any {
	suppressed := make([]string, 0, len(w.suppressAutoWake))
	for id, on := range w.suppressAutoWake {
		if on {
			suppressed = append(suppressed, id)
		}
	}
	return map[string]any{
		"enabled":                    w.enabled,
		"auto_wake":                  w.autoWake,
		"auto_approve_tools":         w.autoApproveTools,
		"waking":                     w.waking,
		"profile_id":                 w.loop.Config.ProfileID,
		"headless":                   w.browser != nil && w.browser.Headless,
		"browser_enabled":            w.browserEnabled,
		"max_turns_per_conversation": w.loop.Config.MaxTurnsPerConversation,
		"wake_cooldown_sec":          int(w.wakeCooldown.Seconds()),
		"auto_wake_suppressed_goals": suppressed,
		"last_error":                 w.lastErr,
		"last":                       w.lastSnapshot(),
	}
}

func (w *Worker) lastSnapshot() map[string]any {
	if w.last.GoalID == "" {
		return nil
	}
	return map[string]any{
		"goal_id":         w.last.GoalID,
		"worker_id":       w.last.WorkerID,
		"lease_id":        w.last.LeaseID,
		"capsule_version": w.last.CapsuleVersion,
		"conversation_id": w.last.ConversationID,
		"rotated":         w.last.Rotated,
		"rotation_reason": w.last.RotationReason,
		"blockers":        w.last.Blockers,
	}
}

func (w *Worker) SetAutoWake(v bool) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.autoWake = v
	w.mu.Unlock()
}

func (w *Worker) SetAutoApproveTools(v bool) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.autoApproveTools = v
	if w.browser != nil {
		w.browser.AutoApproveTools = v
	}
	w.mu.Unlock()
}

func (w *Worker) AutoApproveToolsEnabled() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.autoApproveTools
}

func (w *Worker) AutoWakeEnabled() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.autoWake
}

// MaybeAutoWake triggers async wake when a goal is awaiting reasoning.
// No-op while an orchestrator owns the goal (orchestrator performs Wake itself).
func (w *Worker) MaybeAutoWake(g goal.Goal) {
	if w == nil || !w.AutoWakeEnabled() {
		return
	}
	if g.Status != goal.StatusAwaitingReasoning {
		return
	}
	if w.AutoWakeSuppressed(g.ID) {
		return
	}
	w.WakeAsync(g.ID)
}

// SetAutoWakeSuppressed marks whether L3 orchestrator owns waking for goalID.
// While suppressed, MaybeAutoWake/WakeAsync will not paste resume prompts.
func (w *Worker) SetAutoWakeSuppressed(goalID string, suppressed bool) {
	if w == nil {
		return
	}
	goalID = trimGoalID(goalID)
	if goalID == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.suppressAutoWake == nil {
		w.suppressAutoWake = map[string]bool{}
	}
	if suppressed {
		w.suppressAutoWake[goalID] = true
	} else {
		delete(w.suppressAutoWake, goalID)
	}
}

// AutoWakeSuppressed reports whether auto-wake is blocked for goalID.
func (w *Worker) AutoWakeSuppressed(goalID string) bool {
	if w == nil {
		return false
	}
	goalID = trimGoalID(goalID)
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.suppressAutoWake[goalID]
}

// ClearWakeCooldown allows the next Wake to paste even if one just succeeded.
// Used after a verified commit_turn so the next reasoning cycle can resume.
func (w *Worker) ClearWakeCooldown(goalID string) {
	if w == nil {
		return
	}
	goalID = trimGoalID(goalID)
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.lastSuccessfulWake, goalID)
	delete(w.lastWakeCapsule, goalID)
}

// SoftRebind clears stale page bindings without opening a new ChatGPT conversation.
// Prefer this after transient CDP errors so we stay on the same thread.
func (w *Worker) SoftRebind() {
	if w == nil || w.browser == nil {
		return
	}
	w.browser.SoftRebind()
}

// ForceRotate forces the next Wake to open a new ChatGPT conversation
// and drops any cached browser page_id so OpenChatGPT cannot hit a dead tab.
// It also clears wake cooldown so a recovery wake can actually re-paste.
// Use sparingly: this is what creates the "new chat every time" user experience.
func (w *Worker) ForceRotate() {
	if w == nil {
		return
	}
	if w.loop != nil {
		w.loop.ForceRotate()
	}
	if w.browser != nil {
		// Keep profile_id; only drop process-local session/page bindings.
		w.browser.Reset()
	}
	w.mu.Lock()
	// Drop all cooldowns on rotate — recovery after failure must be allowed to paste.
	w.lastSuccessfulWake = map[string]time.Time{}
	w.lastWakeCapsule = map[string]int{}
	w.mu.Unlock()
}

func trimGoalID(id string) string {
	return strings.TrimSpace(id)
}

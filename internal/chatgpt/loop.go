package chatgpt

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/goal"
)

// Browser is the minimal automation surface the web loop needs.
// Production adapters can wrap AgentDock browser_session/browser_act tools.
type Browser interface {
	EnsureSession(ctx context.Context, profileID string) error
	OpenChatGPT(ctx context.Context) error
	// OpenConversation navigates to a bound ChatGPT thread URL when possible.
	// Implementations may fall back to OpenChatGPT if url is empty/unsupported.
	OpenConversation(ctx context.Context, conversationURL string) error
	NewConversation(ctx context.Context) (conversationID string, err error)
	PasteAndSend(ctx context.Context, text string) error
	WaitIdle(ctx context.Context, timeout time.Duration) error
	DetectBlockers(ctx context.Context) ([]string, error)
	// CurrentURL returns the active page URL (used to persist thread binding).
	CurrentURL(ctx context.Context) (string, error)
}

// GoalService is the subset of Goal store operations the loop needs.
type GoalService interface {
	Get(id string) (goal.Goal, error)
	AcquireLease(goalID, workerID string, ttl time.Duration) (goal.Goal, goal.Lease, error)
	ReleaseLease(goalID, leaseID string) (goal.Goal, error)
	// BindWorkerConversation persists the ChatGPT thread for later wakes.
	// Optional: implementations may no-op; Loop still works without durability.
	BindWorkerConversation(goalID, conversationURL, conversationID string) (goal.Goal, error)
}

// Config tunes rotation and budgets for the ChatGPT web worker loop.
type Config struct {
	ProfileID               string
	WorkerIDPrefix          string
	MaxTurnsPerConversation int
	LeaseTTL                time.Duration
	WaitIdleTimeout         time.Duration
	// PermissionWait is how long Wake retries tool-permission auto-approve
	// before failing hard (no paste into a stuck permission modal).
	PermissionWait time.Duration
	// PostPastePermissionWait watches for Svananda/tool allow dialogs that appear
	// only after the model starts a tool call. Zero uses default; negative disables.
	PostPastePermissionWait time.Duration
	// HandsOffAfterPaste skips continuous post-paste CDP probes after the short
	// bind + permission window. Orchestrator then waits on store/MCP only.
	HandsOffAfterPaste bool
}

// DefaultConfig returns conservative loop settings.
func DefaultConfig() Config {
	return Config{
		ProfileID:               "chatgpt",
		WorkerIDPrefix:          "chatgpt-web",
		// High on purpose: forced new chats interrupt MCP tool use mid-turn.
		// Rotate only on quota/page errors or explicit ForceRotate.
		MaxTurnsPerConversation: 50,
		LeaseTTL:                30 * time.Minute,
		WaitIdleTimeout:         8 * time.Minute,
		PermissionWait:          90 * time.Second,
		// Short window only: click Allow once then hands-off. Long post-paste CDP
		// polling after a successful allow helped pin the renderer (r8).
		PostPastePermissionWait: 35 * time.Second,
		HandsOffAfterPaste:      true,
	}
}

// RotationReason explains why a new conversation should be opened.
type RotationReason string

const (
	RotateNone      RotationReason = ""
	RotateTurnLimit RotationReason = "turn_limit"
	RotateQuota     RotationReason = "quota_or_error"
	RotateTimeout   RotationReason = "timeout"
	RotateNoCommit  RotationReason = "commit_missing"
	RotatePageError RotationReason = "page_error"
	RotateManual    RotationReason = "manual"
)

// Loop is a replaceable ChatGPT Web Adapter. It never parses chat text as Goal state.
// The model must call goal_manage commit_turn via MCP; this loop only delivers Resume Prompts.
type Loop struct {
	Browser Browser
	Goals   GoalService
	Config  Config

	conversationID string
	turnsInConvo   int
	session        int
	lastGoalID     string
}

// ResumeResult is the outcome of one worker wake-up attempt.
type ResumeResult struct {
	GoalID         string         `json:"goal_id"`
	WorkerID       string         `json:"worker_id"`
	LeaseID        string         `json:"lease_id,omitempty"`
	CapsuleVersion int            `json:"capsule_version"`
	ConversationID string         `json:"conversation_id,omitempty"`
	Rotated        bool           `json:"rotated"`
	RotationReason RotationReason `json:"rotation_reason,omitempty"`
	ResumePrompt   string         `json:"resume_prompt"`
	Blockers       []string       `json:"blockers,omitempty"`
}

// ShouldRotate decides whether to open a fresh ChatGPT conversation.
func (l *Loop) ShouldRotate(blockers []string) (bool, RotationReason) {
	cfg := l.Config
	if cfg.MaxTurnsPerConversation <= 0 {
		cfg = DefaultConfig()
	}
	for _, b := range blockers {
		lb := strings.ToLower(b)
		switch {
		case strings.Contains(lb, "quota"), strings.Contains(lb, "rate limit"), strings.Contains(lb, "usage limit"):
			return true, RotateQuota
		case strings.Contains(lb, "something went wrong"), strings.Contains(lb, "network error"):
			return true, RotatePageError
		case strings.Contains(lb, "can't continue"), strings.Contains(lb, "cannot continue"):
			return true, RotateQuota
		}
	}
	if l.turnsInConvo >= cfg.MaxTurnsPerConversation {
		return true, RotateTurnLimit
	}
	return false, RotateNone
}

// Wake delivers a resume prompt for the goal into ChatGPT.
// It acquires a reasoning lease, builds the capsule resume prompt, and sends it.
// It does NOT wait for or parse a natural-language answer as completion.
//
// Conversation reuse rules:
//   - Prefer the existing ChatGPT tab/conversation.
//   - Do NOT navigate to https://chatgpt.com/ on every wake (that abandons the thread).
//   - Wait for the current response to finish before pasting a new resume prompt.
//   - Open a new conversation only on quota/page errors, turn limit, or ForceRotate.
func (l *Loop) Wake(ctx context.Context, goalID string) (ResumeResult, error) {
	if l.Goals == nil {
		return ResumeResult{}, errors.New("goal service is required")
	}
	if l.Browser == nil {
		return ResumeResult{}, errors.New("browser adapter is required")
	}
	cfg := l.Config
	if cfg.WorkerIDPrefix == "" {
		cfg = DefaultConfig()
	}

	g, err := l.Goals.Get(goalID)
	if err != nil {
		return ResumeResult{}, err
	}
	if g.Status == goal.StatusCompleted || g.Status == goal.StatusCancelled || g.Status == goal.StatusFailed {
		return ResumeResult{}, fmt.Errorf("goal %s is terminal: %s", goalID, g.Status)
	}

	if err := l.Browser.EnsureSession(ctx, first(cfg.ProfileID, "chatgpt")); err != nil {
		return ResumeResult{}, fmt.Errorf("ensure browser session: %w", err)
	}

	// Isolate goals: never inherit another goal's in-memory conversation.
	if l.lastGoalID != "" && l.lastGoalID != goalID {
		l.conversationID = ""
		l.turnsInConvo = 0
	}
	l.lastGoalID = goalID

	// Seed in-memory conversation ONLY from this goal's durable binding.
	// If this goal has no binding, clear any leftover id so we open a new chat.
	if strings.TrimSpace(g.WorkerConversationURL) == "" && strings.TrimSpace(g.WorkerConversationID) == "" {
		l.conversationID = ""
	} else if l.conversationID == "" {
		if u := strings.TrimSpace(g.WorkerConversationURL); u != "" {
			l.conversationID = first(g.WorkerConversationID, conversationIDFromURL(u), u)
		} else if id := strings.TrimSpace(g.WorkerConversationID); id != "" {
			l.conversationID = id
		}
	}

	wait := cfg.WaitIdleTimeout
	if wait <= 0 {
		wait = 8 * time.Minute
	}
	// Never paste into a streaming/tool-using/stuck ChatGPT tab. Cap pre-wait so a
	// single Wake does not hold the worker for the full book-job duration; orch
	// should re-check later without pasting if still busy.
	preWait := wait
	if preWait > 2*time.Minute {
		preWait = 2 * time.Minute
	}
	if err := l.Browser.WaitIdle(ctx, preWait); err != nil {
		return ResumeResult{}, fmt.Errorf("page not idle before paste: %w", err)
	}

	blockers, _ := l.Browser.DetectBlockers(ctx)
	if hard := hardBlockers(blockers); len(hard) > 0 {
		return ResumeResult{GoalID: goalID, Blockers: unique(blockers)},
			fmt.Errorf("page blocked before paste: %s", strings.Join(hard, ","))
	}
	rotate, reason := l.ShouldRotate(blockers)

	switch {
	case rotate:
		id, err := l.Browser.NewConversation(ctx)
		if err != nil {
			return ResumeResult{}, fmt.Errorf("new conversation: %w", err)
		}
		l.conversationID = id
		l.turnsInConvo = 0
		l.session++
	case strings.TrimSpace(g.WorkerConversationURL) != "":
		// Default: return to the bound ChatGPT thread.
		if err := l.Browser.OpenConversation(ctx, g.WorkerConversationURL); err != nil {
			return ResumeResult{}, fmt.Errorf("open bound conversation: %w", err)
		}
		if l.session == 0 {
			l.session = 1
		}
		if l.conversationID == "" {
			l.conversationID = first(g.WorkerConversationID, conversationIDFromURL(g.WorkerConversationURL), g.WorkerConversationURL)
		}
		reason = RotateNone
		rotate = false
	default:
		// No durable binding for this goal: always open a *new* ChatGPT conversation.
		// Reusing the active tab caused new goals to inherit the previous goal's thread.
		id, err := l.Browser.NewConversation(ctx)
		if err != nil {
			// Fallback: at least land on ChatGPT home if new-chat UI click fails.
			if err2 := l.Browser.OpenChatGPT(ctx); err2 != nil {
				return ResumeResult{}, fmt.Errorf("new conversation: %w", err)
			}
			l.session++
			id = fmt.Sprintf("chatgpt-new-%02d", l.session)
		} else {
			l.session++
		}
		l.conversationID = id
		l.turnsInConvo = 0
		reason = RotateManual
		rotate = true
	}

	// Re-check idle after navigation: opening a bound thread may land on a still-busy tab.
	postOpenWait := 30 * time.Second
	if wait > 0 && wait < postOpenWait {
		postOpenWait = wait
	}
	if err := l.Browser.WaitIdle(ctx, postOpenWait); err != nil {
		return ResumeResult{}, fmt.Errorf("page not idle after open: %w", err)
	}
	if more, _ := l.Browser.DetectBlockers(ctx); len(more) > 0 {
		blockers = append(blockers, more...)
		if hard := hardBlockers(more); len(hard) > 0 {
			return ResumeResult{GoalID: goalID, Blockers: unique(blockers)},
				fmt.Errorf("page blocked after open: %s", strings.Join(hard, ","))
		}
	}

	// Resolve tool-permission modals BEFORE paste. If auto-approve cannot clear
	// them within PermissionWait, fail hard so orchestrator blocks the goal.
	permWait := cfg.PermissionWait
	if permWait <= 0 {
		permWait = 90 * time.Second
	}
	if err := l.resolveToolPermission(ctx, permWait, &blockers); err != nil {
		return ResumeResult{GoalID: goalID, Blockers: unique(blockers)}, err
	}

	workerID := fmt.Sprintf("%s-%02d", cfg.WorkerIDPrefix, maxInt(l.session, 1))
	ttl := cfg.LeaseTTL
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	g, lease, err := l.Goals.AcquireLease(goalID, workerID, ttl)
	if err != nil {
		return ResumeResult{}, err
	}

	cap := goal.BuildCapsule(g)
	prompt := cap.ResumePrompt
	if err := l.Browser.PasteAndSend(ctx, prompt); err != nil {
		_, _ = l.Goals.ReleaseLease(goalID, lease.LeaseID)
		return ResumeResult{}, fmt.Errorf("paste resume prompt: %w", err)
	}
	l.turnsInConvo++

	// One-shot durable bind: SPA often assigns /c/WEB:… only after send.
	// Do this before the short permission window, then hands-off.
	if err := l.bindLiveConversation(ctx, goalID); err == nil {
		// ok
	} else if strings.TrimSpace(g.WorkerConversationURL) != "" {
		if bound := normalizeChatGPTThreadURL(g.WorkerConversationURL); bound != "" {
			id := conversationIDFromURL(bound)
			if id != "" {
				l.conversationID = id
			}
			_, _ = l.Goals.BindWorkerConversation(goalID, bound, id)
		}
	}

	// Release browser-worker lease after paste so ChatGPT can acquire_lease +
	// commit_turn. Holding chatgpt-web-01 for LeaseTTL made commit_turn fail (r9).
	if _, err := l.Goals.ReleaseLease(goalID, lease.LeaseID); err == nil {
		// ok — model owns subsequent commits
	}

	// Short post-paste permission window: Svananda "要允許 ChatGPT 使用 Svananda 嗎？"
	// usually appears only after the model starts a tool call, not before paste.
	postPerm := cfg.PostPastePermissionWait
	if postPerm == 0 {
		postPerm = 90 * time.Second
	}
	if postPerm > 0 {
		if err := l.waitAndResolveToolPermission(ctx, postPerm, &blockers); err != nil {
			return ResumeResult{
				GoalID:         goalID,
				WorkerID:       workerID,
				LeaseID:        lease.LeaseID,
				CapsuleVersion: g.CapsuleVersion,
				ConversationID: l.conversationID,
				Rotated:        rotate,
				RotationReason: reason,
				ResumePrompt:   prompt,
				Blockers:       unique(blockers),
			}, err
		}
		// Re-bind once more in case SPA navigated during the permission dialog.
		_ = l.bindLiveConversation(ctx, goalID)
	} else if !cfg.HandsOffAfterPaste {
		if more, _ := l.Browser.DetectBlockers(ctx); len(more) > 0 {
			blockers = append(blockers, more...)
		}
	}

	// After bind + optional post-paste permission window: full hands-off.
	// Continuous CDP WaitIdle/snapshot during long tool turns freezes Chrome (r5).

	return ResumeResult{
		GoalID:         goalID,
		WorkerID:       workerID,
		LeaseID:        lease.LeaseID,
		CapsuleVersion: g.CapsuleVersion,
		ConversationID: l.conversationID,
		Rotated:        rotate,
		RotationReason: reason,
		ResumePrompt:   prompt,
		Blockers:       unique(blockers),
	}, nil
}

// bindLiveConversation reads location.href once and persists a real /c/… thread.
func (l *Loop) bindLiveConversation(ctx context.Context, goalID string) error {
	if l == nil || l.Browser == nil || l.Goals == nil {
		return fmt.Errorf("bind deps missing")
	}
	pageURL, err := l.Browser.CurrentURL(ctx)
	if err != nil {
		return err
	}
	bound := normalizeChatGPTThreadURL(pageURL)
	if bound == "" {
		return fmt.Errorf("no chatgpt thread url yet")
	}
	id := conversationIDFromURL(bound)
	if id != "" {
		l.conversationID = id
	}
	_, err = l.Goals.BindWorkerConversation(goalID, bound, id)
	return err
}

// resolveToolPermission retries DetectBlockers (which auto-clicks when enabled)
// until the permission modal is gone or timeout. Fails hard on timeout.
// If no permission is present on first check, returns immediately (pre-paste use).
func (l *Loop) resolveToolPermission(ctx context.Context, timeout time.Duration, blockers *[]string) error {
	if l == nil || l.Browser == nil || timeout <= 0 {
		return nil
	}
	deadline := time.Now().Add(timeout)
	for {
		more, err := l.Browser.DetectBlockers(ctx)
		if err == nil && len(more) > 0 {
			*blockers = append(*blockers, more...)
			*blockers = unique(*blockers)
		}
		if !hasUnresolvedToolPermission(*blockers) {
			return nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return fmt.Errorf("tool_permission_unresolved: permission dialog still present after %s (auto-approve failed or disabled)", timeout)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("tool_permission_unresolved: %w", ctx.Err())
		case <-time.After(2 * time.Second):
		}
		// Drop stale permission labels so the next DetectBlockers result is fresh.
		filtered := (*blockers)[:0]
		for _, b := range *blockers {
			lb := strings.ToLower(b)
			if strings.Contains(lb, "tool_permission") && !strings.Contains(lb, "auto_approved") {
				continue
			}
			filtered = append(filtered, b)
		}
		*blockers = filtered
	}
}

// waitAndResolveToolPermission polls for up to timeout for a tool-permission
	// dialog to appear (post-paste Svananda flow). On first successful auto-approve
	// it returns immediately (r8: continuing CDP polls after Allow pinned Chrome).
	// If a dialog appears but cannot be cleared, hard-fail. If none appears,
	// returns nil (MCP watchdog still covers "model never called tools").
	func (l *Loop) waitAndResolveToolPermission(ctx context.Context, timeout time.Duration, blockers *[]string) error {
		if l == nil || l.Browser == nil || timeout <= 0 {
			return nil
		}
		deadline := time.Now().Add(timeout)
		seen := false
		poll := 3 * time.Second
		for {
			if ctx.Err() != nil {
				if seen {
					return fmt.Errorf("tool_permission_unresolved: %w", ctx.Err())
				}
				return nil
			}
			more, err := l.Browser.DetectBlockers(ctx)
			if err == nil && len(more) > 0 {
				*blockers = append(*blockers, more...)
				*blockers = unique(*blockers)
			}
			// Success path: Allow clicked and verified — stop all CDP immediately.
			if hasAutoApprovedToolPermission(*blockers) {
				return nil
			}
			if hasUnresolvedToolPermission(*blockers) {
				seen = true
				// Cap clear-attempt so we never re-enter a long detect loop after paste.
				remain := time.Until(deadline)
				if remain < 10*time.Second {
					remain = 10 * time.Second
				}
				if remain > 25*time.Second {
					remain = 25 * time.Second
				}
				if err := l.resolveToolPermission(ctx, remain, blockers); err != nil {
					return err
				}
				if hasAutoApprovedToolPermission(*blockers) {
					return nil
				}
				// Cleared without explicit auto_approved label — still hands-off.
				if !hasUnresolvedToolPermission(*blockers) {
					return nil
				}
			}
			if time.Now().After(deadline) {
				if hasUnresolvedToolPermission(*blockers) {
					return fmt.Errorf("tool_permission_unresolved: permission dialog still present after %s (auto-approve failed or disabled)", timeout)
				}
				return nil
			}
			select {
			case <-ctx.Done():
				if seen {
					return fmt.Errorf("tool_permission_unresolved: %w", ctx.Err())
				}
				return nil
			case <-time.After(poll):
			}
			// Drop resolved permission labels between polls.
			filtered := (*blockers)[:0]
			for _, b := range *blockers {
				lb := strings.ToLower(b)
				if strings.Contains(lb, "tool_permission") && !strings.Contains(lb, "auto_approved") {
					continue
				}
				filtered = append(filtered, b)
			}
			*blockers = filtered
		}
	}

	func hasAutoApprovedToolPermission(blockers []string) bool {
		for _, b := range blockers {
			if strings.Contains(strings.ToLower(strings.TrimSpace(b)), "tool_permission_auto_approved") {
				return true
			}
		}
		return false
	}

	func hasUnresolvedToolPermission(blockers []string) bool {
	for _, b := range blockers {
		lb := strings.ToLower(strings.TrimSpace(b))
		if strings.Contains(lb, "tool_permission") && !strings.Contains(lb, "auto_approved") {
			return true
		}
	}
	return false
}

// hardBlockers are conditions where pasting another resume prompt will make things worse.
func hardBlockers(blockers []string) []string {
	var out []string
	for _, b := range blockers {
		lb := strings.ToLower(strings.TrimSpace(b))
		switch {
		case lb == "":
			continue
		// tool_permission is handled by resolveToolPermission (retry + hard fail),
		// not as an immediate hard blocker that skips auto-approve.
		case strings.Contains(lb, "page_stuck"), strings.Contains(lb, "page_busy"):
			out = append(out, b)
		case strings.Contains(lb, "login_required"), strings.Contains(lb, "captcha"),
			strings.Contains(lb, "cloudflare"), strings.Contains(lb, "usage limit"),
			strings.Contains(lb, "rate limit"):
			out = append(out, b)
		}
	}
	return out
}

func normalizeChatGPTThreadURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http://") {
		raw = "https://" + strings.TrimPrefix(raw, "http://")
	}
	// Accept chatgpt.com/c/<id> or chatgpt.com/g/.../c/<id>
	if !(strings.Contains(raw, "chatgpt.com/") || strings.Contains(raw, "chat.openai.com/")) {
		return ""
	}
	if conversationIDFromURL(raw) == "" {
		return ""
	}
	// Strip query/fragment for stable binding.
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	return strings.TrimRight(raw, "/")
}

func conversationIDFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ForceRotate marks the next Wake to open a new conversation.
func (l *Loop) ForceRotate() {
	l.conversationID = ""
	l.turnsInConvo = 0
}

func first(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func unique(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

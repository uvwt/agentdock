package reasoner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Backend identifies a reasoning provider.
type Backend string

const (
	BackendChatGPTWeb Backend = "chatgpt_web"
	BackendAPI        Backend = "api"
	BackendLocal      Backend = "local"
	BackendNone       Backend = "none"
)

// TaskClass drives routing policy.
type TaskClass string

const (
	TaskLogSummary   TaskClass = "log_summary"
	TaskRoutineFix   TaskClass = "routine_fix"
	TaskArchitecture TaskClass = "architecture"
	TaskFinalReview  TaskClass = "final_review"
	TaskUnknown      TaskClass = "unknown"
)

// Request is a unit of reasoning work.
type Request struct {
	GoalID      string
	Class       TaskClass
	Prompt      string
	MaxCostUSD  float64
	PreferLocal bool
}

// Decision is the chosen backend + fallback chain.
type Decision struct {
	Primary   Backend   `json:"primary"`
	Fallbacks []Backend `json:"fallbacks,omitempty"`
	Reason    string    `json:"reason"`
	Estimated float64   `json:"estimated_cost_usd,omitempty"`
}

// Budget tracks spend across a goal or process.
type Budget struct {
	mu       sync.Mutex
	LimitUSD float64
	SpentUSD float64
}

func (b *Budget) Remaining() float64 {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.LimitUSD - b.SpentUSD
}

func (b *Budget) Charge(usd float64) error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.LimitUSD > 0 && b.SpentUSD+usd > b.LimitUSD {
		return fmt.Errorf("reasoner budget exceeded: spent=%.4f limit=%.4f", b.SpentUSD, b.LimitUSD)
	}
	b.SpentUSD += usd
	return nil
}

// Router selects a backend without performing model inference itself.
type Router struct {
	Budget          *Budget
	AllowChatGPTWeb bool
	AllowAPI        bool
	AllowLocal      bool
	// Optional cost estimates per class/backend for budgeting.
	Estimate func(Backend, TaskClass) float64
}

// Route implements the plan's hybrid routing example:
// log summary → local; general fix → API; architecture → ChatGPT web; final review → high capability.
func (r *Router) Route(req Request) (Decision, error) {
	if r == nil {
		return Decision{}, errors.New("router is nil")
	}
	class := req.Class
	if class == "" {
		class = TaskUnknown
	}
	var primary Backend
	var reason string
	switch class {
	case TaskLogSummary:
		primary, reason = BackendLocal, "log summary prefers local/cheap model"
	case TaskRoutineFix:
		primary, reason = BackendAPI, "routine fix prefers coding API model"
	case TaskArchitecture:
		primary, reason = BackendChatGPTWeb, "architecture prefers high-capability ChatGPT web worker"
	case TaskFinalReview:
		primary, reason = BackendChatGPTWeb, "final review prefers high-capability model"
	default:
		primary, reason = BackendAPI, "default to API coding model"
	}
	if req.PreferLocal {
		primary, reason = BackendLocal, "caller prefers local"
	}

	// Capability filters + fallbacks.
	chain := []Backend{primary}
	switch primary {
	case BackendLocal:
		chain = append(chain, BackendAPI, BackendChatGPTWeb)
	case BackendAPI:
		chain = append(chain, BackendChatGPTWeb, BackendLocal)
	case BackendChatGPTWeb:
		chain = append(chain, BackendAPI, BackendLocal)
	}
	filtered := make([]Backend, 0, len(chain))
	for _, b := range chain {
		if r.allowed(b) {
			filtered = append(filtered, b)
		}
	}
	if len(filtered) == 0 {
		return Decision{Primary: BackendNone, Reason: "no reasoning backend enabled"}, errors.New("no reasoning backend enabled")
	}
	// Budget gate on estimated primary cost.
	est := r.estimate(filtered[0], class)
	if req.MaxCostUSD > 0 && est > req.MaxCostUSD {
		// pick cheapest allowed
		filtered = sortByCost(filtered, class, r)
		est = r.estimate(filtered[0], class)
		reason += "; adjusted for max cost"
	}
	if r.Budget != nil && r.Budget.LimitUSD > 0 && est > r.Budget.Remaining() {
		filtered = sortByCost(filtered, class, r)
		// try free/local first
		if r.allowed(BackendLocal) {
			filtered = prepend(filtered, BackendLocal)
		}
		est = r.estimate(filtered[0], class)
		if est > r.Budget.Remaining() {
			return Decision{Primary: BackendNone, Reason: "budget exhausted"}, errors.New("reasoner budget exhausted")
		}
		reason += "; adjusted for remaining budget"
	}
	dec := Decision{Primary: filtered[0], Reason: reason, Estimated: est}
	if len(filtered) > 1 {
		dec.Fallbacks = filtered[1:]
	}
	return dec, nil
}

// Execute is a placeholder: real backends inject themselves. Returns which backend would run.
type Executor func(ctx context.Context, backend Backend, req Request) error

func (r *Router) Run(ctx context.Context, req Request, exec Executor) (Backend, error) {
	if exec == nil {
		return BackendNone, errors.New("executor required")
	}
	dec, err := r.Route(req)
	if err != nil {
		return BackendNone, err
	}
	try := append([]Backend{dec.Primary}, dec.Fallbacks...)
	var last error
	for _, b := range try {
		if ctx.Err() != nil {
			return BackendNone, ctx.Err()
		}
		cost := r.estimate(b, req.Class)
		if err := r.Budget.Charge(cost); err != nil {
			last = err
			continue
		}
		if err := exec(ctx, b, req); err != nil {
			last = err
			continue
		}
		return b, nil
	}
	if last == nil {
		last = errors.New("all backends failed")
	}
	return BackendNone, last
}

func (r *Router) allowed(b Backend) bool {
	switch b {
	case BackendChatGPTWeb:
		return r.AllowChatGPTWeb
	case BackendAPI:
		return r.AllowAPI
	case BackendLocal:
		return r.AllowLocal
	default:
		return false
	}
}

func (r *Router) estimate(b Backend, class TaskClass) float64 {
	if r.Estimate != nil {
		return r.Estimate(b, class)
	}
	switch b {
	case BackendLocal:
		return 0
	case BackendAPI:
		if class == TaskFinalReview || class == TaskArchitecture {
			return 0.05
		}
		return 0.01
	case BackendChatGPTWeb:
		// Web may be subscription-sunk cost; treat as 0 marginal for budget, policy still prefers it for hard tasks.
		return 0
	default:
		return 0
	}
}

func sortByCost(in []Backend, class TaskClass, r *Router) []Backend {
	out := append([]Backend(nil), in...)
	// simple insertion by estimate
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && r.estimate(out[j], class) < r.estimate(out[j-1], class) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

func prepend(in []Backend, b Backend) []Backend {
	for _, x := range in {
		if x == b {
			return in
		}
	}
	return append([]Backend{b}, in...)
}

// ClassifyHeuristic maps free text to a task class (best-effort for demos).
func ClassifyHeuristic(text string) TaskClass {
	t := strings.ToLower(text)
	switch {
	case strings.Contains(t, "architecture"), strings.Contains(t, "design"), strings.Contains(t, "refactor module"):
		return TaskArchitecture
	case strings.Contains(t, "final review"), strings.Contains(t, "code review"):
		return TaskFinalReview
	case strings.Contains(t, "bug"), strings.Contains(t, "patch"), strings.Contains(t, "unit test"), strings.Contains(t, "fix the"), strings.Contains(t, "fix a"), strings.Contains(t, "login"):
		return TaskRoutineFix
	case strings.Contains(t, "stack trace"), strings.Contains(t, "summarize"), strings.Contains(t, "these logs"), strings.Contains(t, "log file"):
		return TaskLogSummary
	default:
		return TaskUnknown
	}
}

// Idle sleep helper for tests / loops.
func Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

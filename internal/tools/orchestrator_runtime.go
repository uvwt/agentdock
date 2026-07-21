package tools

import (
	"context"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/goal"
	"github.com/uvwt/agentdock/internal/orchestrator"
)

// runtimeExecutor runs deterministic goal steps via local workflow runner.
type runtimeExecutor struct{ rt *Runtime }

func (e runtimeExecutor) ExecutePending(ctx context.Context, g goal.Goal) (goal.RunResult, goal.Goal, error) {
	runner := &goal.Runner{WorkDir: e.rt.ws.Root()}
	result := runner.ExecuteGoalSteps(ctx, g)
	updated, err := e.rt.goals.ApplyExecution(g.ID, result)
	if err != nil {
		return result, g, err
	}
	return result, updated, nil
}

// workerWaker adapts chatgpt.Worker to orchestrator.Waker.
type workerWaker struct{ rt *Runtime }

func (w workerWaker) Wake(ctx context.Context, goalID string) (map[string]any, error) {
	if w.rt.chatgptWorker == nil {
		return nil, errWorkerUnavailable()
	}
	return w.rt.chatgptWorker.Wake(ctx, goalID)
}

func (w workerWaker) SoftRebind() {
	if w.rt.chatgptWorker != nil {
		w.rt.chatgptWorker.SoftRebind()
	}
}

func (w workerWaker) ForceRotate() {
	if w.rt.chatgptWorker != nil {
		w.rt.chatgptWorker.ForceRotate()
	}
}

func (w workerWaker) ClearWakeCooldown(goalID string) {
	if w.rt.chatgptWorker != nil {
		w.rt.chatgptWorker.ClearWakeCooldown(goalID)
	}
}

func (w workerWaker) Status() map[string]any {
	if w.rt.chatgptWorker == nil {
		return map[string]any{"enabled": false}
	}
	return w.rt.chatgptWorker.Status()
}

func errWorkerUnavailable() error {
	return toolError("WORKER_UNAVAILABLE", "ChatGPT browser worker is not initialized", "runtime")
}

// runtimeActivity adapts Runtime MCP tool-call log to orchestrator.ActivitySource.
type runtimeActivity struct{ rt *Runtime }

func (a runtimeActivity) MCPToolActivitySince(since time.Time) bool {
	if a.rt == nil {
		return false
	}
	return a.rt.MCPConnectedRecently(since)
}

func (a runtimeActivity) MCPToolActivitySummary(since time.Time) string {
	if a.rt == nil {
		return ""
	}
	calls := a.rt.MCPToolCallsSince(since)
	if len(calls) == 0 {
		return ""
	}
	// Keep short: last few tool names.
	n := len(calls)
	start := 0
	if n > 6 {
		start = n - 6
	}
	parts := make([]string, 0, n-start)
	for _, c := range calls[start:] {
		label := c.Name
		if c.Action != "" {
			label = c.Name + ":" + c.Action
		}
		if !c.OK {
			label += "!"
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, ",")
}

func (r *Runtime) ensureOrchestrator() *orchestrator.Orchestrator {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	if r.orch != nil {
		return r.orch
	}
	r.orch = orchestrator.New(r.goals, workerWaker{rt: r}, runtimeExecutor{rt: r}, orchestrator.DefaultConfig())
	r.orch.SetActivitySource(runtimeActivity{rt: r})
	return r.orch
}

func (r *Runtime) RuntimeOrchestratorStart(goalID string) (Result, error) {
	if r.goals == nil {
		return nil, toolError("GOAL_UNAVAILABLE", "goal store is not initialized", "runtime")
	}
	// While L3 owns this goal, suppress auto-wake so request_reasoning cannot
	// fire a second concurrent paste into ChatGPT (the spam loop users hit).
	if r.chatgptWorker != nil {
		r.chatgptWorker.SetAutoWakeSuppressed(goalID, true)
	}
	orch := r.ensureOrchestrator()
	st, err := orch.Start(goalID)
	res := Result{"ok": err == nil, "action": "orchestrate_start", "orchestrator": st, "source": runtimeAPISource}
	if err != nil {
		res["error"] = err.Error()
		if r.chatgptWorker != nil && !st.Running {
			r.chatgptWorker.SetAutoWakeSuppressed(goalID, false)
		}
		// still return status when already terminal etc.
		return res, err
	}
	return res, nil
}

func (r *Runtime) RuntimeOrchestratorStop(goalID string) Result {
	orch := r.ensureOrchestrator()
	st := orch.Stop(goalID)
	if r.chatgptWorker != nil {
		r.chatgptWorker.SetAutoWakeSuppressed(goalID, false)
	}
	return Result{"ok": true, "action": "orchestrate_stop", "orchestrator": st, "source": runtimeAPISource}
}

func (r *Runtime) RuntimeOrchestratorStatus(goalID string) Result {
	orch := r.ensureOrchestrator()
	st := orch.Status(goalID)
	running := orch.ListRunning()
	return Result{
		"ok": true, "action": "orchestrate_status", "source": runtimeAPISource,
		"orchestrator": st, "running": running, "count_running": len(running),
		"worker": r.RuntimeChatGPTWorkerStatus(),
	}
}

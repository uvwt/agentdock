package tools

import (
	"context"
	"strings"

	"github.com/uvwt/agentdock/internal/goal"
)

func (r *Runtime) RuntimeGoals(status string, limit int) (Result, error) {
	if r.goals == nil {
		return nil, toolError("GOAL_UNAVAILABLE", "goal store is not initialized", "runtime")
	}
	var filter goal.Status
	if s := strings.ToLower(strings.TrimSpace(status)); s != "" {
		filter = goal.Status(s)
	}
	if limit <= 0 {
		limit = 50
	}
	items, err := r.goals.List(filter, limit)
	if err != nil {
		return nil, goalToolError(err)
	}
	out := make([]map[string]any, 0, len(items))
	for _, g := range items {
		sum := compactGoalSummary(g)
		sum["created_at"] = g.CreatedAt
		sum["objective"] = g.Objective
		sum["no_progress_streak"] = g.NoProgressStreak
		sum["criteria_total"] = len(g.SuccessCriteria)
		pendingApprovals := 0
		for _, a := range g.PendingApprovals {
			if a.Status == "" || a.Status == "pending" {
				pendingApprovals++
			}
		}
		sum["pending_approvals"] = pendingApprovals
		out = append(out, sum)
	}
	return Result{
		"ok": true, "source": runtimeAPISource, "action": "list",
		"goals": out, "count": len(out),
		"active_goal_id": r.ActiveGoalID(),
		"state_dir":      r.goals.Root(),
	}, nil
}

func (r *Runtime) RuntimeGoal(id string) (Result, error) {
	if r.goals == nil {
		return nil, toolError("GOAL_UNAVAILABLE", "goal store is not initialized", "runtime")
	}
	g, err := r.goals.Get(strings.TrimSpace(id))
	if err != nil {
		return nil, goalToolError(err)
	}
	report := goal.Verify(g)
	return Result{
		"ok": true, "source": runtimeAPISource, "action": "get",
		"goal": g, "capsule": goal.BuildCapsule(g), "verify": report,
		"active_goal_id": r.ActiveGoalID(),
	}, nil
}

func (r *Runtime) RuntimeResolveGoalApproval(goalID, approvalID, decision, note string) (Result, error) {
	if r.goals == nil {
		return nil, toolError("GOAL_UNAVAILABLE", "goal store is not initialized", "runtime")
	}
	g, err := r.goals.ResolveApproval(strings.TrimSpace(goalID), strings.TrimSpace(approvalID), decision, note)
	if err != nil {
		return nil, goalToolError(err)
	}
	return Result{
		"ok": true, "source": runtimeAPISource, "action": "resolve_approval",
		"goal_id": g.ID, "goal_summary": compactGoalSummary(g), "approvals": g.PendingApprovals,
	}, nil
}

func (r *Runtime) RuntimeGoalPause(goalID, summary string) (Result, error) {
	if r.goals == nil {
		return nil, toolError("GOAL_UNAVAILABLE", "goal store is not initialized", "runtime")
	}
	g, err := r.goals.Pause(strings.TrimSpace(goalID), firstNonEmpty(summary, "paused from dashboard"))
	if err != nil {
		return nil, goalToolError(err)
	}
	return Result{"ok": true, "source": runtimeAPISource, "action": "pause", "goal_summary": compactGoalSummary(g)}, nil
}

func (r *Runtime) RuntimeGoalResume(goalID, summary string) (Result, error) {
	if r.goals == nil {
		return nil, toolError("GOAL_UNAVAILABLE", "goal store is not initialized", "runtime")
	}
	g, err := r.goals.Resume(strings.TrimSpace(goalID), firstNonEmpty(summary, "resumed from dashboard"))
	if err != nil {
		return nil, goalToolError(err)
	}
	return Result{"ok": true, "source": runtimeAPISource, "action": "resume", "goal_summary": compactGoalSummary(g), "capsule": goal.BuildCapsule(g)}, nil
}

func (r *Runtime) RuntimeGoalCancel(goalID, summary string) (Result, error) {
	if r.goals == nil {
		return nil, toolError("GOAL_UNAVAILABLE", "goal store is not initialized", "runtime")
	}
	g, err := r.goals.Cancel(strings.TrimSpace(goalID), firstNonEmpty(summary, "cancelled from dashboard"))
	if err != nil {
		return nil, goalToolError(err)
	}
	if r.ActiveGoalID() == g.ID {
		r.SetActiveGoalID("")
	}
	return Result{"ok": true, "source": runtimeAPISource, "action": "cancel", "goal_summary": compactGoalSummary(g)}, nil
}

func (r *Runtime) RuntimeGoalBind(goalID string) (Result, error) {
	if r.goals == nil {
		return nil, toolError("GOAL_UNAVAILABLE", "goal store is not initialized", "runtime")
	}
	g, err := r.goals.Get(strings.TrimSpace(goalID))
	if err != nil {
		return nil, goalToolError(err)
	}
	r.SetActiveGoalID(g.ID)
	return Result{"ok": true, "source": runtimeAPISource, "action": "bind", "active_goal_id": g.ID, "goal_summary": compactGoalSummary(g)}, nil
}

func (r *Runtime) RuntimeGoalUnbind() Result {
	prev := r.ActiveGoalID()
	r.SetActiveGoalID("")
	return Result{"ok": true, "source": runtimeAPISource, "action": "unbind", "previous_goal_id": prev, "active_goal_id": ""}
}

func (r *Runtime) RuntimeChatGPTWorkerStatus() Result {
	if r.chatgptWorker == nil {
		return Result{"ok": true, "enabled": false, "message": "ChatGPT worker not initialized"}
	}
	st := r.chatgptWorker.Status()
	st["ok"] = true
	st["source"] = runtimeAPISource
	return Result(st)
}

func (r *Runtime) RuntimeChatGPTOpen(ctx context.Context) (Result, error) {
	if r.chatgptWorker == nil {
		return nil, toolError("WORKER_UNAVAILABLE", "ChatGPT browser worker is not initialized", "runtime")
	}
	out, err := r.chatgptWorker.OpenSession(ctx)
	if out == nil {
		out = map[string]any{}
	}
	res := Result(out)
	res["source"] = runtimeAPISource
	res["action"] = "chatgpt_open"
	return res, err
}

func (r *Runtime) RuntimeChatGPTWake(ctx context.Context, goalID string) (Result, error) {
	if r.chatgptWorker == nil {
		return nil, toolError("WORKER_UNAVAILABLE", "ChatGPT browser worker is not initialized", "runtime")
	}
	goalID = strings.TrimSpace(goalID)
	if goalID == "" {
		return nil, toolErrorDetails("VALIDATION_ERROR", "goal_id is required", "validation", map[string]any{"field": "goal_id"})
	}
	// Ensure goal is marked awaiting reasoning so capsule/resume prompt is current.
	if g, err := r.goals.Get(goalID); err == nil {
		if g.Status != goal.StatusAwaitingReasoning && g.Status != goal.StatusCompleted && g.Status != goal.StatusCancelled && g.Status != goal.StatusFailed {
			if _, err := r.goals.RequestReasoning(goalID, g.CurrentRequest, g.CurrentProblem); err != nil {
				// non-fatal: still try wake
			}
		}
	}
	out, err := r.chatgptWorker.Wake(ctx, goalID)
	if out == nil {
		out = map[string]any{}
	}
	res := Result(out)
	res["source"] = runtimeAPISource
	res["action"] = "chatgpt_wake"
	return res, err
}

func (r *Runtime) RuntimeRequestReasoning(goalID, request, problem string) (Result, error) {
	if r.goals == nil {
		return nil, toolError("GOAL_UNAVAILABLE", "goal store is not initialized", "runtime")
	}
	g, err := r.goals.RequestReasoning(strings.TrimSpace(goalID), request, problem)
	if err != nil {
		return nil, goalToolError(err)
	}
	// Auto-wake ChatGPT worker when configured.
	if r.chatgptWorker != nil {
		r.chatgptWorker.MaybeAutoWake(g)
	}
	return Result{
		"ok": true, "source": runtimeAPISource, "action": "request_reasoning",
		"goal_summary": compactGoalSummary(g), "capsule": goal.BuildCapsule(g),
		"worker":  r.RuntimeChatGPTWorkerStatus(),
		"message": "goal is awaiting_reasoning; ChatGPT worker auto-wake requested if enabled",
	}, nil
}

func (r *Runtime) RuntimeSetChatGPTAutoWake(enabled bool) Result {
	if r.chatgptWorker == nil {
		return Result{"ok": false, "error": "worker unavailable"}
	}
	r.chatgptWorker.SetAutoWake(enabled)
	return Result{"ok": true, "auto_wake": enabled, "worker": r.chatgptWorker.Status()}
}

func (r *Runtime) RuntimeSetChatGPTAutoApproveTools(enabled bool) Result {
	if r.chatgptWorker == nil {
		return Result{"ok": false, "error": "worker unavailable"}
	}
	r.chatgptWorker.SetAutoApproveTools(enabled)
	return Result{"ok": true, "auto_approve_tools": enabled, "worker": r.chatgptWorker.Status()}
}


func (r *Runtime) RuntimeChatGPTForceRotate() Result {
	if r.chatgptWorker == nil {
		return Result{"ok": false, "error": "worker unavailable"}
	}
	r.chatgptWorker.ForceRotate()
	st := r.chatgptWorker.Status()
	return Result{"ok": true, "action": "chatgpt_force_rotate", "worker": st, "message": "next wake will open a new ChatGPT conversation"}
}

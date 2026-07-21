package tools

import (
	"strings"
	"sync"

	"github.com/uvwt/agentdock/internal/goal"
)

// activeGoal tracks which goal (if any) gates high-risk built-in tools.
// Empty means no goal gate (legacy free tool use).
type activeGoalState struct {
	mu sync.RWMutex
	id string
}

func (r *Runtime) ActiveGoalID() string {
	if r == nil {
		return ""
	}
	r.activeGoal.mu.RLock()
	defer r.activeGoal.mu.RUnlock()
	return r.activeGoal.id
}

func (r *Runtime) SetActiveGoalID(id string) {
	if r == nil {
		return
	}
	r.activeGoal.mu.Lock()
	r.activeGoal.id = strings.TrimSpace(id)
	r.activeGoal.mu.Unlock()
}

// enforceGoalToolPolicy blocks high-risk built-ins when an active goal's policy denies them.
// Read-only tools are never gated here.
func (r *Runtime) enforceGoalToolPolicy(tool string, args map[string]any) error {
	goalID := r.ActiveGoalID()
	if explicit := strings.TrimSpace(stringArg(args, "goal_id", "")); explicit != "" {
		goalID = explicit
	}
	if goalID == "" || r.goals == nil {
		return nil
	}
	// Only gate mutating / high-impact tools.
	switch tool {
	case "exec_command", "file_edit", "git_write":
	default:
		return nil
	}
	g, err := r.goals.Get(goalID)
	if err != nil {
		// If bound goal is missing, fail closed for gated tools.
		return toolErrorDetails("GOAL_NOT_FOUND", "active goal for policy gate not found: "+goalID, "not_found", map[string]any{"goal_id": goalID})
	}
	switch g.Status {
	case goal.StatusCompleted, goal.StatusCancelled, goal.StatusFailed:
		return toolErrorDetails("POLICY_DENIED", "active goal is terminal; unbind or create a new goal before mutating tools", "policy", map[string]any{
			"goal_id": goalID, "status": g.Status,
		})
	}

	action, targets := mapToolToGoalAction(tool, args)
	decision := goal.CheckPolicy(g, action, targets)
	if decision.Allowed {
		return nil
	}
	code := "POLICY_DENIED"
	if decision.Level == goal.RiskApprove {
		code = "APPROVAL_REQUIRED"
	}
	return toolErrorDetails(code, decision.Reason, "policy", map[string]any{
		"goal_id":         goalID,
		"tool":            tool,
		"step_action":     action,
		"targets":         targets,
		"policy_level":    decision.Level,
		"approval_action": decision.Approval,
		"next_action":     "goal_manage request_approval / resolve_approval, or goal_manage unbind if this work is outside the goal",
	})
}

func mapToolToGoalAction(tool string, args map[string]any) (goal.StepAction, []string) {
	switch tool {
	case "exec_command":
		cmd := stringArg(args, "cmd", "")
		return goal.ActionRunCommand, []string{cmd}
	case "file_edit":
		action := strings.ToLower(stringArg(args, "action", ""))
		path := stringArg(args, "path", "")
		switch action {
		case "delete":
			// treat deletes as high-risk command-like
			return goal.ActionRunCommand, []string{"rm " + path}
		case "patch", "replace", "add", "move":
			return goal.ActionApplyPatch, []string{path}
		default:
			return goal.ActionApplyPatch, []string{path}
		}
	case "git_write":
		action := strings.ToLower(stringArg(args, "action", ""))
		switch action {
		case "push":
			return goal.ActionRunCommand, []string{"git push"}
		case "commit":
			return goal.ActionApplyPatch, []string{"git commit"}
		case "clone", "fetch", "pull":
			return goal.ActionRunCommand, []string{"git " + action}
		default:
			return goal.ActionRunCommand, []string{"git " + action}
		}
	default:
		return goal.ActionRunCommand, nil
	}
}

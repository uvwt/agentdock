package goal

import (
	"fmt"
	"strings"
)

// RiskLevel classifies how freely an action may run.
type RiskLevel string

const (
	RiskAuto     RiskLevel = "auto"      // always allowed in non-readonly modes
	RiskGoalAuth RiskLevel = "goal_auth" // allowed while goal is executing/verifying
	RiskApprove  RiskLevel = "approve"   // requires explicit approval
	RiskForbid   RiskLevel = "forbid"    // blocked by policy/constraints
)

// PolicyDecision is the result of checking an action against goal policy.
type PolicyDecision struct {
	Level    RiskLevel `json:"level"`
	Allowed  bool      `json:"allowed"`
	Reason   string    `json:"reason,omitempty"`
	Approval string    `json:"approval_action,omitempty"` // suggested approval action key
}

// ClassifyStep returns the base risk of a whitelist step action.
func ClassifyStep(action StepAction) RiskLevel {
	switch action {
	case ActionInspectFiles, ActionCollectLogs, ActionCreateCheckpoint, ActionEnterVerify, ActionPreparePatch:
		return RiskAuto
	case ActionRunTests, ActionCollectMetrics, ActionBrowserVerify, ActionBrowserNavigate:
		return RiskGoalAuth
	case ActionApplyPatch, ActionRunCommand, ActionStartProcess, ActionBrowserAct, ActionReplan:
		return RiskGoalAuth
	case ActionRequestApproval, ActionMarkBlocked:
		return RiskAuto
	default:
		return RiskApprove
	}
}

// CheckPolicy evaluates whether a step/command may run under the goal's mode and constraints.
func CheckPolicy(g Goal, action StepAction, targets []string) PolicyDecision {
	if g.Mode == ModeReadonly {
		switch action {
		case ActionInspectFiles, ActionCollectLogs, ActionCreateCheckpoint, ActionBrowserVerify:
			return PolicyDecision{Level: RiskAuto, Allowed: true, Reason: "readonly allows observation"}
		default:
			return PolicyDecision{Level: RiskForbid, Allowed: false, Reason: "readonly mode forbids mutating or executing steps"}
		}
	}

	level := ClassifyStep(action)
	cmd := strings.Join(targets, " ")
	lower := strings.ToLower(cmd)

	// Constraint prohibitions and approval requirements.
	for _, c := range g.Constraints {
		val := strings.ToLower(strings.TrimSpace(c.Value))
		switch c.Type {
		case ConstraintProhibition:
			if prohibitionMatches(val, action, lower) {
				return PolicyDecision{
					Level: RiskForbid, Allowed: false,
					Reason: fmt.Sprintf("constraint prohibition %q blocks this action", c.Value),
				}
			}
		case ConstraintApproval:
			if approvalConstraintMatches(val, action, lower) {
				keys := []string{c.Value, actionKey(action, targets), string(action)}
				if highRiskCommand(lower) {
					keys = append(keys, "run:"+firstToken(lower), "high_risk_command")
				}
				if hasApprovedAny(g, keys...) {
					return PolicyDecision{Level: RiskApprove, Allowed: true, Reason: "approval granted", Approval: c.Value}
				}
				return PolicyDecision{
					Level: RiskApprove, Allowed: false,
					Reason:   fmt.Sprintf("constraint requires approval: %s", c.Value),
					Approval: c.Value,
				}
			}
		}
	}

	// Built-in high-risk command patterns always need approval in guarded mode.
	if action == ActionRunCommand || action == ActionStartProcess || action == ActionRunTests {
		if highRiskCommand(lower) {
			key := "run:" + firstToken(lower)
			if hasApproved(g, key) || hasApproved(g, "high_risk_command") {
				return PolicyDecision{Level: RiskApprove, Allowed: true, Reason: "high-risk command approved", Approval: key}
			}
			return PolicyDecision{
				Level: RiskApprove, Allowed: false,
				Reason:   "high-risk command requires approval (install/push/deploy/delete)",
				Approval: key,
			}
		}
	}

	switch level {
	case RiskAuto:
		return PolicyDecision{Level: level, Allowed: true}
	case RiskGoalAuth:
		switch g.Status {
		case StatusPlanning, StatusAwaitingPlanApproval, StatusDraft:
			// Planning is observation-only; mutations need a commit_turn into executing first.
			if action == ActionInspectFiles || action == ActionCollectLogs || action == ActionCollectMetrics || action == ActionBrowserVerify || action == ActionBrowserNavigate || action == ActionCreateCheckpoint {
				return PolicyDecision{Level: level, Allowed: true, Reason: "planning allows observation"}
			}
			return PolicyDecision{Level: RiskForbid, Allowed: false, Reason: "planning status does not allow mutating or executing steps; commit_turn first"}
		case StatusExecuting, StatusVerifying, StatusReplanning, StatusAwaitingReasoning, StatusAwaitingApproval:
			if g.Status == StatusAwaitingApproval && action == ActionApplyPatch {
				if hasApproved(g, actionKey(action, targets)) || hasApproved(g, string(action)) {
					return PolicyDecision{Level: level, Allowed: true, Reason: "patch approved"}
				}
				return PolicyDecision{Level: RiskApprove, Allowed: false, Reason: "apply_patch requires approval while awaiting_approval", Approval: string(action)}
			}
			return PolicyDecision{Level: level, Allowed: true}
		case StatusPaused, StatusBlocked, StatusCompleted, StatusCancelled, StatusFailed:
			return PolicyDecision{Level: RiskForbid, Allowed: false, Reason: fmt.Sprintf("status %s does not allow execution", g.Status)}
		default:
			return PolicyDecision{Level: level, Allowed: true}
		}
	case RiskApprove:
		key := actionKey(action, targets)
		if hasApproved(g, key) || hasApproved(g, string(action)) {
			return PolicyDecision{Level: level, Allowed: true, Approval: key}
		}
		return PolicyDecision{Level: level, Allowed: false, Reason: "approval required", Approval: key}
	default:
		return PolicyDecision{Level: RiskForbid, Allowed: false, Reason: "unknown risk level"}
	}
}

func actionKey(action StepAction, targets []string) string {
	if len(targets) == 0 {
		return string(action)
	}
	return string(action) + ":" + strings.Join(targets, ",")
}

func hasApproved(g Goal, key string) bool {
	return hasApprovedAny(g, key)
}

func hasApprovedAny(g Goal, keys ...string) bool {
	for _, key := range keys {
		key = strings.TrimSpace(strings.ToLower(key))
		if key == "" {
			continue
		}
		for _, a := range g.PendingApprovals {
			if a.Status != "approved" {
				continue
			}
			act := strings.ToLower(strings.TrimSpace(a.Action))
			if act == key || strings.EqualFold(a.ID, key) {
				return true
			}
			// prefix match for run:npm vs run:npm install ...
			if strings.HasPrefix(act, key) || strings.HasPrefix(key, act) {
				return true
			}
		}
	}
	return false
}

func prohibitionMatches(val string, action StepAction, cmd string) bool {
	switch val {
	case "no_git_push", "no-push", "no_push":
		return strings.Contains(cmd, "git push") || strings.Contains(cmd, "git-push")
	case "no_deploy":
		return strings.Contains(cmd, "deploy") || strings.Contains(cmd, "kubectl apply")
	case "no_delete":
		return action == ActionRunCommand && (strings.Contains(cmd, "rm -") || strings.Contains(cmd, "git clean"))
	default:
		// generic: prohibition value appears in command
		return val != "" && strings.Contains(cmd, val)
	}
}

func approvalConstraintMatches(val string, action StepAction, cmd string) bool {
	switch val {
	case "dependency_install_requires_approval", "install_requires_approval":
		return containsAny(cmd, "npm install", "pnpm install", "yarn add", "pip install", "go get", "brew install", "apt install", "apt-get install")
	case "git_push_requires_approval":
		return strings.Contains(cmd, "git push")
	case "apply_patch_requires_approval":
		return action == ActionApplyPatch
	default:
		return strings.Contains(cmd, val) || string(action) == val
	}
}

func highRiskCommand(cmd string) bool {
	return containsAny(cmd,
		"git push", "npm publish", "docker push",
		"npm install", "pnpm install", "yarn add", "pip install",
		"rm -rf", "rm -r", "mkfs", "dd if=",
		"kubectl apply", "helm install", "terraform apply",
		"shutdown", "reboot",
	)
}

func containsAny(s string, parts ...string) bool {
	for _, p := range parts {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func firstToken(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return "command"
	}
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return "command"
	}
	return fields[0]
}

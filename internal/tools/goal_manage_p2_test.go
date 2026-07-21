package tools

import (
	"context"
	"testing"

	"github.com/uvwt/agentdock/internal/goal"
)

func TestGoalManageWorkflowVerifyAndApproval(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)

	created, err := rt.Call(context.Background(), "goal_manage", map[string]any{
		"action":    "create",
		"title":     "Deterministic check",
		"objective": "true exits 0",
		"success_criteria": []map[string]any{
			{"id": "cmd", "type": "command", "expression": "exit_code == 0"},
		},
		"constraints": []map[string]any{
			{"type": "prohibition", "value": "no_git_push"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	goalID := created["goal_id"].(string)

	leased, err := rt.Call(context.Background(), "goal_manage", map[string]any{
		"action": "acquire_lease", "goal_id": goalID, "worker_id": "runner-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	lease := leased["lease"].(goal.Lease)
	if _, err := rt.Call(context.Background(), "goal_manage", map[string]any{
		"action": "commit_turn", "goal_id": goalID, "reasoning_lease_id": lease.LeaseID,
		"expected_capsule_version": lease.CapsuleVersion, "decision": "continue", "summary": "run true",
		"steps": []map[string]any{{"action": "run_command", "targets": []string{"true"}, "idempotency_key": "run_true"}},
	}); err != nil {
		t.Fatal(err)
	}

	// Policy deny for git push.
	pol, err := rt.Call(context.Background(), "goal_manage", map[string]any{
		"action": "check_policy", "goal_id": goalID,
		"policy_action": "run_command", "policy_targets": []string{"git push origin main"},
	})
	if err != nil {
		t.Fatal(err)
	}
	decision := pol["policy"].(goal.PolicyDecision)
	if decision.Allowed {
		t.Fatalf("git push should be denied: %#v", decision)
	}

	// Approval request + resolve.
	req, err := rt.Call(context.Background(), "goal_manage", map[string]any{
		"action": "request_approval", "goal_id": goalID,
		"approval_action": "run:npm", "summary": "install once", "risk": "medium",
	})
	if err != nil {
		t.Fatal(err)
	}
	// find approval id from full get
	full, err := rt.Call(context.Background(), "goal_manage", map[string]any{
		"action": "get", "goal_id": goalID, "full": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	g := full["goal"].(goal.Goal)
	if len(g.PendingApprovals) == 0 {
		t.Fatalf("expected pending approval after request: %#v", req)
	}
	aprID := g.PendingApprovals[0].ID
	if _, err := rt.Call(context.Background(), "goal_manage", map[string]any{
		"action": "resolve_approval", "goal_id": goalID,
		"approval_id": aprID, "approval_decision": "approved", "summary": "ok",
	}); err != nil {
		t.Fatal(err)
	}

	run, err := rt.Call(context.Background(), "goal_manage", map[string]any{
		"action": "run_workflow", "goal_id": goalID,
		"workflow": map[string]any{
			"name": "smoke",
			"steps": []map[string]any{
				{"type": "run", "name": "run_true", "command": "true", "kind": "tests"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	runResult := run["run"].(goal.RunResult)
	if !runResult.OK {
		t.Fatalf("run failed: %#v", runResult)
	}

	// Structured evidence for criterion + verify + complete.
	if _, err := rt.Call(context.Background(), "goal_manage", map[string]any{
		"action": "add_evidence", "goal_id": goalID,
		"evidence_kind": "tests", "evidence_summary": "true",
		"evidence_data": map[string]any{"exit_code": 0, "criterion_id": "cmd"},
	}); err != nil {
		t.Fatal(err)
	}
	verified, err := rt.Call(context.Background(), "goal_manage", map[string]any{
		"action": "verify", "goal_id": goalID,
	})
	if err != nil {
		t.Fatal(err)
	}
	report := verified["verify"].(goal.VerifyReport)
	if !report.OK {
		t.Fatalf("verify not ok: %#v", report)
	}
	if _, err := rt.Call(context.Background(), "goal_manage", map[string]any{
		"action": "mark_completed", "goal_id": goalID, "summary": "smoke passed",
	}); err != nil {
		t.Fatal(err)
	}
}

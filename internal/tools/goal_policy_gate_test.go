package tools

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/uvwt/agentdock/internal/goal"
)

func TestActiveGoalGatesExecAndGitPush(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)

	// Without bind, high-risk tools remain available (legacy).
	if _, err := rt.Call(context.Background(), "exec_command", map[string]any{
		"cmd": "true", "timeout_ms": 5000,
	}); err != nil {
		t.Fatalf("unbound exec should work: %v", err)
	}

	created, err := rt.Call(context.Background(), "goal_manage", map[string]any{
		"action": "create", "title": "gate", "objective": "policy gate",
		"success_criteria": []map[string]any{{"id": "m", "type": "manual", "expression": "ok"}},
		"constraints": []map[string]any{
			{"type": "prohibition", "value": "no_git_push"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	goalID := created["goal_id"].(string)
	if rt.ActiveGoalID() != goalID {
		t.Fatalf("create should auto-bind, got %q", rt.ActiveGoalID())
	}

	// Planning forbids executing commands via gate.
	if _, err := rt.Call(context.Background(), "exec_command", map[string]any{
		"cmd": "true", "timeout_ms": 5000,
	}); err == nil {
		t.Fatal("expected planning gate to block exec_command")
	} else if te, ok := err.(*ToolError); !ok || te.Code != "POLICY_DENIED" {
		t.Fatalf("want POLICY_DENIED, got %#v", err)
	}

	// Move to executing.
	leased, err := rt.Call(context.Background(), "goal_manage", map[string]any{
		"action": "acquire_lease", "goal_id": goalID, "worker_id": "w",
	})
	if err != nil {
		t.Fatal(err)
	}
	lease := leased["lease"].(goal.Lease)
	if _, err := rt.Call(context.Background(), "goal_manage", map[string]any{
		"action": "commit_turn", "goal_id": goalID, "reasoning_lease_id": lease.LeaseID,
		"expected_capsule_version": lease.CapsuleVersion, "decision": "continue", "summary": "go",
	}); err != nil {
		t.Fatal(err)
	}

	// Safe command allowed.
	if _, err := rt.Call(context.Background(), "exec_command", map[string]any{
		"cmd": "true", "timeout_ms": 5000,
	}); err != nil {
		t.Fatalf("safe exec should pass: %v", err)
	}

	// git push blocked by constraint even through git_write tool.
	if _, err := rt.Call(context.Background(), "git_write", map[string]any{
		"action": "push", "path": root,
	}); err == nil {
		t.Fatal("expected git push to be policy denied")
	} else if te, ok := err.(*ToolError); !ok || (te.Code != "POLICY_DENIED" && te.Code != "APPROVAL_REQUIRED") {
		t.Fatalf("want policy denial, got %#v", err)
	}

	// Unbind restores free tools.
	if _, err := rt.Call(context.Background(), "goal_manage", map[string]any{"action": "unbind"}); err != nil {
		t.Fatal(err)
	}
	if rt.ActiveGoalID() != "" {
		t.Fatal("expected unbound")
	}
	_ = filepath.Separator
}

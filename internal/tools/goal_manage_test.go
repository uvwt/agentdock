package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/goal"
)

func TestGoalManageCreateLeaseCommitAndRestart(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)

	created, err := rt.Call(context.Background(), "goal_manage", map[string]any{
		"action":    "create",
		"title":     "Fix login button",
		"objective": "Users can log in and reach /dashboard",
		"mode":      "guarded",
		"success_criteria": []map[string]any{
			{"id": "tests", "type": "command", "expression": "test_exit_code == 0"},
			{"id": "browser", "type": "browser", "expression": "url_contains:/dashboard"},
		},
		"constraints": []map[string]any{
			{"type": "prohibition", "value": "no_git_push"},
		},
		"milestones": []map[string]any{
			{"id": "repro", "title": "Reproduce"},
			{"id": "fix", "title": "Fix"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	goalID, _ := created["goal_id"].(string)
	if goalID == "" {
		t.Fatalf("missing goal_id: %#v", created)
	}
	capsule, ok := created["capsule"].(goal.Capsule)
	if !ok {
		// Call may return map via JSON round-trip depending on path; accept map too.
		if _, ok := created["capsule"].(map[string]any); !ok {
			t.Fatalf("capsule missing: %#v", created)
		}
	} else if capsule.ResumePrompt == "" {
		t.Fatal("resume prompt empty")
	}

	leased, err := rt.Call(context.Background(), "goal_manage", map[string]any{
		"action": "acquire_lease", "goal_id": goalID, "worker_id": "chatgpt-web-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, ok := leased["lease"].(goal.Lease)
	if !ok {
		t.Fatalf("lease type: %T %#v", leased["lease"], leased)
	}
	capAfterLease, _ := leased["capsule"].(goal.Capsule)
	version := lease.CapsuleVersion
	if capAfterLease.CapsuleVersion != 0 {
		version = capAfterLease.CapsuleVersion
	}

	committed, err := rt.Call(context.Background(), "goal_manage", map[string]any{
		"action":                   "commit_turn",
		"goal_id":                  goalID,
		"reasoning_lease_id":       lease.LeaseID,
		"expected_capsule_version": version,
		"decision":                 "continue",
		"summary":                  "Login API returns 500; inspect handler",
		"next_milestone":           "repro",
		"current_problem":          "POST /api/login → 500",
		"current_request":          "Propose a patch plan",
		"steps": []map[string]any{
			{"action": "inspect_files", "targets": []string{"src/login.ts"}, "summary": "read handler"},
			{"action": "run_tests", "idempotency_key": "run_tests_v1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	summary := committed["goal_summary"].(map[string]any)
	if summary["status"] != string(goal.StatusExecuting) && summary["status"] != goal.StatusExecuting {
		// status may be typed or string
		if s, ok := summary["status"].(goal.Status); !ok || s != goal.StatusExecuting {
			if s, ok := summary["status"].(string); !ok || s != string(goal.StatusExecuting) {
				t.Fatalf("unexpected status after commit: %#v", summary)
			}
		}
	}

	// STATE_CONFLICT on stale version.
	if _, err := rt.Call(context.Background(), "goal_manage", map[string]any{
		"action": "acquire_lease", "goal_id": goalID, "worker_id": "chatgpt-web-test",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Call(context.Background(), "goal_manage", map[string]any{
		"action": "commit_turn", "goal_id": goalID, "reasoning_lease_id": "lease_nope",
		"expected_capsule_version": 1, "decision": "continue", "summary": "stale",
	}); err == nil {
		t.Fatal("expected conflict on stale commit")
	} else if te, ok := err.(*ToolError); !ok || te.Code != "STATE_CONFLICT" && te.Code != "LEASE_REQUIRED" && te.Code != "VALIDATION_ERROR" {
		// lease mismatch maps to STATE_CONFLICT
		if te, ok := err.(*ToolError); ok && te.Code == "STATE_CONFLICT" {
			// ok
		} else if te, ok := err.(*ToolError); ok {
			if te.Code != "STATE_CONFLICT" {
				// Accept LEASE/CONFLICT family
				if te.Code != "STATE_CONFLICT" && !strings.Contains(te.Message, "lease") && te.Code != "LEASE_REQUIRED" {
					t.Fatalf("unexpected error: %#v", te)
				}
			}
		} else {
			t.Fatalf("unexpected error type: %v", err)
		}
	}

	cfg := config.Config{AgentDockDefaultDir: root, AgentDockHome: filepath.Join(root, ".agentdock")}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := restarted.Call(context.Background(), "goal_manage", map[string]any{
		"action": "get", "goal_id": goalID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if loaded["capsule"] == nil {
		t.Fatalf("capsule missing after restart: %#v", loaded)
	}

	// Evidence must satisfy machine criteria before complete.
	if _, err := restarted.Call(context.Background(), "goal_manage", map[string]any{
		"action": "add_evidence", "goal_id": goalID,
		"evidence_kind": "tests", "evidence_summary": "unit tests passed",
		"evidence_data": map[string]any{"test_exit_code": 0, "criterion_id": "tests"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Call(context.Background(), "goal_manage", map[string]any{
		"action": "mark_completed", "goal_id": goalID, "summary": "partial",
	}); err == nil {
		t.Fatal("expected mark_completed to fail without browser evidence")
	}
	if _, err := restarted.Call(context.Background(), "goal_manage", map[string]any{
		"action": "add_evidence", "goal_id": goalID,
		"evidence_kind": "browser", "evidence_summary": "dashboard ok",
		"evidence_data": map[string]any{"url": "http://127.0.0.1:3000/dashboard", "console_errors": 0, "criterion_id": "browser"},
	}); err != nil {
		t.Fatal(err)
	}
	done, err := restarted.Call(context.Background(), "goal_manage", map[string]any{
		"action": "mark_completed", "goal_id": goalID, "summary": "login fixed",
	})
	if err != nil {
		t.Fatal(err)
	}
	doneSummary := done["goal_summary"].(map[string]any)
	status := doneSummary["status"]
	if status != goal.StatusCompleted && status != string(goal.StatusCompleted) {
		t.Fatalf("expected completed, got %#v", doneSummary)
	}
}

func TestGoalManageRejectsUnknownStepAction(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	created, err := rt.Call(context.Background(), "goal_manage", map[string]any{
		"action": "create", "title": "x", "objective": "y",
		"success_criteria": []map[string]any{{"expression": "ok", "type": "manual"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	goalID := created["goal_id"].(string)
	leased, err := rt.Call(context.Background(), "goal_manage", map[string]any{
		"action": "acquire_lease", "goal_id": goalID, "worker_id": "w",
	})
	if err != nil {
		t.Fatal(err)
	}
	lease := leased["lease"].(goal.Lease)
	_, err = rt.Call(context.Background(), "goal_manage", map[string]any{
		"action": "commit_turn", "goal_id": goalID, "reasoning_lease_id": lease.LeaseID,
		"expected_capsule_version": lease.CapsuleVersion, "decision": "continue", "summary": "bad step",
		"steps": []map[string]any{{"action": "rm -rf /"}},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestGoalManageListedInToolDefinitions(t *testing.T) {
	found := false
	for _, def := range ToolDefinitions() {
		if def.Name == "goal_manage" {
			found = true
			if def.InputSchema == nil || def.OutputSchema == nil {
				t.Fatalf("schemas missing: %#v", def)
			}
			break
		}
	}
	if !found {
		t.Fatal("goal_manage not registered")
	}
}

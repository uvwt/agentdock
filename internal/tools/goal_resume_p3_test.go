package tools

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/goal"
)

// P3: a new worker process can resume from capsule alone—no prior chat history.
func TestManualCrossConversationResume(t *testing.T) {
	rt1, root := newCodeToolsRuntime(t)

	created, err := rt1.Call(context.Background(), "goal_manage", map[string]any{
		"action":    "create",
		"title":     "Login fix",
		"objective": "Users reach /dashboard after login",
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
	goalID := created["goal_id"].(string)
	cap1 := created["capsule"].(goal.Capsule)
	if cap1.ResumePrompt == "" {
		t.Fatal("create capsule missing resume_prompt")
	}

	leased, err := rt1.Call(context.Background(), "goal_manage", map[string]any{
		"action": "acquire_lease", "goal_id": goalID, "worker_id": "chatgpt-session-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	lease := leased["lease"].(goal.Lease)
	if _, err := rt1.Call(context.Background(), "goal_manage", map[string]any{
		"action": "commit_turn", "goal_id": goalID, "reasoning_lease_id": lease.LeaseID,
		"expected_capsule_version": lease.CapsuleVersion,
		"decision":                 "continue",
		"summary":                  "login returns 500",
		"next_milestone":           "repro",
		"current_problem":          "POST /api/login → 500",
		"current_request":          "Inspect handler and propose patch",
		"completed":                []string{"reproduced failure"},
		"steps": []map[string]any{
			{"action": "inspect_files", "targets": []string{"src/login.ts"}},
			{"action": "run_tests", "idempotency_key": "tests_v1"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Store content-addressed artifact as conversation 1 evidence trail.
	art, err := rt1.Call(context.Background(), "goal_manage", map[string]any{
		"action": "store_artifact", "goal_id": goalID,
		"artifact_text": "tsan: data race in FrameQueue", "artifact_filename": "tsan.txt",
		"evidence_kind": "log", "evidence_summary": "tsan failure",
		"criterion_id": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if art["artifact"] == nil {
		t.Fatalf("artifact missing: %#v", art)
	}

	// --- Simulate conversation rotation: brand-new Runtime, no chat history. ---
	cfg := config.Config{AgentDockDefaultDir: root, AgentDockHome: filepath.Join(root, ".agentdock")}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	rt2, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Worker only has goal_id from resume prompt paste.
	resumed, err := rt2.Call(context.Background(), "goal_manage", map[string]any{
		"action": "get", "goal_id": goalID,
	})
	if err != nil {
		t.Fatal(err)
	}
	cap2 := resumed["capsule"].(goal.Capsule)
	if cap2.CapsuleVersion < 2 {
		t.Fatalf("expected advanced capsule, got %#v", cap2)
	}
	if cap2.CurrentProblem == "" || cap2.ResumePrompt == "" {
		t.Fatalf("capsule incomplete for resume: %#v", cap2)
	}
	if len(cap2.Completed) == 0 {
		t.Fatalf("completed notes lost: %#v", cap2)
	}

	// Bind + continue as a new worker.
	if _, err := rt2.Call(context.Background(), "goal_manage", map[string]any{
		"action": "bind", "goal_id": goalID,
	}); err != nil {
		t.Fatal(err)
	}
	leased2, err := rt2.Call(context.Background(), "goal_manage", map[string]any{
		"action": "acquire_lease", "goal_id": goalID, "worker_id": "chatgpt-session-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	lease2 := leased2["lease"].(goal.Lease)
	if _, err := rt2.Call(context.Background(), "goal_manage", map[string]any{
		"action": "commit_turn", "goal_id": goalID, "reasoning_lease_id": lease2.LeaseID,
		"expected_capsule_version": lease2.CapsuleVersion,
		"decision":                 "verify",
		"summary":                  "patch applied in conversation 2",
		"next_milestone":           "fix",
	}); err != nil {
		t.Fatal(err)
	}

	// Structured evidence + complete without any session-1 chat state.
	if _, err := rt2.Call(context.Background(), "goal_manage", map[string]any{
		"action": "add_evidence", "goal_id": goalID,
		"evidence_kind": "tests", "evidence_summary": "tests passed",
		"evidence_data": map[string]any{"test_exit_code": 0, "criterion_id": "tests"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := rt2.Call(context.Background(), "goal_manage", map[string]any{
		"action": "add_evidence", "goal_id": goalID,
		"evidence_kind": "browser", "evidence_summary": "dashboard",
		"evidence_data": map[string]any{"url": "http://127.0.0.1:3000/dashboard", "criterion_id": "browser"},
	}); err != nil {
		t.Fatal(err)
	}
	done, err := rt2.Call(context.Background(), "goal_manage", map[string]any{
		"action": "mark_completed", "goal_id": goalID, "summary": "login fixed across conversations",
	})
	if err != nil {
		t.Fatal(err)
	}
	summary := done["goal_summary"].(map[string]any)
	if summary["status"] != goal.StatusCompleted && summary["status"] != string(goal.StatusCompleted) {
		t.Fatalf("expected completed after resume: %#v", summary)
	}
}

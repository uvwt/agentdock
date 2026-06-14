package tools

import (
	"context"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/taskstate"
)

func TestTaskManageLifecycleAndRestartRecovery(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	created, err := rt.Call(context.Background(), "task_manage", map[string]any{
		"action":                "create",
		"title":                 "Deploy AgentDock",
		"goal":                  "deploy and verify AgentDock",
		"completion_conditions": []string{"health endpoint returns 200", "server_info succeeds"},
	})
	if err != nil {
		t.Fatal(err)
	}
	task, ok := created["task"].(taskstate.Task)
	if !ok || task.ID == "" {
		t.Fatalf("unexpected create result: %#v", created)
	}

	for range 3 {
		result, advanceErr := rt.taskManage(map[string]any{"action": "advance", "task_id": task.ID})
		if advanceErr != nil {
			t.Fatal(advanceErr)
		}
		task = result["task"].(taskstate.Task)
	}
	if _, err := rt.taskManage(map[string]any{"action": "complete", "task_id": task.ID, "summary": "done"}); err == nil {
		t.Fatal("completion without condition evidence succeeded")
	}
	for _, condition := range task.Conditions {
		if _, err := rt.taskManage(map[string]any{
			"action": "add_evidence", "task_id": task.ID, "condition_id": condition.ID,
			"summary": "verified", "source": "test tool result",
		}); err != nil {
			t.Fatal(err)
		}
	}
	completed, err := rt.taskManage(map[string]any{
		"action": "complete", "task_id": task.ID, "summary": "all completion conditions verified",
	})
	if err != nil {
		t.Fatal(err)
	}
	if completed["task"].(taskstate.Task).Status != taskstate.StatusCompleted {
		t.Fatalf("unexpected completion result: %#v", completed)
	}

	cfg := config.Config{
		Workspace: root, ToolProfile: config.ProfileUnified, Mode: config.ModeSandboxed,
		PathPolicy: config.PathPolicyWorkspace, AgentDockDir: "AgentDock", EnableViewImage: true,
	}
	cfg.Normalize()
	restarted, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := restarted.taskManage(map[string]any{"action": "get", "task_id": task.ID})
	if err != nil {
		t.Fatal(err)
	}
	if loaded["task"].(taskstate.Task).Status != taskstate.StatusCompleted {
		t.Fatalf("task did not survive runtime restart: %#v", loaded)
	}
}

func TestTaskManageListIsCompact(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	if _, err := rt.taskManage(map[string]any{
		"action": "create", "title": "Repair service", "goal": "restore service",
		"completion_conditions": []string{"service responds"},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := rt.taskManage(map[string]any{"action": "list", "status": "active"})
	if err != nil {
		t.Fatal(err)
	}
	items, ok := result["tasks"].([]map[string]any)
	if !ok || len(items) != 1 {
		t.Fatalf("unexpected list result: %#v", result)
	}
	if _, exists := items[0]["events"]; exists {
		t.Fatalf("list unexpectedly returned full task events: %#v", items[0])
	}
}

func TestTaskManagePhaseCheckpointReturnsCompactSummary(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	created, err := rt.taskManage(map[string]any{
		"action": "create", "title": "Repair service", "goal": "restore service",
		"completion_conditions": []string{"service responds"},
	})
	if err != nil {
		t.Fatal(err)
	}
	task := created["task"].(taskstate.Task)
	result, err := rt.taskManage(map[string]any{
		"action": "phase_checkpoint", "task_id": task.ID,
		"condition_evidence": []map[string]any{{"condition_id": "cond_01", "summary": "service observed", "source": "test"}},
		"advance_phase":      true,
		"summary":            "check milestone complete",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := result["task"]; exists {
		t.Fatalf("phase_checkpoint unexpectedly returned full task: %#v", result)
	}
	summary, ok := result["task_summary"].(map[string]any)
	if !ok {
		t.Fatalf("missing compact task summary: %#v", result)
	}
	if summary["phase"] != taskstate.PhaseExecute || summary["verified_condition_count"] != 1 {
		t.Fatalf("unexpected compact summary: %#v", summary)
	}
}

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
	taskID, ok := created["task_id"].(string)
	if !ok || taskID == "" {
		t.Fatalf("unexpected create result: %#v", created)
	}
	if _, exists := created["task"]; exists {
		t.Fatalf("create should return compact summary instead of full task: %#v", created)
	}
	loadedTask, err := rt.taskManage(map[string]any{"action": "get", "task_id": taskID})
	if err != nil {
		t.Fatal(err)
	}
	task := loadedTask["task"].(taskstate.Task)

	for range 3 {
		result, advanceErr := rt.taskManage(map[string]any{"action": "advance", "task_id": task.ID})
		if advanceErr != nil {
			t.Fatal(advanceErr)
		}
		task = result["task"].(taskstate.Task)
	}
	if _, err := rt.taskManage(map[string]any{"action": "complete", "task_id": task.ID, "summary": ""}); err == nil {
		t.Fatal("completion without final verification summary succeeded")
	}
	completed, err := rt.taskManage(map[string]any{
		"action": "complete", "task_id": task.ID, "summary": "final verification passed",
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

func TestTaskManageCreateReturnsCompactSummary(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	result, err := rt.taskManage(map[string]any{
		"action": "create", "title": "Repair service", "goal": "restore service",
		"completion_conditions": []string{"service responds"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := result["task"]; exists {
		t.Fatalf("create unexpectedly returned full task: %#v", result)
	}
	if result["task_id"] == "" || result["next_required_action"] == "" {
		t.Fatalf("create missing compact guidance: %#v", result)
	}
	summary, ok := result["task_summary"].(map[string]any)
	if !ok {
		t.Fatalf("create missing compact summary: %#v", result)
	}
	refs, ok := summary["condition_refs"].([]map[string]any)
	if !ok || len(refs) != 1 || refs[0]["id"] != "cond_01" {
		t.Fatalf("create summary missing condition refs for checkpoint evidence: %#v", summary)
	}
	steps, ok := summary["current_phase_steps"].([]map[string]any)
	if !ok || len(steps) != 0 {
		t.Fatalf("non-templated create should expose an empty current_phase_steps list: %#v", summary)
	}
}

func TestTaskManageCreateWithTemplateDoesNotReturnSnapshot(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	draft, err := rt.tasks.SaveTemplateDraft(taskstate.Template{
		ID: "compact.template", Version: "1.0.0", Title: "Compact template", Status: taskstate.TemplateDraft,
		CompletionConditions: []string{"done"},
		Steps:                []taskstate.TemplateStep{{ID: "inspect", Title: "Inspect", Phase: taskstate.PhaseCheck, Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.tasks.ValidateTemplate(draft.ID, draft.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.tasks.PublishTemplate(draft.ID, draft.Version); err != nil {
		t.Fatal(err)
	}
	result, err := rt.taskManage(map[string]any{
		"action": "create", "title": "Template task", "goal": "run templated task",
		"template_id": "compact.template", "template_version": "1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := result["task"]; exists {
		t.Fatalf("templated create should not return full snapshot: %#v", result)
	}
	summary := result["task_summary"].(map[string]any)
	steps, ok := summary["current_phase_steps"].([]map[string]any)
	if !ok || len(steps) != 1 || steps[0]["id"] != "inspect" {
		t.Fatalf("templated create summary should expose current phase step ids: %#v", summary)
	}
	loaded, err := rt.taskManage(map[string]any{"action": "get", "task_id": result["task_id"]})
	if err != nil {
		t.Fatal(err)
	}
	if loaded["task"].(taskstate.Task).Template == nil {
		t.Fatalf("full snapshot should still be available through get: %#v", loaded)
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
	taskID := created["task_id"].(string)
	loadedTask, err := rt.taskManage(map[string]any{"action": "get", "task_id": taskID})
	if err != nil {
		t.Fatal(err)
	}
	task := loadedTask["task"].(taskstate.Task)
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
	refs, ok := summary["condition_refs"].([]map[string]any)
	if !ok || len(refs) != 1 || refs[0]["evidence_count"] != 1 {
		t.Fatalf("phase checkpoint summary missing condition evidence refs: %#v", summary)
	}
}

func TestTaskManageCompleteStepAllowsSummaryOnly(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	draft, err := rt.tasks.SaveTemplateDraft(taskstate.Template{
		ID: "summary.step", Version: "1.0.0", Title: "Summary step", Status: taskstate.TemplateDraft,
		CompletionConditions: []string{"done"},
		Steps:                []taskstate.TemplateStep{{ID: "inspect", Title: "Inspect", Phase: taskstate.PhaseCheck, Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.tasks.ValidateTemplate(draft.ID, draft.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.tasks.PublishTemplate(draft.ID, draft.Version); err != nil {
		t.Fatal(err)
	}

	created, err := rt.taskManage(map[string]any{
		"action": "create", "title": "Repair service", "goal": "restore service",
		"template_id": "summary.step", "template_version": "1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	taskID := created["task_id"].(string)
	result, err := rt.taskManage(map[string]any{
		"action": "complete_step", "task_id": taskID, "step_id": "inspect", "summary": "context inspected",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated := result["task"].(taskstate.Task)
	if updated.Steps[0].Status != "completed" || len(updated.Steps[0].Evidence) != 0 {
		t.Fatalf("summary-only complete_step should not require structured evidence: %#v", updated.Steps[0])
	}
}

func TestTaskManageRecordAttemptReturnsActionGuard(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	created, err := rt.taskManage(map[string]any{
		"action": "create", "title": "Repair service", "goal": "restore service",
		"completion_conditions": []string{"service responds"},
	})
	if err != nil {
		t.Fatal(err)
	}
	taskID := created["task_id"].(string)
	result, err := rt.taskManage(map[string]any{
		"action": "record_attempt", "task_id": taskID, "strategy": "restart",
		"outcome": "failure", "diagnosis": "restart failed", "evidence": "systemctl output A",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := result["task"]; exists {
		t.Fatalf("record_attempt should return compact guidance, got full task: %#v", result)
	}
	if result["warning"] == "" || result["next_required_action"] == "" {
		t.Fatalf("record_attempt missing guard guidance: %#v", result)
	}
}

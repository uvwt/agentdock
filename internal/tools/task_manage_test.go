package tools

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
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
	loadedTask, err := rt.taskManage(context.Background(), map[string]any{"action": "get", "task_id": taskID})
	if err != nil {
		t.Fatal(err)
	}
	task := loadedTask["task"].(taskstate.Task)

	if _, err := rt.taskManage(context.Background(), map[string]any{"action": "complete_after_review", "task_id": task.ID, "summary": ""}); err == nil {
		t.Fatal("complete_after_review succeeded before final_review")
	}
	reviewed, err := rt.taskManage(context.Background(), map[string]any{
		"action": "final_review", "task_id": task.ID, "summary": "final verification passed",
		"review_status": "pass", "verified_facts": []string{"health endpoint returns 200"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := reviewed["task"]; exists {
		t.Fatalf("final_review should return compact summary instead of full task: %#v", reviewed)
	}
	completed, err := rt.taskManage(context.Background(), map[string]any{
		"action": "complete_after_review", "task_id": task.ID, "summary": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := completed["task"]; exists {
		t.Fatalf("complete_after_review should return compact summary instead of full task: %#v", completed)
	}
	summary := completed["task_summary"].(map[string]any)
	if summary["status"] != taskstate.StatusCompleted || summary["review_status"] != taskstate.FinalReviewPass {
		t.Fatalf("unexpected completion summary: %#v", summary)
	}

	cfg := config.Config{
		AgentDockDefaultDir: root, AgentDockHome: filepath.Join(root, ".agentdock"),
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	restarted, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := restarted.taskManage(context.Background(), map[string]any{"action": "get", "task_id": task.ID})
	if err != nil {
		t.Fatal(err)
	}
	if loaded["task"].(taskstate.Task).Status != taskstate.StatusCompleted {
		t.Fatalf("task did not survive runtime restart: %#v", loaded)
	}
}

func TestTaskManageRejectsInvalidTemplateCandidates(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	_, err := rt.taskManage(context.Background(), map[string]any{
		"action":                "create",
		"title":                 "Invalid evidence",
		"goal":                  "reject malformed template selection evidence",
		"completion_conditions": []string{"invalid input is rejected"},
		"template_candidates": []any{
			map[string]any{"id": "candidate", "version": "1.0.0", "score": "not-an-integer"},
		},
	})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected ToolError, got %T: %v", err, err)
	}
	if toolErr.Code != "VALIDATION_ERROR" || toolErr.Details["field"] != "template_candidates" {
		t.Fatalf("unexpected error: %#v", toolErr)
	}
}

func TestTaskManageListIsCompact(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	if _, err := rt.taskManage(context.Background(), map[string]any{
		"action": "create", "title": "Repair service", "goal": "restore service",
		"completion_conditions": []string{"service responds"},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := rt.taskManage(context.Background(), map[string]any{"action": "list", "status": "active"})
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
	result, err := rt.taskManage(context.Background(), map[string]any{
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
		t.Fatalf("create summary missing final review checklist refs: %#v", summary)
	}
	if _, exists := refs[0]["evidence_count"]; exists {
		t.Fatalf("condition refs should not guide per-condition evidence: %#v", refs[0])
	}
	steps, ok := summary["current_phase_steps"].([]map[string]any)
	if !ok || len(steps) != 0 {
		t.Fatalf("non-templated create should expose an empty current_phase_steps list: %#v", summary)
	}
}

func TestTaskManageCreateWithTemplateDoesNotReturnSnapshot(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	createTestWorkflowTemplate(t, rt, taskstate.Template{
		ID: "compact.template", Version: "1.0.0", Title: "Compact template",
		CompletionConditions: []string{"done"},
		Steps:                []taskstate.TemplateStep{{ID: "inspect", Title: "Inspect", Phase: taskstate.PhaseCheck}},
	})
	result, err := rt.taskManage(context.Background(), map[string]any{
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
	loaded, err := rt.taskManage(context.Background(), map[string]any{"action": "get", "task_id": result["task_id"]})
	if err != nil {
		t.Fatal(err)
	}
	if loaded["task"].(taskstate.Task).Template == nil {
		t.Fatalf("full snapshot should still be available through get: %#v", loaded)
	}
}

func createTestWorkflowTemplate(t *testing.T, rt *Runtime, template taskstate.Template) {
	t.Helper()
	var templateMap map[string]any
	if err := remarshal(template, &templateMap); err != nil {
		t.Fatalf("template map: %v", err)
	}
	if _, err := rt.workflowTemplateManage(context.Background(), map[string]any{"action": "save", "template": templateMap}); err != nil {
		t.Fatalf("save template: %v", err)
	}
	if _, err := rt.workflowTemplateManage(context.Background(), map[string]any{"action": "validate", "template_id": template.ID, "template_version": template.Version}); err != nil {
		t.Fatalf("validate template: %v", err)
	}
	if _, err := rt.workflowTemplateManage(context.Background(), map[string]any{"action": "publish", "template_id": template.ID, "template_version": template.Version}); err != nil {
		t.Fatalf("publish template: %v", err)
	}
}

func TestTaskManageFinalReviewFlow(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	created, err := rt.taskManage(context.Background(), map[string]any{
		"action": "create", "title": "Repair service", "goal": "restore service",
		"completion_conditions": []string{"service responds"},
	})
	if err != nil {
		t.Fatal(err)
	}
	taskID := created["task_id"].(string)

	if _, err := rt.taskManage(context.Background(), map[string]any{"action": "complete_after_review", "task_id": taskID}); err == nil {
		t.Fatal("complete_after_review succeeded before final_review")
	}
	reviewed, err := rt.taskManage(context.Background(), map[string]any{
		"action": "final_review", "task_id": taskID, "summary": "all checks passed",
		"verified_facts": []string{"health endpoint returned 200"},
	})
	if err != nil {
		t.Fatal(err)
	}
	summary := reviewed["task_summary"].(map[string]any)
	if summary["phase"] != taskstate.PhaseCloseout || summary["review_status"] != taskstate.FinalReviewPass {
		t.Fatalf("unexpected final review summary: %#v", summary)
	}
	finalReview := summary["final_review"].(map[string]any)
	if finalReview["verified_fact_count"] != 1 {
		t.Fatalf("final review facts not summarized: %#v", finalReview)
	}

	completed, err := rt.taskManage(context.Background(), map[string]any{"action": "complete_after_review", "task_id": taskID})
	if err != nil {
		t.Fatal(err)
	}
	completedSummary := completed["task_summary"].(map[string]any)
	if completedSummary["status"] != taskstate.StatusCompleted || completedSummary["review_status"] != taskstate.FinalReviewPass {
		t.Fatalf("unexpected completed summary: %#v", completedSummary)
	}
}

func TestTaskManageStateMutationActionsReturnCompactSummary(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	created, err := rt.taskManage(context.Background(), map[string]any{
		"action": "create", "title": "Compact mutation", "goal": "avoid large task payloads",
		"completion_conditions": []string{"done"},
	})
	if err != nil {
		t.Fatal(err)
	}
	taskID := created["task_id"].(string)

	result, err := rt.taskManage(context.Background(), map[string]any{"action": "block", "task_id": taskID, "blocker": "waiting", "evidence": "test evidence"})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := result["task"]; exists {
		t.Fatalf("block should not return full task: %#v", result)
	}
	if result["task_id"] != taskID {
		t.Fatalf("compact mutation result should keep task id: %#v", result)
	}
	summary := result["task_summary"].(map[string]any)
	if summary["status"] != taskstate.StatusBlocked {
		t.Fatalf("unexpected block summary: %#v", summary)
	}

	result, err = rt.taskManage(context.Background(), map[string]any{"action": "resume", "task_id": taskID, "summary": "continue"})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := result["task"]; exists {
		t.Fatalf("resume should not return full task: %#v", result)
	}
	summary = result["task_summary"].(map[string]any)
	if summary["status"] != taskstate.StatusActive {
		t.Fatalf("unexpected resume summary: %#v", summary)
	}
}

func TestTaskManageTemplateListReturnsCompactSummaries(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	createTestWorkflowTemplate(t, rt, taskstate.Template{
		ID: "large.template", Version: "1.0.0", Title: "Large template", Description: strings.Repeat("description ", 80),
		Match:                taskstate.MatchRule{Keywords: []string{"deploy", "agentdock"}, Devices: []string{"DockMini"}, Type: "deployment"},
		CompletionConditions: []string{"done"},
		Steps: []taskstate.TemplateStep{
			{ID: "inspect", Title: "Inspect", Phase: taskstate.PhaseCheck},
		},
	})

	result, err := rt.workflowTemplateManage(context.Background(), map[string]any{"action": "list", "template_status": "active"})
	if err != nil {
		t.Fatal(err)
	}
	items := result["templates"].([]map[string]any)
	if len(items) != 1 {
		t.Fatalf("unexpected template list: %#v", result)
	}
	if _, exists := items[0]["steps"]; exists {
		t.Fatalf("template_list should not return full steps: %#v", items[0])
	}
	if items[0]["step_count"] != 1 || items[0]["condition_count"] != 1 || items[0]["keyword_count"] != 2 {
		t.Fatalf("compact template summary missing counts: %#v", items[0])
	}
	loaded, err := rt.workflowTemplateManage(context.Background(), map[string]any{"action": "get", "template_id": "large.template", "template_version": "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded["template"].(taskstate.Template).Steps) != 1 {
		t.Fatalf("template_get should still return full template: %#v", loaded)
	}
}

func TestTaskManageTemplateMutationActionsReturnCompactSummaries(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	templateID := "template-mutation"
	templateVersion := "1.0.0"
	templateInput := map[string]any{
		"id": templateID, "version": templateVersion, "title": "Template mutation", "description": strings.Repeat("description ", 80),
		"completion_conditions": []string{"done"},
		"steps":                 []map[string]any{{"id": "inspect", "title": "Inspect", "phase": taskstate.PhaseCheck}},
	}

	for _, action := range []string{"save", "validate", "publish", "retire"} {
		args := map[string]any{"action": action, "template_id": templateID, "template_version": templateVersion}
		if action == "save" {
			args = map[string]any{"action": action, "template": templateInput}
		}
		result, err := rt.workflowTemplateManage(context.Background(), args)
		if err != nil {
			t.Fatalf("%s failed: %v", action, err)
		}
		if _, exists := result["template"]; exists {
			t.Fatalf("%s should not return full template: %#v", action, result)
		}
		summary := result["template_summary"].(map[string]any)
		if summary["id"] != templateID || summary["step_count"] != 1 || summary["condition_count"] != 1 {
			t.Fatalf("%s compact summary missing key fields: %#v", action, summary)
		}
	}

	loaded, err := rt.workflowTemplateManage(context.Background(), map[string]any{"action": "get", "template_id": templateID, "template_version": templateVersion})
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded["template"].(taskstate.Template).Steps) != 1 {
		t.Fatalf("template_get should still return full template: %#v", loaded)
	}
}

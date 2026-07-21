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
		"completion_conditions": []string{"health endpoint returns 200"},
		"steps": []map[string]any{
			{"id": "deploy", "title": "Deploy"},
			{"id": "verify", "title": "Verify"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	taskID := created["task_id"].(string)
	if _, exists := created["task"]; exists {
		t.Fatalf("create should return compact state: %#v", created)
	}
	for _, checkpoint := range []map[string]any{
		{"action": "checkpoint", "task_id": taskID, "step_id": "deploy", "status": "in_progress", "summary": "deploying"},
		{"action": "checkpoint", "task_id": taskID, "step_id": "deploy", "status": "completed", "summary": "deployed"},
		{"action": "checkpoint", "task_id": taskID, "step_id": "verify", "status": "completed", "summary": "verified"},
	} {
		if _, err := rt.taskManage(context.Background(), checkpoint); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := rt.taskManage(context.Background(), map[string]any{
		"action": "final_review", "task_id": taskID, "status": "pass", "summary": "final verification passed",
		"verified": []string{"health endpoint returned 200"}, "risks": []string{},
	}); err != nil {
		t.Fatal(err)
	}
	completed, err := rt.taskManage(context.Background(), map[string]any{"action": "complete", "task_id": taskID})
	if err != nil {
		t.Fatal(err)
	}
	summary := completed["task_summary"].(map[string]any)
	if summary["status"] != taskstate.StatusCompleted || summary["completed_step_count"] != 2 {
		t.Fatalf("unexpected completion summary: %#v", summary)
	}

	cfg := config.Config{AgentDockDefaultDir: root, AgentDockHome: filepath.Join(root, ".agentdock")}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := restarted.taskManage(context.Background(), map[string]any{"action": "get", "task_id": taskID})
	if err != nil {
		t.Fatal(err)
	}
	if loaded["task"].(taskstate.Task).Status != taskstate.StatusCompleted {
		t.Fatalf("task did not survive restart: %#v", loaded)
	}
}

func TestTaskManageCheckpointExposesLiveProgress(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	created, err := rt.taskManage(context.Background(), map[string]any{
		"action": "create", "title": "Implement change", "goal": "implement and test",
		"completion_conditions": []string{"tests pass"},
		"steps":                 []map[string]any{{"id": "code", "title": "Write code"}, {"id": "test", "title": "Run tests"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	taskID := created["task_id"].(string)
	progress, err := rt.taskManage(context.Background(), map[string]any{
		"action": "checkpoint", "task_id": taskID, "step_id": "code", "status": "in_progress", "summary": "editing task state",
	})
	if err != nil {
		t.Fatal(err)
	}
	summary := progress["task_summary"].(map[string]any)
	current := summary["current_step"].(map[string]any)
	if current["id"] != "code" || current["status"] != taskstate.StepInProgress || summary["summary"] != "editing task state" {
		t.Fatalf("live progress missing: %#v", summary)
	}
	listed, err := rt.taskManage(context.Background(), map[string]any{"action": "list", "status": "active"})
	if err != nil {
		t.Fatal(err)
	}
	items := listed["tasks"].([]map[string]any)
	if len(items) != 1 || items[0]["current_step"].(map[string]any)["id"] != "code" {
		t.Fatalf("task list missing live progress: %#v", listed)
	}
}

func TestTaskManageBatchCheckpointAndModeValidation(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	created, err := rt.taskManage(context.Background(), map[string]any{
		"action": "create", "title": "Batch progress", "goal": "record progress atomically",
		"completion_conditions": []string{"done"},
		"steps": []map[string]any{
			{"id": "inspect", "title": "Inspect"},
			{"id": "test", "title": "Test"},
			{"id": "docs", "title": "Docs"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	taskID := created["task_id"].(string)
	progress, err := rt.taskManage(context.Background(), map[string]any{
		"action":             "checkpoint",
		"task_id":            taskID,
		"completed_step_ids": []string{"inspect", "test"},
		"current_step_id":    "docs",
		"summary":            "tests passed; writing docs",
	})
	if err != nil {
		t.Fatal(err)
	}
	summary := progress["task_summary"].(map[string]any)
	if summary["completed_step_count"] != 2 || summary["current_step"].(map[string]any)["id"] != "docs" {
		t.Fatalf("unexpected batch checkpoint summary: %#v", summary)
	}

	_, err = rt.taskManage(context.Background(), map[string]any{
		"action":             "checkpoint",
		"task_id":            taskID,
		"step_id":            "docs",
		"status":             "completed",
		"completed_step_ids": []string{"docs"},
		"summary":            "invalid mixed mode",
	})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "VALIDATION_ERROR" {
		t.Fatalf("expected mixed checkpoint validation error, got %T: %v", err, err)
	}

	_, err = rt.taskManage(context.Background(), map[string]any{
		"action":             "checkpoint",
		"task_id":            taskID,
		"completed_step_ids": []string{},
		"summary":            "empty batch",
	})
	if err == nil || !strings.Contains(err.Error(), "completed_step_ids or current_step_id") {
		t.Fatalf("empty batch should use batch validation: %v", err)
	}
}

func TestTaskManageSingleTemplateResolvesActiveVersion(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	createTestWorkflowTemplate(t, rt, taskstate.Template{
		ID: "compact.template", Version: "1.0.0", Title: "Compact template",
		CompletionConditions: []string{"done"},
		Steps:                []taskstate.TemplateStep{{ID: "inspect", Title: "Inspect", Phase: taskstate.PhaseCheck}},
	})
	result, err := rt.taskManage(context.Background(), map[string]any{
		"action": "create", "title": "Template task", "goal": "run templated task", "template_id": "compact.template",
	})
	if err != nil {
		t.Fatal(err)
	}
	summary := result["task_summary"].(map[string]any)
	if summary["step_count"] != 1 || summary["current_step"].(map[string]any)["id"] != "inspect" {
		t.Fatalf("single template was not applied: %#v", summary)
	}
	loaded, err := rt.taskManage(context.Background(), map[string]any{"action": "get", "task_id": result["task_id"]})
	if err != nil {
		t.Fatal(err)
	}
	task := loaded["task"].(taskstate.Task)
	if len(task.SourceTemplates) != 1 || task.SourceTemplates[0].Version != "1.0.0" {
		t.Fatalf("active template source was not recorded: %#v", task)
	}
}

func TestWorkflowTemplateGetManyRequiresModelComposition(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	for _, template := range []taskstate.Template{
		{ID: "development", Version: "1.0.0", Title: "Development", CompletionConditions: []string{"tests pass"}, Steps: []taskstate.TemplateStep{{ID: "code", Title: "Write code", Phase: taskstate.PhaseExecute}}},
		{ID: "deployment", Version: "2.0.0", Title: "Deployment", CompletionConditions: []string{"production healthy"}, Steps: []taskstate.TemplateStep{{ID: "deploy", Title: "Deploy", Phase: taskstate.PhaseVerify}}},
	} {
		createTestWorkflowTemplate(t, rt, template)
	}
	result, err := rt.workflowTemplateManage(context.Background(), map[string]any{
		"action": "get_many", "template_ids": []string{"development", "deployment"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["composition_required"] != true || result["next_required_action"] == "" {
		t.Fatalf("get_many did not instruct model composition: %#v", result)
	}
	templates := result["templates"].([]taskstate.Template)
	if len(templates) != 2 || len(templates[0].Steps) == 0 || len(templates[1].CompletionConditions) == 0 {
		t.Fatalf("get_many did not return full templates: %#v", result)
	}
}

func TestTaskManageComposedTemplatesRequireExplicitResult(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	for _, template := range []taskstate.Template{
		{ID: "development", Version: "1.0.0", Title: "Development", CompletionConditions: []string{"tests pass"}, Steps: []taskstate.TemplateStep{{ID: "code", Title: "Write code", Phase: taskstate.PhaseExecute}}},
		{ID: "deployment", Version: "2.0.0", Title: "Deployment", CompletionConditions: []string{"production healthy"}, Steps: []taskstate.TemplateStep{{ID: "deploy", Title: "Deploy", Phase: taskstate.PhaseVerify}}},
	} {
		createTestWorkflowTemplate(t, rt, template)
	}
	_, err := rt.taskManage(context.Background(), map[string]any{
		"action": "create", "title": "Develop and deploy", "goal": "combine workflows",
		"source_template_ids": []string{"development", "deployment"},
	})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "TEMPLATE_COMPOSITION_REQUIRED" {
		t.Fatalf("expected composition guard, got %T: %v", err, err)
	}

	created, err := rt.taskManage(context.Background(), map[string]any{
		"action": "create", "title": "Develop and deploy", "goal": "combine workflows",
		"source_template_ids":   []string{"development", "deployment"},
		"steps":                 []map[string]any{{"id": "code", "title": "Write and test code"}, {"id": "deploy", "title": "Deploy and verify"}},
		"completion_conditions": []string{"tests pass", "production healthy"},
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := rt.taskManage(context.Background(), map[string]any{"action": "get", "task_id": created["task_id"]})
	if err != nil {
		t.Fatal(err)
	}
	task := loaded["task"].(taskstate.Task)
	if len(task.SourceTemplates) != 2 || len(task.Steps) != 2 || len(task.Conditions) != 2 {
		t.Fatalf("composed task was not recorded: %#v", task)
	}
}

func TestTaskManageFinalReviewDoesNotAutoCompleteSteps(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	created, err := rt.taskManage(context.Background(), map[string]any{
		"action": "create", "title": "Review", "goal": "verify progress",
		"completion_conditions": []string{"done"}, "steps": []map[string]any{{"id": "work", "title": "Do work"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = rt.taskManage(context.Background(), map[string]any{
		"action": "final_review", "task_id": created["task_id"], "status": "pass", "summary": "claimed done", "verified": []string{"claim"},
	})
	if err == nil || !strings.Contains(err.Error(), "all task steps completed") {
		t.Fatalf("final_review should reject incomplete steps: %v", err)
	}
}

func TestTaskManageBlockResumeAndLegacyActionRemoval(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	created, err := rt.taskManage(context.Background(), map[string]any{
		"action": "create", "title": "Blocked task", "goal": "resume safely", "completion_conditions": []string{"done"},
	})
	if err != nil {
		t.Fatal(err)
	}
	taskID := created["task_id"].(string)
	blocked, err := rt.taskManage(context.Background(), map[string]any{"action": "block", "task_id": taskID, "summary": "SSH timed out three times"})
	if err != nil {
		t.Fatal(err)
	}
	if blocked["task_summary"].(map[string]any)["status"] != taskstate.StatusBlocked {
		t.Fatalf("unexpected block state: %#v", blocked)
	}
	if _, err := rt.taskManage(context.Background(), map[string]any{"action": "resume", "task_id": taskID, "summary": "network restored"}); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.taskManage(context.Background(), map[string]any{"action": "complete_after_review", "task_id": taskID}); err == nil {
		t.Fatal("legacy complete_after_review action is still accepted")
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

func TestWorkflowTemplateListAndMutationRemainCompact(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	template := taskstate.Template{
		ID: "large.template", Version: "1.0.0", Title: "Large template", Description: strings.Repeat("description ", 80),
		Match:                taskstate.MatchRule{Keywords: []string{"deploy", "agentdock"}, Devices: []string{"DockMini"}, Type: "deployment"},
		CompletionConditions: []string{"done"},
		Steps:                []taskstate.TemplateStep{{ID: "inspect", Title: "Inspect", Phase: taskstate.PhaseCheck}},
	}
	createTestWorkflowTemplate(t, rt, template)
	result, err := rt.workflowTemplateManage(context.Background(), map[string]any{"action": "list", "template_status": "active"})
	if err != nil {
		t.Fatal(err)
	}
	items := result["templates"].([]map[string]any)
	if len(items) != 1 || items[0]["step_count"] != 1 {
		t.Fatalf("unexpected compact list: %#v", result)
	}
	if _, exists := items[0]["steps"]; exists {
		t.Fatalf("list returned full template: %#v", items[0])
	}
	loaded, err := rt.workflowTemplateManage(context.Background(), map[string]any{"action": "get", "template_id": template.ID, "template_version": template.Version})
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded["template"].(taskstate.Template).Steps) != 1 {
		t.Fatalf("exact get should return full template: %#v", loaded)
	}
}

func TestWorkflowTemplateGetWithoutVersionResolvesActiveVersion(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	template := taskstate.Template{
		ID: "active.template", Version: "1.2.3", Title: "Active template",
		Match:                taskstate.MatchRule{Keywords: []string{"active"}},
		CompletionConditions: []string{"done"},
		Steps:                []taskstate.TemplateStep{{ID: "inspect", Title: "Inspect", Phase: taskstate.PhaseCheck}},
	}
	createTestWorkflowTemplate(t, rt, template)

	loaded, err := rt.workflowTemplateManage(context.Background(), map[string]any{
		"action": "get", "template_id": template.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := loaded["template"].(taskstate.Template)
	if got.ID != template.ID || got.Version != template.Version || got.Status != taskstate.TemplateActive {
		t.Fatalf("get without version returned %#v", got)
	}
}

func TestWorkflowTemplateMutationRequiresExactVersion(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	_, err := rt.workflowTemplateManage(context.Background(), map[string]any{
		"action": "validate", "template_id": "active.template",
	})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "VALIDATION_ERROR" {
		t.Fatalf("validate without version error = %v", err)
	}
	if toolErr.Message != "template_id and template_version are required" {
		t.Fatalf("unexpected validation message: %q", toolErr.Message)
	}
}

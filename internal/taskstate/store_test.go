package taskstate

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestTaskLifecyclePersistsAndRequiresFinalReview(t *testing.T) {
	root := t.TempDir()
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Create("Deploy AgentDock", "deploy and verify", []string{"health is 200", "tool call succeeds"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Phase != PhaseCheck || task.Status != StatusActive {
		t.Fatalf("unexpected initial state: %#v", task)
	}
	info, err := os.Stat(filepath.Join(root, task.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("task file mode = %o", info.Mode().Perm())
	}
	if _, err := store.CompleteAfterReview(task.ID, ""); err == nil {
		t.Fatal("complete_after_review succeeded before final_review")
	}
	task, err = store.FinalReview(task.ID, FinalReviewInput{Status: FinalReviewPass, Summary: "all checks passed", VerifiedFacts: []string{"health endpoint returned 200"}})
	if err != nil {
		t.Fatal(err)
	}
	task, err = store.CompleteAfterReview(task.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusCompleted || task.CompletedAt == nil {
		t.Fatalf("unexpected completed state: %#v", task)
	}
	reopened, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reopened.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Summary != "all checks passed" {
		t.Fatalf("summary = %q", loaded.Summary)
	}
}

func TestFinalReviewRequiredBeforeCompleteAfterReview(t *testing.T) {
	store, err := New(t.TempDir() + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateFromTemplate("Deploy", "deploy AgentDock", nil, testTemplate(), "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteAfterReview(task.ID, ""); err == nil {
		t.Fatal("complete_after_review succeeded before final_review")
	}
	if _, err := store.FinalReview(task.ID, FinalReviewInput{Status: FinalReviewPass, Summary: "checked", MissingChecks: []string{"go test"}, VerifiedFacts: []string{"build ok"}}); err == nil {
		t.Fatal("passing final review accepted missing checks")
	}
	if _, err := store.FinalReview(task.ID, FinalReviewInput{Status: FinalReviewPass, Summary: "checked"}); err == nil {
		t.Fatal("passing final review accepted no verified facts")
	}
	if _, err := store.FinalReview(task.ID, FinalReviewInput{Status: FinalReviewFailed, Summary: "checked"}); err == nil {
		t.Fatal("failed final review accepted no risks or missing checks")
	}

	task, err = store.FinalReview(task.ID, FinalReviewInput{
		Status:        FinalReviewPass,
		Summary:       "all checks passed",
		VerifiedFacts: []string{"health endpoint returned 200"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.Phase != PhaseCloseout || task.FinalReview == nil || task.FinalReview.Status != FinalReviewPass {
		t.Fatalf("unexpected reviewed state: %#v", task)
	}
	for _, step := range task.Steps {
		if step.Status != "completed" {
			t.Fatalf("pending step was not covered by final_review: %#v", step)
		}
	}

	task, err = store.CompleteAfterReview(task.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusCompleted || task.Summary != "all checks passed" {
		t.Fatalf("unexpected completed state: %#v", task)
	}
}

func TestBlockAndResume(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Create("Repair", "repair service", []string{"service works"})
	if err != nil {
		t.Fatal(err)
	}
	task, err = store.Block(task.ID, "missing prerequisite", "authorization failed")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusBlocked {
		t.Fatalf("status = %s", task.Status)
	}
	if _, err := store.FinalReview(task.ID, FinalReviewInput{Status: FinalReviewPass, Summary: "checked", VerifiedFacts: []string{"blocked task cannot finish"}}); err == nil {
		t.Fatal("blocked task accepted final_review")
	}
	task, err = store.Resume(task.ID, "prerequisite restored")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusActive || task.Blocker != "" {
		t.Fatalf("unexpected resumed state: %#v", task)
	}
}

func TestCreateFromTemplateCopiesSelectionCandidates(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	candidates := []TemplateCandidate{{ID: "agentdock.deploy", Version: "1.0.0", Score: 90}}
	task, err := store.CreateFromTemplate("Deploy", "deploy AgentDock", nil, testTemplate(), "best match", candidates)
	if err != nil {
		t.Fatal(err)
	}
	candidates[0].ID = "mutated"
	if task.Template == nil || len(task.Template.Candidates) != 1 || task.Template.Candidates[0].ID != "agentdock.deploy" {
		t.Fatalf("task selection aliases caller candidates: %#v", task.Template)
	}
}

func TestCreateWithTemplateSkipsSimilarCompletionConditions(t *testing.T) {
	store, err := New(t.TempDir() + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	template := testTemplate()
	template.ID = "development.project-timeboxed-optimization"
	template.Version = "1.0.0"
	template.CompletionConditions = []string{
		"已读取相关记忆、项目约束、用户开发风格、真实仓库状态和可用验证方式",
		"每轮结束都检查 elapsed_minutes；未到目标时长不能仅因产物完成、验证通过、提交推送或部署通过停止",
	}
	task, err := store.CreateFromTemplate("AgentDock 一小时完善", "完善 AgentDock", []string{
		"已读取 AgentDock 相关记忆、项目约束、用户偏好、真实仓库状态和可用验证方式",
		"每轮结束都检查 elapsed_minutes；未到 60 分钟不能仅因产物完成、验证通过、提交推送或部署通过停止",
		"已确认最终提交已经推送到 origin/main",
	}, template, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(task.Conditions) != 3 {
		t.Fatalf("expected two template conditions plus one unique condition, got %d: %#v", len(task.Conditions), task.Conditions)
	}
	if task.Conditions[2].Text != "已确认最终提交已经推送到 origin/main" {
		t.Fatalf("unique condition was not retained: %#v", task.Conditions)
	}
}

func testTemplate() Template {
	return Template{
		ID:      "agentdock.deploy",
		Version: "1.0.0",
		Title:   "Deploy AgentDock",
		Status:  TemplateActive,
		Hash:    "sha256:test",
		Match: MatchRule{
			Keywords: []string{"deploy", "AgentDock"},
			Devices:  []string{"DockMini"},
			Type:     "deployment",
		},
		CompletionConditions: []string{"deployment succeeds", "health check passes"},
		Steps: []TemplateStep{
			{ID: "check", Title: "Check", Phase: PhaseCheck},
			{ID: "deploy", Title: "Deploy", Phase: PhaseExecute},
		},
	}
}

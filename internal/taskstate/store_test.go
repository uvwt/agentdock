package taskstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskLifecyclePersistsAndRequiresFinalVerification(t *testing.T) {
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
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("task file mode = %o", info.Mode().Perm())
	}

	for range 3 {
		task, err = store.Advance(task.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if task.Phase != PhaseCloseout {
		t.Fatalf("phase = %s", task.Phase)
	}
	if _, err := store.Complete(task.ID, ""); err == nil {
		t.Fatal("completion without final verification summary succeeded")
	}
	task, err = store.Complete(task.ID, "all checks passed")
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
	draft, err := store.SaveTemplateDraft(testTemplate())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ValidateTemplate(draft.ID, draft.Version); err != nil {
		t.Fatal(err)
	}
	published, err := store.PublishTemplate(draft.ID, draft.Version)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateWithTemplate("Deploy", "deploy AgentDock", nil, published.ID, published.Version, "test", nil)
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
		if len(step.Evidence) != 0 {
			t.Fatalf("final_review should not create step evidence: %#v", step)
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

func TestAttemptLimitAndFailureEvidence(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Create("Repair", "repair service", []string{"service works"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordAttempt(task.ID, "restart", "failure", "", ""); err == nil {
		t.Fatal("failure without diagnosis and evidence succeeded")
	}
	if _, err := store.RecordAttempt(task.ID, "restart", "failure", "first diagnosis", "first log evidence"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordAttempt(task.ID, "restart", "failure", "second diagnosis", "first log evidence"); err == nil {
		t.Fatal("repeated failure evidence succeeded")
	}
	if _, err := store.RecordAttempt(task.ID, "restart", "failure", "second diagnosis", "second log evidence"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordAttempt(task.ID, "restart", "success", "", ""); err == nil {
		t.Fatal("third attempt with same strategy succeeded")
	}
}

func TestRecordAttemptStopsConsecutiveLoggingLoop(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Create("Repair", "repair service", []string{"service works"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordAttempt(task.ID, "inspect logs", "failure", "log did not identify cause", "log sample A"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordAttempt(task.ID, "restart service", "failure", "restart did not help", "restart output B"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordAttempt(task.ID, "rewrite config", "failure", "config unchanged", "config output C"); err == nil || !strings.Contains(err.Error(), "Stop recording attempts") {
		t.Fatalf("third consecutive attempt should stop logging loop, err=%v", err)
	}

	// 真实证据事件会打断连续 attempt 链；之后才允许继续记录新的尝试。
	if _, err := store.AddEvidence(task.ID, "cond_01", "real command evidence recorded", "exec_command curl /healthz"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordAttempt(task.ID, "rewrite config", "failure", "config still invalid", "config output D"); err != nil {
		t.Fatal(err)
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
	if _, err := store.Advance(task.ID); err == nil {
		t.Fatal("blocked task advanced")
	}
	task, err = store.Resume(task.ID, "prerequisite restored")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusActive || task.Blocker != "" {
		t.Fatalf("unexpected resumed state: %#v", task)
	}
}

func TestStepCompletionAllowsSummaryOnly(t *testing.T) {
	store, err := New(t.TempDir() + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	draft, err := store.SaveTemplateDraft(testTemplate())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ValidateTemplate(draft.ID, draft.Version); err != nil {
		t.Fatal(err)
	}
	published, err := store.PublishTemplate(draft.ID, draft.Version)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateWithTemplate("Deploy", "deploy AgentDock", nil, published.ID, published.Version, "test", nil)
	if err != nil {
		t.Fatal(err)
	}

	task, err = store.CompleteStep(task.ID, "inspect", StepEvidence{Summary: "repository inspected"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Steps[0].Status != "completed" || len(task.Steps[0].Evidence) != 0 {
		t.Fatalf("summary-only step should complete without structured evidence: %#v", task.Steps[0])
	}
}

func TestStepCompletionRejectsIncompleteEvidence(t *testing.T) {
	store, err := New(t.TempDir() + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	draft, err := store.SaveTemplateDraft(testTemplate())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ValidateTemplate(draft.ID, draft.Version); err != nil {
		t.Fatal(err)
	}
	published, err := store.PublishTemplate(draft.ID, draft.Version)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateWithTemplate("Deploy", "deploy AgentDock", nil, published.ID, published.Version, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteStep(task.ID, "inspect", StepEvidence{
		Type: "tool", Source: "cloudflare skill", Result: "HTTP 200", Summary: "Cloudflare 已检查，但 VPS/Caddy 仍待检查",
	}); err == nil || !strings.Contains(err.Error(), "incomplete work") {
		t.Fatalf("incomplete evidence should be rejected, err=%v", err)
	}
	loaded, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Steps[0].Status != "pending" {
		t.Fatalf("rejected evidence completed step: %#v", loaded.Steps[0])
	}
}

func TestPhaseCheckpointBatchesPhaseUpdatesAtomically(t *testing.T) {
	store, err := New(t.TempDir() + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	draft, err := store.SaveTemplateDraft(testTemplate())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ValidateTemplate(draft.ID, draft.Version); err != nil {
		t.Fatal(err)
	}
	published, err := store.PublishTemplate(draft.ID, draft.Version)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateWithTemplate("Deploy", "deploy AgentDock", nil, published.ID, published.Version, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	initialEvents := len(task.Events)
	task, err = store.PhaseCheckpoint(task.ID, PhaseCheckpointInput{
		StepCompletions: []StepCompletionUpdate{{
			StepID:   "inspect",
			Evidence: StepEvidence{Type: "command", Source: "git status", Result: "exit_code=0", Summary: "repository inspected"},
		}},
		ConditionEvidence: []ConditionEvidenceUpdate{{ConditionID: "cond_01", Summary: "tests observed", Source: "test"}},
		AdvancePhase:      true,
		Summary:           "inspection milestone complete",
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.Phase != PhaseExecute || task.Steps[0].Status != "completed" || len(task.Conditions[0].Evidence) != 1 {
		t.Fatalf("unexpected checkpoint state: %#v", task)
	}
	if len(task.Events) != initialEvents+1 || task.Events[len(task.Events)-1].Type != "phase_checkpoint" {
		t.Fatalf("checkpoint should append one aggregate event: %#v", task.Events)
	}

	task, err = store.PhaseCheckpoint(task.ID, PhaseCheckpointInput{
		StepCompletions: []StepCompletionUpdate{{
			StepID:   "install",
			Evidence: StepEvidence{Type: "command", Source: "other installer", Result: "exit_code=0", Summary: "installed"},
		}},
		ConditionEvidence: []ConditionEvidenceUpdate{{ConditionID: "cond_02", Summary: "install verified", Source: "test"}},
		AdvancePhase:      true,
		Summary:           "installation milestone complete",
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err = store.PhaseCheckpoint(task.ID, PhaseCheckpointInput{
		StepCompletions: []StepCompletionUpdate{{
			StepID:   "health",
			Evidence: StepEvidence{Type: "http", Source: "/healthz", Result: "HTTP 200", Summary: "service healthy"},
		}},
		ConditionEvidence: []ConditionEvidenceUpdate{{ConditionID: "cond_02", Summary: "health returned 200", Source: "/healthz"}},
		AdvancePhase:      true,
		Summary:           "verification milestone complete",
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err = store.PhaseCheckpoint(task.ID, PhaseCheckpointInput{
		StepCompletions: []StepCompletionUpdate{{StepID: "record", Summary: "deployment recorded"}},
		CompleteTask:    true,
		Summary:         "all milestones completed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusCompleted || task.CompletedAt == nil || task.Summary != "all milestones completed" {
		t.Fatalf("unexpected completed checkpoint state: %#v", task)
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
	draft, err := store.SaveTemplateDraft(template)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ValidateTemplate(draft.ID, draft.Version); err != nil {
		t.Fatal(err)
	}
	published, err := store.PublishTemplate(draft.ID, draft.Version)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateWithTemplate("AgentDock 一小时完善", "完善 AgentDock", []string{
		"已读取 AgentDock 相关记忆、项目约束、用户偏好、真实仓库状态和可用验证方式",
		"每轮结束都检查 elapsed_minutes；未到 60 分钟不能仅因产物完成、验证通过、提交推送或部署通过停止",
		"已确认最终提交已经推送到 origin/main",
	}, published.ID, published.Version, "test", nil)
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

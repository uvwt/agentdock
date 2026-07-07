package taskstate

import (
	"context"
	"strings"
	"testing"
)

func testTemplate() Template {
	return Template{
		ID: "agentdock.deploy.macos", Version: "1.0.0", Title: "Deploy AgentDock on macOS",
		Match:                MatchRule{Keywords: []string{"AgentDock", "deploy"}, Devices: []string{"DockMini"}, Type: "deployment"},
		CompletionConditions: []string{"tests pass", "health is 200"},
		Steps: []TemplateStep{
			{ID: "inspect", Title: "Inspect repository", Phase: PhaseCheck},
			{ID: "install", Title: "Install signed binary", Phase: PhaseExecute},
			{ID: "health", Title: "Verify health", Phase: PhaseVerify},
			{ID: "logs", Title: "Inspect optional logs", Phase: PhaseVerify},
			{ID: "record", Title: "Record deployment", Phase: PhaseCloseout},
		},
	}
}

func TestTemplateLifecycleMatchAndTaskSnapshot(t *testing.T) {
	store, err := New(t.TempDir() + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	draft, err := store.SaveTemplateDraft(testTemplate())
	if err != nil {
		t.Fatal(err)
	}
	if draft.Status != TemplateDraft {
		t.Fatalf("status=%s", draft.Status)
	}
	validated, err := store.ValidateTemplate(draft.ID, draft.Version)
	if err != nil {
		t.Fatal(err)
	}
	if validated.Status != TemplateValidated {
		t.Fatalf("status=%s", validated.Status)
	}
	published, err := store.PublishTemplate(draft.ID, draft.Version)
	if err != nil {
		t.Fatal(err)
	}
	if published.Status != TemplateActive || published.Hash == "" {
		t.Fatalf("published=%#v", published)
	}
	if _, err := store.PublishTemplate(draft.ID, draft.Version); err == nil {
		t.Fatal("published version was overwritten")
	}
	candidates, err := store.MatchTemplates("deploy AgentDock", "DockMini", "deployment")
	if err != nil || len(candidates) != 1 || candidates[0].Score <= 0 {
		t.Fatalf("candidates=%#v err=%v", candidates, err)
	}
	task, err := store.CreateWithTemplate("Deploy", "deploy AgentDock", nil, published.ID, published.Version, "matched DockMini deployment", candidates)
	if err != nil {
		t.Fatal(err)
	}
	if task.Template == nil || task.Template.Hash != published.Hash || len(task.Steps) != len(published.Steps) {
		t.Fatalf("task snapshot=%#v", task)
	}
	if _, err := store.Advance(task.ID); err != nil {
		t.Fatal(err)
	}
	task, err = store.CompleteStep(task.ID, "inspect", StepEvidence{Type: "command", Source: "git status", Result: "exit_code=0", Summary: "repository inspected"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Steps[0].Status != "completed" {
		t.Fatalf("step=%#v", task.Steps[0])
	}
	if _, err := store.Advance(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteStep(task.ID, "install", StepEvidence{Type: "command", Source: "other installer", Result: "exit_code=0", Summary: "installed"}); err != nil {
		t.Fatal(err)
	}
}

func TestTemplateMatchTreatsProjectNameKeywordAsWeakContext(t *testing.T) {
	store, err := New(t.TempDir() + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	for _, tpl := range []Template{
		{
			ID: "agentdock.deploy.macos", Version: "1.0.0", Title: "Deploy AgentDock",
			Match:                MatchRule{Keywords: []string{"AgentDock", "部署"}, Devices: []string{"DockMini"}},
			CompletionConditions: []string{"done"},
			Steps:                []TemplateStep{{ID: "check", Title: "Check", Phase: PhaseCheck}},
		},
		{
			ID: "development.project-timeboxed-optimization", Version: "1.0.0", Title: "Timeboxed work",
			Match:                MatchRule{Keywords: []string{"一小时", "完善项目"}, Devices: []string{"DockMini"}},
			CompletionConditions: []string{"done"},
			Steps:                []TemplateStep{{ID: "check", Title: "Check", Phase: PhaseCheck}},
		},
	} {
		draft, err := store.SaveTemplateDraft(tpl)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.ValidateTemplate(draft.ID, draft.Version); err != nil {
			t.Fatal(err)
		}
		if _, err := store.PublishTemplate(draft.ID, draft.Version); err != nil {
			t.Fatal(err)
		}
	}
	candidates, err := store.MatchTemplates("针对 AgentDock 进行一个小时功能完善", "DockMini", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ID != "development.project-timeboxed-optimization" {
		t.Fatalf("weak project-name keyword should not make deploy template a semantic candidate: %#v", candidates)
	}
	candidates, err = store.MatchTemplates("AgentDock 部署", "DockMini", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) == 0 || candidates[0].ID != "agentdock.deploy.macos" {
		t.Fatalf("strong deploy keyword should still match deploy template: %#v", candidates)
	}
}

func TestTemplateMatchReturnsLatestActiveVersionPerTemplateID(t *testing.T) {
	store, err := New(t.TempDir() + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1.9.0", "1.10.0"} {
		tpl := Template{
			ID: "development.example", Version: version, Title: "Example " + version,
			Match:                MatchRule{Keywords: []string{"开发"}, Devices: []string{"DockMini"}},
			CompletionConditions: []string{"done"},
			Steps:                []TemplateStep{{ID: "check", Title: "Check", Phase: PhaseCheck}},
		}
		draft, err := store.SaveTemplateDraft(tpl)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.ValidateTemplate(draft.ID, draft.Version); err != nil {
			t.Fatal(err)
		}
		if _, err := store.PublishTemplate(draft.ID, draft.Version); err != nil {
			t.Fatal(err)
		}
	}
	candidates, err := store.MatchTemplates("开发 AgentDock 功能", "DockMini", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ID != "development.example" || candidates[0].Version != "1.10.0" {
		t.Fatalf("expected only latest active version, got %#v", candidates)
	}
}

func TestTemplateMatchNormalizesTimeboxKeywords(t *testing.T) {
	store, err := New(t.TempDir() + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	tpl := Template{
		ID: "development.project-timeboxed-optimization", Version: "1.0.0", Title: "Timeboxed work",
		Match:                MatchRule{Keywords: []string{"一小时", "半小时"}, Devices: []string{"DockMini"}},
		CompletionConditions: []string{"done"},
		Steps:                []TemplateStep{{ID: "check", Title: "Check", Phase: PhaseCheck}},
	}
	draft, err := store.SaveTemplateDraft(tpl)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ValidateTemplate(draft.ID, draft.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishTemplate(draft.ID, draft.Version); err != nil {
		t.Fatal(err)
	}
	candidates, err := store.MatchTemplates("针对 agentdock 进行一个小时的功能完善", "DockMini", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ID != tpl.ID {
		t.Fatalf("expected timeboxed template to match 一个小时, got %#v", candidates)
	}
}

func TestTemplateMatchSemanticSignalsBeatDeviceOnly(t *testing.T) {
	store, err := New(t.TempDir() + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	deviceOnly := Template{
		ID: "unrelated.device", Version: "1.0.0", Title: "Device only",
		Match:                MatchRule{Devices: []string{"DockMini"}},
		CompletionConditions: []string{"done"},
		Steps:                []TemplateStep{{ID: "check", Title: "Check", Phase: PhaseCheck}},
	}
	semantic := Template{
		ID: "notes.question-record", Version: "1.0.0", Title: "Question note",
		Match:                MatchRule{Keywords: []string{"日常问题"}, Devices: []string{"DockMini"}, Type: "daily-question-note"},
		CompletionConditions: []string{"done"},
		Steps:                []TemplateStep{{ID: "check", Title: "Check", Phase: PhaseCheck}},
	}
	for _, tpl := range []Template{deviceOnly, semantic} {
		draft, err := store.SaveTemplateDraft(tpl)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.ValidateTemplate(draft.ID, draft.Version); err != nil {
			t.Fatal(err)
		}
		if _, err := store.PublishTemplate(draft.ID, draft.Version); err != nil {
			t.Fatal(err)
		}
	}
	candidates, err := store.MatchTemplates("记录日常问题笔记", "DockMini", "daily-question-note")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ID != "notes.question-record" {
		t.Fatalf("semantic match should suppress device-only fallback candidates, got %#v", candidates)
	}
}

func TestTemplateMatchFallsBackToDeviceOnlyWhenNoSemanticMatch(t *testing.T) {
	store, err := New(t.TempDir() + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	tpl := Template{
		ID: "fallback.device", Version: "1.0.0", Title: "Device fallback",
		Match:                MatchRule{Devices: []string{"DockMini"}},
		CompletionConditions: []string{"done"},
		Steps:                []TemplateStep{{ID: "check", Title: "Check", Phase: PhaseCheck}},
	}
	draft, err := store.SaveTemplateDraft(tpl)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ValidateTemplate(draft.ID, draft.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishTemplate(draft.ID, draft.Version); err != nil {
		t.Fatal(err)
	}
	candidates, err := store.MatchTemplates("完全没有语义关键词", "DockMini", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ID != tpl.ID {
		t.Fatalf("expected device fallback when there is no semantic match, got %#v", candidates)
	}
}

func TestTemplateMatchRanksDeviceHintWithoutHardFiltering(t *testing.T) {
	store, err := New(t.TempDir() + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	for _, tpl := range []Template{
		{ID: "agentdock.deploy.vps", Version: "1.0.0", Title: "VPS deploy", Match: MatchRule{Keywords: []string{"AgentDock", "部署"}, Devices: []string{"DockVPS"}, Type: "deployment"}, CompletionConditions: []string{"done"}, Steps: []TemplateStep{{ID: "check", Title: "Check", Phase: PhaseCheck}}},
		{ID: "agentdock.deploy.macos", Version: "1.0.0", Title: "macOS deploy", Match: MatchRule{Keywords: []string{"AgentDock", "部署"}, Devices: []string{"DockMini"}, Type: "deployment"}, CompletionConditions: []string{"done"}, Steps: []TemplateStep{{ID: "check", Title: "Check", Phase: PhaseCheck}}},
	} {
		draft, err := store.SaveTemplateDraft(tpl)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.ValidateTemplate(draft.ID, draft.Version); err != nil {
			t.Fatal(err)
		}
		if _, err := store.PublishTemplate(draft.ID, draft.Version); err != nil {
			t.Fatal(err)
		}
	}
	candidates, err := store.MatchTemplates("部署 AgentDock 到 Mac mini", "DockMini", "deployment")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) < 2 || candidates[0].ID != "agentdock.deploy.macos" {
		t.Fatalf("expected matching device hint to rank first without filtering other semantic candidates, got %#v", candidates)
	}
}

func TestTemplateMatchNaturalTaskTypeAndDeviceMismatchStillRecallSemanticTemplate(t *testing.T) {
	store, err := New(t.TempDir() + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	tpl := Template{
		ID:          "github.source-evidence-analysis",
		Version:     "1.0.0",
		Title:       "GitHub 源码证据化分析",
		Description: "用于分析 GitHub 仓库源码结构、核心实现、运行流程、学习价值，并要求引用真实链接和行号。",
		Match: MatchRule{
			Keywords: []string{"GitHub", "github.com", "源码结构", "核心实现", "学习价值", "真实链接", "行号"},
			Devices:  []string{"DockMini", "DockAir"},
			Type:     "github_code_reading",
		},
		CompletionConditions: []string{"done"},
		Steps:                []TemplateStep{{ID: "check", Title: "Check", Phase: PhaseCheck}},
	}
	draft, err := store.SaveTemplateDraft(tpl)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ValidateTemplate(draft.ID, draft.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishTemplate(draft.ID, draft.Version); err != nil {
		t.Fatal(err)
	}
	candidates, err := store.MatchTemplates(
		"分析 GitHub 仓库 example/repo，输出源码结构、关键实现、学习价值，并引用真实 GitHub 链接和行号。",
		"Mac mini",
		"GitHub 源码阅读与分析",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ID != tpl.ID {
		t.Fatalf("expected semantic template despite natural task_type and non-enumerated device hint, got %#v", candidates)
	}
}

func TestTemplateMatchSpecificKeywordsBeatGenericTemplate(t *testing.T) {
	store, err := New(t.TempDir() + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	for _, tpl := range []Template{
		{ID: "nexus.deploy.production", Version: "1.0.0", Title: "Nexus deploy", Match: MatchRule{Keywords: []string{"部署"}, Devices: []string{"DockMini"}, Type: "deployment"}, CompletionConditions: []string{"done"}, Steps: []TemplateStep{{ID: "check", Title: "Check", Phase: PhaseCheck}}},
		{ID: "agentdock.deploy.macos", Version: "1.0.0", Title: "macOS deploy", Match: MatchRule{Keywords: []string{"AgentDock", "部署"}, Devices: []string{"DockMini"}, Type: "deployment"}, CompletionConditions: []string{"done"}, Steps: []TemplateStep{{ID: "check", Title: "Check", Phase: PhaseCheck}}},
	} {
		draft, err := store.SaveTemplateDraft(tpl)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.ValidateTemplate(draft.ID, draft.Version); err != nil {
			t.Fatal(err)
		}
		if _, err := store.PublishTemplate(draft.ID, draft.Version); err != nil {
			t.Fatal(err)
		}
	}
	candidates, err := store.MatchTemplates("部署 AgentDock 到 Mac mini", "DockMini", "deployment")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) < 2 || candidates[0].ID != "agentdock.deploy.macos" {
		t.Fatalf("specific AgentDock template should win, got %#v", candidates)
	}
}

type fakeEmbeddingProvider struct{}

func (fakeEmbeddingProvider) EmbedTexts(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, 0, len(texts))
	for _, text := range texts {
		lower := strings.ToLower(text)
		switch {
		case strings.Contains(lower, "recall") || strings.Contains(text, "长期上下文"):
			out = append(out, []float64{1, 0})
		case strings.Contains(lower, "deploy") || strings.Contains(text, "部署"):
			out = append(out, []float64{0, 1})
		default:
			out = append(out, []float64{0.1, 0.1})
		}
	}
	return out, nil
}

func TestTemplateMatchUsesVectorSemanticRecall(t *testing.T) {
	store, err := NewWithOptions(t.TempDir()+"/tasks", StoreOptions{
		TaskVectorSearch:   true,
		EmbeddingProvider:  fakeEmbeddingProvider{},
		TaskVectorMinScore: 0.8,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tpl := range []Template{
		{
			ID: "recalldock.migration", Version: "1.0.0", Title: "RecallDock migration",
			Description:          "迁移长期上下文、memory cards、notes，并统一到 RecallDock semantic recall.",
			Match:                MatchRule{Keywords: []string{"知识库替换"}, Devices: []string{"DockMini"}},
			CompletionConditions: []string{"done"},
			Steps:                []TemplateStep{{ID: "check", Title: "Check", Phase: PhaseCheck}},
		},
		{
			ID: "fallback.device", Version: "1.0.0", Title: "Device fallback",
			Match:                MatchRule{Devices: []string{"DockMini"}},
			CompletionConditions: []string{"done"},
			Steps:                []TemplateStep{{ID: "check", Title: "Check", Phase: PhaseCheck}},
		},
	} {
		draft, err := store.SaveTemplateDraft(tpl)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.ValidateTemplate(draft.ID, draft.Version); err != nil {
			t.Fatal(err)
		}
		if _, err := store.PublishTemplate(draft.ID, draft.Version); err != nil {
			t.Fatal(err)
		}
	}

	candidates, err := store.MatchTemplates("长期上下文系统全面迁移", "DockMini", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ID != "recalldock.migration" {
		t.Fatalf("expected vector semantic candidate to suppress device-only fallback, got %#v", candidates)
	}
	if !strings.Contains(candidates[0].Reason, "vector:") {
		t.Fatalf("expected vector reason, got %#v", candidates[0])
	}
}

type recordingEmbeddingProvider struct {
	calls [][]string
}

func (p *recordingEmbeddingProvider) EmbedTexts(ctx context.Context, texts []string) ([][]float64, error) {
	p.calls = append(p.calls, append([]string{}, texts...))
	return fakeEmbeddingProvider{}.EmbedTexts(ctx, texts)
}

func TestTemplateVectorIndexPersistsAcrossStores(t *testing.T) {
	root := t.TempDir() + "/tasks"
	provider1 := &recordingEmbeddingProvider{}
	store1, err := NewWithOptions(root, StoreOptions{
		TaskVectorSearch:   true,
		EmbeddingProvider:  provider1,
		EmbeddingModel:     "fake-model",
		TaskVectorMinScore: 0.8,
	})
	if err != nil {
		t.Fatal(err)
	}
	tpl := Template{
		ID:                   "recalldock.persisted",
		Version:              "1.0.0",
		Title:                "RecallDock migration",
		Description:          "迁移长期上下文、memory cards、notes，并统一到 RecallDock semantic recall.",
		Match:                MatchRule{Devices: []string{"DockMini"}},
		CompletionConditions: []string{"done"},
		Steps:                []TemplateStep{{ID: "check", Title: "Check", Phase: PhaseCheck}},
	}
	draft, err := store1.SaveTemplateDraft(tpl)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store1.ValidateTemplate(draft.ID, draft.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := store1.PublishTemplate(draft.ID, draft.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := store1.MatchTemplates("长期上下文系统全面迁移", "DockMini", ""); err != nil {
		t.Fatal(err)
	}
	if len(provider1.calls) != 1 || len(provider1.calls[0]) != 2 {
		t.Fatalf("first match should embed query and missing template once, calls=%#v", provider1.calls)
	}
	status, items, model := store1.VectorIndexInfo()
	if status != "ready" || items != 1 || model != "fake-model" {
		t.Fatalf("unexpected vector index info: status=%s items=%d model=%s", status, items, model)
	}

	provider2 := &recordingEmbeddingProvider{}
	store2, err := NewWithOptions(root, StoreOptions{
		TaskVectorSearch:   true,
		EmbeddingProvider:  provider2,
		EmbeddingModel:     "fake-model",
		TaskVectorMinScore: 0.8,
	})
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := store2.MatchTemplates("长期上下文系统全面迁移", "DockMini", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ID != "recalldock.persisted" || !strings.Contains(candidates[0].Reason, "vector:") {
		t.Fatalf("expected persisted vector candidate, got %#v", candidates)
	}
	if len(provider2.calls) != 1 || len(provider2.calls[0]) != 1 {
		t.Fatalf("second store should embed query only and reuse persisted template vector, calls=%#v", provider2.calls)
	}
}

func TestTemplateMatchDisablesVectorWithoutProvider(t *testing.T) {
	store, err := NewWithOptions(t.TempDir()+"/tasks", StoreOptions{TaskVectorSearch: true})
	if err != nil {
		t.Fatal(err)
	}
	if store.VectorSearchEnabled() {
		t.Fatal("vector search should stay disabled without provider or endpoint")
	}
}

func TestTemplateGuardrailsRejectVerboseTemplates(t *testing.T) {
	store, err := New(t.TempDir() + "/tasks")
	if err != nil {
		t.Fatal(err)
	}

	longSteps := testTemplate()
	longSteps.ID = "guard.long.steps"
	longSteps.Version = "1.0.0"
	for i := len(longSteps.Steps); i < 9; i++ {
		longSteps.Steps = append(longSteps.Steps, TemplateStep{ID: "step_" + string(rune('a'+i)), Title: "Stage", Phase: PhaseVerify})
	}
	if _, err := store.SaveTemplateDraft(longSteps); err == nil {
		t.Fatal("expected template with more than eight steps to be rejected")
	}

	manyConditions := testTemplate()
	manyConditions.ID = "guard.many.conditions"
	manyConditions.Version = "1.0.0"
	manyConditions.CompletionConditions = []string{"c1", "c2", "c3", "c4", "c5", "c6", "c7", "c8", "c9", "c10", "c11"}
	if _, err := store.SaveTemplateDraft(manyConditions); err == nil {
		t.Fatal("expected template with too many completion conditions to be rejected")
	}

	sop := testTemplate()
	sop.ID = "guard.sop"
	sop.Version = "1.0.0"
	sop.Steps[0].Title = "每条命令都记录证据"
	if _, err := store.SaveTemplateDraft(sop); err == nil {
		t.Fatal("expected verbose SOP template text to be rejected")
	}
}

func TestTemplateGuardrailsAllowExplicitLongException(t *testing.T) {
	store, err := New(t.TempDir() + "/tasks")
	if err != nil {
		t.Fatal(err)
	}

	longTemplate := testTemplate()
	longTemplate.ID = "guard.long.allowed"
	longTemplate.Version = "1.0.0"
	longTemplate.AllowLongTemplate = true
	longTemplate.LongTemplateReason = "签名安装类流程需要保留额外平台阶段"
	for i := len(longTemplate.Steps); i < 9; i++ {
		longTemplate.Steps = append(longTemplate.Steps, TemplateStep{ID: "extra_" + string(rune('a'+i)), Title: "Extra platform stage", Phase: PhaseVerify})
	}

	draft, err := store.SaveTemplateDraft(longTemplate)
	if err != nil {
		t.Fatal(err)
	}
	if !draft.AllowLongTemplate || draft.LongTemplateReason == "" {
		t.Fatalf("long template exception was not persisted: %#v", draft)
	}
	if _, err := store.ValidateTemplate(draft.ID, draft.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishTemplate(draft.ID, draft.Version); err != nil {
		t.Fatal(err)
	}
}

func TestTemplateGuardrailsRequireReasonForLongException(t *testing.T) {
	store, err := New(t.TempDir() + "/tasks")
	if err != nil {
		t.Fatal(err)
	}

	longTemplate := testTemplate()
	longTemplate.ID = "guard.long.no.reason"
	longTemplate.Version = "1.0.0"
	longTemplate.AllowLongTemplate = true
	for i := len(longTemplate.Steps); i < 9; i++ {
		longTemplate.Steps = append(longTemplate.Steps, TemplateStep{ID: "extra_" + string(rune('a'+i)), Title: "Extra platform stage", Phase: PhaseVerify})
	}
	if _, err := store.SaveTemplateDraft(longTemplate); err == nil {
		t.Fatal("expected long template exception without reason to be rejected")
	}
}

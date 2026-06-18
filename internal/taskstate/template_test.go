package taskstate

import "testing"

func testTemplate() Template {
	return Template{
		ID: "agentdock.deploy.macos", Version: "1.0.0", Title: "Deploy AgentDock on macOS",
		Match:                MatchRule{Keywords: []string{"AgentDock", "deploy"}, Devices: []string{"DockMini"}, TaskTypes: []string{"deployment"}, Priority: 5},
		CompletionConditions: []string{"tests pass", "health is 200"},
		Steps: []TemplateStep{
			{ID: "inspect", Title: "Inspect repository", Phase: PhaseCheck, Required: true, SuggestedCommands: []string{"git status"}, Substitution: "allowed", SubstitutionReasonRequired: true},
			{ID: "install", Title: "Install signed binary", Phase: PhaseExecute, Required: true, DependsOn: []string{"inspect"}, SuggestedCommands: []string{"make install-macos"}, Substitution: "forbidden"},
			{ID: "health", Title: "Verify health", Phase: PhaseVerify, Required: true, DependsOn: []string{"install"}, SuggestedCommands: []string{"curl healthz"}, Substitution: "allowed", SubstitutionReasonRequired: true},
			{ID: "logs", Title: "Inspect optional logs", Phase: PhaseVerify, Required: false},
			{ID: "record", Title: "Record deployment", Phase: PhaseCloseout, Required: true, DependsOn: []string{"health"}},
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
	if _, err := store.Advance(task.ID); err == nil {
		t.Fatal("advanced with incomplete required check step")
	}
	task, err = store.CompleteStep(task.ID, "inspect", StepEvidence{Type: "command", Source: "git status", Result: "exit_code=0", Summary: "repository inspected"}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if task.Steps[0].Status != "completed" {
		t.Fatalf("step=%#v", task.Steps[0])
	}
	if _, err := store.Advance(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteStep(task.ID, "install", StepEvidence{Type: "command", Source: "other installer", Result: "exit_code=0", Summary: "installed"}, true, "equivalent"); err == nil {
		t.Fatal("forbidden substitution succeeded")
	}
}

func TestOptionalStepSkipAndDependencyValidation(t *testing.T) {
	store, err := New(t.TempDir() + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	bad := testTemplate()
	bad.Version = "1.0.1"
	bad.Steps[0].DependsOn = []string{"missing"}
	if _, err := store.SaveTemplateDraft(bad); err == nil {
		t.Fatal("unknown dependency validated")
	}
}

func TestTemplateMatchSemanticSignalsBeatDeviceOnly(t *testing.T) {
	store, err := New(t.TempDir() + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	deviceOnly := Template{
		ID: "unrelated.device", Version: "1.0.0", Title: "Device only",
		Match:                MatchRule{Devices: []string{"DockMini"}, Priority: 999},
		CompletionConditions: []string{"done"},
		Steps:                []TemplateStep{{ID: "check", Title: "Check", Phase: PhaseCheck, Required: true}},
	}
	semantic := Template{
		ID: "notes.question-record", Version: "1.0.0", Title: "Question note",
		Match:                MatchRule{Keywords: []string{"日常问题"}, Devices: []string{"DockMini"}, TaskTypes: []string{"daily-question-note"}, Priority: 57},
		CompletionConditions: []string{"done"},
		Steps:                []TemplateStep{{ID: "check", Title: "Check", Phase: PhaseCheck, Required: true}},
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
	if len(candidates) < 2 {
		t.Fatalf("expected two candidates, got %#v", candidates)
	}
	if candidates[0].ID != "notes.question-record" {
		t.Fatalf("semantic match should win, got %#v", candidates)
	}
	if candidates[1].Score >= 30 {
		t.Fatalf("device-only score should remain tiny, got %#v", candidates)
	}
}

func TestTemplateMatchSkipsTemplatesForOtherDevices(t *testing.T) {
	store, err := New(t.TempDir() + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	for _, tpl := range []Template{
		{ID: "agentdock.deploy.vps", Version: "1.0.0", Title: "VPS deploy", Match: MatchRule{Keywords: []string{"AgentDock", "部署"}, Devices: []string{"DockVPS"}, TaskTypes: []string{"deployment"}, Priority: 35}, CompletionConditions: []string{"done"}, Steps: []TemplateStep{{ID: "check", Title: "Check", Phase: PhaseCheck, Required: true}}},
		{ID: "agentdock.deploy.macos", Version: "1.0.0", Title: "macOS deploy", Match: MatchRule{Keywords: []string{"AgentDock", "部署"}, Devices: []string{"DockMini"}, TaskTypes: []string{"deployment"}, Priority: 15}, CompletionConditions: []string{"done"}, Steps: []TemplateStep{{ID: "check", Title: "Check", Phase: PhaseCheck, Required: true}}},
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
	if len(candidates) != 1 || candidates[0].ID != "agentdock.deploy.macos" {
		t.Fatalf("expected only macOS deployment candidate, got %#v", candidates)
	}
}

func TestTemplateMatchSpecificKeywordsBeatGenericPriority(t *testing.T) {
	store, err := New(t.TempDir() + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	for _, tpl := range []Template{
		{ID: "nexus.deploy.production", Version: "1.0.0", Title: "Nexus deploy", Match: MatchRule{Keywords: []string{"部署"}, Devices: []string{"DockMini"}, TaskTypes: []string{"deployment"}, Priority: 35}, CompletionConditions: []string{"done"}, Steps: []TemplateStep{{ID: "check", Title: "Check", Phase: PhaseCheck, Required: true}}},
		{ID: "agentdock.deploy.macos", Version: "1.0.0", Title: "macOS deploy", Match: MatchRule{Keywords: []string{"AgentDock", "部署"}, Devices: []string{"DockMini"}, TaskTypes: []string{"deployment"}, Priority: 10}, CompletionConditions: []string{"done"}, Steps: []TemplateStep{{ID: "check", Title: "Check", Phase: PhaseCheck, Required: true}}},
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

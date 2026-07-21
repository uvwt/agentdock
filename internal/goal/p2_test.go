package goal

import (
	"context"
	"os"
	"path/filepath"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPolicyBlocksGitPushAndAllowsApprovedInstall(t *testing.T) {
	g := Goal{
		Mode:   ModeGuarded,
		Status: StatusExecuting,
		Constraints: []Constraint{
			{Type: ConstraintProhibition, Value: "no_git_push"},
			{Type: ConstraintApproval, Value: "dependency_install_requires_approval"},
		},
	}
	d := CheckPolicy(g, ActionRunCommand, []string{"git push origin main"})
	if d.Allowed {
		t.Fatalf("git push should be forbidden: %#v", d)
	}
	d = CheckPolicy(g, ActionRunCommand, []string{"npm install lodash"})
	if d.Allowed || d.Level != RiskApprove {
		t.Fatalf("npm install should need approval: %#v", d)
	}
	g.PendingApprovals = []Approval{{ID: "apr_1", Action: "run:npm", Status: "approved"}}
	d = CheckPolicy(g, ActionRunCommand, []string{"npm install lodash"})
	if !d.Allowed {
		t.Fatalf("approved install should pass: %#v", d)
	}
}

func TestVerifierCommandBrowserMetric(t *testing.T) {
	g := Goal{
		SuccessCriteria: []SuccessCriterion{
			{ID: "tests", Type: CriterionCommand, Expression: "test_exit_code == 0"},
			{ID: "dash", Type: CriterionBrowser, Expression: "url_contains:/dashboard"},
			{ID: "fps", Type: CriterionMetric, Expression: "fps_median >= 29.5"},
		},
		Evidence: []EvidenceRef{
			{ID: "e1", Kind: "tests", Summary: "ok", Data: map[string]any{"test_exit_code": 0, "criterion_id": "tests"}},
			{ID: "e2", Kind: "browser", Summary: "nav", Data: map[string]any{"url": "http://x/dashboard", "criterion_id": "dash"}},
			{ID: "e3", Kind: "metric", Summary: "perf", Data: map[string]any{"fps_median": 30.0, "criterion_id": "fps"}},
		},
	}
	report := Verify(g)
	if !report.OK || report.Satisfied != 3 {
		t.Fatalf("expected all satisfied: %#v", report)
	}
	g.Evidence[0].Data["test_exit_code"] = 1
	report = Verify(g)
	if report.OK || report.Failed != 1 {
		t.Fatalf("expected failure: %#v", report)
	}
}

func TestWorkflowRunnerRunsCommandAndPolicy(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(CreateInput{
		Title:     "wf",
		Objective: "run true",
		SuccessCriteria: []SuccessCriterionInput{
			{ID: "cmd", Type: CriterionCommand, Expression: "exit_code == 0"},
		},
		Constraints: []Constraint{{Type: ConstraintProhibition, Value: "no_git_push"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// move to executing via lease/commit
	g, lease, err := store.AcquireLease(g.ID, "w", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	g, err = store.CommitTurn(CommitTurnInput{
		GoalID: g.ID, ReasoningLeaseID: lease.LeaseID, ExpectedCapsuleVersion: g.CapsuleVersion,
		Decision: DecisionContinue, Summary: "run checks",
		Steps: []CommitStepInput{{Action: ActionRunCommand, Targets: []string{"true"}, Idempotency: "run_true"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	runner := &Runner{}
	result := runner.RunWorkflow(context.Background(), g, Workflow{
		Name: "check",
		Steps: []WorkflowStep{
			{Type: "run", Name: "run_true", Command: "true", Kind: "tests"},
			{Type: "verify_browser", Name: "browser", Expression: "url_contains:/dashboard", Observation: map[string]any{"url": "http://localhost/dashboard"}, CriterionID: "unused"},
		},
	})
	if !result.OK {
		t.Fatalf("workflow failed: %#v", result)
	}
	g, err = store.ApplyExecution(g.ID, result)
	if err != nil {
		t.Fatal(err)
	}
	// add criterion-linked evidence for complete
	if _, err := store.AddEvidence(g.ID, EvidenceRef{
		Kind: "tests", Summary: "true", Data: map[string]any{"exit_code": 0, "criterion_id": "cmd"},
	}); err != nil {
		t.Fatal(err)
	}
	g, report, err := store.VerifyGoal(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK {
		t.Fatalf("verify: %#v", report)
	}
	g, err = store.MarkCompleted(g.ID, "done", nil)
	if err != nil {
		t.Fatal(err)
	}
	if g.Status != StatusCompleted {
		t.Fatalf("status=%s", g.Status)
	}

	// policy deny path
	g2, err := store.Create(CreateInput{
		Title: "push", Objective: "blocked",
		SuccessCriteria: []SuccessCriterionInput{{Expression: "ok", Type: CriterionManual}},
		Constraints:     []Constraint{{Type: ConstraintProhibition, Value: "no_git_push"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	g2, lease, err = store.AcquireLease(g2.ID, "w", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	g2, err = store.CommitTurn(CommitTurnInput{
		GoalID: g2.ID, ReasoningLeaseID: lease.LeaseID, ExpectedCapsuleVersion: g2.CapsuleVersion,
		Decision: DecisionContinue, Summary: "try push",
	})
	if err != nil {
		t.Fatal(err)
	}
	denied := runner.RunWorkflow(context.Background(), g2, Workflow{Steps: []WorkflowStep{
		{Type: "run", Command: "git push origin main"},
	}})
	if denied.OK || denied.Steps[0].Policy == nil || denied.Steps[0].Policy.Allowed {
		t.Fatalf("expected policy deny: %#v", denied)
	}
}

func TestResolveApprovalFlow(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(CreateInput{
		Title: "apr", Objective: "need approval",
		SuccessCriteria: []SuccessCriterionInput{{Expression: "ok", Type: CriterionManual}},
	})
	if err != nil {
		t.Fatal(err)
	}
	g, err = store.RequestApproval(RequestApprovalInput{GoalID: g.ID, Action: "run:npm", Summary: "install deps", Risk: "medium"})
	if err != nil {
		t.Fatal(err)
	}
	if g.Status != StatusAwaitingApproval || len(g.PendingApprovals) != 1 {
		t.Fatalf("unexpected: %#v", g)
	}
	id := g.PendingApprovals[0].ID
	g, err = store.ResolveApproval(g.ID, id, "approved", "ok once")
	if err != nil {
		t.Fatal(err)
	}
	if g.Status != StatusExecuting || g.PendingApprovals[0].Status != "approved" {
		t.Fatalf("after approve: %#v", g)
	}
}

func TestMarkCompletedRequiresVerify(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(CreateInput{
		Title: "v", Objective: "need real evidence",
		SuccessCriteria: []SuccessCriterionInput{
			{ID: "tests", Type: CriterionCommand, Expression: "test_exit_code == 0"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddEvidence(g.ID, EvidenceRef{Kind: "note", Summary: "looks fine"}); err != nil {
		t.Fatal(err)
	}
	_, err = store.MarkCompleted(g.ID, "done", nil)
	if err == nil || !errors.Is(err, ErrVerifyFailed) {
		t.Fatalf("want verify failed, got %v", err)
	}
	if !strings.Contains(err.Error(), "pending") && !strings.Contains(err.Error(), "unmet") && !errors.Is(err, ErrVerifyFailed) {
		t.Fatalf("error=%v", err)
	}
}

func TestShellCommandFromTargetsRejectsToolNameList(t *testing.T) {
	if _, ok := shellCommandFromTargets([]string{"pdfinfo", "pdftotext"}); ok {
		t.Fatal("tool-name list must not become a shell command")
	}
	if _, ok := shellCommandFromTargets([]string{"/tmp/bhagavad_gita_goal/full_raw.txt"}); ok {
		t.Fatal("lone data file path must not become a shell command")
	}
	cmd, ok := shellCommandFromTargets([]string{"true"})
	if !ok || cmd != "true" {
		t.Fatalf("got %q %v", cmd, ok)
	}
	cmd, ok = shellCommandFromTargets([]string{"pdfinfo", "/tmp/book.pdf"})
	if !ok || cmd != "pdfinfo /tmp/book.pdf" {
		t.Fatalf("got %q %v", cmd, ok)
	}
	cmd, ok = shellCommandFromTargets([]string{"pdftotext", "-layout", "in.pdf", "out.txt"})
	if !ok {
		t.Fatalf("expected real command, got %q", cmd)
	}
}

func TestExecuteGoalStepsSkipsToolNameList(t *testing.T) {
	g := Goal{
		Status: StatusExecuting,
		Steps: []Step{
			{ID: "step_bad", Action: ActionRunCommand, Targets: []string{"pdfinfo", "pdftotext"}, Status: StepPending},
			{ID: "step_ok", Action: ActionRunCommand, Targets: []string{"true"}, Status: StepPending},
		},
	}
	result := (&Runner{}).ExecuteGoalSteps(context.Background(), g)
	if !result.OK {
		t.Fatalf("expected ok with skip+true: %#v", result)
	}
	if len(result.Steps) < 2 {
		t.Fatalf("steps=%#v", result.Steps)
	}
	if !result.Steps[0].Skipped || !result.Steps[0].OK {
		t.Fatalf("first step should skip: %#v", result.Steps[0])
	}
	if !result.Steps[1].OK || result.Steps[1].Skipped {
		t.Fatalf("second step should run true: %#v", result.Steps[1])
	}
}

func TestApplyExecutionMarksSkippedSteps(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(CreateInput{
		Title: "skip", Objective: "skip bad targets",
		SuccessCriteria: []SuccessCriterionInput{{Expression: "ok", Type: CriterionManual}},
	})
	if err != nil {
		t.Fatal(err)
	}
	g, lease, err := store.AcquireLease(g.ID, "w", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	g, err = store.CommitTurn(CommitTurnInput{
		GoalID: g.ID, ReasoningLeaseID: lease.LeaseID, ExpectedCapsuleVersion: g.CapsuleVersion,
		Decision: DecisionContinue, Summary: "plan tools",
		Steps: []CommitStepInput{{Action: ActionRunCommand, Targets: []string{"pdfinfo", "pdftotext"}, Idempotency: "bad"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := (&Runner{}).ExecuteGoalSteps(context.Background(), g)
	g, err = store.ApplyExecution(g.ID, result)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range g.Steps {
		if s.Idempotency == "bad" || s.Action == ActionRunCommand {
			found = true
			if s.Status != StepSkipped {
				t.Fatalf("expected skipped, got %#v", s)
			}
		}
	}
	if !found {
		t.Fatal("step not found")
	}
}


func TestShellCommandFromTargetsAcceptsScripts(t *testing.T) {
	if _, ok := shellCommandFromTargets([]string{"python3", "/tmp/build_index.py"}); !ok {
		t.Fatal("interpreter+script should be accepted")
	}
	heredoc := "python3 - <<'PY'\nprint(1)\nPY"
	if _, ok := shellCommandFromTargets([]string{heredoc}); !ok {
		t.Fatal("heredoc should be accepted")
	}
	if _, ok := shellCommandFromTargets([]string{"/tmp/build_index.py"}); !ok {
		// bare script path with .py is accepted via isExecutableScriptCommand only if has / and ext
		// "/tmp/build_index.py" has both
		t.Fatal("script path should be accepted")
	}
	if _, ok := shellCommandFromTargets([]string{"pdfinfo", "pdftotext"}); ok {
		t.Fatal("tool list still rejected")
	}
}

func TestApplyExecutionInjectsStderrIntoRequest(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(CreateInput{
		Title: "stderr", Objective: "inject",
		SuccessCriteria: []SuccessCriterionInput{{Expression: "ok", Type: CriterionManual}},
	})
	if err != nil {
		t.Fatal(err)
	}
	g, lease, err := store.AcquireLease(g.ID, "w", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	g, err = store.CommitTurn(CommitTurnInput{
		GoalID: g.ID, ReasoningLeaseID: lease.LeaseID, ExpectedCapsuleVersion: g.CapsuleVersion,
		Decision: DecisionContinue, Summary: "run bad",
		Steps: []CommitStepInput{{Action: ActionRunCommand, Targets: []string{"false"}, Idempotency: "bad"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := (&Runner{}).ExecuteGoalSteps(context.Background(), g)
	if result.OK {
		t.Fatalf("expected failure: %#v", result)
	}
	g, err = store.ApplyExecution(g.ID, result)
	if err != nil {
		t.Fatal(err)
	}
	if g.CurrentRequest == "" || !strings.Contains(g.CurrentRequest, "local step failed") {
		t.Fatalf("request not injected: %q", g.CurrentRequest)
	}
}

func TestResumePromptMentionsChapterChunkPolicy(t *testing.T) {
	c := BuildCapsule(Goal{
		ID: "goal_x", CapsuleVersion: 3, Title: "t", Objective: "o",
		CurrentRequest: "do next chapter",
		SuccessCriteria: []SuccessCriterion{{ID: "m", Type: CriterionManual, Expression: "ok", Status: CriterionPending}},
		Evidence: []EvidenceRef{{Kind: "command", Summary: "step exit 1", Data: map[string]any{"stderr_tail": "KeyError: E"}}},
	})
	p := c.ResumePrompt
	if !strings.Contains(p, "小切片") {
		t.Fatalf("missing chunk policy: %s", p)
	}
	if !strings.Contains(p, "manual") && !strings.Contains(p, "criterion_id") {
		t.Fatalf("missing manual gate: %s", p)
	}
	if !strings.Contains(p, "KeyError") {
		t.Fatalf("missing stderr hint: %s", p)
	}
}

func TestMapGoalStepPreparePatchSkips(t *testing.T) {
	ws, ok := mapGoalStep(Step{ID: "p", Action: ActionPreparePatch, Targets: []string{"/tmp/x"}})
	if !ok || ws.Type != "skip" {
		t.Fatalf("%#v ok=%v", ws, ok)
	}
}


func TestFileScaleCriteria(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small.md")
	big := filepath.Join(dir, "big.md")
	if err := os.WriteFile(small, []byte("# hi\n目前已完成前言\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("行\n", 1000)
	if err := os.WriteFile(big, []byte("# title\n"+body), 0o600); err != nil {
		t.Fatal(err)
	}
	g := Goal{SuccessCriteria: []SuccessCriterion{
		{ID: "s1", Type: CriterionCommand, Expression: "file_min_bytes:" + small + ":1000"},
		{ID: "s2", Type: CriterionCommand, Expression: "file_min_bytes:" + big + ":1000"},
		{ID: "s3", Type: CriterionCommand, Expression: "file_not_contains:" + small + ":目前已完成前言"},
		{ID: "s4", Type: CriterionCommand, Expression: "file_not_contains:" + big + ":目前已完成前言"},
		{ID: "s5", Type: CriterionCommand, Expression: "file_min_lines:" + big + ":500"},
	}}
	rep := Verify(g)
	by := map[string]CriterionVerifyResult{}
	for _, r := range rep.Results {
		by[r.ID] = r
	}
	if by["s1"].Status == CriterionSatisfied {
		t.Fatalf("small should fail min bytes: %#v", by["s1"])
	}
	if by["s2"].Status != CriterionSatisfied {
		t.Fatalf("big should pass min bytes: %#v", by["s2"])
	}
	if by["s3"].Status == CriterionSatisfied {
		t.Fatalf("small contains partial marker: %#v", by["s3"])
	}
	if by["s4"].Status != CriterionSatisfied {
		t.Fatalf("big should not contain partial: %#v", by["s4"])
	}
	if by["s5"].Status != CriterionSatisfied {
		t.Fatalf("big lines: %#v", by["s5"])
	}
}


func TestRunCommandScriptMissingPreflight(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.py")
	res := (&Runner{}).runCommandStep(context.Background(), Goal{Mode: ModeAutopilot, Status: StatusExecuting}, WorkflowStep{
		Type: "run", Name: "run_missing", Command: "python3 " + missing, Targets: []string{"python3", missing},
	})
	if res.OK {
		t.Fatalf("expected failure, got %#v", res)
	}
	if res.ExitCode != 127 {
		t.Fatalf("exit=%d want 127", res.ExitCode)
	}
	if res.Evidence == nil || res.Evidence.Data["code"] != "SCRIPT_MISSING" {
		t.Fatalf("evidence=%#v", res.Evidence)
	}
	if !strings.Contains(res.Error, "SCRIPT_MISSING") {
		t.Fatalf("error=%q", res.Error)
	}
}

func TestMissingScriptFromCommandIgnoresInlineC(t *testing.T) {
	if abs, _ := missingScriptFromCommand("python3 -c print(1)", ""); abs != "" {
		t.Fatalf("inline -c should not be treated as missing script: %q", abs)
	}
}


func TestDetectEmptyArtifactRegressions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.md")
	// previously non-empty evidence, now missing/empty
	g := Goal{
		SuccessCriteria: []SuccessCriterion{
			{ID: "c1", Type: CriterionCommand, Expression: "file_min_bytes:" + path + ":1000"},
		},
		Evidence: []EvidenceRef{
			{Kind: "command", Summary: "wrote", Data: map[string]any{"path": path, "bytes": float64(5000)}},
		},
	}
	regs := detectEmptyArtifactRegressions(g)
	if len(regs) != 1 {
		t.Fatalf("regs=%v", regs)
	}
	// write non-empty enough
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 1500)), 0o600); err != nil {
		t.Fatal(err)
	}
	if regs := detectEmptyArtifactRegressions(g); len(regs) != 0 {
		t.Fatalf("expected no regression, got %v", regs)
	}
}

func TestApplyBookJobTemplateProgressiveCriteria(t *testing.T) {
	in := CreateInput{Title: "letters", Objective: "translate letters to /tmp/out.md"}
	ApplyBookJobTemplate(&in, BookJobTemplateInput{
		Kind: BookJobLetter, OutputPath: "/tmp/out.md", PartsDir: "/tmp/parts", PartCount: 3, PartMinBytes: 1000, FinalMinBytes: 5000, FinalMinLines: 50,
	})
	if len(in.Milestones) < 5 { // prep + 3 + assemble
		t.Fatalf("milestones=%d", len(in.Milestones))
	}
	if len(in.SuccessCriteria) < 9 {
		t.Fatalf("criteria=%d %#v", len(in.SuccessCriteria), in.SuccessCriteria)
	}
	foundFinal := false
	for _, c := range in.SuccessCriteria {
		if c.ID == "final_bytes" {
			foundFinal = true
			if !strings.Contains(c.Expression, "file_min_bytes:/tmp/out.md:5000") {
				t.Fatalf("expr=%s", c.Expression)
			}
		}
	}
	if !foundFinal {
		t.Fatal("missing final_bytes")
	}
}


func TestBookTemplatePartsDirUsesOutputDir(t *testing.T) {
	in := CreateInput{Title: "靈性書信", Objective: "translate letters"}
	out := "/Users/sigi/Documents/真理書密室/英文/RSSB/《靈性書信》Spiritual-Letters-Jaimal Singh_繁體中文.md"
	ApplyBookJobTemplate(&in, BookJobTemplateInput{Kind: BookJobLetter, OutputPath: out, PartCount: 2})
	if !strings.Contains(in.SuccessCriteria[0].Expression, "/Users/sigi/Documents/真理書密室/英文/RSSB/parts/") {
		t.Fatalf("expected absolute parts dir in criteria, got %#v", in.SuccessCriteria[0])
	}
}

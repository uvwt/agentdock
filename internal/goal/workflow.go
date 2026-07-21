package goal

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Workflow is a deterministic multi-step plan executed without model reasoning.
type Workflow struct {
	Name  string         `json:"name,omitempty"`
	Steps []WorkflowStep `json:"steps"`
}

// WorkflowStep is one deterministic unit.
type WorkflowStep struct {
	// Type: run | verify_command | verify_browser | verify_metric | artifact | sleep
	Type       string            `json:"type"`
	Name       string            `json:"name,omitempty"`
	Command    string            `json:"command,omitempty"`
	Dir        string            `json:"dir,omitempty"`
	TimeoutSec int               `json:"timeout_sec,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	// For verify_* steps
	CriterionID string `json:"criterion_id,omitempty"`
	Expression  string `json:"expression,omitempty"`
	// Browser/metric observation injection (caller-provided facts)
	Observation map[string]any `json:"observation,omitempty"`
	// Artifact
	Kind    string `json:"kind,omitempty"`
	Summary string `json:"summary,omitempty"`
	URI     string `json:"uri,omitempty"`
	// Sleep
	Seconds int `json:"seconds,omitempty"`
	// Optional policy targets override
	Targets []string `json:"targets,omitempty"`
}

// StepResult is one executed workflow/step outcome.
type StepResult struct {
	Name       string          `json:"name,omitempty"`
	Type       string          `json:"type"`
	OK         bool            `json:"ok"`
	Skipped    bool            `json:"skipped,omitempty"`
	ExitCode   int             `json:"exit_code,omitempty"`
	Stdout     string          `json:"stdout,omitempty"`
	Stderr     string          `json:"stderr,omitempty"`
	DurationMS int64           `json:"duration_ms,omitempty"`
	Evidence   *EvidenceRef    `json:"evidence,omitempty"`
	Policy     *PolicyDecision `json:"policy,omitempty"`
	Error      string          `json:"error,omitempty"`
	Detail     string          `json:"detail,omitempty"`
}

// RunResult is the aggregate workflow outcome.
type RunResult struct {
	OK      bool         `json:"ok"`
	Steps   []StepResult `json:"steps"`
	Summary string       `json:"summary"`
}

// Runner executes deterministic workflows against a goal's policy.
type Runner struct {
	// WorkDir is the default directory for commands.
	WorkDir string
	// LookPath resolves a binary; defaults to exec.LookPath.
	LookPath func(string) (string, error)
	// Exec runs a command; nil uses the local process executor.
	Exec func(ctx context.Context, dir string, env []string, name string, args ...string) (stdout, stderr string, exitCode int, err error)
}

// RunWorkflow executes all steps, recording evidence-ready results.
// It does not mutate the goal store; callers persist evidence/status.
func (r *Runner) RunWorkflow(ctx context.Context, g Goal, wf Workflow) RunResult {
	out := RunResult{Steps: make([]StepResult, 0, len(wf.Steps))}
	allOK := true
	for i, step := range wf.Steps {
		if ctx.Err() != nil {
			res := StepResult{Name: step.Name, Type: step.Type, OK: false, Error: ctx.Err().Error()}
			out.Steps = append(out.Steps, res)
			allOK = false
			break
		}
		if step.Name == "" {
			step.Name = fmt.Sprintf("step_%02d_%s", i+1, step.Type)
		}
		res := r.runStep(ctx, g, step)
		out.Steps = append(out.Steps, res)
		if !res.OK && !res.Skipped {
			allOK = false
			// fail-fast for deterministic workflows
			break
		}
	}
	out.OK = allOK
	if allOK {
		out.Summary = fmt.Sprintf("workflow %q completed %d steps", wf.Name, len(out.Steps))
	} else {
		out.Summary = fmt.Sprintf("workflow %q failed at step %d", wf.Name, len(out.Steps))
	}
	return out
}

// ExecuteGoalSteps runs pending whitelist goal steps that the runner understands.
func (r *Runner) ExecuteGoalSteps(ctx context.Context, g Goal) RunResult {
	wf := Workflow{Name: "goal-pending-steps"}
	for _, step := range g.Steps {
		if step.Status != StepPending && step.Status != StepFailed {
			continue
		}
		ws, ok := mapGoalStep(step)
		if !ok {
			wf.Steps = append(wf.Steps, WorkflowStep{
				Type: "skip", Name: step.ID, Summary: "step action not executable by local runner: " + string(step.Action),
			})
			continue
		}
		wf.Steps = append(wf.Steps, ws)
	}
	return r.RunWorkflow(ctx, g, wf)
}

func mapGoalStep(step Step) (WorkflowStep, bool) {
	switch step.Action {
	case ActionRunTests:
		cmd := "go test ./..."
		if len(step.Targets) > 0 {
			if c, ok := shellCommandFromTargets(step.Targets); ok {
				cmd = c
			} else {
				// targets are packages/paths for go test
				cmd = "go test " + strings.Join(step.Targets, " ")
			}
		}
		return WorkflowStep{Type: "run", Name: step.ID, Command: cmd, Targets: step.Targets, Summary: step.Summary, Kind: "tests"}, true
	case ActionRunCommand:
		if len(step.Targets) == 0 {
			return WorkflowStep{}, false
		}
		cmd, ok := shellCommandFromTargets(step.Targets)
		if !ok {
			return WorkflowStep{
				Type:    "skip",
				Name:    step.ID,
				Summary: "run_command targets are not a shell command (need one command line or script path): " + strings.Join(step.Targets, ", "),
				Targets: step.Targets,
			}, true
		}
		return WorkflowStep{Type: "run", Name: step.ID, Command: cmd, Targets: step.Targets, Summary: step.Summary}, true
	case ActionCollectLogs:
		return WorkflowStep{Type: "artifact", Name: step.ID, Kind: "log", Summary: firstNonEmptyStr(step.Summary, "logs collected")}, true
	case ActionCreateCheckpoint:
		return WorkflowStep{Type: "artifact", Name: step.ID, Kind: "checkpoint", Summary: firstNonEmptyStr(step.Summary, "checkpoint")}, true
	case ActionInspectFiles:
		return WorkflowStep{Type: "artifact", Name: step.ID, Kind: "inspect", Summary: firstNonEmptyStr(step.Summary, "inspect "+strings.Join(step.Targets, ",")), Targets: step.Targets}, true
	case ActionEnterVerify:
		return WorkflowStep{Type: "artifact", Name: step.ID, Kind: "phase", Summary: "enter verify"}, true
	case ActionPreparePatch:
		return WorkflowStep{
			Type: "skip", Name: step.ID, Targets: step.Targets,
			Summary: firstNonEmptyStr(step.Summary, "prepare_patch is not locally executable; commit concrete run_command/script or file_edit steps"),
		}, true
	case ActionApplyPatch:
		return WorkflowStep{
			Type: "skip", Name: step.ID, Targets: step.Targets,
			Summary: firstNonEmptyStr(step.Summary, "apply_patch is not locally executable; use file_edit/exec_command with explicit paths"),
		}, true
	default:
		return WorkflowStep{}, false
	}
}

// shellCommandFromTargets decides whether step.Targets form a real shell command line.
// Accepts multi-line scripts/heredocs and interpreter+script invocations. Rejects bare
// tool-name lists and lone data files that previously crashed the goal loop.
func shellCommandFromTargets(targets []string) (string, bool) {
	if len(targets) == 0 {
		return "", false
	}
	if len(targets) == 1 {
		t := strings.TrimSpace(targets[0])
		if t == "" {
			return "", false
		}
		if isExecutableScriptCommand(t) || looksLikeInterpreterInvocation(t) {
			return t, true
		}
		if looksLikeDataFile(t) || strings.HasSuffix(t, "/") {
			return "", false
		}
	}
	if len(targets) > 1 {
		allBareNames := true
		for _, t := range targets {
			t = strings.TrimSpace(t)
			if t == "" || !isBareToolName(t) {
				allBareNames = false
				break
			}
		}
		if allBareNames {
			return "", false
		}
		joined := strings.Join(targets, " ")
		if looksLikeInterpreterInvocation(joined) || isExecutableScriptCommand(joined) {
			return joined, true
		}
	}
	return strings.Join(targets, " "), true
}

func isExecutableScriptCommand(t string) bool {
	if strings.Contains(t, "\n") || strings.Contains(t, "\r") || strings.Contains(t, "<<") {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(t))
	for _, p := range []string{"python3 ", "python ", "bash ", "sh ", "zsh ", "node ", "ruby ", "perl ", "osascript "} {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	for _, ext := range []string{".sh", ".bash", ".zsh", ".py", ".js", ".mjs", ".rb", ".pl"} {
		if strings.HasSuffix(lower, ext) && (strings.Contains(t, "/") || strings.HasPrefix(t, ".")) {
			return true
		}
	}
	return false
}

func looksLikeInterpreterInvocation(t string) bool {
	fields := strings.Fields(strings.TrimSpace(t))
	if len(fields) < 2 {
		return false
	}
	bin := strings.ToLower(filepath.Base(fields[0]))
	switch bin {
	case "python", "python3", "bash", "sh", "zsh", "node", "ruby", "perl", "osascript", "go":
		return true
	default:
		return false
	}
}

func isBareToolName(t string) bool {
	if t == "" || strings.HasPrefix(t, "-") {
		return false
	}
	if strings.ContainsAny(t, " \t/\\'\"|&;<>()$`*?") {
		return false
	}
	if strings.HasPrefix(t, "~") {
		return false
	}
	// allow common binary names: pdfinfo, go, npm, true
	for _, r := range t {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func looksLikeDataFile(t string) bool {
	lower := strings.ToLower(t)
	// Has a path separator or home prefix and a non-executable extension.
	hasPath := strings.Contains(t, "/") || strings.HasPrefix(t, "~") || strings.Contains(t, `\`)
	exts := []string{
		".txt", ".md", ".markdown", ".pdf", ".json", ".jsonl", ".csv", ".tsv",
		".html", ".htm", ".xml", ".yaml", ".yml", ".toml", ".ini",
		".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg",
		".zip", ".tar", ".gz", ".tgz", ".bz2",
		".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
		".log", ".out", ".err",
	}
	for _, ext := range exts {
		if strings.HasSuffix(lower, ext) {
			return hasPath || strings.Count(t, ".") >= 1
		}
	}
	// Absolute/relative path without shell metacharacters and without obvious binary name → data path.
	if hasPath && !strings.ContainsAny(t, " \t|&;<>()$`") {
		base := t
		if i := strings.LastIndexAny(t, `/`); i >= 0 {
			base = t[i+1:]
		}
		// no extension and not clearly a script → still allow (could be a binary path)
		if strings.Contains(base, ".") {
			// unknown extension with path: treat as data if not .sh/.bash/.py etc.
			for _, okExt := range []string{".sh", ".bash", ".zsh", ".py", ".rb", ".pl", ".js", ".mjs", ".ts", ".go"} {
				if strings.HasSuffix(lower, okExt) {
					return false
				}
			}
			return true
		}
	}
	return false
}

func (r *Runner) runStep(ctx context.Context, g Goal, step WorkflowStep) StepResult {
	res := StepResult{Name: step.Name, Type: step.Type}
	switch strings.ToLower(strings.TrimSpace(step.Type)) {
	case "skip":
		res.OK = true
		res.Skipped = true
		res.Detail = step.Summary
		return res
	case "sleep":
		sec := step.Seconds
		if sec <= 0 {
			sec = 1
		}
		timer := time.NewTimer(time.Duration(sec) * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			res.Error = ctx.Err().Error()
			return res
		case <-timer.C:
			res.OK = true
			return res
		}
	case "artifact":
		ev := EvidenceRef{
			Kind:    firstNonEmptyStr(step.Kind, "artifact"),
			Summary: firstNonEmptyStr(step.Summary, step.Name),
			URI:     step.URI,
			Data:    cloneMap(step.Observation),
		}
		res.Evidence = &ev
		res.OK = true
		return res
	case "run", "command", "verify_command":
		return r.runCommandStep(ctx, g, step)
	case "verify_browser":
		return r.verifyObservationStep(g, step, "browser", CriterionBrowser)
	case "verify_metric":
		return r.verifyObservationStep(g, step, "metric", CriterionMetric)
	default:
		res.Error = "unknown workflow step type: " + step.Type
		return res
	}
}

func (r *Runner) runCommandStep(ctx context.Context, g Goal, step WorkflowStep) StepResult {
	res := StepResult{Name: step.Name, Type: step.Type}
	cmdLine := strings.TrimSpace(step.Command)
	if cmdLine == "" {
		res.Error = "command is required"
		return res
	}
	targets := step.Targets
	if len(targets) == 0 {
		targets = []string{cmdLine}
	}
	decision := CheckPolicy(g, ActionRunCommand, targets)
	res.Policy = &decision
	if !decision.Allowed {
		res.Error = decision.Reason
		return res
	}

	// Preflight: interpreter + script path must exist before shell execution.
	// Models often commit "python3 /tmp/foo.py" before writing foo.py, which
	// used to hard-fail the goal loop with a generic exit 2.
	if missing, display := missingScriptFromCommand(cmdLine, firstNonEmptyStr(step.Dir, r.WorkDir)); missing != "" {
		msg := fmt.Sprintf("SCRIPT_MISSING: %s does not exist; create it with file_edit action=add (or atomic_write) before run_command", display)
		res.OK = false
		res.ExitCode = 127
		res.Error = msg
		res.Stderr = msg
		res.Evidence = &EvidenceRef{
			Kind:    "command",
			Summary: msg,
			Data: map[string]any{
				"ok":             false,
				"exit_code":      127,
				"test_exit_code": 127,
				"command":        trimOutput(cmdLine, 4<<10),
				"code":           "SCRIPT_MISSING",
				"missing_script": display,
				"hint":           "file_edit action=add path=<script> content=...; then re-run the same run_command",
			},
		}
		return res
	}

	timeout := time.Duration(step.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dir := step.Dir
	if dir == "" {
		dir = r.WorkDir
	}
	start := time.Now()
	stdout, stderr, exitCode, err := r.execShell(cctx, dir, step.Env, cmdLine)
	res.DurationMS = time.Since(start).Milliseconds()
	res.Stdout = trimOutput(stdout, 32<<10)
	res.Stderr = trimOutput(stderr, 16<<10)
	res.ExitCode = exitCode
	if err != nil && exitCode == 0 {
		res.Error = err.Error()
		return res
	}
	res.OK = exitCode == 0
	kind := "command"
	if step.Kind != "" {
		kind = step.Kind
	} else if step.Type == "verify_command" || strings.Contains(strings.ToLower(cmdLine), "test") {
		kind = "tests"
	}
	ev := EvidenceRef{
		Kind:    kind,
		Summary: fmt.Sprintf("%s exit %d", firstNonEmptyStr(step.Name, cmdLine), exitCode),
		Data: map[string]any{
			"exit_code":      exitCode,
			"test_exit_code": exitCode,
			"command":        trimOutput(cmdLine, 4<<10),
			"ok":             exitCode == 0,
			"stdout_tail":    trimOutput(stdout, 4<<10),
			"stderr_tail":    trimOutput(stderr, 4<<10),
		},
	}
	if exitCode != 0 {
		ev.Summary = fmt.Sprintf("%s exit %d: %s", firstNonEmptyStr(step.Name, "command"), exitCode, firstNonEmptyStr(trimOutput(stderr, 240), trimOutput(stdout, 240), "no output"))
	}
	if step.CriterionID != "" {
		ev.Data["criterion_id"] = step.CriterionID
	}
	if step.Expression != "" {
		// evaluate expression against this evidence for verify_command
		tmp := SuccessCriterion{ID: step.CriterionID, Type: CriterionCommand, Expression: step.Expression}
		cr := evaluateCriterion(tmp, []EvidenceRef{ev})
		res.Detail = cr.Detail
		res.OK = cr.Status == CriterionSatisfied
		ev.Data["criterion_status"] = string(cr.Status)
	}
	res.Evidence = &ev
	if !res.OK && res.Error == "" {
		res.Error = fmt.Sprintf("command exit %d", exitCode)
	}
	return res
}

func (r *Runner) verifyObservationStep(g Goal, step WorkflowStep, kind string, typ CriterionType) StepResult {
	res := StepResult{Name: step.Name, Type: step.Type}
	// observation steps are auto-allowed (read-only verification facts)
	decision := CheckPolicy(g, ActionBrowserVerify, step.Targets)
	res.Policy = &decision
	if !decision.Allowed && typ == CriterionBrowser {
		// still allow if readonly observation
		if g.Mode != ModeReadonly {
			res.Error = decision.Reason
			return res
		}
	}
	data := cloneMap(step.Observation)
	if data == nil {
		data = map[string]any{}
	}
	if step.CriterionID != "" {
		data["criterion_id"] = step.CriterionID
	}
	ev := EvidenceRef{
		Kind:    kind,
		Summary: firstNonEmptyStr(step.Summary, step.Name),
		URI:     step.URI,
		Data:    data,
	}
	expr := step.Expression
	if expr == "" && step.CriterionID != "" {
		for _, c := range g.SuccessCriteria {
			if c.ID == step.CriterionID {
				expr = c.Expression
				typ = c.Type
				break
			}
		}
	}
	if expr != "" {
		cr := evaluateCriterion(SuccessCriterion{ID: step.CriterionID, Type: typ, Expression: expr}, []EvidenceRef{ev})
		res.Detail = cr.Detail
		res.OK = cr.Status == CriterionSatisfied
		ev.Data["criterion_status"] = string(cr.Status)
	} else {
		res.OK = true
	}
	res.Evidence = &ev
	if !res.OK && res.Error == "" {
		res.Error = firstNonEmptyStr(res.Detail, "verification failed")
	}
	return res
}

func (r *Runner) execShell(ctx context.Context, dir string, envMap map[string]string, command string) (stdout, stderr string, exitCode int, err error) {
	if r.Exec != nil {
		return r.Exec(ctx, dir, flattenEnv(envMap), "sh", "-c", command)
	}
	if dir == "" {
		dir, _ = os.Getwd()
	}
	if abs, e := filepath.Abs(dir); e == nil {
		dir = abs
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	if len(envMap) > 0 {
		cmd.Env = append(os.Environ(), flattenEnv(envMap)...)
	}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	runErr := cmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	} else if runErr != nil {
		exitCode = 1
	}
	if runErr != nil {
		if exitCode == -1 {
			exitCode = 1
		}
		// still return exit code; RunResult uses exitCode for OK
		return stdout, stderr, exitCode, nil
	}
	return stdout, stderr, exitCode, nil
}

func flattenEnv(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

func trimOutput(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "…(truncated)"
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}


// missingScriptFromCommand detects interpreter invocations whose primary script
// path is absent on disk. Returns absolute path and display path when missing.
func missingScriptFromCommand(cmdLine, workDir string) (absPath, display string) {
	fields := strings.Fields(strings.TrimSpace(cmdLine))
	if len(fields) < 2 {
		return "", ""
	}
	bin := strings.ToLower(filepath.Base(fields[0]))
	switch bin {
	case "python", "python3", "bash", "sh", "zsh", "node", "ruby", "perl":
	default:
		return "", ""
	}
	// Skip common flags until a path-like token.
	script := ""
	for i := 1; i < len(fields); i++ {
		f := fields[i]
		if f == "-c" || f == "-lc" || f == "-e" || f == "--eval" {
			// Inline code forms are not file scripts.
			return "", ""
		}
		if strings.HasPrefix(f, "-") {
			continue
		}
		script = f
		break
	}
	if script == "" {
		return "", ""
	}
	// Only treat path-like scripts (has / or known script extension).
	lower := strings.ToLower(script)
	hasExt := false
	for _, ext := range []string{".py", ".sh", ".bash", ".zsh", ".js", ".mjs", ".rb", ".pl"} {
		if strings.HasSuffix(lower, ext) {
			hasExt = true
			break
		}
	}
	if !strings.Contains(script, "/") && !hasExt {
		return "", ""
	}
	if strings.HasPrefix(script, "-") {
		return "", ""
	}
	abs := script
	if !filepath.IsAbs(script) {
		base := strings.TrimSpace(workDir)
		if base == "" {
			base, _ = os.Getwd()
		}
		abs = filepath.Join(base, script)
	}
	abs = filepath.Clean(abs)
	if st, err := os.Stat(abs); err == nil && !st.IsDir() {
		return "", ""
	}
	return abs, script
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

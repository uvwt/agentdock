package goal

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// VerifyReport is the result of evaluating success criteria against evidence.
type VerifyReport struct {
	OK        bool                    `json:"ok"`
	Satisfied int                     `json:"satisfied"`
	Failed    int                     `json:"failed"`
	Pending   int                     `json:"pending"`
	Results   []CriterionVerifyResult `json:"results"`
	UnmetIDs  []string                `json:"unmet_ids,omitempty"`
	Summary   string                  `json:"summary"`
}

// CriterionVerifyResult is one criterion evaluation.
type CriterionVerifyResult struct {
	ID         string          `json:"id"`
	Type       CriterionType   `json:"type"`
	Expression string          `json:"expression"`
	Status     CriterionStatus `json:"status"`
	EvidenceID string          `json:"evidence_id,omitempty"`
	Detail     string          `json:"detail,omitempty"`
}

// Verify evaluates all success criteria using structured evidence data and summaries.
func Verify(g Goal) VerifyReport {
	report := VerifyReport{Results: make([]CriterionVerifyResult, 0, len(g.SuccessCriteria))}
	if len(g.SuccessCriteria) == 0 {
		report.OK = true
		report.Summary = "no success criteria"
		return report
	}
	for _, c := range g.SuccessCriteria {
		res := evaluateCriterion(c, g.Evidence)
		report.Results = append(report.Results, res)
		switch res.Status {
		case CriterionSatisfied:
			report.Satisfied++
		case CriterionFailed:
			report.Failed++
			report.UnmetIDs = append(report.UnmetIDs, c.ID)
		default:
			report.Pending++
			report.UnmetIDs = append(report.UnmetIDs, c.ID)
		}
	}
	report.OK = report.Failed == 0 && report.Pending == 0
	if report.OK {
		report.Summary = fmt.Sprintf("all %d criteria satisfied", report.Satisfied)
	} else {
		report.Summary = fmt.Sprintf("satisfied=%d failed=%d pending=%d", report.Satisfied, report.Failed, report.Pending)
	}
	return report
}

func evaluateCriterion(c SuccessCriterion, evidence []EvidenceRef) CriterionVerifyResult {
	out := CriterionVerifyResult{
		ID: c.ID, Type: c.Type, Expression: c.Expression, Status: CriterionPending,
	}
	// Prefer evidence explicitly linked to this criterion.
	linked := findEvidence(evidence, func(e EvidenceRef) bool {
		if e.ID == c.EvidenceID && c.EvidenceID != "" {
			return true
		}
		if e.Data == nil {
			return false
		}
		if cid, _ := e.Data["criterion_id"].(string); cid == c.ID {
			return true
		}
		return false
	})

	expr := strings.TrimSpace(c.Expression)
	switch c.Type {
	case CriterionManual:
		// Manual is satisfied only with explicit evidence for this criterion.
		if linked != nil {
			if ok, _ := boolish(linked.Data["satisfied"]); ok || linked.Data["satisfied"] == nil {
				out.Status = CriterionSatisfied
				out.EvidenceID = linked.ID
				out.Detail = "manual evidence present"
				return out
			}
			out.Status = CriterionFailed
			out.EvidenceID = linked.ID
			out.Detail = "manual evidence marked unsatisfied"
			return out
		}
		out.Detail = "manual criterion needs evidence with criterion_id"
		return out

	case CriterionCommand:
		// Filesystem content-scale checks (book jobs). These do not require prior evidence.
		if ok, detail, done := matchFileScaleExpression(expr); done {
			out.Detail = detail
			if ok {
				out.Status = CriterionSatisfied
			} else {
				out.Status = CriterionFailed
			}
			return out
		}
		ev := linked
		if ev == nil {
			ev = findEvidence(evidence, func(e EvidenceRef) bool {
				return e.Kind == "command" || e.Kind == "test_log" || e.Kind == "tests"
			})
		}
		if ev == nil {
			out.Detail = "no command/test evidence"
			return out
		}
		ok, detail := matchCommandExpression(expr, *ev)
		out.EvidenceID = ev.ID
		out.Detail = detail
		if ok {
			out.Status = CriterionSatisfied
		} else if detail != "" && strings.Contains(detail, "mismatch") {
			out.Status = CriterionFailed
		}
		return out

	case CriterionBrowser:
		ev := linked
		if ev == nil {
			ev = findEvidence(evidence, func(e EvidenceRef) bool {
				return e.Kind == "browser" || e.Kind == "screenshot" || e.Kind == "browser_verify"
			})
		}
		if ev == nil {
			out.Detail = "no browser evidence"
			return out
		}
		ok, detail := matchBrowserExpression(expr, *ev)
		out.EvidenceID = ev.ID
		out.Detail = detail
		if ok {
			out.Status = CriterionSatisfied
		} else if strings.Contains(detail, "mismatch") {
			out.Status = CriterionFailed
		}
		return out

	case CriterionMetric:
		ev := linked
		if ev == nil {
			ev = findEvidence(evidence, func(e EvidenceRef) bool {
				return e.Kind == "metric" || e.Kind == "metrics" || e.Kind == "profile"
			})
		}
		if ev == nil {
			out.Detail = "no metric evidence"
			return out
		}
		ok, detail := matchMetricExpression(expr, *ev)
		out.EvidenceID = ev.ID
		out.Detail = detail
		if ok {
			out.Status = CriterionSatisfied
		} else if strings.Contains(detail, "mismatch") {
			out.Status = CriterionFailed
		}
		return out
	default:
		out.Detail = "unknown criterion type"
		return out
	}
}

func findEvidence(list []EvidenceRef, pred func(EvidenceRef) bool) *EvidenceRef {
	// Prefer newest match.
	for i := len(list) - 1; i >= 0; i-- {
		if pred(list[i]) {
			e := list[i]
			return &e
		}
	}
	return nil
}

func matchCommandExpression(expr string, ev EvidenceRef) (bool, string) {
	exitCode, ok := numberish(ev.Data["exit_code"])
	if !ok {
		exitCode, ok = numberish(ev.Data["test_exit_code"])
	}
	if !ok && strings.Contains(strings.ToLower(ev.Summary), "exit 0") {
		exitCode, ok = 0, true
	}
	if !ok && strings.Contains(strings.ToLower(ev.Summary), "passed") {
		exitCode, ok = 0, true
	}

	left, op, right, parsed := parseComparison(expr)
	if !parsed {
		// bare forms: test_exit_code == 0 already handled; fallback substring
		if ok && exitCode == 0 && (strings.Contains(expr, "0") || strings.Contains(expr, "pass")) {
			return true, "command exit 0"
		}
		return false, "unparsed command expression"
	}
	var actual float64
	var have bool
	switch left {
	case "test_exit_code", "exit_code", "code":
		actual, have = exitCode, ok
	default:
		actual, have = numberish(ev.Data[left])
	}
	if !have {
		return false, "missing value for " + left
	}
	okCmp, err := compare(actual, op, right)
	if err != nil {
		return false, err.Error()
	}
	if okCmp {
		return true, fmt.Sprintf("%s %s %v (actual %v)", left, op, right, actual)
	}
	return false, fmt.Sprintf("mismatch %s actual=%v want %s %v", left, actual, op, right)
}

func matchBrowserExpression(expr string, ev EvidenceRef) (bool, string) {
	expr = strings.TrimSpace(expr)
	// url_contains:/dashboard
	if strings.HasPrefix(expr, "url_contains:") {
		want := strings.TrimPrefix(expr, "url_contains:")
		url, _ := ev.Data["url"].(string)
		if url == "" {
			url, _ = ev.Data["final_url"].(string)
		}
		if url == "" {
			url = ev.Summary
		}
		if strings.Contains(url, want) {
			return true, "url contains " + want
		}
		return false, "mismatch url does not contain " + want
	}
	if strings.HasPrefix(expr, "console_errors") {
		_, op, right, parsed := parseComparison(expr)
		if !parsed {
			return false, "unparsed console_errors expression"
		}
		actual, ok := numberish(ev.Data["console_errors"])
		if !ok {
			actual, ok = numberish(ev.Data["console_error_count"])
		}
		if !ok {
			return false, "missing console_errors"
		}
		okCmp, err := compare(actual, op, right)
		if err != nil {
			return false, err.Error()
		}
		if okCmp {
			return true, fmt.Sprintf("console_errors %s %v", op, right)
		}
		return false, fmt.Sprintf("mismatch console_errors actual=%v", actual)
	}
	// generic data field comparison
	left, op, right, parsed := parseComparison(expr)
	if parsed {
		actual, ok := numberish(ev.Data[left])
		if !ok {
			if s, ok := ev.Data[left].(string); ok {
				if op == "==" || op == "=" {
					if s == fmt.Sprint(right) || s == strings.Trim(right, `"'`) {
						return true, left + " matched"
					}
					return false, "mismatch " + left
				}
			}
			return false, "missing " + left
		}
		okCmp, err := compare(actual, op, right)
		if err != nil {
			return false, err.Error()
		}
		if okCmp {
			return true, "ok"
		}
		return false, "mismatch " + left
	}
	return false, "unparsed browser expression"
}

func matchMetricExpression(expr string, ev EvidenceRef) (bool, string) {
	left, op, right, parsed := parseComparison(expr)
	if !parsed {
		return false, "unparsed metric expression"
	}
	actual, ok := numberish(ev.Data[left])
	if !ok {
		// allow nested metrics map
		if m, ok := ev.Data["metrics"].(map[string]any); ok {
			actual, ok = numberish(m[left])
		}
		if !ok {
			return false, "missing metric " + left
		}
	}
	okCmp, err := compare(actual, op, right)
	if err != nil {
		return false, err.Error()
	}
	if okCmp {
		return true, fmt.Sprintf("%s %s %v (actual %v)", left, op, right, actual)
	}
	return false, fmt.Sprintf("mismatch %s actual=%v want %s %v", left, actual, op, right)
}

func parseComparison(expr string) (left, op, right string, ok bool) {
	expr = strings.TrimSpace(expr)
	for _, candidate := range []string{">=", "<=", "==", "!=", "=", ">", "<"} {
		if i := strings.Index(expr, candidate); i > 0 {
			left = strings.TrimSpace(expr[:i])
			op = candidate
			if op == "=" {
				op = "=="
			}
			right = strings.TrimSpace(expr[i+len(candidate):])
			right = strings.Trim(right, `"'`)
			if left != "" && right != "" {
				return left, op, right, true
			}
		}
	}
	return "", "", "", false
}

func compare(actual float64, op, right string) (bool, error) {
	want, err := strconv.ParseFloat(right, 64)
	if err != nil {
		return false, fmt.Errorf("right-hand side not a number: %s", right)
	}
	switch op {
	case "==":
		return actual == want, nil
	case "!=":
		return actual != want, nil
	case ">":
		return actual > want, nil
	case ">=":
		return actual >= want, nil
	case "<":
		return actual < want, nil
	case "<=":
		return actual <= want, nil
	default:
		return false, fmt.Errorf("unsupported op %s", op)
	}
}

func numberish(v any) (float64, bool) {
	switch x := v.(type) {
	case nil:
		return 0, false
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

func boolish(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true", "1", "yes":
			return true, true
		case "false", "0", "no":
			return false, true
		}
	case float64:
		return x != 0, true
	case int:
		return x != 0, true
	}
	return false, false
}

// matchFileScaleExpression evaluates durable file completeness gates without evidence.
// Supported forms (colon-separated, path may contain colons on Windows? we use last field as number):
//   file_min_bytes:/abs/path:80000
//   file_min_lines:/abs/path:500
//   file_not_contains:/abs/path:目前已完成前言
// Returns done=false when expr is not a file-scale gate.
func matchFileScaleExpression(expr string) (ok bool, detail string, done bool) {
	expr = strings.TrimSpace(expr)
	switch {
	case strings.HasPrefix(expr, "file_min_bytes:"):
		path, n, err := splitPathInt(strings.TrimPrefix(expr, "file_min_bytes:"))
		if err != nil {
			return false, err.Error(), true
		}
		st, err := os.Stat(path)
		if err != nil {
			return false, "file missing: " + path, true
		}
		if st.Size() >= n {
			return true, fmt.Sprintf("file size %d >= %d", st.Size(), n), true
		}
		return false, fmt.Sprintf("file size %d < %d (incomplete)", st.Size(), n), true
	case strings.HasPrefix(expr, "file_min_lines:"):
		path, n, err := splitPathInt(strings.TrimPrefix(expr, "file_min_lines:"))
		if err != nil {
			return false, err.Error(), true
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return false, "file missing: " + path, true
		}
		lines := int64(1)
		if len(b) == 0 {
			lines = 0
		} else {
			lines = int64(strings.Count(string(b), "\n"))
			if !strings.HasSuffix(string(b), "\n") {
				lines++
			}
		}
		if lines >= n {
			return true, fmt.Sprintf("file lines %d >= %d", lines, n), true
		}
		return false, fmt.Sprintf("file lines %d < %d (incomplete)", lines, n), true
	case strings.HasPrefix(expr, "file_not_contains:"):
		// file_not_contains:/path:needle  (needle is remainder after last path segment split by first colon after drive? use SplitN 3 on full)
		rest := strings.TrimPrefix(expr, "file_not_contains:")
		// split from right for needle? paths can have no colon. Use strings.SplitN(rest, ":", 2) but abs paths on unix have no colon.
		// Format requires path then colon then needle: /abs/path:needle
		i := strings.LastIndex(rest, ":")
		if i <= 0 || i == len(rest)-1 {
			return false, "file_not_contains needs path:needle", true
		}
		path := rest[:i]
		needle := rest[i+1:]
		b, err := os.ReadFile(path)
		if err != nil {
			return false, "file missing: " + path, true
		}
		if strings.Contains(string(b), needle) {
			return false, "file still contains partial marker: " + needle, true
		}
		return true, "file does not contain partial marker", true
	case strings.HasPrefix(expr, "file_has_heading:") || isGrepHeadingExpr(expr):
		// Book template emits: grep -q '^#' '/abs/path.md'
		path := ""
		if strings.HasPrefix(expr, "file_has_heading:") {
			path = strings.TrimSpace(strings.TrimPrefix(expr, "file_has_heading:"))
		} else {
			path = grepQuotedPath(expr)
		}
		if path == "" {
			return false, "heading check needs a file path", true
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return false, "file missing: " + path, true
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				return true, "file has markdown heading", true
			}
		}
		return false, "file has no markdown heading line", true
	default:
		return false, "", false
	}
}

// isGrepHeadingExpr matches book-template heading gates: grep -q '^#' 'path'
func isGrepHeadingExpr(expr string) bool {
	e := strings.TrimSpace(expr)
	return strings.Contains(e, "grep") && strings.Contains(e, "^#")
}

func grepQuotedPath(expr string) string {
	// Template form: grep -q '^#' '/abs/path.md' — take the last single-quoted token
	// that looks like a path (not the pattern ^#).
	var last string
	for i := 0; i < len(expr); i++ {
		if expr[i] != '\'' {
			continue
		}
		j := strings.IndexByte(expr[i+1:], '\'')
		if j < 0 {
			break
		}
		tok := expr[i+1 : i+1+j]
		i = i + 1 + j
		if tok == "" || tok == "^#" || strings.HasPrefix(tok, "^") {
			continue
		}
		last = tok
	}
	if last != "" {
		return last
	}
	// double-quoted fallback: last non-pattern token
	for i := 0; i < len(expr); i++ {
		if expr[i] != '"' {
			continue
		}
		j := strings.IndexByte(expr[i+1:], '"')
		if j < 0 {
			break
		}
		tok := expr[i+1 : i+1+j]
		i = i + 1 + j
		if tok == "" || strings.HasPrefix(tok, "^") {
			continue
		}
		last = tok
	}
	return last
}

func splitPathInt(rest string) (path string, n int64, err error) {
	rest = strings.TrimSpace(rest)
	i := strings.LastIndex(rest, ":")
	if i <= 0 || i == len(rest)-1 {
		return "", 0, fmt.Errorf("need path:number")
	}
	path = rest[:i]
	v, e := strconv.ParseInt(rest[i+1:], 10, 64)
	if e != nil || v < 0 {
		return "", 0, fmt.Errorf("invalid number in file scale expression")
	}
	return path, v, nil
}


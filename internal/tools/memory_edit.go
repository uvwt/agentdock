package tools

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/uvwt/agentdock/internal/textutil"
)

type memoryPatchOutcome struct {
	Content        string
	OperationCount int
	ChangeCount    int
}

type memoryPatchOperation struct {
	Type        string `json:"type"`
	Op          string `json:"op"`
	Kind        string `json:"kind"`
	Old         string `json:"old"`
	New         string `json:"new"`
	Pattern     string `json:"pattern"`
	Replacement string `json:"replacement"`
	Heading     string `json:"heading"`
	Content     string `json:"content"`
	All         *bool  `json:"all"`
}

func (op memoryPatchOperation) operationType() string {
	return firstNonEmptyText(op.Type, op.Op, op.Kind)
}

func (op memoryPatchOperation) replaceAll(defaultValue bool) bool {
	if op.All == nil {
		return defaultValue
	}
	return *op.All
}

func boolPtr(value bool) *bool { return &value }

type memoryLintFinding struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Term string `json:"term"`
	Text string `json:"text"`
}

func (r *Runtime) memoryDiff(ctx context.Context, args map[string]any) (Result, error) {
	p := strings.TrimSpace(stringArg(args, "path", ""))
	if p == "" {
		return nil, toolError("MISSING_PATH", "path is required", "validation")
	}
	current, err := r.memoryReadContent(ctx, p)
	if err != nil {
		return nil, err
	}
	proposed := firstNonEmptyString(args, "content", "proposed_content", "new_content")
	operationCount, changeCount := 0, 0
	if proposed == "" {
		out, err := applyMemoryPatchOperations(current, args)
		if err != nil {
			return nil, err
		}
		proposed = out.Content
		operationCount = out.OperationCount
		changeCount = out.ChangeCount
	}
	maxBytes := intArg(args, "max_bytes", 60000)
	if maxBytes <= 0 {
		maxBytes = 60000
	}
	diff := memoryUnifiedDiff(p, current, proposed, maxBytes)
	return Result{"path": p, "changed": current != proposed, "diff": diff, "truncated": len(diff) >= maxBytes, "operation_count": operationCount, "change_count": changeCount}, nil
}

func (r *Runtime) memoryPatch(ctx context.Context, args map[string]any) (Result, error) {
	p := strings.TrimSpace(stringArg(args, "path", ""))
	if p == "" {
		return nil, toolError("MISSING_PATH", "path is required", "validation")
	}
	current, err := r.memoryReadContent(ctx, p)
	if err != nil {
		return nil, err
	}
	out, err := applyMemoryPatchOperations(current, args)
	if err != nil {
		return nil, err
	}
	dryRun := boolArg(args, "dry_run", !boolArg(args, "confirmed", false))
	confirmed := boolArg(args, "confirmed", false)
	maxBytes := intArg(args, "max_bytes", 60000)
	if maxBytes <= 0 {
		maxBytes = 60000
	}
	diff := memoryUnifiedDiff(p, current, out.Content, maxBytes)
	result := Result{"path": p, "changed": current != out.Content, "dry_run": dryRun, "confirmed": confirmed, "diff": diff, "truncated": len(diff) >= maxBytes, "operation_count": out.OperationCount, "change_count": out.ChangeCount}
	if dryRun || current == out.Content {
		return result, nil
	}
	if !confirmed {
		return nil, toolError("CONFIRMATION_REQUIRED", "recall patch writes require confirmed=true unless dry_run=true", "validation")
	}
	writeResult, err := r.memoryWriteContent(ctx, p, out.Content)
	if err != nil {
		return nil, err
	}
	result["written"] = true
	result["recall"] = writeResult["recall"]
	return result, nil
}

func (r *Runtime) memoryUpdateFact(ctx context.Context, args map[string]any) (Result, error) {
	p := strings.TrimSpace(stringArg(args, "path", ""))
	if p == "" {
		return nil, toolError("MISSING_PATH", "path is required", "validation")
	}
	facts := map[string]string{}
	if key := strings.TrimSpace(stringArg(args, "key", "")); key != "" {
		value, ok := args["value"]
		if !ok || value == nil {
			return nil, toolError("MISSING_VALUE", "value is required when key is provided", "validation")
		}
		facts[key] = fmt.Sprint(value)
	}
	if m := mapArg(args, "facts"); len(m) > 0 {
		for k, v := range m {
			key := strings.TrimSpace(k)
			if key != "" {
				facts[key] = fmt.Sprint(v)
			}
		}
	}
	if len(facts) == 0 {
		return nil, toolError("MISSING_FACTS", "provide key/value or facts", "validation")
	}
	current, err := r.memoryReadContent(ctx, p)
	if err != nil {
		return nil, err
	}
	section := strings.TrimSpace(stringArg(args, "section", ""))
	appendIfMissing := boolArg(args, "append_if_missing", false)
	keys := make([]string, 0, len(facts))
	for k := range facts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	updated := current
	updates := []map[string]any{}
	missing := []string{}
	for _, key := range keys {
		var changed, found bool
		updated, found, changed = updateMemoryFactLine(updated, section, key, facts[key], appendIfMissing)
		updates = append(updates, map[string]any{"key": key, "value": facts[key], "found": found, "changed": changed})
		if !found {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 && !appendIfMissing {
		return nil, toolErrorDetails("FACT_NOT_FOUND", "one or more facts were not found", "validation", map[string]any{"path": p, "missing": missing})
	}
	dryRun := boolArg(args, "dry_run", !boolArg(args, "confirmed", false))
	confirmed := boolArg(args, "confirmed", false)
	maxBytes := intArg(args, "max_bytes", 60000)
	if maxBytes <= 0 {
		maxBytes = 60000
	}
	diff := memoryUnifiedDiff(p, current, updated, maxBytes)
	result := Result{"path": p, "changed": current != updated, "dry_run": dryRun, "confirmed": confirmed, "updates": updates, "diff": diff, "truncated": len(diff) >= maxBytes}
	if dryRun || current == updated {
		return result, nil
	}
	if !confirmed {
		return nil, toolError("CONFIRMATION_REQUIRED", "recall fact writes require confirmed=true unless dry_run=true", "validation")
	}
	writeResult, err := r.memoryWriteContent(ctx, p, updated)
	if err != nil {
		return nil, err
	}
	result["written"] = true
	result["recall"] = writeResult["recall"]
	return result, nil
}

func (r *Runtime) memoryLint(ctx context.Context, args map[string]any) (Result, error) {
	terms := stringSliceArg(args, "terms")
	if len(terms) == 0 {
		terms = []string{"Connector", "connector", "CONNECTOR", "connectors", "connector_"}
	}
	maxEntries := intArg(args, "max_entries", 200)
	if maxEntries <= 0 {
		maxEntries = 200
	}
	maxFindings := intArg(args, "max_findings", 200)
	if maxFindings <= 0 {
		maxFindings = 200
	}
	listArgs := map[string]any{"max_entries": maxEntries}
	if prefix := strings.TrimSpace(stringArg(args, "prefix", "")); prefix != "" {
		listArgs["prefix"] = prefix
	}
	listed, err := r.memoryList(ctx, listArgs)
	if err != nil {
		return nil, err
	}
	paths := memoryPathsFromList(listed)
	regexMode := boolArg(args, "regex", false)
	findings := []memoryLintFinding{}
	filesScanned := 0
	for _, p := range paths {
		if len(findings) >= maxFindings {
			break
		}
		if !isRecallLintTextPath(p) {
			continue
		}
		content, err := r.memoryReadContent(ctx, p)
		if err != nil {
			findings = append(findings, memoryLintFinding{Path: p, Line: 0, Term: "READ_ERROR", Text: err.Error()})
			continue
		}
		filesScanned++
		findings = append(findings, lintMemoryContent(p, content, terms, regexMode, maxFindings-len(findings))...)
	}
	return Result{"terms": terms, "regex": regexMode, "files_scanned": filesScanned, "finding_count": len(findings), "findings": findings, "truncated": len(findings) >= maxFindings}, nil
}

func (r *Runtime) memoryReadContent(ctx context.Context, p string) (string, error) {
	// 编辑、diff、patch 需要完整 Markdown，不能使用默认瘦身后的 body，
	// 否则写回时可能丢失 frontmatter。
	result, err := r.memoryRead(ctx, map[string]any{"path": p, "include_raw": true})
	if err != nil {
		return "", err
	}
	memory, ok := result["recall"].(map[string]any)
	if !ok {
		return "", toolErrorDetails("RECALL_RESPONSE_MISSING_RECALL", "NexusDock Recall response does not contain recall object", "network", map[string]any{"path": p})
	}
	if content, ok := memory["raw_content"].(string); ok {
		return content, nil
	}
	if content, ok := memory["content"].(string); ok {
		return content, nil
	}
	if body, ok := memory["body"].(string); ok {
		return body, nil
	}
	return "", toolErrorDetails("RECALL_RESPONSE_MISSING_CONTENT", "NexusDock Recall recall object does not contain raw_content/content/body", "network", map[string]any{"path": p})
}

func (r *Runtime) memoryWriteContent(ctx context.Context, p, content string) (Result, error) {
	payload := map[string]any{"path": recallBackendPath(p), "content": content, "overwrite": true, "confirmed": true}
	return r.memoryRequest(ctx, http.MethodPost, "/v1/recall", payload)
}

func applyMemoryPatchOperations(content string, args map[string]any) (memoryPatchOutcome, error) {
	ops := memoryOperationArgs(args)
	if len(ops) == 0 {
		return memoryPatchOutcome{Content: content}, toolError("MISSING_OPERATIONS", "provide operations, old/new, pattern/replacement, section/content, append, or prepend", "validation")
	}
	current := content
	operationCount, changeCount := 0, 0
	for _, op := range ops {
		operationCount++
		kind := op.operationType()
		var n int
		var err error
		switch kind {
		case "replace_text", "replace":
			current, n, err = applyTextReplace(current, op.Old, op.New, op.replaceAll(true))
		case "replace_regex", "regex":
			current, n, err = applyRegexReplace(current, op.Pattern, op.Replacement, op.replaceAll(true))
		case "replace_section", "section":
			current, n, err = applySectionReplace(current, op.Heading, op.Content)
		case "append":
			if op.Content == "" {
				err = toolError("MISSING_CONTENT", "append operation requires content", "validation")
			} else {
				current = ensureTrailingNewline(current) + op.Content
				n = 1
			}
		case "prepend":
			if op.Content == "" {
				err = toolError("MISSING_CONTENT", "prepend operation requires content", "validation")
			} else {
				current = op.Content + ensureLeadingNewline(current)
				n = 1
			}
		default:
			err = toolErrorDetails("UNKNOWN_OPERATION", "unknown recall patch operation", "validation", map[string]any{"operation": kind})
		}
		if err != nil {
			return memoryPatchOutcome{}, err
		}
		changeCount += n
	}
	return memoryPatchOutcome{Content: current, OperationCount: operationCount, ChangeCount: changeCount}, nil
}

func memoryOperationArgs(args map[string]any) []memoryPatchOperation {
	ops := make([]memoryPatchOperation, 0)
	if raw, ok := args["operations"]; ok && raw != nil {
		var decoded []memoryPatchOperation
		if err := remarshal(raw, &decoded); err == nil {
			ops = append(ops, decoded...)
		}
	}
	if old := stringArg(args, "old", ""); old != "" {
		ops = append(ops, memoryPatchOperation{Type: "replace_text", Old: old, New: stringArg(args, "new", ""), All: boolPtr(boolArg(args, "all", true))})
	}
	if pattern := stringArg(args, "pattern", ""); pattern != "" {
		ops = append(ops, memoryPatchOperation{Type: "replace_regex", Pattern: pattern, Replacement: stringArg(args, "replacement", ""), All: boolPtr(boolArg(args, "all", true))})
	}
	if heading := stringArg(args, "section", ""); heading != "" {
		ops = append(ops, memoryPatchOperation{Type: "replace_section", Heading: heading, Content: firstNonEmptyString(args, "section_content", "content")})
	}
	if appendText := stringArg(args, "append", ""); appendText != "" {
		ops = append(ops, memoryPatchOperation{Type: "append", Content: appendText})
	}
	if prependText := stringArg(args, "prepend", ""); prependText != "" {
		ops = append(ops, memoryPatchOperation{Type: "prepend", Content: prependText})
	}
	return ops
}

func applyTextReplace(content, old, new string, all bool) (string, int, error) {
	if old == "" {
		return content, 0, toolError("MISSING_OLD", "replace_text operation requires old", "validation")
	}
	count := strings.Count(content, old)
	if count == 0 {
		return content, 0, toolError("TEXT_NOT_FOUND", "old text was not found", "validation")
	}
	if all {
		return strings.ReplaceAll(content, old, new), count, nil
	}
	return strings.Replace(content, old, new, 1), 1, nil
}

func applyRegexReplace(content, pattern, replacement string, all bool) (string, int, error) {
	if pattern == "" {
		return content, 0, toolError("MISSING_PATTERN", "replace_regex operation requires pattern", "validation")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return content, 0, toolErrorDetails("INVALID_REGEX", err.Error(), "validation", map[string]any{"pattern": pattern})
	}
	matches := re.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return content, 0, toolError("REGEX_NOT_FOUND", "pattern did not match", "validation")
	}
	if all {
		return re.ReplaceAllString(content, replacement), len(matches), nil
	}
	loc := matches[0]
	return content[:loc[0]] + re.ReplaceAllString(content[loc[0]:loc[1]], replacement) + content[loc[1]:], 1, nil
}

func applySectionReplace(content, heading, sectionContent string) (string, int, error) {
	heading = strings.TrimSpace(heading)
	if heading == "" {
		return content, 0, toolError("MISSING_HEADING", "replace_section operation requires heading", "validation")
	}
	lines := strings.Split(content, "\n")
	headingRe := regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)
	start, end, level := -1, len(lines), 0
	for i, line := range lines {
		m := headingRe.FindStringSubmatch(line)
		if len(m) == 3 && strings.TrimSpace(m[2]) == heading {
			start = i
			level = len(m[1])
			break
		}
	}
	if start < 0 {
		return content, 0, toolErrorDetails("SECTION_NOT_FOUND", "section heading was not found", "validation", map[string]any{"heading": heading})
	}
	for i := start + 1; i < len(lines); i++ {
		m := headingRe.FindStringSubmatch(lines[i])
		if len(m) == 3 && len(m[1]) <= level {
			end = i
			break
		}
	}
	newSection := []string{lines[start]}
	if strings.TrimSpace(sectionContent) != "" {
		newSection = append(newSection, strings.Split(strings.Trim(sectionContent, "\n"), "\n")...)
	}
	out := append([]string{}, lines[:start]...)
	out = append(out, newSection...)
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n"), 1, nil
}

func memoryUnifiedDiff(p, oldText, newText string, maxBytes int) string {
	if oldText == newText {
		return ""
	}
	oldLines := strings.Split(oldText, "\n")
	newLines := strings.Split(newText, "\n")
	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix && oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}
	contextLines := 3
	start := prefix - contextLines
	if start < 0 {
		start = 0
	}
	oldEnd := len(oldLines) - suffix + contextLines
	if oldEnd > len(oldLines) {
		oldEnd = len(oldLines)
	}
	newEnd := len(newLines) - suffix + contextLines
	if newEnd > len(newLines) {
		newEnd = len(newLines)
	}
	var b strings.Builder
	b.WriteString("--- " + p + "\n")
	b.WriteString("+++ " + p + "\n")
	b.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", start+1, oldEnd-start, start+1, newEnd-start))
	for i := start; i < prefix && i < len(oldLines); i++ {
		b.WriteString(" " + oldLines[i] + "\n")
	}
	for i := prefix; i < len(oldLines)-suffix; i++ {
		b.WriteString("-" + oldLines[i] + "\n")
		if maxBytes > 0 && b.Len() >= maxBytes {
			return textutil.SafeTruncateString(b.String(), maxBytes).Text
		}
	}
	for i := prefix; i < len(newLines)-suffix; i++ {
		b.WriteString("+" + newLines[i] + "\n")
		if maxBytes > 0 && b.Len() >= maxBytes {
			return textutil.SafeTruncateString(b.String(), maxBytes).Text
		}
	}
	for i := len(oldLines) - suffix; i < oldEnd && i < len(oldLines); i++ {
		b.WriteString(" " + oldLines[i] + "\n")
	}
	out := b.String()
	if maxBytes > 0 && len(out) > maxBytes {
		return textutil.SafeTruncateString(out, maxBytes).Text
	}
	return out
}

func firstNonEmptyText(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptyString(args map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringArg(args, key, ""); value != "" {
			return value
		}
	}
	return ""
}

func ensureTrailingNewline(value string) string {
	if value == "" || strings.HasSuffix(value, "\n") {
		return value
	}
	return value + "\n"
}

func ensureLeadingNewline(value string) string {
	if value == "" || strings.HasPrefix(value, "\n") {
		return value
	}
	return "\n" + value
}

func isRecallLintTextPath(p string) bool {
	ext := strings.ToLower(path.Ext(strings.TrimSpace(p)))
	return ext == ".md" || ext == ".markdown" || ext == ".txt"
}

func memoryPathsFromList(result Result) []string {
	entries, _ := result["entries"].([]any)
	paths := []string{}
	for _, item := range entries {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if typ := strings.TrimSpace(fmt.Sprint(m["type"])); typ != "" && typ != "<nil>" && typ != "file" {
			continue
		}
		if p := strings.TrimSpace(fmt.Sprint(m["path"])); p != "" && p != "<nil>" {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return paths
}

func updateMemoryFactLine(content, section, key, value string, appendIfMissing bool) (string, bool, bool) {
	lines := strings.Split(content, "\n")
	section = strings.TrimSpace(section)
	key = strings.TrimSpace(key)
	headingRe := regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)
	inSection := section == ""
	sectionLevel := 0
	insertAfter := -1
	for i, line := range lines {
		if m := headingRe.FindStringSubmatch(line); len(m) == 3 {
			level := len(m[1])
			name := strings.TrimSpace(m[2])
			if section != "" && name == section {
				inSection = true
				sectionLevel = level
				insertAfter = i
				continue
			}
			if section != "" && inSection && level <= sectionLevel {
				inSection = false
			}
		}
		if !inSection || !lineLooksLikeFact(line, key) {
			continue
		}
		updated := replaceFactValue(line, key, value)
		lines[i] = updated
		return strings.Join(lines, "\n"), true, updated != line
	}
	if !appendIfMissing {
		return content, false, false
	}
	newLine := key + "：" + value
	if section != "" && insertAfter >= 0 {
		out := append([]string{}, lines[:insertAfter+1]...)
		out = append(out, newLine)
		out = append(out, lines[insertAfter+1:]...)
		return strings.Join(out, "\n"), true, true
	}
	return ensureTrailingNewline(content) + newLine + "\n", true, true
}

func lineLooksLikeFact(line, key string) bool {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "- ")
	return strings.HasPrefix(trimmed, key+"：") || strings.HasPrefix(trimmed, key+":") || strings.HasPrefix(trimmed, key+" =") || strings.HasPrefix(trimmed, key+"=")
}

func replaceFactValue(line, key, value string) string {
	idx := strings.Index(line, key)
	if idx < 0 {
		return line
	}
	rest := line[idx+len(key):]
	sep := "："
	if strings.HasPrefix(rest, ":") {
		sep = ":"
	} else if strings.HasPrefix(rest, " =") || strings.HasPrefix(rest, "=") {
		sep = " ="
	}
	prefix := line[:idx]
	if sep == " =" {
		return prefix + key + " = " + value
	}
	return prefix + key + sep + value
}

func lintMemoryContent(p, content string, terms []string, regexMode bool, limit int) []memoryLintFinding {
	if limit <= 0 {
		return nil
	}
	findings := []memoryLintFinding{}
	lines := strings.Split(content, "\n")
	regexes := []*regexp.Regexp{}
	regexTerms := []string{}
	if regexMode {
		for _, term := range terms {
			if term == "" {
				continue
			}
			if re, err := regexp.Compile(term); err == nil {
				regexes = append(regexes, re)
				regexTerms = append(regexTerms, term)
			}
		}
	}
	for i, line := range lines {
		if len(findings) >= limit {
			break
		}
		if regexMode {
			for idx, re := range regexes {
				if re.MatchString(line) {
					findings = append(findings, memoryLintFinding{Path: p, Line: i + 1, Term: regexTerms[idx], Text: strings.TrimSpace(line)})
					break
				}
			}
			continue
		}
		for _, term := range terms {
			if term != "" && strings.Contains(line, term) {
				findings = append(findings, memoryLintFinding{Path: p, Line: i + 1, Term: term, Text: strings.TrimSpace(line)})
				break
			}
		}
	}
	return findings
}

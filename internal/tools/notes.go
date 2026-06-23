package tools

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const recallNotesPrefix = "recall/managed/notes"

type notesScope struct {
	Name       string
	Prefix     string
	IndexPath  string
	Categories []string
}

type notesCandidate struct {
	Path   string `json:"path"`
	Score  int    `json:"score"`
	Reason string `json:"reason"`
}

func (r *Runtime) notesSearch(ctx context.Context, args map[string]any) (Result, error) {
	query := strings.TrimSpace(stringArg(args, "query", ""))
	if query == "" {
		return nil, toolError("MISSING_QUERY", "query is required", "validation")
	}
	scope, err := resolveNotesScope(stringArg(args, "scope", "questions"))
	if err != nil {
		return nil, err
	}
	maxResults := intArg(args, "max_results", 8)
	if maxResults <= 0 {
		maxResults = 8
	}

	terms := notesTerms(query)
	candidates := map[string]notesCandidate{}
	indexFound := false
	if index, err := r.memoryRead(ctx, map[string]any{"path": scope.IndexPath}); err == nil {
		indexFound = true
		if recallDoc, ok := index["recall"].(map[string]any); ok {
			for _, candidatePath := range extractNotesCandidatePaths(scope, memoryText(recallDoc)) {
				addNotesCandidate(candidates, candidatePath, scoreNotesText(candidatePath, terms), "index")
			}
		}
	}

	searchResults := []any{}
	if result, err := r.memorySearch(ctx, map[string]any{"query": query, "prefix": scope.Prefix, "max_results": maxResults}); err == nil {
		if results, ok := result["results"].([]any); ok {
			searchResults = results
			for _, item := range results {
				if resultMap, ok := item.(map[string]any); ok {
					p := recallPublicPath(strings.TrimSpace(fmt.Sprint(resultMap["path"])))
					if p != "" && p != scope.IndexPath && strings.HasPrefix(p, scope.Prefix+"/") {
						addNotesCandidate(candidates, p, 20+scoreNotesText(p, terms), "search")
					}
				}
			}
		}
	}

	ordered := make([]notesCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		ordered = append(ordered, candidate)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Score == ordered[j].Score {
			return ordered[i].Path < ordered[j].Path
		}
		return ordered[i].Score > ordered[j].Score
	})
	if len(ordered) > maxResults {
		ordered = ordered[:maxResults]
	}

	recommendedAction := "create_new"
	reason := "no existing candidate found"
	if len(ordered) > 0 && ordered[0].Score >= 20 {
		recommendedAction = "update_existing"
		reason = "matched existing notes by index or search"
	} else if !indexFound {
		recommendedAction = "put_open"
		reason = "index missing; keep new notes reviewable under open/"
	}

	candidatePaths := make([]string, 0, len(ordered))
	for _, candidate := range ordered {
		candidatePaths = append(candidatePaths, candidate.Path)
	}
	result := Result{
		"ok":                  true,
		"scope":               scope.Name,
		"prefix":              scope.Prefix,
		"index_path":          scope.IndexPath,
		"index_found":         indexFound,
		"query":               query,
		"strategy":            "index_first",
		"recommended_action":  recommendedAction,
		"reason":              reason,
		"candidate_paths":     candidatePaths,
		"candidates":          ordered,
		"search_result_count": len(searchResults),
		"auto_write":          false,
	}
	if boolArg(args, "include_search_results", false) {
		result["search_results"] = searchResults
	}
	return result, nil
}

func (r *Runtime) notesCapture(ctx context.Context, args map[string]any) (Result, error) {
	question := strings.TrimSpace(stringArg(args, "question", ""))
	if question == "" {
		question = strings.TrimSpace(stringArg(args, "query", ""))
	}
	if question == "" {
		return nil, toolError("MISSING_QUESTION", "question or query is required", "validation")
	}
	scope, err := resolveNotesScope(stringArg(args, "scope", "questions"))
	if err != nil {
		return nil, err
	}
	search, err := r.notesSearch(ctx, map[string]any{"scope": scope.Name, "query": question, "max_results": intArg(args, "max_results", 5)})
	if err != nil {
		return nil, err
	}

	targetPath := ""
	if paths, ok := search["candidate_paths"].([]string); ok && len(paths) > 0 {
		targetPath = paths[0]
	}
	action := fmt.Sprint(search["recommended_action"])
	if targetPath == "" || action != "update_existing" {
		targetPath = defaultNotesCapturePath(scope, question)
		if action == "create_new" {
			action = "create_new"
		} else {
			action = "put_open"
		}
	}

	draft := map[string]any{
		"question":       question,
		"summary":        strings.TrimSpace(stringArg(args, "summary", "")),
		"conclusion":     strings.TrimSpace(stringArg(args, "conclusion", "")),
		"open_questions": stringSliceArg(args, "open_questions"),
		"source":         strings.TrimSpace(stringArg(args, "source", "current conversation")),
	}
	plan := map[string]any{
		"recommended_action": action,
		"target_path":        targetPath,
		"write_mode":         "append_section",
		"section":            strings.TrimSpace(stringArg(args, "section", "相关问题")),
		"needs_review":       true,
		"auto_write":         false,
		"reason":             search["reason"],
		"draft":              draft,
	}
	return Result{
		"ok":           true,
		"scope":        scope.Name,
		"capture_plan": plan,
		"search":       search,
	}, nil
}

func (r *Runtime) notesWrite(ctx context.Context, args map[string]any) (Result, error) {
	scope, err := resolveNotesScope(stringArg(args, "scope", "questions"))
	if err != nil {
		return nil, err
	}
	rawPath := strings.TrimSpace(stringArg(args, "path", ""))
	if rawPath == "" {
		return nil, toolError("MISSING_PATH", "path is required", "validation")
	}
	if hasUnsafeNotesPathSegment(rawPath) {
		return nil, toolErrorDetails("INVALID_NOTES_PATH", "recall_write blocks hidden or escaping path segments", "validation", map[string]any{"path": rawPath})
	}
	p := path.Clean(rawPath)
	if !strings.HasPrefix(p, scope.Prefix+"/") {
		return nil, toolErrorDetails("INVALID_NOTES_PATH", "recall_write can only write inside the selected notes scope", "validation", map[string]any{"path": p, "scope": scope.Name, "prefix": scope.Prefix})
	}
	content := stringArg(args, "content", "")
	if strings.TrimSpace(content) == "" {
		return nil, toolError("MISSING_CONTENT", "content is required", "validation")
	}
	if hasNotesSensitiveMarker(content) {
		return nil, toolError("SENSITIVE_CONTENT", "recall_write blocked content that looks like a secret", "validation")
	}
	if strings.Contains(p, "/decisions/") && !boolArg(args, "confirmed", false) {
		return nil, toolError("CONFIRMATION_REQUIRED", "writing decisions notes requires confirmed=true", "validation")
	}
	if !boolArg(args, "confirmed", false) {
		return nil, toolError("CONFIRMATION_REQUIRED", "recall_write requires confirmed=true", "validation")
	}
	writeArgs := map[string]any{
		"path":      p,
		"content":   content,
		"confirmed": true,
		"overwrite": boolArg(args, "overwrite", false),
		"type":      "note",
		"scope":     "notes",
	}
	result, err := r.memoryWrite(ctx, writeArgs)
	if err != nil {
		return nil, err
	}
	result["recall_note_tool"] = "recall_write"
	result["scope"] = scope.Name
	return result, nil
}

func resolveNotesScope(value string) (notesScope, error) {
	scope := strings.TrimSpace(strings.ToLower(value))
	scope = strings.TrimPrefix(scope, "notes/")
	scope = strings.TrimPrefix(scope, recallNotesPrefix+"/")
	switch scope {
	case "", "questions", "question":
		return notesScope{Name: "questions", Prefix: recallNotesPrefix + "/questions", IndexPath: recallNotesPrefix + "/questions/index.md", Categories: []string{"topics", "decisions", "open"}}, nil
	case "github-learning", "github_learning", "github":
		return notesScope{Name: "github-learning", Prefix: recallNotesPrefix + "/github-learning", IndexPath: recallNotesPrefix + "/github-learning/index.md", Categories: []string{"projects", "topics", "patterns", "comparisons"}}, nil
	default:
		return notesScope{}, toolErrorDetails("INVALID_NOTES_SCOPE", "unsupported notes scope", "validation", map[string]any{"scope": value, "allowed": []string{"questions", "github-learning"}})
	}
}

func extractNotesCandidatePaths(scope notesScope, body string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = strings.Trim(strings.TrimSpace(p), "`.,，。:：;；()（）[]【】")
		if p == "" {
			return
		}
		if strings.HasPrefix(p, scope.Prefix+"/") && strings.HasSuffix(p, ".md") && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	fullPathRe := regexp.MustCompile(regexp.QuoteMeta(scope.Prefix) + `/[A-Za-z0-9._/-]+\.md`)
	for _, match := range fullPathRe.FindAllString(body, -1) {
		add(match)
	}
	fileRe := regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9._-]*\.md`)
	for _, match := range fileRe.FindAllString(body, -1) {
		if strings.Contains(match, "/") {
			continue
		}
		for _, category := range scope.Categories {
			add(scope.Prefix + "/" + category + "/" + match)
		}
	}
	return out
}

func memoryText(memory map[string]any) string {
	for _, key := range []string{"body", "raw_content", "content"} {
		if value := strings.TrimSpace(fmt.Sprint(memory[key])); value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func addNotesCandidate(candidates map[string]notesCandidate, p string, score int, reason string) {
	if p == "" {
		return
	}
	current, ok := candidates[p]
	if !ok || score > current.Score {
		candidates[p] = notesCandidate{Path: p, Score: score, Reason: reason}
	}
}

func notesTerms(query string) []string {
	fields := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("，。,.；;：:、/\\|()[]{}<>`\"'", r)
	})
	seen := map[string]bool{}
	terms := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if len([]rune(field)) < 2 || seen[field] {
			continue
		}
		seen[field] = true
		terms = append(terms, field)
	}
	return terms
}

func scoreNotesText(text string, terms []string) int {
	text = strings.ToLower(text)
	score := 0
	for _, term := range terms {
		if strings.Contains(text, term) {
			score += 10
		}
	}
	return score
}

func defaultNotesCapturePath(scope notesScope, question string) string {
	slug := notesSlug(question)
	if slug == "" {
		slug = "untitled"
	}
	if scope.Name == "github-learning" {
		return scope.Prefix + "/topics/" + slug + ".md"
	}
	return scope.Prefix + "/open/" + slug + ".md"
}

func notesSlug(value string) string {
	value = strings.ToLower(value)
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if r > unicode.MaxASCII && unicode.IsLetter(r) {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && builder.Len() > 0 {
			builder.WriteRune('-')
			lastDash = true
		}
		if builder.Len() >= 48 {
			break
		}
	}
	return strings.Trim(builder.String(), "-")
}

func hasUnsafeNotesPathSegment(value string) bool {
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." || strings.HasPrefix(segment, ".") {
			return true
		}
	}
	return false
}

func hasNotesSensitiveMarker(content string) bool {
	lower := strings.ToLower(content)
	markers := []string{"begin private key", "github_pat_", "ghp_", "xoxb-", "sk-"}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

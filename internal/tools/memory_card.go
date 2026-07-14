package tools

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
)

const recallCardsPrefix = "recall/managed/cards"

type memoryCardSpec struct {
	Title      string
	Content    string
	CardType   string
	Scope      string
	Project    string
	Status     string
	Confidence string
	Source     string
	Evidence   string
	Tags       []string
	Boundary   string
}

func (r *Runtime) memoryCardCapture(ctx context.Context, args map[string]any) (Result, error) {
	card, warnings, err := memoryCardFromArgs(args, false)
	if err != nil {
		return nil, err
	}

	queryParts := []string{card.Title, card.Content, card.Project, card.CardType}
	if len(card.Tags) > 0 {
		queryParts = append(queryParts, strings.Join(card.Tags, " "))
	}
	searchArgs := map[string]any{"query": strings.Join(queryParts, " "), "prefix": recallCardsPrefix, "max_results": intArg(args, "max_results", 8)}
	similar := []any{}
	searchError := ""
	if result, err := r.memorySearch(ctx, searchArgs); err == nil {
		if items, ok := result["results"].([]any); ok {
			similar = items
		}
	} else {
		searchError = err.Error()
	}

	action := "create_card"
	reason := "no similar card found"
	if len(similar) > 0 {
		action = "review_similar_then_merge_or_supersede"
		reason = "similar Recall cards were found; avoid duplicate active cards"
	}
	if len(warnings) > 0 {
		action = "review_before_write"
		reason = "candidate has warnings that need review before writing"
	}

	plan := Result{
		"recommended_action": action,
		"reason":             reason,
		"auto_write":         false,
		"needs_review":       true,
		"target_path":        memoryCardPath(card),
		"write_tool":         "recall_write",
		"write_status":       card.Status,
	}
	if searchError != "" {
		plan["search_error"] = searchError
	}
	return Result{
		"card":            memoryCardResult(card),
		"warnings":        warnings,
		"capture_plan":    plan,
		"similar_results": similar,
		"similar_count":   len(similar),
	}, nil
}

func (r *Runtime) memoryCardWrite(ctx context.Context, args map[string]any) (Result, error) {
	if !boolArg(args, "confirmed", false) {
		return nil, toolError("CONFIRMATION_REQUIRED", "recall_write requires confirmed=true", "validation")
	}
	card, warnings, err := memoryCardFromArgs(args, true)
	if err != nil {
		return nil, err
	}
	if len(warnings) > 0 && !boolArg(args, "allow_warnings", false) {
		return nil, toolErrorDetails("CARD_REVIEW_REQUIRED", "recall card has warnings; fix it or pass allow_warnings=true after review", "validation", map[string]any{"warnings": warnings})
	}

	p := strings.TrimSpace(stringArg(args, "path", ""))
	if p == "" {
		p = memoryCardPath(card)
	}
	p = path.Clean(p)
	if !strings.HasPrefix(p, recallCardsPrefix+"/") || hasUnsafeNotesPathSegment(p) {
		return nil, toolErrorDetails("INVALID_RECALL_CARD_PATH", "recall_write only writes under recall/managed/cards/ with safe path segments", "validation", map[string]any{"path": p})
	}

	content := memoryCardMarkdown(card)
	writeArgs := map[string]any{
		"path":       p,
		"content":    content,
		"confirmed":  true,
		"overwrite":  boolArg(args, "overwrite", false),
		"type":       "recall-card",
		"scope":      card.Scope,
		"project":    card.Project,
		"source":     card.Source,
		"confidence": card.Confidence,
		"tags":       card.Tags,
	}
	result, err := r.memoryWrite(ctx, writeArgs)
	if err != nil {
		return nil, err
	}
	result["recall_card_tool"] = "recall_write"
	result["card"] = memoryCardResult(card)
	result["path"] = p
	result["status"] = card.Status
	result["index_policy"] = "recall service should rebuild search and embedding indexes after card writes when supported"
	return result, nil
}

func memoryCardFromArgs(args map[string]any, requireEvidenceForActive bool) (memoryCardSpec, []string, error) {
	rawScope := strings.TrimSpace(stringArg(args, "scope", ""))
	rawProject := strings.TrimSpace(stringArg(args, "project", ""))
	card := memoryCardSpec{
		Title:      strings.TrimSpace(stringArg(args, "title", "")),
		Content:    strings.TrimSpace(firstNonEmptyString(args, "content", "summary")),
		CardType:   strings.TrimSpace(stringArg(args, "type", "")),
		Scope:      rawScope,
		Project:    rawProject,
		Status:     strings.TrimSpace(stringArg(args, "status", "inbox")),
		Confidence: strings.TrimSpace(stringArg(args, "confidence", "medium")),
		Source:     strings.TrimSpace(stringArg(args, "source", "current conversation")),
		Evidence:   strings.TrimSpace(stringArg(args, "evidence", "")),
		Tags:       normalizedMemoryCardTags(stringSliceArg(args, "tags")),
		Boundary:   strings.TrimSpace(stringArg(args, "boundary", "")),
	}
	if card.Title == "" {
		return card, nil, toolError("MISSING_TITLE", "title is required", "validation")
	}
	if card.Content == "" {
		return card, nil, toolError("MISSING_CONTENT", "content or summary is required", "validation")
	}
	if card.CardType == "" {
		card.CardType = "runbook"
	}
	if card.Project == "" {
		card.Project = "global"
	}
	if card.Scope == "" {
		if rawProject == "" || strings.EqualFold(card.Project, "global") {
			card.Scope = "global"
		} else {
			card.Scope = "project"
		}
	}
	if err := validateMemoryCardEnum("type", card.CardType, []string{"preference", "runbook", "bug_pattern", "deploy_note", "project_trap", "architecture", "decision", "anti_pattern"}); err != nil {
		return card, nil, err
	}
	if err := validateMemoryCardEnum("scope", card.Scope, []string{"global", "project", "device", "domain"}); err != nil {
		return card, nil, err
	}
	if err := validateMemoryCardEnum("status", card.Status, []string{"inbox", "active", "verified", "stale", "archived", "rejected"}); err != nil {
		return card, nil, err
	}
	if err := validateMemoryCardEnum("confidence", card.Confidence, []string{"low", "medium", "high"}); err != nil {
		return card, nil, err
	}

	warnings := memoryCardWarnings(card)
	if requireEvidenceForActive && (card.Status == "active" || card.Status == "verified") && card.Evidence == "" {
		warnings = append(warnings, "active/verified card should include evidence")
	}
	sort.Strings(warnings)
	return card, warnings, nil
}

func memoryCardWarnings(card memoryCardSpec) []string {
	warnings := []string{}
	contentRunes := []rune(card.Content)
	if len(contentRunes) > 500 {
		warnings = append(warnings, "content is longer than 500 runes; split it into smaller cards")
	}
	if len(contentRunes) < 20 {
		warnings = append(warnings, "content is very short; make the reusable action explicit")
	}
	if hasNotesSensitiveMarker(card.Title + "\n" + card.Content + "\n" + card.Evidence) {
		warnings = append(warnings, "content looks like it may contain credential material")
	}
	lower := strings.ToLower(card.Content)
	for _, marker := range []string{"当前端口", "现在运行", "刚才日志", "临时", "一次性", "today", "now running"} {
		if strings.Contains(lower, strings.ToLower(marker)) {
			warnings = append(warnings, "content may describe temporary fact-layer state instead of reusable experience")
			break
		}
	}
	return uniqueStrings(warnings)
}

func validateMemoryCardEnum(name, value string, allowed []string) error {
	for _, item := range allowed {
		if value == item {
			return nil
		}
	}
	return toolErrorDetails("INVALID_RECALL_CARD_"+strings.ToUpper(name), "unsupported recall card field value", "validation", map[string]any{"field": name, "value": value, "allowed": allowed})
}

func normalizedMemoryCardTags(tags []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, tag := range tags {
		tag = strings.Trim(strings.ToLower(strings.TrimSpace(tag)), "#，,;； ")
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

func memoryCardPath(card memoryCardSpec) string {
	project := notesSlug(card.Project)
	if project == "" {
		project = "global"
	}
	slug := notesSlug(card.Title)
	if slug == "" {
		slug = "untitled"
	}
	return path.Join(recallCardsPrefix, project, card.Status, card.CardType, slug+".md")
}

func memoryCardMarkdown(card memoryCardSpec) string {
	var builder strings.Builder
	builder.WriteString("---\n")
	builder.WriteString("type: recall-card\n")
	builder.WriteString("card_type: " + card.CardType + "\n")
	builder.WriteString("scope: " + card.Scope + "\n")
	builder.WriteString("project: " + card.Project + "\n")
	builder.WriteString("status: " + card.Status + "\n")
	builder.WriteString("confidence: " + card.Confidence + "\n")
	builder.WriteString("source: " + yamlSingleLine(card.Source) + "\n")
	if len(card.Tags) > 0 {
		builder.WriteString("tags: " + strings.Join(card.Tags, ",") + "\n")
	}
	if card.Evidence != "" {
		builder.WriteString("evidence: " + yamlSingleLine(card.Evidence) + "\n")
	}
	builder.WriteString("---\n\n")
	builder.WriteString("# " + card.Title + "\n\n")
	builder.WriteString(card.Content + "\n")
	if card.Boundary != "" {
		builder.WriteString("\n## 使用边界\n\n")
		builder.WriteString(card.Boundary + "\n")
	}
	return builder.String()
}

func memoryCardResult(card memoryCardSpec) Result {
	return Result{
		"title":      card.Title,
		"content":    card.Content,
		"type":       card.CardType,
		"scope":      card.Scope,
		"project":    card.Project,
		"status":     card.Status,
		"confidence": card.Confidence,
		"source":     card.Source,
		"evidence":   card.Evidence,
		"tags":       card.Tags,
		"boundary":   card.Boundary,
	}
}

func yamlSingleLine(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.TrimSpace(value)
	if value == "" {
		return "\"\""
	}
	return fmt.Sprintf("%q", value)
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := values[:0]
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

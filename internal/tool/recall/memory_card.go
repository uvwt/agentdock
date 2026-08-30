package recall

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode"
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

func (svc *Service) memoryCardCapture(ctx context.Context, request WriteRequest) (Result, error) {
	card, warnings, err := parseMemoryCard(request, false)
	if err != nil {
		return nil, err
	}

	queryParts := []string{card.Title, card.Content, card.Project, card.CardType}
	if len(card.Tags) > 0 {
		queryParts = append(queryParts, strings.Join(card.Tags, " "))
	}
	searchRequest := memorySearchRequest{Query: strings.Join(queryParts, " "), Prefix: recallCardsPrefix, MaxResults: intValue(request.MaxResults, 8)}
	similar := []any{}
	searchError := ""
	if result, err := svc.memorySearch(ctx, searchRequest); err == nil {
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

func (svc *Service) memoryCardWrite(ctx context.Context, request WriteRequest) (Result, error) {
	if !boolValue(request.Confirmed, false) {
		return nil, toolError("CONFIRMATION_REQUIRED", "recall_write requires confirmed=true", "validation")
	}
	card, warnings, err := parseMemoryCard(request, true)
	if err != nil {
		return nil, err
	}
	if len(warnings) > 0 && !request.AllowWarnings {
		return nil, toolErrorDetails("CARD_REVIEW_REQUIRED", "recall card has warnings; fix it or pass allow_warnings=true after review", "validation", map[string]any{"warnings": warnings})
	}

	p := strings.TrimSpace(request.Path)
	if p == "" {
		p = memoryCardPath(card)
	}
	p = path.Clean(p)
	if !strings.HasPrefix(p, recallCardsPrefix+"/") || hasUnsafeCardPathSegment(p) {
		return nil, toolErrorDetails("INVALID_RECALL_CARD_PATH", "recall_write only writes under recall/managed/cards/ with safe path segments", "validation", map[string]any{"path": p})
	}

	content := memoryCardMarkdown(card)
	writeRequest := WriteRequest{
		Path: p, Content: content, Confirmed: boolPtrValue(true), Overwrite: boolPtrValue(boolValue(request.Overwrite, false)),
		Type: "recall-card", Scope: card.Scope, Project: card.Project, Source: card.Source, Confidence: card.Confidence,
		Tags: append([]string(nil), card.Tags...),
	}
	result, err := svc.memoryWrite(ctx, writeRequest)
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

func parseMemoryCard(request WriteRequest, requireEvidenceForActive bool) (memoryCardSpec, []string, error) {
	rawScope := strings.TrimSpace(request.Scope)
	rawProject := strings.TrimSpace(request.Project)
	status := strings.TrimSpace(request.Status)
	if status == "" {
		status = "inbox"
	}
	confidence := strings.TrimSpace(request.Confidence)
	if confidence == "" {
		confidence = "medium"
	}
	source := strings.TrimSpace(request.Source)
	if source == "" {
		source = "current conversation"
	}
	card := memoryCardSpec{
		Title:      strings.TrimSpace(request.Title),
		Content:    strings.TrimSpace(firstNonEmptyText(request.Content, request.Summary)),
		CardType:   strings.TrimSpace(request.Type),
		Scope:      rawScope,
		Project:    rawProject,
		Status:     status,
		Confidence: confidence,
		Source:     source,
		Evidence:   strings.TrimSpace(request.Evidence),
		Tags:       normalizedMemoryCardTags(request.Tags),
		Boundary:   strings.TrimSpace(request.Boundary),
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
	if hasCardSensitiveMarker(card.Title + "\n" + card.Content + "\n" + card.Evidence) {
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
	project := cardSlug(card.Project)
	if project == "" {
		project = "global"
	}
	slug := cardSlug(card.Title)
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

func cardSlug(value string) string {
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

func hasUnsafeCardPathSegment(value string) bool {
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." || strings.HasPrefix(segment, ".") {
			return true
		}
	}
	return false
}

func hasCardSensitiveMarker(content string) bool {
	lower := strings.ToLower(content)
	for _, marker := range []string{"begin private key", "github_pat_", "ghp_", "xoxb-", "sk-"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
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

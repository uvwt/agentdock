package tools

import (
	"context"
	"net/http"
	"strings"
)

func (r *Runtime) recallBootstrap(ctx context.Context, args map[string]any) (Result, error) {
	result, err := r.memoryBootstrap(ctx, args)
	if err != nil {
		return nil, err
	}
	decorateRecallResult(result)
	result["recommended_use"] = "Call recall_bootstrap before substantial AgentDock, project, deployment, debugging, or preference-sensitive tasks."
	return result, nil
}

func (r *Runtime) recallSearch(ctx context.Context, args map[string]any) (Result, error) {
	kind := strings.ToLower(strings.TrimSpace(stringArg(args, "kind", "all")))
	switch kind {
	case "note", "notes":
		result, err := r.notesSearch(ctx, args)
		if err != nil {
			return nil, err
		}
		decorateRecallResult(result)
		relabelRecallWriteResult(result)
		result["recall_kind"] = "note"
		return result, nil
	case "card", "cards":
		searchArgs := copyArgs(args)
		if strings.TrimSpace(stringArg(searchArgs, "prefix", "")) == "" {
			searchArgs["prefix"] = "cards"
		}
		result, err := r.memorySearch(ctx, searchArgs)
		if err != nil {
			return nil, err
		}
		decorateRecallResult(result)
		relabelRecallWriteResult(result)
		result["recall_kind"] = "card"
		return result, nil
	case "", "all", "markdown":
		searchArgs := copyArgs(args)
		prefix := strings.TrimSpace(stringArg(searchArgs, "prefix", ""))
		if strings.HasPrefix(prefix, "private-notes") {
			return nil, toolError("PRIVATE_NOTES_OUT_OF_RECALL_SCOPE", "private-notes is not searchable through recall_search; use private_notes_search", "validation")
		}
		if prefix == "" {
			searchArgs["prefix"] = ""
		}
		result, err := r.memorySearch(ctx, searchArgs)
		if err != nil {
			return nil, err
		}
		decorateRecallResult(result)
		result["recall_kind"] = kind
		return result, nil
	default:
		return nil, toolErrorDetails("INVALID_RECALL_KIND", "unsupported recall_search kind", "validation", map[string]any{"kind": kind})
	}
}

func (r *Runtime) recallRead(ctx context.Context, args map[string]any) (Result, error) {
	if strings.HasPrefix(strings.TrimSpace(stringArg(args, "path", "")), "private-notes/") {
		return nil, toolError("PRIVATE_NOTES_OUT_OF_RECALL_SCOPE", "private-notes is not readable through recall_read; use private_notes_read", "validation")
	}
	result, err := r.memoryRead(ctx, args)
	if err != nil {
		return nil, err
	}
	decorateRecallResult(result)
	return result, nil
}

func (r *Runtime) recallWrite(ctx context.Context, args map[string]any) (Result, error) {
	if strings.HasPrefix(strings.TrimSpace(stringArg(args, "path", "")), "private-notes/") {
		return nil, toolError("PRIVATE_NOTES_OUT_OF_RECALL_SCOPE", "private-notes is not writable through recall_write; use private_notes_write", "validation")
	}
	kind := strings.ToLower(strings.TrimSpace(stringArg(args, "kind", "")))
	if kind == "" {
		kind = "auto"
	}
	switch kind {
	case "auto", "plan", "classify":
		result, err := r.recallWriteAutoPlan(ctx, args)
		if err != nil {
			return nil, err
		}
		decorateRecallResult(result)
		result["recall_kind"] = "auto"
		return result, nil
	case "card":
		var result Result
		var err error
		if boolArg(args, "confirmed", false) {
			result, err = r.memoryCardWrite(ctx, args)
		} else {
			result, err = r.memoryCardCapture(ctx, args)
		}
		if err != nil {
			return nil, err
		}
		decorateRecallResult(result)
		relabelRecallWriteResult(result)
		result["recall_kind"] = "card"
		return result, nil
	case "note", "notes":
		var result Result
		var err error
		if boolArg(args, "confirmed", false) {
			result, err = r.notesWrite(ctx, args)
		} else {
			result, err = r.notesCapture(ctx, args)
		}
		if err != nil {
			return nil, err
		}
		decorateRecallResult(result)
		relabelRecallWriteResult(result)
		result["recall_kind"] = "note"
		return result, nil
	case "markdown", "write", "create", "replace":
		result, err := r.memoryWrite(ctx, args)
		if err != nil {
			return nil, err
		}
		decorateRecallResult(result)
		result["recall_kind"] = "markdown"
		return result, nil
	case "append_note", "append":
		result, err := r.memoryAppendNote(ctx, args)
		if err != nil {
			return nil, err
		}
		decorateRecallResult(result)
		result["recall_kind"] = "append_note"
		return result, nil
	case "patch", "edit":
		result, err := r.memoryPatch(ctx, args)
		if err != nil {
			return nil, err
		}
		decorateRecallResult(result)
		result["recall_kind"] = "patch"
		return result, nil
	case "diff", "preview":
		result, err := r.memoryDiff(ctx, args)
		if err != nil {
			return nil, err
		}
		decorateRecallResult(result)
		result["recall_kind"] = "diff"
		return result, nil
	case "fact", "update_fact":
		result, err := r.memoryUpdateFact(ctx, args)
		if err != nil {
			return nil, err
		}
		decorateRecallResult(result)
		result["recall_kind"] = "fact"
		return result, nil
	case "delete", "remove":
		result, err := r.memoryDelete(ctx, args)
		if err != nil {
			return nil, err
		}
		decorateRecallResult(result)
		result["recall_kind"] = "delete"
		return result, nil
	default:
		return nil, toolErrorDetails("INVALID_RECALL_KIND", "unsupported recall_write kind", "validation", map[string]any{"kind": kind})
	}
}

func (r *Runtime) recallWriteAutoPlan(ctx context.Context, args map[string]any) (Result, error) {
	selectedKind, reason := classifyRecallWriteKind(args)
	nextArgs := recallWriteNextArgs(args, selectedKind)
	plan := Result{
		"selected_kind":      selectedKind,
		"reason":             reason,
		"auto_write":         false,
		"needs_review":       true,
		"recommended_action": "review_next_call",
		"policy":             "model should choose card/note/markdown explicitly; auto is only a safe fallback when kind is omitted or genuinely uncertain",
		"next_call": Result{
			"tool": "recall_write",
			"args": nextArgs,
		},
	}

	result := Result{
		"ok":            true,
		"selected_kind": selectedKind,
		"auto_plan":     plan,
		"capture_plan":  plan,
	}

	switch selectedKind {
	case "card":
		if strings.TrimSpace(stringArg(nextArgs, "title", "")) != "" && strings.TrimSpace(firstNonEmptyString(nextArgs, "content", "summary")) != "" {
			capture, err := r.memoryCardCapture(ctx, nextArgs)
			if err != nil {
				return nil, err
			}
			for k, v := range capture {
				result[k] = v
			}
			result["auto_plan"] = plan
		}
	case "note":
		if strings.TrimSpace(firstNonEmptyString(nextArgs, "question", "query")) != "" {
			capture, err := r.notesCapture(ctx, nextArgs)
			if err != nil {
				return nil, err
			}
			for k, v := range capture {
				result[k] = v
			}
			result["auto_plan"] = plan
		}
	}
	result["selected_kind"] = selectedKind
	return result, nil
}

func classifyRecallWriteKind(args map[string]any) (string, string) {
	pathValue := strings.TrimSpace(stringArg(args, "path", ""))
	content := strings.TrimSpace(firstNonEmptyString(args, "content", "summary"))
	query := strings.TrimSpace(firstNonEmptyString(args, "query", "question"))
	title := strings.TrimSpace(stringArg(args, "title", ""))
	if pathValue != "" && (strings.TrimSpace(stringArg(args, "key", "")) != "" || len(mapArg(args, "facts")) > 0) {
		return "fact", "path with key/value or facts updates an existing structured fact"
	}
	if pathValue != "" && hasRecallPatchArgs(args) {
		return "patch", "path with edit fields updates an existing recall entry"
	}
	if pathValue != "" && content != "" {
		return "markdown", "path with content writes or replaces a known Markdown entry"
	}
	if query != "" && (content == "" || strings.Contains(query, "?") || strings.Contains(query, "？")) {
		return "note", "query/question is best captured as a reviewable note"
	}
	if content != "" || title != "" {
		return "card", "title/content is best captured as an atomic experience card"
	}
	return "note", "insufficient write fields; start with a note planning step"
}

func hasRecallPatchArgs(args map[string]any) bool {
	if _, ok := args["operations"]; ok {
		return true
	}
	for _, key := range []string{"old", "pattern", "section", "section_content", "append", "prepend"} {
		if strings.TrimSpace(stringArg(args, key, "")) != "" {
			return true
		}
	}
	return false
}

func recallWriteNextArgs(args map[string]any, selectedKind string) Result {
	next := Result{"kind": selectedKind}
	copyIfPresent := func(key string) {
		if value, ok := args[key]; ok && value != nil {
			next[key] = value
		}
	}
	for _, key := range []string{"path", "title", "content", "summary", "query", "question", "overwrite", "max_bytes"} {
		copyIfPresent(key)
	}
	switch selectedKind {
	case "card":
		if strings.TrimSpace(stringArg(next, "title", "")) == "" {
			if summary := strings.TrimSpace(stringArg(next, "summary", "")); summary != "" {
				next["title"] = firstRunes(summary, 32)
			} else if content := strings.TrimSpace(stringArg(next, "content", "")); content != "" {
				next["title"] = firstRunes(content, 32)
			}
		}
		for _, key := range []string{"type", "tags", "boundary", "source", "confidence", "evidence", "status", "scope"} {
			copyIfPresent(key)
		}
	case "note":
		if strings.TrimSpace(stringArg(next, "question", "")) == "" {
			if query := strings.TrimSpace(stringArg(next, "query", "")); query != "" {
				next["question"] = query
			} else if title := strings.TrimSpace(stringArg(next, "title", "")); title != "" {
				next["question"] = title
			}
		}
		for _, key := range []string{"scope", "conclusion", "open_questions", "section", "source"} {
			copyIfPresent(key)
		}
	case "patch":
		for _, key := range []string{"old", "new", "pattern", "replacement", "section", "section_content", "append", "prepend", "operations", "dry_run"} {
			copyIfPresent(key)
		}
	case "fact":
		for _, key := range []string{"key", "value", "facts", "section", "append_if_missing"} {
			copyIfPresent(key)
		}
	case "markdown":
		for _, key := range []string{"type", "tags", "source", "confidence", "scope"} {
			copyIfPresent(key)
		}
	}
	if selectedKind != "note" && selectedKind != "card" {
		next["confirmed"] = false
	}
	return next
}

func (r *Runtime) recallMaintain(ctx context.Context, args map[string]any) (Result, error) {
	action := strings.ToLower(strings.TrimSpace(stringArg(args, "action", "sync_status")))
	switch action {
	case "sync_status", "sync", "status":
		result, err := r.memoryRequest(ctx, http.MethodGet, "/v1/sync/status", nil)
		if err != nil {
			return nil, err
		}
		decorateRecallResult(result)
		result["recall_action"] = "sync_status"
		return result, nil
	case "list":
		listArgs := copyArgs(args)
		if strings.HasPrefix(strings.TrimSpace(stringArg(listArgs, "prefix", "")), "private-notes") {
			return nil, toolError("PRIVATE_NOTES_OUT_OF_RECALL_SCOPE", "private-notes is not listable through recall_maintain; use private_notes_maintain", "validation")
		}
		if strings.TrimSpace(stringArg(listArgs, "prefix", "")) == "" {
			listArgs["prefix"] = ""
		}
		result, err := r.memoryList(ctx, listArgs)
		if err != nil {
			return nil, err
		}
		decorateRecallResult(result)
		result["recall_action"] = "list"
		return result, nil
	case "lint":
		lintArgs := copyArgs(args)
		if strings.HasPrefix(strings.TrimSpace(stringArg(lintArgs, "prefix", "")), "private-notes") {
			return nil, toolError("PRIVATE_NOTES_OUT_OF_RECALL_SCOPE", "private-notes is not lintable through recall_maintain; use private_notes_maintain", "validation")
		}
		if strings.TrimSpace(stringArg(lintArgs, "prefix", "")) == "" {
			lintArgs["prefix"] = ""
		}
		result, err := r.memoryLint(ctx, lintArgs)
		if err != nil {
			return nil, err
		}
		decorateRecallResult(result)
		result["recall_action"] = "lint"
		return result, nil
	case "embedding_status", "embeddings_status":
		result, err := r.memoryRequest(ctx, http.MethodGet, "/v1/embeddings/status", nil)
		if err != nil {
			return nil, err
		}
		decorateRecallResult(result)
		result["recall_action"] = "embedding_status"
		return result, nil
	case "reindex", "reindex_cards":
		payload := map[string]any{}
		if prefix := strings.TrimSpace(stringArg(args, "prefix", "")); prefix != "" {
			payload["prefix"] = prefix
		}
		if action == "reindex_cards" && payload["prefix"] == nil {
			payload["prefix"] = "cards"
		}
		result, err := r.memoryRequest(ctx, http.MethodPost, "/v1/embeddings/reindex", payload)
		if err != nil {
			return nil, err
		}
		decorateRecallResult(result)
		result["recall_action"] = action
		return result, nil
	default:
		return nil, toolErrorDetails("INVALID_RECALL_ACTION", "unsupported recall_maintain action", "validation", map[string]any{"action": action})
	}
}

func relabelRecallWriteResult(result Result) {
	if result == nil {
		return
	}
	if plan, ok := result["capture_plan"].(map[string]any); ok {
		plan["write_tool"] = "recall_write"
		if _, ok := plan["write_args"]; !ok {
			plan["write_args"] = map[string]any{"confirmed": true}
		}
	}
	delete(result, "recall_card_tool")
	delete(result, "recall_note_tool")
}

func decorateRecallResult(result Result) {
	if result == nil {
		return
	}
	result["recall_store"] = "RecallDock"
}

func copyArgs(args map[string]any) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		out[k] = v
	}
	return out
}

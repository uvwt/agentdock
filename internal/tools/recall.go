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
		result, err := r.memorySearch(ctx, recallSearchArgs(args, "cards"))
		if err != nil {
			return nil, err
		}
		decorateRecallResult(result)
		relabelRecallWriteResult(result)
		result["recall_kind"] = "card"
		return result, nil
	case "", "all", "markdown":
		result, err := r.memorySearch(ctx, recallSearchArgs(args, ""))
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
		return nil, toolError("PRIVATE_NOTES_OUT_OF_RECALL_SCOPE", "private-notes is not readable through recall_read; use private_note_manage action=read", "validation")
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
		return nil, toolError("PRIVATE_NOTES_OUT_OF_RECALL_SCOPE", "private-notes is not writable through recall_write; use private_note_manage action=write", "validation")
	}
	target := strings.ToLower(strings.TrimSpace(stringArg(args, "target", "")))
	action := strings.ToLower(strings.TrimSpace(stringArg(args, "action", "")))
	if target == "" || action == "" {
		return nil, toolErrorDetails("MISSING_RECALL_TARGET_ACTION", "recall_write requires target and action", "validation", map[string]any{"targets": []string{"card", "note", "markdown"}, "actions": []string{"plan", "create", "replace", "append", "patch", "update_fact", "diff", "delete"}})
	}

	var result Result
	var err error
	switch target {
	case "card":
		switch action {
		case "plan":
			result, err = r.memoryCardCapture(ctx, args)
		case "create":
			if boolArg(args, "confirmed", false) {
				result, err = r.memoryCardWrite(ctx, args)
			} else {
				result, err = r.memoryCardCapture(ctx, args)
			}
		default:
			return nil, invalidRecallTargetAction(target, action)
		}
	case "note", "notes":
		target = "note"
		switch action {
		case "plan":
			result, err = r.notesCapture(ctx, args)
		case "create":
			if boolArg(args, "confirmed", false) {
				result, err = r.notesWrite(ctx, args)
			} else {
				result, err = r.notesCapture(ctx, args)
			}
		default:
			return nil, invalidRecallTargetAction(target, action)
		}
	case "markdown":
		switch action {
		case "create", "replace":
			result, err = r.memoryWrite(ctx, args)
		case "append":
			result, err = r.memoryAppendNote(ctx, args)
		case "patch":
			result, err = r.memoryPatch(ctx, args)
		case "update_fact":
			result, err = r.memoryUpdateFact(ctx, args)
		case "diff":
			result, err = r.memoryDiff(ctx, args)
		case "delete":
			result, err = r.memoryDelete(ctx, args)
		default:
			return nil, invalidRecallTargetAction(target, action)
		}
	default:
		return nil, toolErrorDetails("INVALID_RECALL_TARGET", "unsupported recall_write target", "validation", map[string]any{"target": target, "allowed": []string{"card", "note", "markdown"}})
	}
	if err != nil {
		return nil, err
	}
	decorateRecallResult(result)
	relabelRecallWriteResult(result)
	result["recall_target"] = target
	result["recall_action"] = action
	return result, nil
}

func invalidRecallTargetAction(target, action string) error {
	return toolErrorDetails("INVALID_RECALL_ACTION", "unsupported recall_write action for target", "validation", map[string]any{"target": target, "action": action})
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
		listArgs, err := recallMaintainListArgs(args)
		if err != nil {
			return nil, err
		}
		result, err := r.memoryList(ctx, listArgs)
		if err != nil {
			return nil, err
		}
		decorateRecallResult(result)
		result["recall_action"] = "list"
		return result, nil
	case "lint":
		lintArgs, err := recallMaintainLintArgs(args)
		if err != nil {
			return nil, err
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

func recallSearchArgs(args map[string]any, prefix string) map[string]any {
	out := map[string]any{"query": stringArg(args, "query", "")}
	if maxResults := intArg(args, "max_results", 0); maxResults > 0 {
		out["max_results"] = maxResults
	}
	if prefix != "" {
		out["prefix"] = prefix
	}
	return out
}

func recallMaintainListArgs(args map[string]any) (map[string]any, error) {
	prefix := strings.TrimSpace(stringArg(args, "prefix", ""))
	if strings.HasPrefix(prefix, "private-notes") {
		return nil, toolError("PRIVATE_NOTES_OUT_OF_RECALL_SCOPE", "private-notes is not listable through recall_maintain; use private_note_manage action=status status_action=list", "validation")
	}
	out := map[string]any{"prefix": prefix}
	if maxEntries := intArg(args, "max_entries", 0); maxEntries > 0 {
		out["max_entries"] = maxEntries
	}
	return out, nil
}

func recallMaintainLintArgs(args map[string]any) (map[string]any, error) {
	prefix := strings.TrimSpace(stringArg(args, "prefix", ""))
	if strings.HasPrefix(prefix, "private-notes") {
		return nil, toolError("PRIVATE_NOTES_OUT_OF_RECALL_SCOPE", "private-notes is not lintable through recall_maintain; use private_note_manage action=status or action=maintain", "validation")
	}
	out := map[string]any{"prefix": prefix}
	if terms := stringSliceArg(args, "terms"); len(terms) > 0 {
		out["terms"] = terms
	}
	if _, ok := args["regex"]; ok {
		out["regex"] = boolArg(args, "regex", false)
	}
	if maxEntries := intArg(args, "max_entries", 0); maxEntries > 0 {
		out["max_entries"] = maxEntries
	}
	if maxFindings := intArg(args, "max_findings", 0); maxFindings > 0 {
		out["max_findings"] = maxFindings
	}
	return out, nil
}

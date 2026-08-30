package recall

import (
	"context"
	"net/http"
	"net/url"
	pathpkg "path"
	"strings"
)

// ContextIndex 仅供 agentdock_context 内部加载紧凑 Recall 启动索引，不作为独立模型工具暴露。
func (svc *Service) ContextIndex(ctx context.Context, maxBytes int) (Result, error) {
	result, err := svc.memoryContextIndex(ctx, maxBytes)
	if err != nil {
		return nil, err
	}
	decorateRecallResult(result)
	return result, nil
}

func (svc *Service) Search(ctx context.Context, request SearchRequest) (Result, error) {
	kind := strings.ToLower(strings.TrimSpace(request.Kind))
	prefix, excludePrefix := "", ""
	resultKind := kind
	switch kind {
	case "card", "cards":
		prefix = recallCardsPrefix
		resultKind = "card"
	case "markdown":
		excludePrefix = recallCardsPrefix
	case "", "all":
		resultKind = "all"
	default:
		return nil, toolErrorDetails("INVALID_RECALL_KIND", "unsupported recall_search kind", "validation", map[string]any{"kind": kind})
	}

	result, err := svc.memorySearch(ctx, memorySearchRequest{
		Query:         request.Query,
		Prefix:        prefix,
		ExcludePrefix: excludePrefix,
		MaxResults:    intValue(request.MaxResults, 0),
	})
	if err != nil {
		return nil, err
	}
	decorateRecallResult(result)
	decorateRecallSearchResults(result, svc.config().NexusEndpoint)
	if resultKind == "card" {
		relabelRecallWriteResult(result)
	}
	result["recall_kind"] = resultKind
	return result, nil
}

func (svc *Service) Read(ctx context.Context, request ReadRequest) (Result, error) {
	if strings.HasPrefix(strings.TrimSpace(request.Path), "private-notes/") {
		return nil, toolError("PRIVATE_NOTES_OUT_OF_RECALL_SCOPE", "private-notes is not readable through recall_read; use private_note_manage action=read", "validation")
	}
	result, err := svc.memoryRead(ctx, request)
	if err != nil {
		return nil, err
	}
	decorateRecallResult(result)
	return result, nil
}

func (svc *Service) Write(ctx context.Context, request WriteRequest) (Result, error) {
	if strings.HasPrefix(strings.TrimSpace(request.Path), "private-notes/") {
		return nil, toolError("PRIVATE_NOTES_OUT_OF_RECALL_SCOPE", "private-notes is not writable through recall_write; use private_note_manage action=write", "validation")
	}
	target := strings.ToLower(strings.TrimSpace(request.Target))
	action := strings.ToLower(strings.TrimSpace(request.Action))
	if target == "" || action == "" {
		return nil, toolErrorDetails("MISSING_RECALL_TARGET_ACTION", "recall_write requires target and action", "validation", map[string]any{"targets": []string{"card", "markdown"}, "actions": []string{"plan", "create", "replace", "append", "patch", "update_fact", "diff", "delete"}})
	}

	confirmed := boolValue(request.Confirmed, false)
	dryRun := boolValue(request.DryRun, false)
	var result Result
	var err error
	switch target {
	case "card":
		switch action {
		case "plan":
			result, err = svc.memoryCardCapture(ctx, request)
			if err == nil {
				result["dry_run"] = true
				result["confirmed"] = confirmed
			}
		case "create":
			if dryRun || !confirmed {
				result, err = svc.memoryCardCapture(ctx, request)
				if err == nil {
					result["dry_run"] = true
					result["confirmed"] = confirmed
				}
			} else {
				result, err = svc.memoryCardWrite(ctx, request)
			}
		default:
			return nil, invalidRecallTargetAction(target, action)
		}
	case "markdown":
		switch action {
		case "plan":
			writeRequest := request
			writeRequest.Overwrite = boolPtrValue(false)
			result, err = svc.memoryPreviewWrite(ctx, writeRequest)
		case "create":
			writeRequest := request
			writeRequest.Overwrite = boolPtrValue(false)
			if dryRun {
				result, err = svc.memoryPreviewWrite(ctx, writeRequest)
			} else {
				result, err = svc.memoryWrite(ctx, writeRequest)
			}
		case "replace":
			writeRequest := request
			writeRequest.Overwrite = boolPtrValue(true)
			if dryRun || !confirmed {
				result, err = svc.memoryPreviewWrite(ctx, writeRequest)
			} else {
				result, err = svc.memoryWrite(ctx, writeRequest)
			}
		case "append":
			result, err = svc.memoryAppend(ctx, request)
		case "patch":
			result, err = svc.memoryPatch(ctx, request)
		case "update_fact":
			result, err = svc.memoryUpdateFact(ctx, request)
		case "diff":
			result, err = svc.memoryDiff(ctx, request)
		case "delete":
			result, err = svc.memoryDelete(ctx, request)
		default:
			return nil, invalidRecallTargetAction(target, action)
		}
	default:
		return nil, toolErrorDetails("INVALID_RECALL_TARGET", "unsupported recall_write target", "validation", map[string]any{"target": target, "allowed": []string{"card", "markdown"}})
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

func (svc *Service) Maintain(ctx context.Context, request MaintainRequest) (Result, error) {
	action := strings.ToLower(strings.TrimSpace(request.Action))
	if action == "" {
		action = "list"
	}
	prefix := strings.TrimSpace(request.Prefix)
	if strings.HasPrefix(prefix, "private-notes") && (action == "list" || action == "lint") {
		message := "private-notes is not listable through recall_maintain; use private_note_manage action=status status_action=list"
		if action == "lint" {
			message = "private-notes is not lintable through recall_maintain; use private_note_manage action=status or action=maintain"
		}
		return nil, toolError("PRIVATE_NOTES_OUT_OF_RECALL_SCOPE", message, "validation")
	}

	switch action {
	case "list":
		result, err := svc.memoryList(ctx, memoryListRequest{Prefix: prefix, MaxEntries: intValue(request.MaxEntries, 0)})
		if err != nil {
			return nil, err
		}
		decorateRecallResult(result)
		result["recall_action"] = "list"
		return result, nil
	case "lint":
		result, err := svc.memoryLint(ctx, request)
		if err != nil {
			return nil, err
		}
		decorateRecallResult(result)
		result["recall_action"] = "lint"
		return result, nil
	case "embedding_status", "embeddings_status":
		result, err := svc.request(ctx, http.MethodGet, "/v1/embeddings/status", nil)
		if err != nil {
			return nil, err
		}
		decorateRecallResult(result)
		result["recall_action"] = "embedding_status"
		return result, nil
	case "reindex", "reindex_cards":
		payload := map[string]any{}
		if prefix != "" {
			payload["prefix"] = prefix
		}
		if action == "reindex_cards" && payload["prefix"] == nil {
			payload["prefix"] = recallCardsPrefix
		}
		result, err := svc.request(ctx, http.MethodPost, "/v1/embeddings/reindex", payload)
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
}

func decorateRecallResult(result Result) {
	if result == nil {
		return
	}
	result["recall_store"] = "NexusDock Recall"
}

// decorateRecallSearchResults 保留 Recall 原始搜索字段，只补充稳定文档标识和可打开来源 URL。
func decorateRecallSearchResults(result Result, endpoint string) {
	baseURL, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || !baseURL.IsAbs() || baseURL.Host == "" {
		return
	}
	items, _ := result["results"].([]any)
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		path, _ := item["path"].(string)
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if title, _ := item["title"].(string); strings.TrimSpace(title) == "" {
			name := pathpkg.Base(path)
			item["title"] = strings.TrimSuffix(name, pathpkg.Ext(name))
		}

		sourceURL := *baseURL
		query := sourceURL.Query()
		query.Set("path", path)
		sourceURL.RawQuery = query.Encode()
		sourceURL.Fragment = "recall/library"
		item["id"] = path
		item["url"] = sourceURL.String()
	}
}

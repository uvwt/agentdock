package recall

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/config"
)

func (svc *Service) memoryContextIndex(ctx context.Context, maxBytes int) (Result, error) {
	if maxBytes <= 0 {
		maxBytes = 3000
	}
	return svc.Request(ctx, http.MethodPost, "/v1/recall/context-index", map[string]any{
		"project":   "agentdock",
		"max_bytes": maxBytes,
	})
}

func (svc *Service) memoryList(ctx context.Context, args map[string]any) (Result, error) {
	query := url.Values{}
	if prefix := strings.TrimSpace(stringArg(args, "prefix", "")); prefix != "" {
		query.Set("prefix", BackendPath(prefix))
	}
	if maxEntries := intArg(args, "max_entries", 0); maxEntries > 0 {
		query.Set("max_entries", fmt.Sprint(maxEntries))
	}
	endpoint := "/v1/recall"
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	return svc.Request(ctx, http.MethodGet, endpoint, nil)
}

func (svc *Service) memoryRead(ctx context.Context, args map[string]any) (Result, error) {
	p := strings.TrimSpace(stringArg(args, "path", ""))
	if p == "" {
		return nil, toolError("MISSING_PATH", "path is required", "validation")
	}
	result, err := svc.Request(ctx, http.MethodGet, "/v1/recall/"+escapeMemoryPath(BackendPath(p)), nil)
	if err != nil {
		return nil, err
	}
	if memory, ok := result["recall"].(map[string]any); ok {
		compactedMemory := make(map[string]any, len(memory))
		for key, value := range memory {
			compactedMemory[key] = value
		}

		// NexusDock Recall 返回的 content 与 body 会重复占用上下文；recall_read 的主流程
		// 直接展示这个取舍：默认只保留轻量字段，只有 include_raw=true 才暴露原文。
		content, hasContent := compactedMemory["content"]
		rawContent, hasRawContent := compactedMemory["raw_content"]
		delete(compactedMemory, "content")
		delete(compactedMemory, "raw_content")
		if boolArg(args, "include_raw", false) {
			if hasContent {
				compactedMemory["raw_content"] = content
			} else if hasRawContent {
				compactedMemory["raw_content"] = rawContent
			}
		}
		result["recall"] = compactedMemory
	}
	return result, nil
}

func (svc *Service) memorySearch(ctx context.Context, args map[string]any) (Result, error) {
	query := strings.TrimSpace(stringArg(args, "query", ""))
	if query == "" {
		return nil, toolError("MISSING_QUERY", "query is required", "validation")
	}
	maxResults := intArg(args, "max_results", 8)
	if maxResults <= 0 {
		maxResults = 8
	}
	payload := map[string]any{"query": query, "max_results": maxResults}
	if prefix := strings.TrimSpace(stringArg(args, "prefix", "")); prefix != "" {
		payload["prefix"] = BackendPath(prefix)
	}
	if excludePrefix := strings.TrimSpace(stringArg(args, "exclude_prefix", "")); excludePrefix != "" {
		payload["exclude_prefix"] = BackendPath(excludePrefix)
	}
	return svc.Request(ctx, http.MethodPost, "/v1/recall/search", payload)
}

func (svc *Service) memoryWrite(ctx context.Context, args map[string]any) (Result, error) {
	payload, err := memoryWritePayload(args)
	if err != nil {
		return nil, err
	}
	return svc.Request(ctx, http.MethodPost, "/v1/recall", payload)
}

func (svc *Service) memoryPreviewWrite(ctx context.Context, args map[string]any) (Result, error) {
	payload, err := memoryWritePayload(args)
	if err != nil {
		return nil, err
	}
	return svc.Request(ctx, http.MethodPost, "/v1/recall/preview", payload)
}

func memoryWritePayload(args map[string]any) (map[string]any, error) {
	content := stringArg(args, "content", "")
	if strings.TrimSpace(content) == "" {
		return nil, toolError("MISSING_CONTENT", "content is required", "validation")
	}
	payload := map[string]any{"content": content}
	copyMemoryString(args, payload, "path")
	if p, ok := payload["path"].(string); ok {
		payload["path"] = BackendPath(p)
	}
	copyMemoryString(args, payload, "type")
	copyMemoryString(args, payload, "scope")
	copyMemoryString(args, payload, "project")
	copyMemoryString(args, payload, "source")
	copyMemoryString(args, payload, "confidence")
	if tags := stringSliceArg(args, "tags"); len(tags) > 0 {
		payload["tags"] = tags
	}
	if _, ok := args["confirmed"]; ok {
		payload["confirmed"] = boolArg(args, "confirmed", false)
	}
	if _, ok := args["overwrite"]; ok {
		payload["overwrite"] = boolArg(args, "overwrite", false)
	}
	return payload, nil
}

func (svc *Service) memoryDelete(ctx context.Context, args map[string]any) (Result, error) {
	p := strings.TrimSpace(stringArg(args, "path", ""))
	if p == "" {
		return nil, toolError("MISSING_PATH", "path is required", "validation")
	}
	if !boolArg(args, "confirmed", false) {
		return nil, toolError("CONFIRMATION_REQUIRED", "recall delete requires confirmed=true", "validation")
	}
	query := url.Values{}
	query.Set("confirmed", "true")
	endpoint := "/v1/recall/" + escapeMemoryPath(BackendPath(p))
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	return svc.Request(ctx, http.MethodDelete, endpoint, nil)
}

const recallEmbeddingReindexTimeout = 180 * time.Second

func RecallRequestTimeout(endpoint string) time.Duration {
	if endpoint == "/v1/embeddings/reindex" {
		// BGE-M3 全量重建会随卡片数量和文本长度增长，使用与 NexusDock 服务端一致的长操作窗口。
		return recallEmbeddingReindexTimeout
	}
	return time.Duration(config.RecallTimeoutMS) * time.Millisecond
}

func (svc *Service) Request(ctx context.Context, method, endpoint string, payload any) (Result, error) {
	base := strings.TrimRight(strings.TrimSpace(svc.config().NexusEndpoint), "/")
	if base == "" {
		return nil, toolError("RECALL_NOT_CONFIGURED", "pair this AgentDock device with NexusDock to use Recall", "configuration")
	}
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	requestCtx, cancel := context.WithTimeout(ctx, RecallRequestTimeout(endpoint))
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, method, base+endpoint, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	token := strings.TrimSpace(svc.config().NexusDeviceToken)
	if token == "" {
		return nil, toolError("RECALL_NOT_CONFIGURED", "NexusDock Device Token is unavailable; pair this AgentDock device again", "configuration")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, toolErrorCause("RECALL_REQUEST_FAILED", err.Error(), "network", map[string]any{"endpoint": endpoint}, err)
	}
	defer resp.Body.Close()
	data, err := readBoundedBody(resp.Body, 2*1024*1024)
	if err != nil {
		return nil, toolErrorCause("RECALL_RESPONSE_TOO_LARGE", err.Error(), "network", map[string]any{"status": resp.StatusCode}, err)
	}
	var parsed map[string]any
	if len(data) > 0 {
		if err := json.Unmarshal(data, &parsed); err != nil {
			return nil, toolErrorCause("RECALL_INVALID_RESPONSE", err.Error(), "network", map[string]any{"status": resp.StatusCode, "response_bytes": len(data)}, err)
		}
	} else {
		parsed = map[string]any{}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := resp.Status
		if errObj, ok := parsed["error"].(map[string]any); ok {
			if msg, ok := errObj["message"].(string); ok && msg != "" {
				message = msg
			}
		}
		return nil, toolErrorDetails("RECALL_HTTP_ERROR", message, "network", map[string]any{"status": resp.StatusCode, "response": parsed})
	}
	delete(parsed, "ok")
	parsed["recall_endpoint"] = base
	if mapped, ok := recallMapResponsePaths(parsed).(map[string]any); ok {
		return Result(mapped), nil
	}
	return Result(parsed), nil
}

func copyMemoryString(src, dst map[string]any, key string) {
	if value := strings.TrimSpace(stringArg(src, key, "")); value != "" {
		dst[key] = value
	}
}

func escapeMemoryPath(value string) string {
	parts := strings.Split(path.Clean(strings.TrimPrefix(value, "/")), "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

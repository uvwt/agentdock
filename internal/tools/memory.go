package tools

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
)

func (r *Runtime) memoryBootstrap(ctx context.Context, args map[string]any) (Result, error) {
	project := strings.TrimSpace(stringArg(args, "project", ""))
	if project == "" {
		project = "agentdock"
	}
	maxBytes := intArg(args, "max_bytes", 12000)
	if maxBytes <= 0 {
		maxBytes = 12000
	}
	payload := map[string]any{"project": project, "max_bytes": maxBytes}
	result, err := r.memoryRequest(ctx, http.MethodPost, "/v1/recall/pack", payload)
	if err != nil {
		return nil, err
	}
	includeRaw := boolArg(args, "include_raw", false)
	includeBody := boolArg(args, "include_body", false)
	if sections, ok := result["sections"].([]any); ok {
		compactedSections := make([]any, 0, len(sections))
		for _, section := range sections {
			memory, ok := section.(map[string]any)
			if !ok {
				compactedSections = append(compactedSections, section)
				continue
			}

			compactedMemory := make(map[string]any, len(memory))
			for key, value := range memory {
				compactedMemory[key] = value
			}

			// bootstrap 是每个重要任务的入口，默认应像索引而不是正文包。
			// max_bytes 只控制 RecallDock 打包预算，不应因为模型显式传了默认值就返回长正文；
			// 需要正文时用 recall_read，或明确传 include_body/include_raw。
			content, hasContent := compactedMemory["content"]
			rawContent, hasRawContent := compactedMemory["raw_content"]
			body, hasBody := compactedMemory["body"].(string)
			delete(compactedMemory, "content")
			delete(compactedMemory, "raw_content")
			if !includeBody {
				delete(compactedMemory, "body")
				if hasBody && strings.TrimSpace(body) != "" {
					compactedMemory["body_excerpt"] = firstRunes(strings.TrimSpace(body), 320)
				}
			}
			if includeRaw {
				if hasContent {
					compactedMemory["raw_content"] = content
				} else if hasRawContent {
					compactedMemory["raw_content"] = rawContent
				}
			}
			compactedSections = append(compactedSections, compactedMemory)
		}
		result["sections"] = compactedSections
	}
	if !includeBody {
		result["compact"] = true
		result["body_policy"] = "body hidden by default; use include_body=true or recall_read for full body"
	}
	result["max_bytes"] = maxBytes
	result["bootstrap"] = true
	result["recommended_use"] = "Call recall_bootstrap at the start of substantial AgentDock, project, deployment, debugging, or preference-sensitive tasks before editing files or running destructive commands."
	return result, nil
}

func firstRunes(value string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max]) + "…"
}

func (r *Runtime) memoryList(ctx context.Context, args map[string]any) (Result, error) {
	query := url.Values{}
	if prefix := strings.TrimSpace(stringArg(args, "prefix", "")); prefix != "" {
		query.Set("prefix", prefix)
	}
	if maxEntries := intArg(args, "max_entries", 0); maxEntries > 0 {
		query.Set("max_entries", fmt.Sprint(maxEntries))
	}
	endpoint := "/v1/recall"
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	return r.memoryRequest(ctx, http.MethodGet, endpoint, nil)
}

func (r *Runtime) memoryRead(ctx context.Context, args map[string]any) (Result, error) {
	p := strings.TrimSpace(stringArg(args, "path", ""))
	if p == "" {
		return nil, toolError("MISSING_PATH", "path is required", "validation")
	}
	result, err := r.memoryRequest(ctx, http.MethodGet, "/v1/recall/"+escapeMemoryPath(p), nil)
	if err != nil {
		return nil, err
	}
	if memory, ok := result["recall"].(map[string]any); ok {
		compactedMemory := make(map[string]any, len(memory))
		for key, value := range memory {
			compactedMemory[key] = value
		}

		// RecallDock 返回的 content 与 body 会重复占用上下文；recall_read 的主流程
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

func (r *Runtime) memorySearch(ctx context.Context, args map[string]any) (Result, error) {
	query := strings.TrimSpace(stringArg(args, "query", ""))
	if query == "" {
		return nil, toolError("MISSING_QUERY", "query is required", "validation")
	}
	payload := map[string]any{"query": query}
	if prefix := strings.TrimSpace(stringArg(args, "prefix", "")); prefix != "" {
		payload["prefix"] = prefix
	}
	if maxResults := intArg(args, "max_results", 0); maxResults > 0 {
		payload["max_results"] = maxResults
	}
	return r.memoryRequest(ctx, http.MethodPost, "/v1/recall/search", payload)
}

func (r *Runtime) memoryAppendNote(ctx context.Context, args map[string]any) (Result, error) {
	content := strings.TrimSpace(stringArg(args, "content", ""))
	if content == "" {
		return nil, toolError("MISSING_CONTENT", "content is required", "validation")
	}
	payload := map[string]any{"content": content}
	if scope := strings.TrimSpace(stringArg(args, "scope", "")); scope != "" {
		payload["scope"] = scope
	}
	if name := strings.TrimSpace(stringArg(args, "name", "")); name != "" {
		payload["name"] = name
	}
	return r.memoryRequest(ctx, http.MethodPost, "/v1/recall/notes/append", payload)
}

func (r *Runtime) memoryWrite(ctx context.Context, args map[string]any) (Result, error) {
	content := stringArg(args, "content", "")
	if strings.TrimSpace(content) == "" {
		return nil, toolError("MISSING_CONTENT", "content is required", "validation")
	}
	payload := map[string]any{"content": content}
	copyMemoryString(args, payload, "path")
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
	return r.memoryRequest(ctx, http.MethodPost, "/v1/recall", payload)
}

func (r *Runtime) memoryDelete(ctx context.Context, args map[string]any) (Result, error) {
	p := strings.TrimSpace(stringArg(args, "path", ""))
	if p == "" {
		return nil, toolError("MISSING_PATH", "path is required", "validation")
	}
	query := url.Values{}
	if boolArg(args, "confirmed", false) {
		query.Set("confirmed", "true")
	}
	endpoint := "/v1/recall/" + escapeMemoryPath(p)
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	return r.memoryRequest(ctx, http.MethodDelete, endpoint, nil)
}

func (r *Runtime) memoryRequest(ctx context.Context, method, endpoint string, payload any) (Result, error) {
	base := strings.TrimRight(strings.TrimSpace(r.cfg.RecallEndpoint), "/")
	if base == "" {
		return nil, toolError("RECALL_NOT_CONFIGURED", "AGENTDOCK_RECALL_ENDPOINT is not configured", "configuration")
	}
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(r.cfg.RecallTimeoutMS)*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, method, base+endpoint, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if user, value := strings.TrimSpace(r.cfg.RecallLoginUser), r.cfg.RecallLoginValue; user != "" || value != "" {
		req.SetBasicAuth(user, value)
	}
	if token := strings.TrimSpace(r.cfg.RecallToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, toolErrorDetails("RECALL_REQUEST_FAILED", err.Error(), "network", map[string]any{"endpoint": endpoint})
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}
	var parsed map[string]any
	if len(data) > 0 {
		if err := json.Unmarshal(data, &parsed); err != nil {
			return nil, toolErrorDetails("RECALL_INVALID_RESPONSE", err.Error(), "network", map[string]any{"status": resp.StatusCode, "body": string(data)})
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
	if _, ok := parsed["ok"]; !ok {
		parsed["ok"] = true
	}
	parsed["recall_endpoint"] = base
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

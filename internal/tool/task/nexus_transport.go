package task

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/nexusclient"
)

func (s *Service) nexusWorkflowJSON(ctx context.Context, method, path string, payload any) (Result, error) {
	cfg := s.config()
	base := strings.TrimRight(strings.TrimSpace(cfg.NexusEndpoint), "/")
	if base == "" {
		return nil, toolErrorDetails("NEXUS_NOT_CONFIGURED", "pair this AgentDock device with NexusDock to use workflow_template_manage", "configuration", map[string]any{"retryable": false})
	}
	var body []byte
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, taskToolError(err)
		}
		body = data
	}
	token := strings.TrimSpace(cfg.NexusDeviceToken)
	if token == "" {
		return nil, toolErrorDetails("NEXUS_NOT_CONFIGURED", "NexusDock Device Token is unavailable; pair this AgentDock device again", "configuration", map[string]any{"retryable": false})
	}
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client := nexusclient.New(base, token)
	resp, err := client.Do(requestCtx, method, path, body)
	if err != nil {
		return nil, toolErrorCause("NEXUS_REQUEST_FAILED", err.Error(), "network", map[string]any{"retryable": true}, err)
	}
	defer resp.Body.Close()
	data, err := nexusclient.ReadBoundedBody(resp.Body, 2*1024*1024)
	if err != nil {
		return nil, toolErrorCause("NEXUS_RESPONSE_BODY_INVALID", err.Error(), "response", map[string]any{"status": resp.StatusCode}, err)
	}
	var result Result
	trimmedBody := strings.TrimSpace(string(data))
	if trimmedBody != "" {
		if err := json.Unmarshal(data, &result); err != nil {
			details := map[string]any{
				"status":           resp.StatusCode,
				"response_bytes":   len(data),
				"response_preview": truncateString(trimmedBody, 500),
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return nil, toolErrorCause("NEXUS_WORKFLOW_ERROR", resp.Status, "nexus", details, err)
			}
			return nil, toolErrorCause("NEXUS_INVALID_RESPONSE", err.Error(), "response", details, err)
		}
	} else {
		result = Result{}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := resp.Status
		if errMap, ok := result["error"].(map[string]any); ok {
			if msg, ok := errMap["message"].(string); ok && msg != "" {
				message = msg
			}
		} else if msg, ok := result["message"].(string); ok && msg != "" {
			message = msg
		}
		return nil, toolErrorDetails("NEXUS_WORKFLOW_ERROR", message, "nexus", map[string]any{"status": resp.StatusCode})
	}
	if result == nil {
		result = Result{}
	}
	for key, value := range result {
		result[key] = normalizeJSONToolValue(value)
	}
	result["nexus_endpoint"] = base
	return result, nil
}

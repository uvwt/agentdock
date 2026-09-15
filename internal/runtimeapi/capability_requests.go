package runtimeapi

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/uvwt/agentdock/internal/app"
)

type runtimeSkillManageRequest struct {
	Action string `json:"action"`
	Skill  string `json:"skill"`
}

func decodeRuntimeSkillRequest(body []byte) (map[string]any, error) {
	if len(body) > 8*1024 {
		return nil, runtimeCapabilityRequestError("INVALID_SKILL_REQUEST", "Skill request body is too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var request runtimeSkillManageRequest
	if err := decoder.Decode(&request); err != nil {
		return nil, runtimeCapabilityRequestError("INVALID_SKILL_REQUEST", "invalid Skill request body")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, runtimeCapabilityRequestError("INVALID_SKILL_REQUEST", "request body must contain exactly one JSON value")
	}
	action := strings.ToLower(strings.TrimSpace(request.Action))
	if action != "enable" && action != "disable" {
		return nil, runtimeCapabilityRequestError("SKILL_ACTION_UNSUPPORTED", "Runtime API only supports enable or disable for Skills")
	}
	if strings.TrimSpace(request.Skill) == "" {
		return nil, runtimeCapabilityRequestError("SKILL_NAME_REQUIRED", "Skill name is required")
	}
	return map[string]any{"action": action, "skill": request.Skill}, nil
}

type runtimePluginManageRequest struct {
	Action      string   `json:"action"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Enabled     *bool    `json:"enabled"`
	Skills      []string `json:"skills"`
	MCPServers  []string `json:"mcp_servers"`
}

var runtimePluginManageActions = map[string]bool{
	"upsert": true, "remove": true, "enable": true, "disable": true,
}

func decodeRuntimePluginRequest(body []byte) (map[string]any, error) {
	if len(body) > 64*1024 {
		return nil, runtimeCapabilityRequestError("INVALID_PLUGIN_REQUEST", "plugin request body is too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var request runtimePluginManageRequest
	if err := decoder.Decode(&request); err != nil {
		return nil, runtimeCapabilityRequestError("INVALID_PLUGIN_REQUEST", "invalid plugin request body")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, runtimeCapabilityRequestError("INVALID_PLUGIN_REQUEST", "request body must contain exactly one JSON value")
	}
	action := strings.ToLower(strings.TrimSpace(request.Action))
	if !runtimePluginManageActions[action] {
		return nil, runtimeCapabilityRequestError("PLUGIN_ACTION_UNSUPPORTED", "plugin action is not available through the Runtime API")
	}
	args := map[string]any{"action": action}
	if request.Name != "" {
		args["name"] = request.Name
	}
	if request.Description != "" {
		args["description"] = request.Description
	}
	if request.Enabled != nil {
		args["enabled"] = *request.Enabled
	}
	if request.Skills != nil {
		args["skills"] = request.Skills
	}
	if request.MCPServers != nil {
		args["mcp_servers"] = request.MCPServers
	}
	return args, nil
}

func runtimePluginName(path string) (string, bool) {
	const prefix = "/internal/runtime/plugins/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	name := strings.TrimSpace(strings.TrimPrefix(path, prefix))
	if name == "" || strings.Contains(name, "/") {
		return "", false
	}
	return name, true
}

func runtimeCapabilityRequestError(code, message string) error {
	return &app.ToolError{Code: code, Message: message, Category: "validation"}
}

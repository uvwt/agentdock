package runtimeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/uvwt/agentdock/internal/app"
)

// MethodAllowed 返回指定 Runtime API 路径允许当前方法与否。
// HTTP 入口用它生成 405，其他传输也复用同一份方法契约。
func MethodAllowed(method, path string) bool {
	method = strings.ToUpper(strings.TrimSpace(method))
	cleanPath := strings.TrimSuffix(strings.TrimSpace(path), "/")
	if method == http.MethodGet {
		return true
	}
	if method == http.MethodDelete {
		_, ok := runtimeTaskID(cleanPath)
		return ok
	}
	return method == http.MethodPost && (cleanPath == "/internal/runtime/capabilities" || cleanPath == "/internal/runtime/mcp" || cleanPath == "/internal/runtime/evolve")
}

func AllowHeader(path string) string {
	cleanPath := strings.TrimSuffix(strings.TrimSpace(path), "/")
	if _, ok := runtimeTaskID(cleanPath); ok {
		return "GET, DELETE"
	}
	if cleanPath == "/internal/runtime/capabilities" || cleanPath == "/internal/runtime/mcp" {
		return "GET, POST"
	}
	if cleanPath == "/internal/runtime/evolve" {
		return "POST"
	}
	return "GET"
}

// Dispatch 只负责 Runtime API 的路由、参数校验与应用能力调用，不感知 HTTP request/response。
func Dispatch(ctx context.Context, runtime Runtime, request Request) (map[string]any, error) {
	method := strings.ToUpper(strings.TrimSpace(request.Method))
	path := strings.TrimSuffix(strings.TrimSpace(request.Path), "/")
	if !MethodAllowed(method, path) {
		return nil, &app.ToolError{Code: "NOT_FOUND", Message: "runtime API route not found", Category: "not_found"}
	}

	taskID, isTaskPath := runtimeTaskID(path)
	switch {
	case path == "/internal/runtime/status":
		return map[string]any(runtime.RuntimeStatus()), nil
	case path == "/internal/runtime/capabilities":
		refresh := strings.EqualFold(request.queryValue("refresh"), "true") || method == http.MethodPost
		result, err := runtime.RuntimeCapabilities(ctx, refresh)
		return map[string]any(result), err
	case path == "/internal/runtime/skills":
		result, err := runtime.RuntimeSkills()
		return map[string]any(result), err
	case strings.HasPrefix(path, "/internal/runtime/skills/"):
		skill, filePath, action, ok := runtimeSkillRoute(path)
		if !ok {
			return nil, &app.ToolError{Code: "NOT_FOUND", Message: "runtime Skill API route not found", Category: "not_found"}
		}
		switch action {
		case "detail":
			result, err := runtime.RuntimeSkill(skill)
			return map[string]any(result), err
		case "files":
			result, err := runtime.RuntimeSkillFiles(skill)
			return map[string]any(result), err
		case "file":
			result, err := runtime.RuntimeSkillFile(skill, filePath)
			return map[string]any(result), err
		default:
			return nil, &app.ToolError{Code: "NOT_FOUND", Message: "runtime Skill API route not found", Category: "not_found"}
		}
	case path == "/internal/runtime/evolve" && method == http.MethodPost:
		args, err := decodeRuntimeEvolutionRequest(request.Body)
		if err != nil {
			return nil, err
		}
		result, err := runtime.RuntimeEvolve(ctx, args)
		return map[string]any(result), err
	case path == "/internal/runtime/mcp" && method == http.MethodPost:
		args, err := decodeRuntimeMCPRequest(request.Body)
		if err != nil {
			return nil, err
		}
		result, err := runtime.RuntimeMCPManage(ctx, args)
		return map[string]any(result), err
	case path == "/internal/runtime/mcp":
		result, err := runtime.RuntimeMCPServers(ctx)
		return map[string]any(result), err
	case strings.HasPrefix(path, "/internal/runtime/mcp/"):
		name, ok := runtimeMCPName(path)
		if !ok {
			return nil, &app.ToolError{Code: "MCP_NAME_REQUIRED", Message: "dynamic MCP server name is required", Category: "validation"}
		}
		result, err := runtime.RuntimeMCPServer(ctx, name)
		return map[string]any(result), err
	case path == "/internal/runtime/tasks":
		limit, err := parseRuntimeTaskLimit(request.queryValue("limit"))
		if err != nil {
			return nil, err
		}
		result, err := runtime.RuntimeTasks(request.queryValue("status"), limit)
		return map[string]any(result), err
	case isTaskPath && method == http.MethodDelete:
		result, err := runtime.RuntimeTaskDelete(taskID)
		return map[string]any(result), err
	case isTaskPath:
		result, err := runtime.RuntimeTask(taskID)
		return map[string]any(result), err
	default:
		return nil, &app.ToolError{Code: "NOT_FOUND", Message: "runtime API route not found", Category: "not_found"}
	}
}

type runtimeMCPRequest struct {
	Action      string            `json:"action"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Transport   string            `json:"transport"`
	URL         string            `json:"url"`
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	Cwd         string            `json:"cwd"`
	HeaderEnv   map[string]string `json:"header_env"`
	EnvFromEnv  map[string]string `json:"env_from_env"`
	Enabled     *bool             `json:"enabled"`
	TimeoutMS   int               `json:"timeout_ms"`
	Key         string            `json:"key"`
	Value       *string           `json:"value"`
}

var runtimeMCPManageActions = map[string]bool{
	"add": true, "remove": true, "enable": true, "disable": true,
	"env_set": true, "env_unset": true, "env_list": true, "refresh": true,
}

func decodeRuntimeMCPRequest(body []byte) (map[string]any, error) {
	if len(body) > 64*1024 {
		return nil, runtimeMCPRequestError("MCP request body is too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var request runtimeMCPRequest
	if err := decoder.Decode(&request); err != nil {
		return nil, runtimeMCPRequestError("invalid MCP request body")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, runtimeMCPRequestError("request body must contain exactly one JSON value")
	}
	action := strings.ToLower(strings.TrimSpace(request.Action))
	if action == "" {
		return nil, &app.ToolError{Code: "MCP_ACTION_REQUIRED", Message: "dynamic MCP action is required", Category: "validation"}
	}
	if !runtimeMCPManageActions[action] {
		return nil, &app.ToolError{Code: "MCP_ACTION_UNSUPPORTED", Message: "dynamic MCP action is not available through the Runtime API", Category: "validation"}
	}
	args := map[string]any{"action": action}
	// Runtime API 与模型工具最终进入同一份公共契约。只转发请求中真正提供的可选字段，
	// 避免 Go 零值被解释成 schema 中有语义的空 enum 值或未声明允许的 null。
	if request.Name != "" {
		args["name"] = request.Name
	}
	if request.Description != "" {
		args["description"] = request.Description
	}
	if request.Transport != "" {
		args["transport"] = request.Transport
	}
	if request.URL != "" {
		args["url"] = request.URL
	}
	if request.Command != "" {
		args["command"] = request.Command
	}
	if request.Cwd != "" {
		args["cwd"] = request.Cwd
	}
	if request.Key != "" {
		args["key"] = request.Key
	}
	if request.Args != nil {
		args["args"] = request.Args
	}
	if request.HeaderEnv != nil {
		args["header_env"] = request.HeaderEnv
	}
	if request.EnvFromEnv != nil {
		args["env_from_env"] = request.EnvFromEnv
	}
	if request.Value != nil {
		args["value"] = *request.Value
	}
	if request.Enabled != nil {
		args["enabled"] = *request.Enabled
	}
	if request.TimeoutMS > 0 {
		args["timeout_ms"] = request.TimeoutMS
	}
	return args, nil
}

func runtimeMCPRequestError(message string) error {
	return &app.ToolError{Code: "INVALID_MCP_REQUEST", Message: message, Category: "validation"}
}

func runtimeSkillRoute(path string) (skill, filePath, action string, ok bool) {
	const prefix = "/internal/runtime/skills/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", "", false
	}
	rest := strings.TrimPrefix(path, prefix)
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return "", "", "", false
	}
	skill = parts[0]
	switch {
	case len(parts) == 1:
		return skill, "", "detail", true
	case len(parts) == 2 && parts[1] == "files":
		return skill, "", "files", true
	case len(parts) >= 3 && parts[1] == "files":
		filePath = strings.Join(parts[2:], "/")
		if strings.TrimSpace(filePath) == "" {
			return "", "", "", false
		}
		return skill, filePath, "file", true
	default:
		return "", "", "", false
	}
}

func runtimeMCPName(path string) (string, bool) {
	const prefix = "/internal/runtime/mcp/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	name := strings.TrimSpace(strings.TrimPrefix(path, prefix))
	if name == "" || strings.Contains(name, "/") {
		return "", false
	}
	return name, true
}

func runtimeTaskID(path string) (string, bool) {
	const prefix = "/internal/runtime/tasks/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	id := strings.TrimPrefix(path, prefix)
	if strings.TrimSpace(id) == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

func parseRuntimeTaskLimit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 || limit > 200 {
		return 0, &app.ToolError{
			Code: "INVALID_LIMIT", Message: "limit must be an integer between 0 and 200", Category: "validation",
			Details: map[string]any{"limit": raw, "minimum": 0, "maximum": 200},
		}
	}
	return limit, nil
}

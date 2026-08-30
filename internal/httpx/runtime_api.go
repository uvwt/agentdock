package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/app"
	"github.com/uvwt/agentdock/internal/auth"
	"github.com/uvwt/agentdock/internal/config"
)

type RuntimeAPI interface {
	RuntimeStatus() app.Result
	RuntimeSkills() (app.Result, error)
	RuntimeSkill(skill string) (app.Result, error)
	RuntimeSkillFiles(skill string) (app.Result, error)
	RuntimeSkillFile(skill, path string) (app.Result, error)
	RuntimeTasks(status string, limit int) (app.Result, error)
	RuntimeTask(id string) (app.Result, error)
	RuntimeTaskDelete(id string) (app.Result, error)
	RuntimeCapabilities(context.Context, bool) (app.Result, error)
	RuntimeMCPServers(context.Context) (app.Result, error)
	RuntimeMCPServer(context.Context, string) (app.Result, error)
	RuntimeMCPManage(context.Context, map[string]any) (app.Result, error)
	RuntimeEvolve(context.Context, map[string]any) (app.Result, error)
}

type RuntimeBridgeRequest struct {
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Query  url.Values      `json:"query,omitempty"`
	Body   json.RawMessage `json:"body,omitempty"`
}

func DispatchRuntimeBridgeRequest(ctx context.Context, runtime RuntimeAPI, request RuntimeBridgeRequest) (map[string]any, error) {
	method := strings.ToUpper(strings.TrimSpace(request.Method))
	path := strings.TrimSpace(request.Path)
	if !strings.HasPrefix(path, "/internal/runtime/") || !runtimeAPIMethodAllowed(method, path) {
		return nil, &app.ToolError{Code: "NOT_FOUND", Message: "runtime API route not found", Category: "not_found"}
	}
	if len(request.Body) > 64*1024 {
		return nil, &app.ToolError{Code: "INVALID_ARGUMENT", Message: "runtime request body is too large", Category: "validation"}
	}
	parsed := &url.URL{Path: path, RawQuery: request.Query.Encode()}
	httpRequest := &http.Request{
		Method: method,
		URL:    parsed,
		Body:   io.NopCloser(bytes.NewReader(request.Body)),
	}
	return dispatchRuntimeAPI(ctx, runtime, httpRequest)
}

func registerRuntimeAPI(mux *http.ServeMux, runtime RuntimeAPI, cfg config.Config, oauthStore *auth.OAuthStore) {
	h := runtimeAPIHandler(runtime, cfg, oauthStore)
	mux.HandleFunc("/internal/runtime/status", h)
	mux.HandleFunc("/internal/runtime/capabilities", h)
	mux.HandleFunc("/internal/runtime/skills", h)
	mux.HandleFunc("/internal/runtime/skills/", h)
	mux.HandleFunc("/internal/runtime/tasks", h)
	mux.HandleFunc("/internal/runtime/tasks/", h)
	mux.HandleFunc("/internal/runtime/evolve", h)
	mux.HandleFunc("/internal/runtime/mcp", h)
	mux.HandleFunc("/internal/runtime/mcp/", h)
}

func runtimeAPIHandler(runtime RuntimeAPI, cfg config.Config, oauthStore *auth.OAuthStore) http.HandlerFunc {
	authorizer := auth.Bearer{Token: cfg.AuthToken}
	authRequired := cfg.AuthRequired()
	return func(w http.ResponseWriter, r *http.Request) {
		if !runtimeAPIMethodAllowed(r.Method, r.URL.Path) {
			w.Header().Set("Allow", runtimeAPIAllowHeader(r.URL.Path))
			writeRuntimeAPIError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
			return
		}
		staticOK := cfg.AuthToken != "" && authorizer.Authorized(r)
		oauthOK := authorizedOAuth(r, cfg, oauthStore)
		if authRequired && !staticOK && !oauthOK {
			setBearerChallenge(w, cfg, r, strings.TrimSpace(r.Header.Get("Authorization")) != "")
			writeRuntimeAPIError(w, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		result, err := dispatchRuntimeAPI(ctx, runtime, r)
		if err != nil {
			writeRuntimeAPIHandlerError(w, err)
			return
		}
		writeJSON(w, result)
	}
}

func runtimeAPIMethodAllowed(method, path string) bool {
	cleanPath := strings.TrimSuffix(path, "/")
	if method == http.MethodGet {
		return true
	}
	if method == http.MethodDelete {
		_, ok := runtimeTaskID(cleanPath)
		return ok
	}
	return method == http.MethodPost && (cleanPath == "/internal/runtime/capabilities" || cleanPath == "/internal/runtime/mcp" || cleanPath == "/internal/runtime/evolve")
}

func runtimeAPIAllowHeader(path string) string {
	cleanPath := strings.TrimSuffix(path, "/")
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

func dispatchRuntimeAPI(ctx context.Context, runtime RuntimeAPI, r *http.Request) (map[string]any, error) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	taskID, isTaskPath := runtimeTaskID(path)
	switch {
	case path == "/internal/runtime/status":
		return map[string]any(runtime.RuntimeStatus()), nil
	case path == "/internal/runtime/capabilities":
		refresh := strings.EqualFold(r.URL.Query().Get("refresh"), "true") || r.Method == http.MethodPost
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
	case path == "/internal/runtime/evolve" && r.Method == http.MethodPost:
		args, err := decodeRuntimeEvolutionRequest(r)
		if err != nil {
			return nil, err
		}
		result, err := runtime.RuntimeEvolve(ctx, args)
		return map[string]any(result), err
	case path == "/internal/runtime/mcp" && r.Method == http.MethodPost:
		args, err := decodeRuntimeMCPRequest(r)
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
		limit, err := parseRuntimeTaskLimit(r.URL.Query().Get("limit"))
		if err != nil {
			return nil, err
		}
		result, err := runtime.RuntimeTasks(r.URL.Query().Get("status"), limit)
		return map[string]any(result), err
	case isTaskPath && r.Method == http.MethodDelete:
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

func decodeRuntimeMCPRequest(r *http.Request) (map[string]any, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024+1))
	if err != nil {
		return nil, runtimeMCPRequestError("failed to read MCP request body")
	}
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

func writeRuntimeAPIHandlerError(w http.ResponseWriter, err error) {
	var toolErr *app.ToolError
	if errors.As(err, &toolErr) {
		status := http.StatusInternalServerError
		switch toolErr.Category {
		case "validation":
			status = http.StatusBadRequest
		case "not_found":
			status = http.StatusNotFound
		}
		writeRuntimeAPIError(w, status, toolErr.Code, toolErr.Message)
		return
	}
	writeRuntimeAPIError(w, http.StatusInternalServerError, "RUNTIME_API_ERROR", err.Error())
}

func writeRuntimeAPIError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]any{"ok": false, "code": code, "error": message}); err != nil {
		slog.Warn("write runtime API error response failed", "status", status, "code", code, "error", err)
	}
}

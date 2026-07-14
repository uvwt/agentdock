package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/auth"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/mcp"
	"github.com/uvwt/agentdock/internal/tools"
)

func registerRuntimeAPI(mux *http.ServeMux, server *mcp.Server, cfg config.Config, oauthStore *auth.OAuthStore) {
	h := runtimeAPIHandler(server, cfg, oauthStore)
	mux.HandleFunc("/internal/runtime/status", h)
	mux.HandleFunc("/internal/runtime/capabilities", h)
	mux.HandleFunc("/internal/runtime/skills", h)
	mux.HandleFunc("/internal/runtime/skills/", h)
	mux.HandleFunc("/internal/runtime/tasks", h)
	mux.HandleFunc("/internal/runtime/tasks/", h)
	mux.HandleFunc("/internal/runtime/mcp", h)
	mux.HandleFunc("/internal/runtime/mcp/", h)
}

func runtimeAPIHandler(server *mcp.Server, cfg config.Config, oauthStore *auth.OAuthStore) http.HandlerFunc {
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
		result, err := dispatchRuntimeAPI(ctx, server, r)
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
	return method == http.MethodPost && (cleanPath == "/internal/runtime/capabilities" || cleanPath == "/internal/runtime/mcp")
}

func runtimeAPIAllowHeader(path string) string {
	cleanPath := strings.TrimSuffix(path, "/")
	if _, ok := runtimeTaskID(cleanPath); ok {
		return "GET, DELETE"
	}
	if cleanPath == "/internal/runtime/capabilities" || cleanPath == "/internal/runtime/mcp" {
		return "GET, POST"
	}
	return "GET"
}

func dispatchRuntimeAPI(ctx context.Context, server *mcp.Server, r *http.Request) (map[string]any, error) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	taskID, isTaskPath := runtimeTaskID(path)
	switch {
	case path == "/internal/runtime/status":
		return map[string]any(server.RuntimeStatus()), nil
	case path == "/internal/runtime/capabilities":
		refresh := strings.EqualFold(r.URL.Query().Get("refresh"), "true") || r.Method == http.MethodPost
		result, err := server.RuntimeCapabilities(ctx, refresh)
		return map[string]any(result), err
	case path == "/internal/runtime/skills":
		result, err := server.RuntimeSkills()
		return map[string]any(result), err
	case strings.HasPrefix(path, "/internal/runtime/skills/"):
		skill := strings.TrimPrefix(path, "/internal/runtime/skills/")
		result, err := server.RuntimeSkill(skill)
		return map[string]any(result), err
	case path == "/internal/runtime/mcp" && r.Method == http.MethodPost:
		args, err := decodeRuntimeMCPRequest(r)
		if err != nil {
			return nil, err
		}
		result, err := server.RuntimeMCPManage(ctx, args)
		return map[string]any(result), err
	case path == "/internal/runtime/mcp":
		result, err := server.RuntimeMCPServers(ctx)
		return map[string]any(result), err
	case strings.HasPrefix(path, "/internal/runtime/mcp/"):
		name, ok := runtimeMCPName(path)
		if !ok {
			return nil, &tools.ToolError{Code: "MCP_NAME_REQUIRED", Message: "dynamic MCP server name is required", Category: "validation"}
		}
		result, err := server.RuntimeMCPServer(ctx, name)
		return map[string]any(result), err
	case path == "/internal/runtime/tasks":
		limit, err := parseRuntimeTaskLimit(r.URL.Query().Get("limit"))
		if err != nil {
			return nil, err
		}
		result, err := server.RuntimeTasks(r.URL.Query().Get("status"), limit)
		return map[string]any(result), err
	case isTaskPath && r.Method == http.MethodDelete:
		result, err := server.RuntimeTaskDelete(taskID)
		return map[string]any(result), err
	case isTaskPath:
		result, err := server.RuntimeTask(taskID)
		return map[string]any(result), err
	default:
		return nil, &tools.ToolError{Code: "NOT_FOUND", Message: "runtime API route not found", Category: "not_found"}
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
		return nil, &tools.ToolError{Code: "MCP_ACTION_REQUIRED", Message: "dynamic MCP action is required", Category: "validation"}
	}
	if !runtimeMCPManageActions[action] {
		return nil, &tools.ToolError{Code: "MCP_ACTION_UNSUPPORTED", Message: "dynamic MCP action is not available through the Runtime API", Category: "validation"}
	}
	args := map[string]any{
		"action":       action,
		"name":         request.Name,
		"description":  request.Description,
		"transport":    request.Transport,
		"url":          request.URL,
		"command":      request.Command,
		"args":         request.Args,
		"cwd":          request.Cwd,
		"header_env":   request.HeaderEnv,
		"env_from_env": request.EnvFromEnv,
		"key":          request.Key,
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
	return &tools.ToolError{Code: "INVALID_MCP_REQUEST", Message: message, Category: "validation"}
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
		return 0, &tools.ToolError{
			Code: "INVALID_LIMIT", Message: "limit must be an integer between 0 and 200", Category: "validation",
			Details: map[string]any{"limit": raw, "minimum": 0, "maximum": 200},
		}
	}
	return limit, nil
}

func writeRuntimeAPIHandlerError(w http.ResponseWriter, err error) {
	var toolErr *tools.ToolError
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

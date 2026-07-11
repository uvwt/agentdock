package httpx

import (
	"context"
	"encoding/json"
	"errors"
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

func registerRuntimeAPI(mux *http.ServeMux, server *mcp.Server, cfg config.Config) {
	h := runtimeAPIHandler(server, cfg)
	mux.HandleFunc("/internal/runtime/status", h)
	mux.HandleFunc("/internal/runtime/capabilities", h)
	mux.HandleFunc("/internal/runtime/skills", h)
	mux.HandleFunc("/internal/runtime/skills/", h)
	mux.HandleFunc("/internal/runtime/tasks", h)
	mux.HandleFunc("/internal/runtime/tasks/", h)
}

func runtimeAPIHandler(server *mcp.Server, cfg config.Config) http.HandlerFunc {
	authorizer := auth.Bearer{Token: cfg.AuthToken}
	authRequired := cfg.AuthRequired()
	return func(w http.ResponseWriter, r *http.Request) {
		if !runtimeAPIMethodAllowed(r.Method, r.URL.Path) {
			w.Header().Set("Allow", runtimeAPIAllowHeader(r.URL.Path))
			writeRuntimeAPIError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
			return
		}
		staticOK := cfg.AuthToken != "" && authorizer.Authorized(r)
		oauthOK := authorizedOAuth(r, cfg)
		if authRequired && !staticOK && !oauthOK {
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
	if method == http.MethodGet {
		return true
	}
	return method == http.MethodPost && strings.TrimSuffix(path, "/") == "/internal/runtime/capabilities"
}

func runtimeAPIAllowHeader(path string) string {
	if strings.TrimSuffix(path, "/") == "/internal/runtime/capabilities" {
		return "GET, POST"
	}
	return "GET"
}

func dispatchRuntimeAPI(ctx context.Context, server *mcp.Server, r *http.Request) (map[string]any, error) {
	path := strings.TrimSuffix(r.URL.Path, "/")
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
	case path == "/internal/runtime/tasks":
		limit, err := parseRuntimeTaskLimit(r.URL.Query().Get("limit"))
		if err != nil {
			return nil, err
		}
		result, err := server.RuntimeTasks(r.URL.Query().Get("status"), limit)
		return map[string]any(result), err
	case strings.HasPrefix(path, "/internal/runtime/tasks/"):
		id := strings.TrimPrefix(path, "/internal/runtime/tasks/")
		result, err := server.RuntimeTask(id)
		return map[string]any(result), err
	default:
		return nil, &tools.ToolError{Code: "NOT_FOUND", Message: "runtime API route not found", Category: "not_found"}
	}
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

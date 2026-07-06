package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/auth"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/mcp"
)

func registerRuntimeAPI(mux *http.ServeMux, server *mcp.Server, cfg config.Config) {
	h := runtimeAPIHandler(server, cfg)
	mux.HandleFunc("/internal/runtime/status", h)
	mux.HandleFunc("/internal/runtime/capabilities", h)
	mux.HandleFunc("/internal/runtime/skills", h)
	mux.HandleFunc("/internal/runtime/skills/", h)
	mux.HandleFunc("/internal/runtime/tasks", h)
	mux.HandleFunc("/internal/runtime/tasks/", h)
	mux.HandleFunc("/internal/runtime/workflows", h)
	mux.HandleFunc("/internal/runtime/workflows/", h)
	mux.HandleFunc("/internal/runtime/env", h)
}

func runtimeAPIHandler(server *mcp.Server, cfg config.Config) http.HandlerFunc {
	authorizer := auth.Bearer{Token: cfg.AuthToken}
	authRequired := cfg.AuthToken != "" || cfg.OAuthClientID != "" || cfg.OAuthServerURL != ""
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
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
			writeRuntimeAPIError(w, http.StatusInternalServerError, "RUNTIME_API_ERROR", err.Error())
			return
		}
		writeJSON(w, result)
	}
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
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		result, err := server.RuntimeTasks(r.URL.Query().Get("status"), limit)
		return map[string]any(result), err
	case strings.HasPrefix(path, "/internal/runtime/tasks/"):
		id := strings.TrimPrefix(path, "/internal/runtime/tasks/")
		result, err := server.RuntimeTask(id)
		return map[string]any(result), err
	case path == "/internal/runtime/workflows":
		status := firstNonEmpty(r.URL.Query().Get("status"), r.URL.Query().Get("template_status"))
		result, err := server.RuntimeTemplates(status)
		return map[string]any(result), err
	case strings.HasPrefix(path, "/internal/runtime/workflows/"):
		id, version, ok := splitTemplatePath(strings.TrimPrefix(path, "/internal/runtime/workflows/"))
		if !ok {
			return map[string]any{"ok": false, "error": "template id and version are required"}, nil
		}
		result, err := server.RuntimeTemplate(id, version)
		return map[string]any(result), err
	case path == "/internal/runtime/env":
		result, err := server.RuntimeEnv()
		return map[string]any(result), err
	default:
		return map[string]any{"ok": false, "error": "not found"}, nil
	}
}

func splitTemplatePath(value string) (string, string, bool) {
	value = strings.Trim(value, "/")
	if value == "" {
		return "", "", false
	}
	parts := strings.Split(value, "/")
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return parts[0], parts[1], true
	}
	if strings.Contains(value, "@") {
		pair := strings.SplitN(value, "@", 2)
		if pair[0] != "" && pair[1] != "" {
			return pair[0], pair[1], true
		}
	}
	return "", "", false
}

func writeRuntimeAPIError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "code": code, "error": message})
}

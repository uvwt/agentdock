package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/app"
	"github.com/uvwt/agentdock/internal/auth"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/runtimeapi"
)

func registerRuntimeAPI(mux *http.ServeMux, runtime runtimeapi.Runtime, cfg config.Config, oauthStore *auth.OAuthStore) {
	h := runtimeAPIHandler(runtime, cfg, oauthStore)
	mux.HandleFunc("/internal/runtime/status", h)
	mux.HandleFunc("/internal/runtime/analytics", h)
	mux.HandleFunc("/internal/runtime/overview", h)
	mux.HandleFunc("/internal/runtime/diagnostics", h)
	mux.HandleFunc("/internal/runtime/capabilities", h)
	mux.HandleFunc("/internal/runtime/skills", h)
	mux.HandleFunc("/internal/runtime/skills/", h)
	mux.HandleFunc("/internal/runtime/plugins", h)
	mux.HandleFunc("/internal/runtime/plugins/", h)
	mux.HandleFunc("/internal/runtime/tasks", h)
	mux.HandleFunc("/internal/runtime/tasks/", h)
	mux.HandleFunc("/internal/runtime/evolve", h)
	mux.HandleFunc(runtimeapi.ResourceLibraryPath, h)
	mux.HandleFunc("/internal/runtime/mcp", h)
	mux.HandleFunc("/internal/runtime/mcp/", h)
}

func runtimeAPIHandler(runtime runtimeapi.Runtime, cfg config.Config, oauthStore *auth.OAuthStore) http.HandlerFunc {
	authorizer := auth.Bearer{Token: cfg.AuthToken}
	authRequired := cfg.AuthRequired()
	publicURLConfigured := strings.TrimSpace(cfg.OAuthServerURL) != ""
	return func(w http.ResponseWriter, r *http.Request) {
		if !runtimeapi.MethodAllowed(r.Method, r.URL.Path) {
			w.Header().Set("Allow", runtimeapi.AllowHeader(r.URL.Path))
			writeRuntimeAPIError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
			return
		}
		if !authRequired {
			// 已配置公网 Origin（如 Quick Tunnel / 反向代理）但未启用认证时，
			// 绝不允许任何无凭证请求触达管理面，防止公网或本地回环中继越权调用。
			if publicURLConfigured {
				writeRuntimeAPIError(w, http.StatusForbidden, "AUTH_REQUIRED_FOR_PUBLIC_EXPOSURE", "runtime API requires authentication when AGENTDOCK_SERVER_URL is configured")
				return
			}
			if !isDirectLoopbackRequest(r) {
				writeRuntimeAPIError(w, http.StatusForbidden, "LOCAL_ACCESS_REQUIRED", "runtime API requires local access or authentication")
				return
			}
		} else {
			if !validateRuntimeRequestOriginAndHost(r, cfg) {
				writeRuntimeAPIError(w, http.StatusForbidden, "UNTRUSTED_ORIGIN_OR_HOST", "runtime API rejected untrusted Host or Origin")
				return
			}
			staticOK := cfg.AuthToken != "" && authorizer.Authorized(r)
			oauthOK := authorizedOAuth(r, cfg, oauthStore)
			if !staticOK && !oauthOK {
				setBearerChallenge(w, cfg, r, strings.TrimSpace(r.Header.Get("Authorization")) != "")
				writeRuntimeAPIError(w, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized")
				return
			}
		}

		if runtimeRequiresJSONContentType(r) && !hasJSONContentType(r) {
			writeRuntimeAPIError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "content-type must be application/json")
			return
		}

		body, err := runtimeRequestBody(r)
		if err != nil {
			writeRuntimeAPIError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "failed to read runtime request body")
			return
		}
		timeout := 8 * time.Second
		// install_prepare 要在同一次控制请求里下载有上限的 ZIP。沿用 Skill 下载的两分钟预算；
		// 超时、断网和摘要失败都直接返回，不转入后台补装。
		if strings.TrimSuffix(r.URL.Path, "/") == runtimeapi.ResourceLibraryPath {
			timeout = 2 * time.Minute
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		result, err := runtimeapi.Dispatch(ctx, runtime, runtimeapi.Request{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.Query(),
			Body:   body,
		})
		if err != nil {
			writeRuntimeAPIHandlerError(w, err)
			return
		}
		writeJSON(w, result)
	}
}

func runtimeRequiresJSONContentType(r *http.Request) bool {
	if r == nil || r.Method != http.MethodPost {
		return false
	}
	cleanPath := strings.TrimSuffix(r.URL.Path, "/")
	return cleanPath == "/internal/runtime/mcp" ||
		cleanPath == "/internal/runtime/mcp/oauth/callback" ||
		cleanPath == "/internal/runtime/evolve" ||
		cleanPath == runtimeapi.ResourceLibraryPath
}

func hasJSONContentType(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func runtimeRequestBody(r *http.Request) ([]byte, error) {
	if !runtimeRequiresJSONContentType(r) {
		return nil, nil
	}
	return io.ReadAll(io.LimitReader(r.Body, 64*1024+1))
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

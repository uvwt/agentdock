package httpx

import (
	"net/http"
	"strings"

	"github.com/uvwt/agentdock/internal/mcp/oauthclient"
	"github.com/uvwt/agentdock/internal/runtimeapi"
)

const mcpOAuthCallbackPath = "/oauth/mcp/callback"

func registerMCPOAuthCallback(mux *http.ServeMux, runtime runtimeapi.Runtime) {
	mux.HandleFunc(mcpOAuthCallbackPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeRuntimeAPIError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
			return
		}
		oauthRuntime, ok := runtime.(runtimeapi.MCPOAuthRuntime)
		if !ok {
			writeRuntimeAPIError(w, http.StatusNotFound, "MCP_AUTH_UNSUPPORTED", "Remote MCP OAuth is unavailable")
			return
		}
		callback, ok := parseMCPOAuthCallbackQuery(r)
		if !ok {
			writeRuntimeAPIError(w, http.StatusBadRequest, "INVALID_MCP_AUTH_CALLBACK", "invalid OAuth callback query")
			return
		}
		if err := oauthRuntime.RuntimeMCPOAuthCallback(r.Context(), callback); err != nil {
			writeRuntimeAPIHandlerError(w, err)
			return
		}

		// OAuth code/state 属于一次性凭据。响应页不回显任何 query 数据，也禁止浏览器
		// 缓存或通过 Referer 带给后续页面。
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<!doctype html><meta charset=\"utf-8\"><title>AgentDock</title><p>授权信息已收到，可以关闭此页面。</p>"))
	})
}

func parseMCPOAuthCallbackQuery(r *http.Request) (oauthclient.CallbackResult, bool) {
	if r == nil || r.URL == nil || len(r.URL.RawQuery) > 16*1024 {
		return oauthclient.CallbackResult{}, false
	}
	query := r.URL.Query()
	one := func(name string) (string, bool) {
		values, exists := query[name]
		if !exists {
			return "", true
		}
		if len(values) != 1 {
			return "", false
		}
		return strings.TrimSpace(values[0]), true
	}
	state, ok := one("state")
	if !ok || state == "" {
		return oauthclient.CallbackResult{}, false
	}
	code, ok := one("code")
	if !ok {
		return oauthclient.CallbackResult{}, false
	}
	issuer, ok := one("iss")
	if !ok {
		return oauthclient.CallbackResult{}, false
	}
	oauthError, ok := one("error")
	if !ok {
		return oauthclient.CallbackResult{}, false
	}
	description, ok := one("error_description")
	if !ok || (code == "" && oauthError == "") || (code != "" && oauthError != "") {
		return oauthclient.CallbackResult{}, false
	}
	return oauthclient.CallbackResult{
		State: state, Code: code, Issuer: issuer, Error: oauthError, ErrorDescription: description,
	}, true
}

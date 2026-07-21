package httpx

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/auth"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/mcp"
	"github.com/uvwt/agentdock/internal/tools"
)

func registerOpsAPI(mux *http.ServeMux, server *mcp.Server, cfg config.Config, oauthStore *auth.OAuthStore) {
	h := opsAPIHandler(server, cfg, oauthStore)
	mux.HandleFunc("/internal/runtime/tunnel", h)
	mux.HandleFunc("/internal/runtime/tunnel/", h)
	mux.HandleFunc("/internal/runtime/devices", h)
}

func opsAPIHandler(server *mcp.Server, cfg config.Config, oauthStore *auth.OAuthStore) http.HandlerFunc {
	authorizer := auth.Bearer{Token: cfg.AuthToken}
	authRequired := cfg.AuthRequired()
	return func(w http.ResponseWriter, r *http.Request) {
		staticOK := cfg.AuthToken != "" && authorizer.Authorized(r)
		oauthOK := authorizedOAuth(r, cfg, oauthStore)
		if authRequired && !staticOK && !oauthOK {
			setBearerChallenge(w, cfg, r, strings.TrimSpace(r.Header.Get("Authorization")) != "")
			writeRuntimeAPIError(w, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized")
			return
		}
		path := strings.TrimSuffix(r.URL.Path, "/")
		switch {
		case path == "/internal/runtime/tunnel" && r.Method == http.MethodGet:
			writeJSON(w, server.RuntimeTunnelStatus())
		case path == "/internal/runtime/tunnel/start" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			var payload struct {
				Mode           string `json:"mode"`
				CustomURL      string `json:"custom_url"`
				TunnelToken    string `json:"tunnel_token"`
				TunnelName     string `json:"tunnel_name"`
				CloudflaredBin string `json:"cloudflared_bin"`
				ClearCustomURL bool   `json:"clear_custom_url"`
			}
			_ = json.Unmarshal(body, &payload)
			// Cloudflare quick tunnels often need >20s to print the public URL.
			ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
			defer cancel()
			result, err := server.RuntimeTunnelStartOpts(ctx, tools.TunnelStartOptions{
				Mode:           payload.Mode,
				CustomURL:      payload.CustomURL,
				TunnelToken:    payload.TunnelToken,
				TunnelName:     payload.TunnelName,
				CloudflaredBin: payload.CloudflaredBin,
				ClearCustomURL: payload.ClearCustomURL,
			})
			if err != nil {
				writeJSONStatus(w, http.StatusBadRequest, result)
				return
			}
			writeJSON(w, result)
		case path == "/internal/runtime/tunnel/stop" && r.Method == http.MethodPost:
			result, err := server.RuntimeTunnelStop()
			if err != nil {
				writeRuntimeAPIHandlerError(w, err)
				return
			}
			writeJSON(w, result)
		case path == "/internal/runtime/devices" && r.Method == http.MethodGet:
			result, err := server.RuntimeDevices()
			if err != nil {
				writeRuntimeAPIHandlerError(w, err)
				return
			}
			writeJSON(w, result)
		default:
			writeRuntimeAPIError(w, http.StatusNotFound, "NOT_FOUND", "ops API route not found")
		}
	}
}

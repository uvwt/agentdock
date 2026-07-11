package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/auth"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/jsonrpc"
	"github.com/uvwt/agentdock/internal/mcp"
	"github.com/uvwt/agentdock/internal/publicartifacts"
	"github.com/uvwt/agentdock/internal/requestmeta"
)

func Serve(server *mcp.Server, cfg config.Config) error {
	authRequired := cfg.AuthRequired()
	oauthCodes := auth.NewOAuthStore()
	mux := http.NewServeMux()
	publicArtifactStore := publicartifacts.New(cfg.AgentDockHome, cfg.OAuthServerURL, cfg.Port)
	if err := publicArtifactStore.EnsureSecret(); err != nil {
		return fmt.Errorf("ensure public artifact secret: %w", err)
	}
	if err := publicArtifactStore.Cleanup(time.Now().UTC()); err != nil {
		return fmt.Errorf("clean public artifacts: %w", err)
	}
	slog.Info("http server configured", "host", cfg.Host, "port", cfg.Port, "auth_required", authRequired, "endpoint", "/mcp")
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		if _, err := io.WriteString(w, "{\"ok\":true}"); err != nil {
			slog.Warn("write health response failed", "error", err)
		}
	})
	mux.HandleFunc("/.well-known/mcp.json", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, serverCard(cfg, r))
	})
	mux.HandleFunc("/.well-known/mcp/server-card.json", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, serverCard(cfg, r))
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, oauthMetadata(cfg, r))
	})
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, oauthMetadata(cfg, r))
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		issuer := issuerFor(cfg, r)
		writeJSON(w, map[string]any{"resource": issuer + "/mcp", "authorization_servers": []string{issuer}})
	})
	mux.HandleFunc("/artifacts/public/", func(w http.ResponseWriter, r *http.Request) {
		publicArtifactStore.ServeHTTP(w, r, "/artifacts/public/")
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		handleRegister(w, r, cfg)
	})
	mux.HandleFunc("/oauth/authorize", func(w http.ResponseWriter, r *http.Request) {
		handleAuthorize(w, r, cfg, oauthCodes)
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		handleAuthorize(w, r, cfg, oauthCodes)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		handleToken(w, r, cfg, oauthCodes)
	})
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		handleToken(w, r, cfg, oauthCodes)
	})
	mux.HandleFunc("/context", agentDockContextHandler(server, cfg))
	registerRuntimeAPI(mux, server, cfg)
	mux.HandleFunc("/mcp", mcpEndpointHandler(server, cfg))

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	httpServer := newHTTPServer(addr, loggingMiddleware(mux))
	slog.Info("http server listening", "addr", addr)
	return httpServer.ListenAndServe()
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

func agentDockContextHandler(server *mcp.Server, cfg config.Config) http.HandlerFunc {
	authorizer := auth.Bearer{Token: cfg.AuthToken}
	authRequired := cfg.AuthRequired()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		staticOK := cfg.AuthToken != "" && authorizer.Authorized(r)
		oauthOK := authorizedOAuth(r, cfg)
		if authRequired && !staticOK && !oauthOK {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		result, err := server.AgentDockContext(ctx)
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, result)
	}
}

func mcpEndpointHandler(server *mcp.Server, cfg config.Config) http.HandlerFunc {
	authorizer := auth.Bearer{Token: cfg.AuthToken}
	authRequired := cfg.AuthRequired()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		staticOK := cfg.AuthToken != "" && authorizer.Authorized(r)
		oauthOK := authorizedOAuth(r, cfg)
		if authRequired && !staticOK && !oauthOK {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req jsonrpc.Request
		body := http.MaxBytesReader(w, r.Body, 1<<20)
		if err := decodeSingleJSON(body, &req); err != nil {
			writeJSON(w, jsonrpc.Failure(nil, -32700, "Parse error", err.Error()))
			return
		}
		resp := server.Dispatch(requestmeta.WithBaseURL(r.Context(), requestPublicBaseURL(cfg, r)), req)
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSON(w, resp)
	}
}

func decodeSingleJSON(reader io.Reader, target any) error {
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("request body must contain exactly one JSON value")
}

func requestPublicBaseURL(cfg config.Config, r *http.Request) string {
	configured := strings.TrimRight(strings.TrimSpace(cfg.OAuthServerURL), "/")
	if configured != "" {
		return configured
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return ""
	}
	scheme := "http"
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		candidate := strings.ToLower(strings.TrimSpace(parts[0]))
		if candidate == "http" || candidate == "https" {
			scheme = candidate
		}
	} else if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + host
}

func handleRegister(w http.ResponseWriter, r *http.Request, cfg config.Config) {
	if !cfg.OAuthEnabled() {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	method, err := requestedTokenEndpointAuthMethod(w, r)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_client_metadata", "error_description": err.Error()})
		return
	}
	writeJSONStatus(w, http.StatusCreated, map[string]any{"client_id": cfg.OAuthClientID, "token_endpoint_auth_method": method})
}

func oauthMetadata(cfg config.Config, r *http.Request) map[string]any {
	issuer := issuerFor(cfg, r)
	return map[string]any{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + "/oauth/authorize",
		"token_endpoint":                        issuer + "/oauth/token",
		"registration_endpoint":                 issuer + "/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": tokenEndpointAuthMethods(),
	}
}

func tokenEndpointAuthMethods() []string {
	if auth.ConfiguredClientSecret() == "" {
		return []string{"none"}
	}
	return []string{"client_secret_post"}
}

func issuerFor(cfg config.Config, r *http.Request) string {
	issuer := strings.TrimRight(cfg.OAuthServerURL, "/")
	if issuer != "" {
		return issuer
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func handleAuthorize(w http.ResponseWriter, r *http.Request, cfg config.Config, codes *auth.OAuthStore) {
	if !cfg.OAuthEnabled() {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
	}
	values := r.URL.Query()
	if r.Method == http.MethodPost {
		values = r.PostForm
	}
	clientID := values.Get("client_id")
	redirectURI := values.Get("redirect_uri")
	challenge := values.Get("code_challenge")
	method := values.Get("code_challenge_method")
	state := values.Get("state")
	if cfg.OAuthClientID != "" && clientID != cfg.OAuthClientID {
		http.Error(w, "invalid client_id", http.StatusBadRequest)
		return
	}
	if !validOAuthRedirectURI(redirectURI) || !auth.ValidPKCEChallenge(challenge) || method != "S256" {
		http.Error(w, "invalid oauth request", http.StatusBadRequest)
		return
	}
	loginPassword := auth.ConfiguredLoginValue()
	if loginPassword != "" && r.Method == http.MethodGet {
		writeAuthorizeForm(w, values, "")
		return
	}
	if loginPassword != "" && !auth.ConstantTimeEqual(r.FormValue("password"), loginPassword) {
		writeAuthorizeForm(w, values, "invalid password")
		return
	}
	code, err := codes.Create(auth.OAuthCode{ClientID: clientID, RedirectURI: redirectURI, Challenge: challenge, State: state})
	if err != nil {
		http.Error(w, "failed to create authorization code", http.StatusInternalServerError)
		return
	}
	location := auth.AppendQuery(redirectURI, url.Values{"code": []string{code}, "state": []string{state}})
	http.Redirect(w, r, location, http.StatusFound)
}

func handleToken(w http.ResponseWriter, r *http.Request, cfg config.Config, codes *auth.OAuthStore) {
	if !cfg.OAuthEnabled() {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if !validClientAuthentication(r, cfg) {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]any{"error": "invalid_client"})
		return
	}
	if r.FormValue("grant_type") != "authorization_code" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "unsupported_grant_type"})
		return
	}
	code, ok := codes.Consume(r.FormValue("code"))
	if !ok {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
		return
	}
	if code.RedirectURI != r.FormValue("redirect_uri") {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
		return
	}
	if postedClientID := r.FormValue("client_id"); postedClientID != "" && postedClientID != code.ClientID {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
		return
	}
	if !auth.VerifyPKCE(r.FormValue("code_verifier"), code.Challenge) {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
		return
	}
	issuer := issuerFor(cfg, r)
	token, err := auth.IssueToken(issuer, oauthSigningKey(), 30*24*time.Hour)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}
	writeJSON(w, map[string]any{"access_token": token, "token_type": "Bearer", "expires_in": 2592000})
}

func authorizedOAuth(r *http.Request, cfg config.Config) bool {
	if !cfg.OAuthEnabled() {
		return false
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	issuer := issuerFor(cfg, r)
	return auth.ValidateToken(strings.TrimPrefix(header, "Bearer "), issuer, oauthSigningKey())
}

func oauthSigningKey() string { return os.Getenv("AGENTDOCK_OAUTH_TOKEN_SECRET") }

func requestedTokenEndpointAuthMethod(w http.ResponseWriter, r *http.Request) (string, error) {
	body := http.MaxBytesReader(w, r.Body, 1<<20)
	var payload struct {
		TokenEndpointAuthMethod string `json:"token_endpoint_auth_method"`
	}
	if err := decodeSingleJSON(body, &payload); err != nil {
		return "", fmt.Errorf("decode client metadata: %w", err)
	}
	method := strings.TrimSpace(payload.TokenEndpointAuthMethod)
	if method == "" {
		method = defaultTokenEndpointAuthMethod()
	}
	if method != defaultTokenEndpointAuthMethod() {
		return "", fmt.Errorf("token_endpoint_auth_method %q is not supported", method)
	}
	return method, nil
}

func defaultTokenEndpointAuthMethod() string {
	if auth.ConfiguredClientSecret() == "" {
		return "none"
	}
	return "client_secret_post"
}

func validOAuthRedirectURI(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Opaque != "" {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return true
	case "http":
		hostname := strings.ToLower(parsed.Hostname())
		if hostname == "localhost" {
			return true
		}
		ip := net.ParseIP(hostname)
		return ip != nil && ip.IsLoopback()
	default:
		return false
	}
}

func validClientAuthentication(r *http.Request, cfg config.Config) bool {
	clientID := strings.TrimSpace(r.FormValue("client_id"))
	if clientID == "" || clientID != cfg.OAuthClientID {
		return false
	}
	if _, _, ok := r.BasicAuth(); ok {
		return false
	}
	configured := auth.ConfiguredClientSecret()
	provided := r.FormValue("client_secret")
	if configured == "" {
		return provided == ""
	}
	return auth.ConstantTimeEqual(provided, configured)
}

func writeAuthorizeForm(w http.ResponseWriter, values url.Values, errorText string) {
	w.Header().Set("content-type", "text/html; charset=utf-8")
	errBlock := ""
	if errorText != "" {
		errBlock = "<p style='color:red'>" + html.EscapeString(errorText) + "</p>"
	}
	if _, err := io.WriteString(w, "<html><body><h1>Authorize AgentDock</h1>"+errBlock+"<form method='POST'><input type='hidden' name='client_id' value='"+html.EscapeString(values.Get("client_id"))+"'><input type='hidden' name='redirect_uri' value='"+html.EscapeString(values.Get("redirect_uri"))+"'><input type='hidden' name='code_challenge' value='"+html.EscapeString(values.Get("code_challenge"))+"'><input type='hidden' name='code_challenge_method' value='"+html.EscapeString(values.Get("code_challenge_method"))+"'><input type='hidden' name='state' value='"+html.EscapeString(values.Get("state"))+"'><label>Password <input type='password' name='password'></label><button type='submit'>Authorize</button></form></body></html>"); err != nil {
		slog.Warn("write OAuth authorization form failed", "error", err)
	}
}

func serverCard(cfg config.Config, r *http.Request) map[string]any {
	issuer := issuerFor(cfg, r)
	authInfo := map[string]any{"type": "none"}
	if cfg.AuthToken != "" {
		authInfo = map[string]any{"type": "bearer", "scheme": "Bearer", "header": "Authorization"}
	}
	if cfg.OAuthEnabled() {
		authInfo = map[string]any{"type": "oauth2", "scheme": "Bearer", "header": "Authorization", "authorizationUrl": issuer + "/oauth/authorize", "tokenUrl": issuer + "/oauth/token"}
	}
	return map[string]any{"name": config.ServerName, "title": "AgentDock", "version": config.Version, "description": "Local coding tools MCP server", "transport": map[string]any{"type": "streamable-http", "url": issuer + "/mcp"}, "auth": authInfo}
}

func writeJSON(w http.ResponseWriter, value any) {
	writeJSONStatus(w, http.StatusOK, value)
}

func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Warn("write JSON response failed", "status", status, "error", err)
	}
}

package httpx

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/local/coding-tools-mcp-go/internal/auth"
	"github.com/local/coding-tools-mcp-go/internal/config"
	"github.com/local/coding-tools-mcp-go/internal/jsonrpc"
	"github.com/local/coding-tools-mcp-go/internal/mcp"
)

func Serve(server *mcp.Server, cfg config.Config) error {
	authorizer := auth.Bearer{Token: cfg.AuthToken}
	authRequired := cfg.AuthToken != "" || cfg.OAuthClientID != "" || cfg.OAuthServerURL != ""
	oauthCodes := auth.NewOAuthStore()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = io.WriteString(w, "{\"ok\":true}")
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
	mux.HandleFunc("/register", func(w http.ResponseWriter, _ *http.Request) {
		method := "none"
		if auth.ConfiguredClientSecret() != "" {
			method = "client_secret_post"
		}
		writeJSON(w, map[string]any{"client_id": firstNonEmpty(cfg.OAuthClientID, "coding-tools-client"), "token_endpoint_auth_method": method})
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
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
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
		defer r.Body.Close()
		var req jsonrpc.Request
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			writeJSON(w, jsonrpc.Failure(nil, -32700, "Parse error", err.Error()))
			return
		}
		writeJSON(w, server.Dispatch(r.Context(), req))
	})

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	httpServer := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	return httpServer.ListenAndServe()
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
		"token_endpoint_auth_methods_supported": []string{"client_secret_post", "client_secret_basic", "none"},
	}
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
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
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
	if redirectURI == "" || challenge == "" || method != "S256" {
		http.Error(w, "invalid oauth request", http.StatusBadRequest)
		return
	}
	loginPassword := auth.ConfiguredLoginValue()
	if loginPassword != "" && r.Method == http.MethodGet {
		writeAuthorizeForm(w, values, "")
		return
	}
	if loginPassword != "" && r.FormValue("password") != loginPassword {
		writeAuthorizeForm(w, values, "invalid password")
		return
	}
	code := codes.Create(auth.OAuthCode{ClientID: clientID, RedirectURI: redirectURI, Challenge: challenge, State: state})
	location := auth.AppendQuery(redirectURI, url.Values{"code": []string{code}, "state": []string{state}})
	http.Redirect(w, r, location, http.StatusFound)
}

func handleToken(w http.ResponseWriter, r *http.Request, cfg config.Config, codes *auth.OAuthStore) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if !validClientAuthentication(r) {
		writeJSON(w, map[string]any{"error": "invalid_client"})
		return
	}
	if r.FormValue("grant_type") != "authorization_code" {
		writeJSON(w, map[string]any{"error": "unsupported_grant_type"})
		return
	}
	code, ok := codes.Consume(r.FormValue("code"))
	if !ok {
		writeJSON(w, map[string]any{"error": "invalid_grant"})
		return
	}
	if code.RedirectURI != r.FormValue("redirect_uri") {
		writeJSON(w, map[string]any{"error": "invalid_grant"})
		return
	}
	if postedClientID := r.FormValue("client_id"); postedClientID != "" && postedClientID != code.ClientID {
		writeJSON(w, map[string]any{"error": "invalid_grant"})
		return
	}
	if !auth.VerifyPKCE(r.FormValue("code_verifier"), code.Challenge) {
		writeJSON(w, map[string]any{"error": "invalid_grant"})
		return
	}
	issuer := issuerFor(cfg, r)
	token := auth.IssueToken(issuer, oauthSigningKey(), 30*24*time.Hour)
	writeJSON(w, map[string]any{"access_token": token, "token_type": "Bearer", "expires_in": 2592000})
}

func authorizedOAuth(r *http.Request, cfg config.Config) bool {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	issuer := issuerFor(cfg, r)
	return auth.ValidateToken(strings.TrimPrefix(header, "Bearer "), issuer, oauthSigningKey())
}

func oauthSigningKey() string { return os.Getenv("CODING_TOOLS_MCP_OAUTH_TOKEN_SECRET") }

func validClientAuthentication(r *http.Request) bool {
	configured := auth.ConfiguredClientSecret()
	if configured == "" {
		return true
	}
	clientSecret := r.FormValue("client_secret")
	if user, password, ok := r.BasicAuth(); ok {
		_ = user
		clientSecret = password
	}
	return auth.ConstantTimeEqual(clientSecret, configured)
}

func writeAuthorizeForm(w http.ResponseWriter, values url.Values, errorText string) {
	w.Header().Set("content-type", "text/html; charset=utf-8")
	errBlock := ""
	if errorText != "" {
		errBlock = "<p style='color:red'>" + html.EscapeString(errorText) + "</p>"
	}
	_, _ = io.WriteString(w, "<html><body><h1>Authorize Coding Tools MCP</h1>"+errBlock+"<form method='POST'><input type='hidden' name='client_id' value='"+html.EscapeString(values.Get("client_id"))+"'><input type='hidden' name='redirect_uri' value='"+html.EscapeString(values.Get("redirect_uri"))+"'><input type='hidden' name='code_challenge' value='"+html.EscapeString(values.Get("code_challenge"))+"'><input type='hidden' name='code_challenge_method' value='"+html.EscapeString(values.Get("code_challenge_method"))+"'><input type='hidden' name='state' value='"+html.EscapeString(values.Get("state"))+"'><label>Password <input type='password' name='password'></label><button type='submit'>Authorize</button></form></body></html>")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func serverCard(cfg config.Config, r *http.Request) map[string]any {
	issuer := issuerFor(cfg, r)
	authInfo := map[string]any{"type": "none"}
	if cfg.AuthToken != "" {
		authInfo = map[string]any{"type": "bearer", "scheme": "Bearer", "header": "Authorization"}
	}
	if cfg.OAuthClientID != "" || cfg.OAuthServerURL != "" {
		authInfo = map[string]any{"type": "oauth2", "scheme": "Bearer", "header": "Authorization", "authorizationUrl": issuer + "/oauth/authorize", "tokenUrl": issuer + "/oauth/token"}
	}
	return map[string]any{"name": config.ServerName, "title": "Coding Tools MCP", "version": config.Version, "description": "Local coding tools MCP server", "transport": map[string]any{"type": "streamable-http", "url": issuer + "/mcp"}, "auth": authInfo}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}


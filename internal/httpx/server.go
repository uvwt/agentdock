package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/auth"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/jsonrpc"
	"github.com/uvwt/agentdock/internal/mcp"
	"github.com/uvwt/agentdock/internal/publicartifacts"
	"github.com/uvwt/agentdock/internal/requestmeta"
)

const (
	oauthAccessTokenTTL  = time.Hour
	oauthRefreshTokenTTL = 90 * 24 * time.Hour
)

func Serve(server *mcp.Server, cfg config.Config) error {
	authRequired := cfg.AuthRequired()
	oauthStore := auth.NewOAuthStore()
	if cfg.OAuthEnabled() {
		persistentStore, err := auth.NewPersistentOAuthStore(filepath.Join(cfg.AgentDockHome, "oauth", "refresh-tokens.json"))
		if err != nil {
			return fmt.Errorf("initialize OAuth token store: %w", err)
		}
		oauthStore = persistentStore
	}
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
	protectedResourceHandler := func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, protectedResourceMetadata(cfg, r))
	}
	mux.HandleFunc("/.well-known/oauth-protected-resource", protectedResourceHandler)
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", protectedResourceHandler)
	mux.HandleFunc("/mcp/.well-known/oauth-protected-resource", protectedResourceHandler)
	mux.HandleFunc("/artifacts/public/", func(w http.ResponseWriter, r *http.Request) {
		publicArtifactStore.ServeHTTP(w, r, "/artifacts/public/")
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		handleRegister(w, r, cfg, oauthStore)
	})
	mux.HandleFunc("/oauth/authorize", func(w http.ResponseWriter, r *http.Request) {
		handleAuthorize(w, r, cfg, oauthStore)
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		handleAuthorize(w, r, cfg, oauthStore)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		handleToken(w, r, cfg, oauthStore)
	})
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		handleToken(w, r, cfg, oauthStore)
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
			setBearerChallenge(w, cfg, r)
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
			setBearerChallenge(w, cfg, r)
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

func handleRegister(w http.ResponseWriter, r *http.Request, cfg config.Config, store *auth.OAuthStore) {
	if !cfg.OAuthEnabled() {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	metadata, err := decodeClientRegistration(w, r)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_client_metadata", "error_description": err.Error()})
		return
	}
	clientID, err := store.RegisterClient(metadata.ClientName, metadata.RedirectURIs, metadata.GrantTypes)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}
	response := map[string]any{
		"client_id":                  clientID,
		"client_id_issued_at":        time.Now().Unix(),
		"redirect_uris":              metadata.RedirectURIs,
		"token_endpoint_auth_method": "none",
		"grant_types":                metadata.GrantTypes,
		"response_types":             metadata.ResponseTypes,
	}
	if metadata.ClientName != "" {
		response["client_name"] = metadata.ClientName
	}
	writeJSONStatus(w, http.StatusCreated, response)
}

type clientRegistrationMetadata struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
}

func decodeClientRegistration(w http.ResponseWriter, r *http.Request) (clientRegistrationMetadata, error) {
	body := http.MaxBytesReader(w, r.Body, 1<<20)
	var metadata clientRegistrationMetadata
	if err := decodeSingleJSON(body, &metadata); err != nil {
		return metadata, fmt.Errorf("decode client metadata: %w", err)
	}
	metadata.ClientName = strings.TrimSpace(metadata.ClientName)
	if len(metadata.ClientName) > 200 {
		return metadata, errors.New("client_name exceeds 200 characters")
	}
	method := strings.TrimSpace(metadata.TokenEndpointAuthMethod)
	if method == "" {
		method = "none"
	}
	if method != "none" {
		return metadata, fmt.Errorf("token_endpoint_auth_method %q is not supported", method)
	}
	if len(metadata.RedirectURIs) == 0 || len(metadata.RedirectURIs) > 10 {
		return metadata, errors.New("redirect_uris must contain between 1 and 10 entries")
	}
	seenRedirects := make(map[string]struct{}, len(metadata.RedirectURIs))
	redirectURIs := make([]string, 0, len(metadata.RedirectURIs))
	for _, raw := range metadata.RedirectURIs {
		redirectURI := strings.TrimSpace(raw)
		if !validOAuthRedirectURI(redirectURI) {
			return metadata, fmt.Errorf("invalid redirect_uri %q", redirectURI)
		}
		if _, exists := seenRedirects[redirectURI]; exists {
			continue
		}
		seenRedirects[redirectURI] = struct{}{}
		redirectURIs = append(redirectURIs, redirectURI)
	}
	grantTypes, err := normalizeClientMetadataValues(
		metadata.GrantTypes,
		[]string{"authorization_code"},
		map[string]struct{}{"authorization_code": {}, "refresh_token": {}},
		"grant_type",
	)
	if err != nil {
		return metadata, err
	}
	if containsString(grantTypes, "refresh_token") && !containsString(grantTypes, "authorization_code") {
		return metadata, errors.New("grant_type \"refresh_token\" requires \"authorization_code\"")
	}
	responseTypes, err := normalizeClientMetadataValues(
		metadata.ResponseTypes,
		[]string{"code"},
		map[string]struct{}{"code": {}},
		"response_type",
	)
	if err != nil {
		return metadata, err
	}
	metadata.RedirectURIs = redirectURIs
	metadata.TokenEndpointAuthMethod = "none"
	metadata.GrantTypes = grantTypes
	metadata.ResponseTypes = responseTypes
	return metadata, nil
}

func normalizeClientMetadataValues(values, defaults []string, supported map[string]struct{}, label string) ([]string, error) {
	if len(values) == 0 {
		values = defaults
	}
	seen := make(map[string]struct{}, len(values))
	clean := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if _, ok := supported[value]; !ok {
			return nil, fmt.Errorf("%s %q is not supported", label, value)
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		clean = append(clean, value)
	}
	return clean, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func protectedResourceMetadata(cfg config.Config, r *http.Request) map[string]any {
	issuer := issuerFor(cfg, r)
	return map[string]any{
		"resource":                 issuer + "/mcp",
		"authorization_servers":    []string{issuer},
		"bearer_methods_supported": []string{"header"},
	}
}

func setBearerChallenge(w http.ResponseWriter, cfg config.Config, r *http.Request) {
	if cfg.OAuthEnabled() {
		metadataURL := issuerFor(cfg, r) + "/.well-known/oauth-protected-resource/mcp"
		w.Header().Set("WWW-Authenticate", "Bearer resource_metadata=\""+metadataURL+"\"")
		return
	}
	w.Header().Set("WWW-Authenticate", "Bearer")
}

func oauthMetadata(cfg config.Config, r *http.Request) map[string]any {
	issuer := issuerFor(cfg, r)
	return map[string]any{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + "/oauth/authorize",
		"token_endpoint":                        issuer + "/oauth/token",
		"registration_endpoint":                 issuer + "/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"resource_indicators_supported":         true,
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
	responseType := values.Get("response_type")
	if responseType != "code" {
		http.Error(w, "unsupported response_type", http.StatusBadRequest)
		return
	}
	if !codes.ValidateClientRedirect(clientID, redirectURI) ||
		!codes.ClientAllowsGrant(clientID, "authorization_code") {
		http.Error(w, "invalid client_id or redirect_uri", http.StatusBadRequest)
		return
	}
	if !auth.ValidPKCEChallenge(challenge) || method != "S256" {
		http.Error(w, "invalid oauth request", http.StatusBadRequest)
		return
	}
	expectedResource := issuerFor(cfg, r) + "/mcp"
	resource := strings.TrimSpace(values.Get("resource"))
	if resource == "" {
		resource = expectedResource
	}
	if resource != expectedResource {
		http.Error(w, "invalid resource", http.StatusBadRequest)
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
	code, err := codes.Create(auth.OAuthCode{
		ClientID: clientID, RedirectURI: redirectURI, Challenge: challenge, State: state, Resource: resource,
	})
	if err != nil {
		http.Error(w, "failed to create authorization code", http.StatusInternalServerError)
		return
	}
	location := auth.AppendQuery(redirectURI, url.Values{"code": []string{code}, "state": []string{state}})
	http.Redirect(w, r, location, http.StatusFound)
}

func handleToken(w http.ResponseWriter, r *http.Request, cfg config.Config, store *auth.OAuthStore) {
	if !cfg.OAuthEnabled() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	grantType := strings.TrimSpace(r.FormValue("grant_type"))
	if grantType != "authorization_code" && grantType != "refresh_token" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "unsupported_grant_type"})
		return
	}
	if !validClientAuthentication(r, grantType, store) {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]any{"error": "invalid_client"})
		return
	}
	if grantType == "refresh_token" {
		handleRefreshTokenGrant(w, r, cfg, store)
		return
	}
	handleAuthorizationCodeGrant(w, r, cfg, store)
}

func handleAuthorizationCodeGrant(w http.ResponseWriter, r *http.Request, cfg config.Config, store *auth.OAuthStore) {
	code, ok := store.Consume(r.FormValue("code"))
	if !ok {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
		return
	}
	clientID := strings.TrimSpace(r.FormValue("client_id"))
	if code.RedirectURI != r.FormValue("redirect_uri") || clientID != code.ClientID {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
		return
	}
	if !auth.VerifyPKCE(r.FormValue("code_verifier"), code.Challenge) {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
		return
	}
	resource := strings.TrimSpace(r.FormValue("resource"))
	if resource == "" {
		resource = code.Resource
	}
	if resource != code.Resource {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_target"})
		return
	}
	issuer := issuerFor(cfg, r)
	accessToken, err := auth.IssueToken(issuer, resource, oauthSigningKey(), oauthAccessTokenTTL)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}
	refreshToken := ""
	if store.ClientAllowsGrant(clientID, "refresh_token") {
		refreshToken, err = store.IssueRefreshToken(clientID, resource, oauthRefreshTokenTTL)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
			return
		}
	}
	writeOAuthTokenResponse(w, accessToken, refreshToken)
}

func handleRefreshTokenGrant(w http.ResponseWriter, r *http.Request, cfg config.Config, store *auth.OAuthStore) {
	clientID := strings.TrimSpace(r.FormValue("client_id"))
	newRefreshToken, resource, ok, err := store.RotateRefreshToken(
		r.FormValue("refresh_token"),
		clientID,
		r.FormValue("resource"),
		oauthRefreshTokenTTL,
	)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}
	if !ok {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
		return
	}
	accessToken, err := auth.IssueToken(issuerFor(cfg, r), resource, oauthSigningKey(), oauthAccessTokenTTL)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}
	writeOAuthTokenResponse(w, accessToken, newRefreshToken)
}

func writeOAuthTokenResponse(w http.ResponseWriter, accessToken, refreshToken string) {
	payload := map[string]any{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   int(oauthAccessTokenTTL / time.Second),
	}
	if refreshToken != "" {
		payload["refresh_token"] = refreshToken
	}
	writeJSON(w, payload)
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
	audience := issuer + "/mcp"
	return auth.ValidateToken(strings.TrimPrefix(header, "Bearer "), issuer, audience, oauthSigningKey())
}

func oauthSigningKey() string { return os.Getenv("AGENTDOCK_OAUTH_TOKEN_SECRET") }

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

func validClientAuthentication(r *http.Request, grantType string, store *auth.OAuthStore) bool {
	if _, _, ok := r.BasicAuth(); ok {
		return false
	}
	if r.FormValue("client_secret") != "" {
		return false
	}
	clientID := strings.TrimSpace(r.FormValue("client_id"))
	if !store.ValidateClientID(clientID) || !store.ClientAllowsGrant(clientID, grantType) {
		return false
	}
	if grantType == "authorization_code" {
		return store.ValidateClientRedirect(clientID, strings.TrimSpace(r.FormValue("redirect_uri")))
	}
	return grantType == "refresh_token"
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

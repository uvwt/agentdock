package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	oauthFormBodyLimit   = 64 << 10
)

func Serve(ctx context.Context, server *mcp.Server, cfg config.Config) error {
	authRequired := cfg.AuthRequired()
	oauthStore := auth.NewOAuthStore()
	if cfg.OAuthEnabled {
		persistentStore, err := auth.NewPersistentOAuthStore(filepath.Join(cfg.AgentDockHome, "oauth", "state-v1.json"), oauthSigningKey())
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
		writeJSON(w, map[string]any{"ok": true, "version": config.Version})
	})
	mux.HandleFunc("/.well-known/mcp.json", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, serverCard(cfg, r))
	})
	mux.HandleFunc("/.well-known/mcp/server-card.json", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, serverCard(cfg, r))
	})
	mux.HandleFunc("/artifacts/public/", func(w http.ResponseWriter, r *http.Request) {
		publicArtifactStore.ServeHTTP(w, r, "/artifacts/public/")
	})
	registerOAuthRoutes(mux, cfg, oauthStore)
	mux.HandleFunc("/context", agentDockContextHandler(server, cfg, oauthStore))
	registerRuntimeAPI(mux, server, cfg, oauthStore)
	mux.HandleFunc("/mcp", mcpEndpointHandler(server, cfg, oauthStore))

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	httpServer := newHTTPServer(addr, loggingMiddleware(mux))
	slog.Info("http server listening", "addr", addr)
	return serveHTTP(ctx, httpServer)
}

func serveHTTP(ctx context.Context, server *http.Server) error {
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func registerOAuthRoutes(mux *http.ServeMux, cfg config.Config, store *auth.OAuthStore) {
	if !cfg.OAuthEnabled {
		return
	}
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, oauthMetadata(cfg, r))
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, protectedResourceMetadata(cfg, r))
	})
	registrationLimiter := newFixedWindowLimiter(30, time.Minute)
	passwordLimiter := newFixedWindowLimiter(10, 5*time.Minute)
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && !registrationLimiter.Allow(requestRemoteIP(r, cfg), time.Now()) {
			w.Header().Set("Retry-After", "60")
			writeJSONStatus(w, http.StatusTooManyRequests, map[string]any{"error": "temporarily_unavailable"})
			return
		}
		handleRegister(w, r, cfg, store)
	})
	mux.HandleFunc("/oauth/authorize", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && !passwordLimiter.Allow(requestRemoteIP(r, cfg), time.Now()) {
			w.Header().Set("Retry-After", "300")
			http.Error(w, "too many authorization attempts", http.StatusTooManyRequests)
			return
		}
		handleAuthorize(w, r, cfg, store)
	})
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		handleToken(w, r, cfg, store)
	})
}

type fixedWindowLimiter struct {
	mu      sync.Mutex
	maximum int
	window  time.Duration
	entries map[string]fixedWindowEntry
}

type fixedWindowEntry struct {
	started time.Time
	count   int
}

func newFixedWindowLimiter(maximum int, window time.Duration) *fixedWindowLimiter {
	return &fixedWindowLimiter{maximum: maximum, window: window, entries: map[string]fixedWindowEntry{}}
}

func (l *fixedWindowLimiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, exists := l.entries[key]
	if !exists && len(l.entries) >= 4096 {
		for candidate, value := range l.entries {
			if now.Sub(value.started) >= l.window {
				delete(l.entries, candidate)
			}
		}
		if len(l.entries) >= 4096 {
			return false
		}
	}
	if entry.started.IsZero() || now.Sub(entry.started) >= l.window {
		l.entries[key] = fixedWindowEntry{started: now, count: 1}
		return true
	}
	if entry.count >= l.maximum {
		return false
	}
	entry.count++
	l.entries[key] = entry
	return true
}

func requestRemoteIP(r *http.Request, cfg config.Config) string {
	remote := parseRemoteIP(r.RemoteAddr)
	if remote == nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	networks := trustedProxyNetworks(cfg.TrustedProxyCIDRs)
	if !ipInNetworks(remote, networks) {
		return remote.String()
	}

	rawForwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if rawForwarded == "" {
		return remote.String()
	}
	parts := strings.Split(rawForwarded, ",")
	chain := make([]net.IP, 0, len(parts)+1)
	for _, part := range parts {
		ip := net.ParseIP(strings.TrimSpace(part))
		if ip == nil {
			// 可信代理传来的链必须完全可解析；异常链回退到直接对端，不能部分信任。
			return remote.String()
		}
		chain = append(chain, ip)
	}
	chain = append(chain, remote)
	for index := len(chain) - 1; index >= 0; index-- {
		if !ipInNetworks(chain[index], networks) {
			return chain[index].String()
		}
	}
	return chain[0].String()
}

func parseRemoteIP(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err == nil {
		return net.ParseIP(host)
	}
	return net.ParseIP(strings.Trim(strings.TrimSpace(remoteAddr), "[]"))
}

func trustedProxyNetworks(values []string) []*net.IPNet {
	networks := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err == nil {
			networks = append(networks, network)
		}
	}
	return networks
}

func ipInNetworks(ip net.IP, networks []*net.IPNet) bool {
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
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

func agentDockContextHandler(server *mcp.Server, cfg config.Config, oauthStore *auth.OAuthStore) http.HandlerFunc {
	authorizer := auth.Bearer{Token: cfg.AuthToken}
	authRequired := cfg.AuthRequired()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		staticOK := cfg.AuthToken != "" && authorizer.Authorized(r)
		oauthOK := authorizedOAuth(r, cfg, oauthStore)
		if authRequired && !staticOK && !oauthOK {
			setBearerChallenge(w, cfg, r, strings.TrimSpace(r.Header.Get("Authorization")) != "")
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

func mcpEndpointHandler(server *mcp.Server, cfg config.Config, oauthStore *auth.OAuthStore) http.HandlerFunc {
	authorizer := auth.Bearer{Token: cfg.AuthToken}
	authRequired := cfg.AuthRequired()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		staticOK := cfg.AuthToken != "" && authorizer.Authorized(r)
		oauthOK := authorizedOAuth(r, cfg, oauthStore)
		if authRequired && !staticOK && !oauthOK {
			setBearerChallenge(w, cfg, r, strings.TrimSpace(r.Header.Get("Authorization")) != "")
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
	if !cfg.OAuthEnabled {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_client_metadata", "error_description": "content-type must be application/json"})
		return
	}
	metadata, err := decodeClientRegistration(w, r)
	if err != nil {
		errorCode := "invalid_client_metadata"
		description := "client metadata is invalid"
		if errors.Is(err, errInvalidOAuthRedirectURI) {
			errorCode = "invalid_redirect_uri"
			description = "one or more redirect_uris are invalid"
		}
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": errorCode, "error_description": description})
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

var errInvalidOAuthRedirectURI = errors.New("invalid OAuth redirect URI")

type clientRegistrationMetadata struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
}

func decodeClientRegistration(w http.ResponseWriter, r *http.Request) (clientRegistrationMetadata, error) {
	body := http.MaxBytesReader(w, r.Body, 1<<20)
	data, err := io.ReadAll(body)
	if err != nil {
		return clientRegistrationMetadata{}, fmt.Errorf("read client metadata: %w", err)
	}
	if err := rejectDuplicateTopLevelJSONKeys(data); err != nil {
		return clientRegistrationMetadata{}, err
	}
	var metadata clientRegistrationMetadata
	if err := decodeSingleJSON(strings.NewReader(string(data)), &metadata); err != nil {
		return metadata, fmt.Errorf("decode client metadata: %w", err)
	}
	metadata.ClientName = strings.TrimSpace(metadata.ClientName)
	if len(metadata.ClientName) > 200 {
		return metadata, errors.New("client_name exceeds 200 characters")
	}
	method := strings.TrimSpace(metadata.TokenEndpointAuthMethod)
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
			return metadata, fmt.Errorf("%w: %q", errInvalidOAuthRedirectURI, redirectURI)
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

func rejectDuplicateTopLevelJSONKeys(data []byte) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	start, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode client metadata: %w", err)
	}
	delim, ok := start.(json.Delim)
	if !ok || delim != '{' {
		return errors.New("client metadata must be a JSON object")
	}
	seen := map[string]struct{}{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("decode client metadata key: %w", err)
		}
		key, ok := token.(string)
		if !ok {
			return errors.New("client metadata key must be a string")
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate client metadata field %q", key)
		}
		seen[key] = struct{}{}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return fmt.Errorf("decode client metadata value: %w", err)
		}
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("decode client metadata: %w", err)
	}
	return nil
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

func setBearerChallenge(w http.ResponseWriter, cfg config.Config, r *http.Request, invalidToken bool) {
	if cfg.OAuthEnabled {
		metadataURL := issuerFor(cfg, r) + "/.well-known/oauth-protected-resource/mcp"
		challenge := `Bearer resource_metadata="` + metadataURL + `"`
		if invalidToken {
			challenge += `, error="invalid_token"`
		}
		w.Header().Set("WWW-Authenticate", challenge)
		return
	}
	challenge := "Bearer"
	if invalidToken {
		challenge += ` error="invalid_token"`
	}
	w.Header().Set("WWW-Authenticate", challenge)
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
	if !cfg.OAuthEnabled {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	if r.Method == http.MethodPost {
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/x-www-form-urlencoded" {
			http.Error(w, "content-type must be application/x-www-form-urlencoded", http.StatusBadRequest)
			return
		}
		if len(r.URL.Query()["password"]) != 0 {
			http.Error(w, "password must be supplied in the request body", http.StatusBadRequest)
			return
		}
		for _, name := range authorizationParameterNames {
			if len(r.URL.Query()[name]) != 0 {
				http.Error(w, "OAuth parameters must be supplied in the request body", http.StatusBadRequest)
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, oauthFormBodyLimit)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
	}
	values := r.URL.Query()
	if r.Method == http.MethodPost {
		values = r.PostForm
		if len(values["password"]) > 1 {
			http.Error(w, "password must not be repeated", http.StatusBadRequest)
			return
		}
	}
	if duplicated := repeatedOAuthParameter(values, []string{"client_id", "redirect_uri"}); duplicated != "" {
		http.Error(w, "OAuth parameter must not be repeated: "+duplicated, http.StatusBadRequest)
		return
	}
	clientID := values.Get("client_id")
	redirectURI := values.Get("redirect_uri")
	challenge := values.Get("code_challenge")
	method := values.Get("code_challenge_method")
	state := values.Get("state")
	if !codes.ValidateClientRedirect(clientID, redirectURI) ||
		!codes.ClientAllowsGrant(clientID, "authorization_code") {
		http.Error(w, "invalid client_id or redirect_uri", http.StatusBadRequest)
		return
	}
	if duplicated := repeatedOAuthParameter(values, []string{
		"response_type", "code_challenge", "code_challenge_method", "state",
	}); duplicated != "" {
		redirectOAuthError(w, r, redirectURI, state, "invalid_request")
		return
	}
	responseType := values.Get("response_type")
	if responseType != "code" {
		redirectOAuthError(w, r, redirectURI, state, "unsupported_response_type")
		return
	}
	if !auth.ValidPKCEChallenge(challenge) || method != "S256" {
		redirectOAuthError(w, r, redirectURI, state, "invalid_request")
		return
	}
	expectedResource := issuerFor(cfg, r) + "/mcp"
	resource := strings.TrimSpace(values.Get("resource"))
	if len(values["resource"]) != 1 || resource == "" {
		redirectOAuthError(w, r, redirectURI, state, "invalid_target")
		return
	}
	if !auth.EquivalentResourceURI(resource, expectedResource) {
		redirectOAuthError(w, r, redirectURI, state, "invalid_target")
		return
	}
	resource = expectedResource
	loginPassword := auth.ConfiguredLoginValue()
	registration, _ := codes.ClientRegistration(clientID)
	if loginPassword != "" && r.Method == http.MethodGet {
		writeAuthorizeForm(w, values, "", registration.ClientName)
		return
	}
	if loginPassword != "" && !auth.ConstantTimeEqual(r.PostForm.Get("password"), loginPassword) {
		writeAuthorizeForm(w, values, "invalid password", registration.ClientName)
		return
	}
	code, err := codes.Create(auth.OAuthCode{
		ClientID: clientID, RedirectURI: redirectURI, Challenge: challenge, Resource: resource,
	})
	if err != nil {
		redirectOAuthError(w, r, redirectURI, state, "server_error")
		return
	}
	responseValues := url.Values{"code": []string{code}}
	if state != "" {
		responseValues.Set("state", state)
	}
	location := auth.AppendQuery(redirectURI, responseValues)
	http.Redirect(w, r, location, http.StatusFound)
}

var authorizationParameterNames = []string{
	"response_type", "client_id", "redirect_uri", "code_challenge",
	"code_challenge_method", "resource", "state",
}

func repeatedOAuthParameter(values url.Values, names []string) string {
	for _, name := range names {
		if len(values[name]) > 1 {
			return name
		}
	}
	return ""
}

func redirectOAuthError(w http.ResponseWriter, r *http.Request, redirectURI, state, code string) {
	values := url.Values{"error": []string{code}}
	if state != "" {
		values.Set("state", state)
	}
	http.Redirect(w, r, auth.AppendQuery(redirectURI, values), http.StatusFound)
}

func handleToken(w http.ResponseWriter, r *http.Request, cfg config.Config, store *auth.OAuthStore) {
	if !cfg.OAuthEnabled {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, oauthFormBodyLimit)
	if err := parseOAuthTokenForm(r); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_request", "error_description": err.Error()})
		return
	}
	grantType := strings.TrimSpace(r.FormValue("grant_type"))
	if grantType != "authorization_code" && grantType != "refresh_token" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "unsupported_grant_type"})
		return
	}
	if !validClientAuthentication(r, grantType, store) {
		status := http.StatusBadRequest
		if _, _, ok := r.BasicAuth(); ok {
			status = http.StatusUnauthorized
			w.Header().Set("WWW-Authenticate", `Basic realm="oauth-token"`)
		}
		writeJSONStatus(w, status, map[string]any{"error": "invalid_client"})
		return
	}
	if grantType == "refresh_token" {
		handleRefreshTokenGrant(w, r, cfg, store)
		return
	}
	handleAuthorizationCodeGrant(w, r, cfg, store)
}

func parseOAuthTokenForm(r *http.Request) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		return errors.New("content-type must be application/x-www-form-urlencoded")
	}
	if err := r.ParseForm(); err != nil {
		return errors.New("invalid form body")
	}
	if len(r.URL.Query()["resource"]) != 0 {
		return errors.New("parameter resource must be supplied in the request body")
	}
	singleValueFields := []string{"grant_type", "code", "redirect_uri", "client_id", "code_verifier", "refresh_token", "client_secret"}
	for _, name := range singleValueFields {
		if len(r.URL.Query()[name]) != 0 {
			return fmt.Errorf("parameter %s must be supplied in the request body", name)
		}
		if len(r.PostForm[name]) > 1 {
			return fmt.Errorf("parameter %s must not be repeated", name)
		}
	}
	return nil
}

func handleAuthorizationCodeGrant(w http.ResponseWriter, r *http.Request, cfg config.Config, store *auth.OAuthStore) {
	clientID := strings.TrimSpace(r.PostForm.Get("client_id"))
	resource := strings.TrimSpace(r.PostForm.Get("resource"))
	if len(r.PostForm["resource"]) != 1 || resource == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_target"})
		return
	}
	code, ok, replay := store.Redeem(
		r.PostForm.Get("code"), clientID, r.PostForm.Get("redirect_uri"),
		r.PostForm.Get("code_verifier"), resource,
	)
	if replay {
		if err := store.RevokeGrant(code.GrantID, oauthRefreshTokenTTL); err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
			return
		}
	}
	if !ok {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
		return
	}
	resource = code.Resource
	issuer := issuerFor(cfg, r)
	grantTTL := oauthAccessTokenTTL
	if store.ClientAllowsGrant(clientID, "refresh_token") {
		grantTTL = oauthRefreshTokenTTL
	}
	if err := store.ActivateGrant(clientID, resource, code.GrantID, grantTTL); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
		return
	}
	accessToken, err := auth.IssueToken(issuer, resource, code.GrantID, oauthSigningKey(), oauthAccessTokenTTL)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}
	refreshToken := ""
	if store.ClientAllowsGrant(clientID, "refresh_token") {
		refreshToken, err = store.IssueRefreshToken(clientID, resource, code.GrantID, oauthRefreshTokenTTL)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
			return
		}
	}
	writeOAuthTokenResponse(w, accessToken, refreshToken)
}

func handleRefreshTokenGrant(w http.ResponseWriter, r *http.Request, cfg config.Config, store *auth.OAuthStore) {
	clientID := strings.TrimSpace(r.FormValue("client_id"))
	resource := strings.TrimSpace(r.FormValue("resource"))
	if len(r.PostForm["resource"]) != 1 || resource == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_target"})
		return
	}
	newRefreshToken, resource, grantID, ok, err := store.RotateRefreshToken(
		r.FormValue("refresh_token"),
		clientID,
		resource,
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
	accessToken, err := auth.IssueToken(issuerFor(cfg, r), resource, grantID, oauthSigningKey(), oauthAccessTokenTTL)
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

func authorizedOAuth(r *http.Request, cfg config.Config, store *auth.OAuthStore) bool {
	if !cfg.OAuthEnabled {
		return false
	}
	token, ok := auth.ParseBearerToken(r.Header.Get("Authorization"))
	if !ok {
		return false
	}
	issuer := issuerFor(cfg, r)
	audience := issuer + "/mcp"
	grantID, valid := auth.ValidateToken(token, issuer, audience, oauthSigningKey())
	return valid && store.GrantActive(grantID)
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
	if strings.TrimSpace(r.Header.Get("Authorization")) != "" {
		return false
	}
	if err := r.ParseForm(); err != nil {
		return false
	}
	if len(r.PostForm["client_secret"]) != 0 {
		return false
	}
	clientID := strings.TrimSpace(r.PostForm.Get("client_id"))
	if !store.ValidateClientID(clientID) || !store.ClientAllowsGrant(clientID, grantType) {
		return false
	}
	if grantType == "authorization_code" {
		return store.ValidateClientRedirect(clientID, strings.TrimSpace(r.PostForm.Get("redirect_uri")))
	}
	return grantType == "refresh_token"
}

func serverCard(cfg config.Config, r *http.Request) map[string]any {
	issuer := issuerFor(cfg, r)
	authInfo := map[string]any{"type": "none"}
	if cfg.AuthToken != "" {
		authInfo = map[string]any{"type": "bearer", "scheme": "Bearer", "header": "Authorization"}
	}
	if cfg.OAuthEnabled {
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

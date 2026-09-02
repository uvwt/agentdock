package httpx

import (
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
	"strings"
	"time"

	gooauth2 "github.com/go-oauth2/oauth2/v4"
	oautherrors "github.com/go-oauth2/oauth2/v4/errors"
	oauthserver "github.com/go-oauth2/oauth2/v4/server"
	"github.com/uvwt/agentdock/internal/auth"
	"github.com/uvwt/agentdock/internal/config"
)

const (
	defaultOAuthAccessTokenTTLSeconds = int64(time.Hour / time.Second)
	neverOAuthClientExpiresInSeconds  = int64(999999 * 24 * 60 * 60)
	oauthRefreshTokenTTL              = 90 * 24 * time.Hour
	oauthFormBodyLimit                = 64 << 10
)

var errInvalidOAuthRedirectURI = errors.New("invalid OAuth redirect URI")

type clientRegistrationMetadata struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
}

var authorizationParameterNames = []string{
	"response_type", "client_id", "redirect_uri", "code_challenge",
	"code_challenge_method", "resource", "state",
}

func registerOAuthRoutes(mux *http.ServeMux, cfg config.Config, store *auth.OAuthStore) {
	if !cfg.OAuthEnabled {
		return
	}
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		writeOAuthMetadata(w, r, oauthMetadata(cfg, r))
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		writeOAuthMetadata(w, r, protectedResourceMetadata(cfg, r))
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

func writeOAuthMetadata(w http.ResponseWriter, r *http.Request, metadata map[string]any) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodHead)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	}
	writeJSON(w, metadata)
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
	resourceValues := values["resource"]
	if len(resourceValues) > 1 {
		redirectOAuthError(w, r, redirectURI, state, "invalid_target")
		return
	}
	resource := expectedResource
	if len(resourceValues) == 1 {
		resource = strings.TrimSpace(resourceValues[0])
	}
	if resource == "" || !auth.EquivalentResourceURI(resource, expectedResource) {
		redirectOAuthError(w, r, redirectURI, state, "invalid_target")
		return
	}
	// AgentDock 只暴露当前 issuer 下唯一的 MCP protected resource。客户端省略
	// RFC 8707 resource 时直接绑定到这个固定资源；显式提供时仍必须严格匹配。
	// 同时把请求规范化，确保授权码内始终保存实际绑定的 protected resource。
	resource = expectedResource
	values.Set("resource", resource)
	if r.Method == http.MethodGet {
		r.URL.RawQuery = values.Encode()
	} else {
		r.PostForm.Set("resource", resource)
		r.Form.Set("resource", resource)
	}
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
	protocol := newOAuthProtocolServer(cfg, codes)
	if err := protocol.HandleAuthorizeRequest(w, r); err != nil {
		slog.Warn("OAuth authorize request failed", "error", err)
	}
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
	if reason := clientAuthenticationFailureReason(r, grantType, store); reason != "" {
		// 这里只记录固定枚举原因，避免把 client secret、授权码、令牌或回调地址写入日志。
		slog.Warn("OAuth token client rejected", "reason", reason, "grant_type", grantType)
		status := http.StatusBadRequest
		if _, _, ok := r.BasicAuth(); ok {
			status = http.StatusUnauthorized
			w.Header().Set("WWW-Authenticate", `Basic realm="oauth-token"`)
		}
		writeJSONStatus(w, status, map[string]any{"error": "invalid_client"})
		return
	}
	// resource 已在授权码或 Refresh Grant 中绑定；客户端在 Token 请求中省略时沿用原绑定，
	// 显式提供时仍由 TokenStore 校验是否与原绑定一致。
	resource := strings.TrimSpace(r.PostForm.Get("resource"))
	if len(r.PostForm["resource"]) == 1 && resource == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_target"})
		return
	}
	issuer := issuerFor(cfg, r)
	r = r.WithContext(auth.WithOAuthRequest(r.Context(), issuer, resource, strings.TrimSpace(r.PostForm.Get("client_id"))))
	protocol := newOAuthProtocolServer(cfg, store)
	if err := protocol.HandleTokenRequest(w, r); err != nil {
		slog.Warn("OAuth token request failed", "error", err)
	}
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
	singleValueFields := []string{"grant_type", "code", "redirect_uri", "client_id", "code_verifier", "refresh_token", "client_secret", "resource"}
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
func authorizedOAuth(r *http.Request, cfg config.Config, store *auth.OAuthStore) bool {
	if !cfg.OAuthEnabled {
		return false
	}
	issuer := issuerFor(cfg, r)
	resource := issuer + "/mcp"
	ctx := auth.WithOAuthRequest(r.Context(), issuer, resource, "")
	_, err := newOAuthProtocolServer(cfg, store).ValidationBearerToken(r.WithContext(ctx))
	return err == nil
}
func newOAuthProtocolServer(cfg config.Config, store *auth.OAuthStore) *oauthserver.Server {
	accessTokenTTLSeconds := cfg.OAuthAccessTokenTTLSeconds
	if cfg.OAuthAccessTokenNeverExpires {
		accessTokenTTLSeconds = 0
	} else if accessTokenTTLSeconds <= 0 {
		accessTokenTTLSeconds = defaultOAuthAccessTokenTTLSeconds
	}
	manager := auth.NewOAuthManager(
		store,
		oauthSigningKey(),
		accessTokenTTLSeconds,
		oauthRefreshTokenTTL,
	)
	protocolConfig := oauthserver.NewConfig()
	protocolConfig.AllowedResponseTypes = []gooauth2.ResponseType{gooauth2.Code}
	protocolConfig.AllowedGrantTypes = []gooauth2.GrantType{gooauth2.AuthorizationCode, gooauth2.Refreshing}
	protocolConfig.AllowedCodeChallengeMethods = []gooauth2.CodeChallengeMethod{gooauth2.CodeChallengeS256}
	protocolConfig.ForcePKCE = true

	protocol := oauthserver.NewServer(protocolConfig, manager)
	protocol.SetClientInfoHandler(oauthserver.ClientFormHandler)
	protocol.SetAccessTokenResolveHandler(func(request *http.Request) (string, bool) {
		return auth.ParseBearerToken(request.Header.Get("Authorization"))
	})
	protocol.SetResponseTokenHandler(func(w http.ResponseWriter, data map[string]interface{}, header http.Header, statusCode ...int) error {
		if _, issued := data["access_token"]; issued {
			if cfg.OAuthAccessTokenNeverExpires {
				// ChatGPT 当前不会持久保存缺少 expires_in 的 OAuth 令牌。
				// 服务端仍按永久令牌校验，这个极远的有限值只用于客户端兼容。
				data["expires_in"] = neverOAuthClientExpiresInSeconds
			} else {
				data["expires_in"] = accessTokenTTLSeconds
			}
		}
		w.Header().Set("Content-Type", "application/json;charset=UTF-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		for key := range header {
			w.Header().Set(key, header.Get(key))
		}
		status := http.StatusOK
		if len(statusCode) > 0 && statusCode[0] > 0 {
			status = statusCode[0]
		}
		w.WriteHeader(status)
		return json.NewEncoder(w).Encode(data)
	})
	protocol.SetClientAuthorizedHandler(func(clientID string, grant gooauth2.GrantType) (bool, error) {
		return store.ClientAllowsGrant(clientID, grant.String()), nil
	})
	protocol.SetUserAuthorizationHandler(func(http.ResponseWriter, *http.Request) (string, error) {
		return "agentdock-user", nil
	})
	protocol.SetResponseErrorHandler(func(response *oautherrors.Response) {
		switch response.Error {
		case oautherrors.ErrInvalidGrant, oautherrors.ErrInvalidAuthorizeCode,
			oautherrors.ErrInvalidRefreshToken, oautherrors.ErrExpiredRefreshToken:
			response.Error = oautherrors.ErrInvalidGrant
			response.StatusCode = http.StatusBadRequest
		}
	})
	return protocol
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
	return clientAuthenticationFailureReason(r, grantType, store) == ""
}
func clientAuthenticationFailureReason(r *http.Request, grantType string, store *auth.OAuthStore) string {
	if strings.TrimSpace(r.Header.Get("Authorization")) != "" {
		return "authorization_header_present"
	}
	if err := r.ParseForm(); err != nil {
		return "form_parse_failed"
	}
	if len(r.PostForm["client_secret"]) != 0 {
		return "client_secret_present"
	}
	clientID := strings.TrimSpace(r.PostForm.Get("client_id"))
	if !store.ValidateClientID(clientID) {
		return "client_id_unregistered"
	}
	if !store.ClientAllowsGrant(clientID, grantType) {
		return "grant_not_allowed"
	}
	if grantType == "authorization_code" && !store.ValidateClientRedirect(clientID, strings.TrimSpace(r.PostForm.Get("redirect_uri"))) {
		return "redirect_uri_mismatch"
	}
	if grantType != "authorization_code" && grantType != "refresh_token" {
		return "unsupported_grant_type"
	}
	return ""
}

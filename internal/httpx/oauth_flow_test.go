package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/auth"
	"github.com/uvwt/agentdock/internal/config"
)

const (
	oauthTestVerifier  = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	oauthTestChallenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	oauthTestRedirect  = "https://client.example/callback"
)

func TestOAuthDynamicRegistrationAuthorizationCodeAndRefreshFlow(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_PASSWORD", "")
	t.Setenv("AGENTDOCK_OAUTH_CLIENT_SECRET", "legacy-secret-is-ignored")
	t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "token-signing-secret")
	cfg := oauthTestConfig(t)
	store := auth.NewOAuthStore()
	resource := cfg.OAuthServerURL + "/mcp"

	registrationRequest := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{
		"client_name":"ChatGPT",
		"redirect_uris":["https://client.example/callback"],
		"token_endpoint_auth_method":"none",
		"grant_types":["authorization_code","refresh_token"],
		"response_types":["code"]
	}`))
	registrationRequest.Header.Set("Content-Type", "application/json")
	registrationResponse := httptest.NewRecorder()
	handleRegister(registrationResponse, registrationRequest, cfg, store)
	if registrationResponse.Code != http.StatusCreated {
		t.Fatalf("registration status = %d; body=%s", registrationResponse.Code, registrationResponse.Body.String())
	}
	var registration struct {
		ClientID                string   `json:"client_id"`
		ClientName              string   `json:"client_name"`
		RedirectURIs            []string `json:"redirect_uris"`
		GrantTypes              []string `json:"grant_types"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}
	if err := json.Unmarshal(registrationResponse.Body.Bytes(), &registration); err != nil {
		t.Fatal(err)
	}
	if registration.ClientID == "" || len(registration.ClientID) > 64 || registration.ClientName != "ChatGPT" ||
		registration.TokenEndpointAuthMethod != "none" || len(registration.RedirectURIs) != 1 ||
		!containsString(registration.GrantTypes, "refresh_token") {
		t.Fatalf("registration = %#v", registration)
	}
	if !store.ValidateClientRedirect(registration.ClientID, oauthTestRedirect, oauthSigningKey()) ||
		!store.ClientAllowsGrant(registration.ClientID, "refresh_token", oauthSigningKey()) {
		t.Fatal("registered client ID is not bound to its redirect URI and grants")
	}

	authorizeValues := url.Values{
		"response_type":         {"code"},
		"client_id":             {registration.ClientID},
		"redirect_uri":          {oauthTestRedirect},
		"code_challenge":        {oauthTestChallenge},
		"code_challenge_method": {"S256"},
		"resource":              {resource},
		"state":                 {"state-value"},
	}
	authorizeResponse := httptest.NewRecorder()
	handleAuthorizeForTest(
		authorizeResponse,
		httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+authorizeValues.Encode(), nil),
		cfg,
		store,
	)
	if authorizeResponse.Code != http.StatusFound {
		t.Fatalf("authorize status = %d; body=%s", authorizeResponse.Code, authorizeResponse.Body.String())
	}
	location, err := url.Parse(authorizeResponse.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	code := location.Query().Get("code")
	if code == "" || location.Query().Get("state") != "state-value" {
		t.Fatalf("authorization redirect = %s", location)
	}

	tokenValues := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {oauthTestRedirect},
		"client_id":     {registration.ClientID},
		"code_verifier": {oauthTestVerifier},
		"resource":      {resource},
	}
	tokenResponse := postTokenRequest(t, cfg, store, tokenValues)
	if tokenResponse.Code != http.StatusOK {
		t.Fatalf("token status = %d; body=%s", tokenResponse.Code, tokenResponse.Body.String())
	}
	if tokenResponse.Header().Get("Cache-Control") != "no-store" || tokenResponse.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("token cache headers = %q %q", tokenResponse.Header().Get("Cache-Control"), tokenResponse.Header().Get("Pragma"))
	}
	var tokenPayload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(tokenResponse.Body.Bytes(), &tokenPayload); err != nil {
		t.Fatal(err)
	}
	if tokenPayload.AccessToken == "" || tokenPayload.RefreshToken == "" ||
		tokenPayload.TokenType != "Bearer" || tokenPayload.ExpiresIn != 3600 {
		t.Fatalf("token payload = %#v", tokenPayload)
	}
	assertAuthorizedAccessToken(t, cfg, tokenPayload.AccessToken)

	replayResponse := postTokenRequest(t, cfg, store, tokenValues)
	assertOAuthError(t, replayResponse, http.StatusBadRequest, "invalid_grant")

	wrongClientID := oauthRegisteredClientID(t, "https://other.example/callback")
	wrongClientRefresh := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tokenPayload.RefreshToken},
		"client_id":     {wrongClientID},
		"resource":      {resource},
	}
	assertOAuthError(t, postTokenRequest(t, cfg, store, wrongClientRefresh), http.StatusBadRequest, "invalid_grant")

	wrongResourceRefresh := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tokenPayload.RefreshToken},
		"client_id":     {registration.ClientID},
		"resource":      {cfg.OAuthServerURL + "/other"},
	}
	assertOAuthError(t, postTokenRequest(t, cfg, store, wrongResourceRefresh), http.StatusBadRequest, "invalid_grant")

	refreshValues := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tokenPayload.RefreshToken},
		"client_id":     {registration.ClientID},
		"resource":      {resource},
	}
	refreshResponse := postTokenRequest(t, cfg, store, refreshValues)
	if refreshResponse.Code != http.StatusOK {
		t.Fatalf("refresh status = %d; body=%s", refreshResponse.Code, refreshResponse.Body.String())
	}
	var refreshed struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(refreshResponse.Body.Bytes(), &refreshed); err != nil {
		t.Fatal(err)
	}
	if refreshed.AccessToken == "" || refreshed.RefreshToken == "" ||
		refreshed.RefreshToken == tokenPayload.RefreshToken || refreshed.ExpiresIn != 3600 {
		t.Fatalf("refreshed token payload = %#v", refreshed)
	}
	assertAuthorizedAccessToken(t, cfg, refreshed.AccessToken)
	assertOAuthError(t, postTokenRequest(t, cfg, store, refreshValues), http.StatusBadRequest, "invalid_grant")

	wrongAudienceRequest := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	wrongAudienceToken, err := auth.IssueToken(cfg.OAuthServerURL, cfg.OAuthServerURL+"/other", oauthSigningKey(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	wrongAudienceRequest.Header.Set("Authorization", "Bearer "+wrongAudienceToken)
	if authorizedOAuth(wrongAudienceRequest, cfg) {
		t.Fatal("wrong-audience access token was accepted")
	}
}

func assertAuthorizedAccessToken(t *testing.T, cfg config.Config, token string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	if !authorizedOAuth(request, cfg) {
		t.Fatal("fresh access token was rejected")
	}
}

func TestOAuthAuthorizePasswordGate(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_PASSWORD", "login-secret")
	t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "token-signing-secret")
	cfg := oauthTestConfig(t)
	clientID := oauthRegisteredClientID(t, oauthTestRedirect)
	values := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {oauthTestRedirect},
		"code_challenge":        {oauthTestChallenge},
		"code_challenge_method": {"S256"},
		"resource":              {cfg.OAuthServerURL + "/mcp"},
		"state":                 {"password-gate"},
	}

	getResponse := httptest.NewRecorder()
	handleAuthorizeForTest(getResponse, httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+values.Encode(), nil), cfg, auth.NewOAuthStore())
	page := getResponse.Body.String()
	if getResponse.Code != http.StatusOK ||
		!strings.Contains(page, `type="password"`) ||
		!strings.Contains(page, "连接到 AgentDock") ||
		!strings.Contains(page, ">继续</button>") ||
		!strings.Contains(page, oauthTestRedirect) {
		t.Fatalf("GET authorize status=%d body=%s", getResponse.Code, page)
	}
	if getResponse.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(getResponse.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") ||
		getResponse.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("authorization page security headers=%v", getResponse.Header())
	}

	wrongValues := cloneValues(values)
	wrongValues.Set("password", "wrong")
	wrongResponse := httptest.NewRecorder()
	handleAuthorizeForTest(wrongResponse, formAuthorizeRequest(wrongValues), cfg, auth.NewOAuthStore())
	if wrongResponse.Code != http.StatusOK ||
		!strings.Contains(wrongResponse.Body.String(), "密码不正确，请重试。") ||
		!strings.Contains(wrongResponse.Body.String(), `aria-invalid="true"`) {
		t.Fatalf("wrong password status=%d body=%s", wrongResponse.Code, wrongResponse.Body.String())
	}

	validValues := cloneValues(values)
	validValues.Set("password", "login-secret")
	validResponse := httptest.NewRecorder()
	handleAuthorizeForTest(validResponse, formAuthorizeRequest(validValues), cfg, auth.NewOAuthStore())
	if validResponse.Code != http.StatusFound {
		t.Fatalf("valid password status=%d body=%s", validResponse.Code, validResponse.Body.String())
	}
}

func TestOAuthPublicClientAuthenticationContract(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "token-signing-secret")
	clientID := oauthRegisteredClientID(t, oauthTestRedirect)
	store := auth.NewOAuthStore()

	valid := url.Values{"client_id": {clientID}, "redirect_uri": {oauthTestRedirect}}
	if !validClientAuthentication(formRequest(valid), "authorization_code", oauthTestClients(store)) {
		t.Fatal("registered public client was rejected")
	}
	if !validClientAuthentication(formRequest(url.Values{"client_id": {clientID}}), "refresh_token", oauthTestClients(store)) {
		t.Fatal("registered refresh-token client was rejected")
	}
	withSecret := cloneValues(valid)
	withSecret.Set("client_secret", "legacy-secret")
	if validClientAuthentication(formRequest(withSecret), "authorization_code", oauthTestClients(store)) {
		t.Fatal("client_secret_post was accepted")
	}
	basic := formRequest(valid)
	basic.SetBasicAuth(clientID, "secret")
	if validClientAuthentication(basic, "authorization_code", oauthTestClients(store)) {
		t.Fatal("client_secret_basic was accepted")
	}
	wrongRedirect := url.Values{"client_id": {clientID}, "redirect_uri": {"https://other.example/callback"}}
	if validClientAuthentication(formRequest(wrongRedirect), "authorization_code", oauthTestClients(store)) {
		t.Fatal("unregistered redirect URI was accepted")
	}
}

func TestOAuthRegistrationRejectsInvalidMetadata(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "token-signing-secret")
	cfg := oauthTestConfig(t)
	store := auth.NewOAuthStore()
	cases := []string{
		`{"token_endpoint_auth_method":"none"}`,
		`{"redirect_uris":["http://attacker.example/callback"],"token_endpoint_auth_method":"none"}`,
		`{"redirect_uris":["https://client.example/callback"],"token_endpoint_auth_method":"client_secret_post"}`,
		`{"redirect_uris":["https://client.example/callback"],"grant_types":["client_credentials"]}`,
		`{"redirect_uris":["https://client.example/callback"],"grant_types":["refresh_token"]}`,
	}
	for _, body := range cases {
		response := httptest.NewRecorder()
		handleRegister(response, httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(body)), cfg, store)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %s status = %d; response=%s", body, response.Code, response.Body.String())
		}
	}
}

func TestOAuthAuthorizeBindsClientRedirectAndResource(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_PASSWORD", "")
	t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "token-signing-secret")
	cfg := oauthTestConfig(t)
	clientID := oauthRegisteredClientID(t, oauthTestRedirect)
	base := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {oauthTestRedirect},
		"code_challenge":        {oauthTestChallenge},
		"code_challenge_method": {"S256"},
		"resource":              {cfg.OAuthServerURL + "/mcp"},
	}
	for name, mutate := range map[string]func(url.Values){
		"wrong redirect":         func(v url.Values) { v.Set("redirect_uri", "https://other.example/callback") },
		"wrong resource":         func(v url.Values) { v.Set("resource", cfg.OAuthServerURL+"/other") },
		"wrong response type":    func(v url.Values) { v.Set("response_type", "token") },
		"wrong challenge method": func(v url.Values) { v.Set("code_challenge_method", "plain") },
	} {
		t.Run(name, func(t *testing.T) {
			values := cloneValues(base)
			mutate(values)
			response := httptest.NewRecorder()
			handleAuthorizeForTest(response, httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+values.Encode(), nil), cfg, auth.NewOAuthStore())
			if response.Code != http.StatusBadRequest || response.Header().Get("Location") != "" {
				t.Fatalf("status=%d location=%q body=%s", response.Code, response.Header().Get("Location"), response.Body.String())
			}
		})
	}
}

func oauthTestClients(store *auth.OAuthStore) *oauthClientValidator {
	return newOAuthClientValidator(store, oauthSigningKey())
}

func handleAuthorizeForTest(w http.ResponseWriter, r *http.Request, cfg config.Config, store *auth.OAuthStore) {
	handleAuthorize(w, r, cfg, store, oauthTestClients(store))
}

func oauthRegisteredClientID(t *testing.T, redirectURI string) string {
	t.Helper()
	clientID, err := auth.IssueClientID(
		[]string{redirectURI},
		[]string{"authorization_code", "refresh_token"},
		oauthSigningKey(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return clientID
}

func oauthTestConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := testConfig(t)
	cfg.OAuthClientID = "oauth-enabled"
	cfg.OAuthServerURL = "https://agentdock.example"
	return cfg
}

func postTokenRequest(t *testing.T, cfg config.Config, codes *auth.OAuthStore, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	request := formRequest(values)
	response := httptest.NewRecorder()
	handleToken(response, request, cfg, codes, oauthTestClients(codes))
	return response
}

func formRequest(values url.Values) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return request
}

func formAuthorizeRequest(values url.Values) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return request
}

func cloneValues(input url.Values) url.Values {
	output := make(url.Values, len(input))
	for key, values := range input {
		output[key] = append([]string(nil), values...)
	}
	return output
}

func assertOAuthError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, status, response.Body.String())
	}
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error != code {
		t.Fatalf("error = %q, want %q", payload.Error, code)
	}
}

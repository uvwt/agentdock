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
	t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "token-signing-secret")
	cfg := oauthTestConfig(t)
	store := auth.NewOAuthStore()
	resource := cfg.OAuthServerURL + "/mcp"

	registrationRequest := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{
		"client_name":"Test Client",
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
	if registration.ClientID == "" || len(registration.ClientID) > 64 || registration.ClientName != "Test Client" ||
		registration.TokenEndpointAuthMethod != "none" || len(registration.RedirectURIs) != 1 ||
		!containsString(registration.GrantTypes, "refresh_token") {
		t.Fatalf("registration = %#v", registration)
	}
	if !store.ValidateClientRedirect(registration.ClientID, oauthTestRedirect) ||
		!store.ClientAllowsGrant(registration.ClientID, "refresh_token") {
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
	assertAuthorizedAccessToken(t, cfg, store, tokenPayload.AccessToken)

	wrongClientID := oauthRegisteredClientID(t, store, "https://other.example/callback")
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
	assertAuthorizedAccessToken(t, cfg, store, refreshed.AccessToken)

	replayResponse := postTokenRequest(t, cfg, store, tokenValues)
	assertOAuthError(t, replayResponse, http.StatusBadRequest, "invalid_grant")
	revokedAccessRequest := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	revokedAccessRequest.Header.Set("Authorization", "Bearer "+refreshed.AccessToken)
	if authorizedOAuth(revokedAccessRequest, cfg, store) {
		t.Fatal("access token remained valid after authorization code replay revoked its grant")
	}
	refreshedValues := url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {refreshed.RefreshToken},
		"client_id": {registration.ClientID}, "resource": {resource},
	}
	assertOAuthError(t, postTokenRequest(t, cfg, store, refreshedValues), http.StatusBadRequest, "invalid_grant")

	wrongAudienceRequest := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	wrongAudienceToken, err := auth.IssueToken(cfg.OAuthServerURL, cfg.OAuthServerURL+"/other", "grant-id", oauthSigningKey(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	wrongAudienceRequest.Header.Set("Authorization", "Bearer "+wrongAudienceToken)
	if authorizedOAuth(wrongAudienceRequest, cfg, store) {
		t.Fatal("wrong-audience access token was accepted")
	}
}

func assertAuthorizedAccessToken(t *testing.T, cfg config.Config, store *auth.OAuthStore, token string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	if !authorizedOAuth(request, cfg, store) {
		t.Fatal("fresh access token was rejected")
	}
}

func TestOAuthAuthorizePasswordGate(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_PASSWORD", "login-secret")
	t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "token-signing-secret")
	cfg := oauthTestConfig(t)
	store := auth.NewOAuthStore()
	clientID := oauthRegisteredClientID(t, store, oauthTestRedirect)
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
	handleAuthorizeForTest(getResponse, httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+values.Encode(), nil), cfg, store)
	page := getResponse.Body.String()
	if getResponse.Code != http.StatusOK ||
		!strings.Contains(page, `type="password"`) ||
		!strings.Contains(page, "连接到 AgentDock") ||
		!strings.Contains(page, "AgentDock 服务端密码") ||
		!strings.Contains(page, ">验证并连接</button>") ||
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
	handleAuthorizeForTest(wrongResponse, formAuthorizeRequest(wrongValues), cfg, store)
	if wrongResponse.Code != http.StatusOK ||
		!strings.Contains(wrongResponse.Body.String(), "密码不正确，请重试。") ||
		!strings.Contains(wrongResponse.Body.String(), `aria-invalid="true"`) {
		t.Fatalf("wrong password status=%d body=%s", wrongResponse.Code, wrongResponse.Body.String())
	}

	validValues := cloneValues(values)
	validValues.Set("password", "login-secret")
	validResponse := httptest.NewRecorder()
	handleAuthorizeForTest(validResponse, formAuthorizeRequest(validValues), cfg, store)
	if validResponse.Code != http.StatusFound {
		t.Fatalf("valid password status=%d body=%s", validResponse.Code, validResponse.Body.String())
	}

	queryPasswordRequest := formAuthorizeRequest(values)
	queryPasswordRequest.URL.RawQuery = "password=login-secret"
	queryPasswordResponse := httptest.NewRecorder()
	handleAuthorizeForTest(queryPasswordResponse, queryPasswordRequest, cfg, store)
	if queryPasswordResponse.Code != http.StatusBadRequest || queryPasswordResponse.Header().Get("Location") != "" {
		t.Fatalf("query password status=%d location=%q", queryPasswordResponse.Code, queryPasswordResponse.Header().Get("Location"))
	}

	repeatedPasswordValues := cloneValues(values)
	repeatedPasswordValues["password"] = []string{"login-secret", "login-secret"}
	repeatedPasswordResponse := httptest.NewRecorder()
	handleAuthorizeForTest(repeatedPasswordResponse, formAuthorizeRequest(repeatedPasswordValues), cfg, store)
	if repeatedPasswordResponse.Code != http.StatusBadRequest || repeatedPasswordResponse.Header().Get("Location") != "" {
		t.Fatalf("repeated password status=%d location=%q", repeatedPasswordResponse.Code, repeatedPasswordResponse.Header().Get("Location"))
	}
}

func TestOAuthPublicClientAuthenticationContract(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "token-signing-secret")
	store := auth.NewOAuthStore()
	clientID := oauthRegisteredClientID(t, store, oauthTestRedirect)

	valid := url.Values{"client_id": {clientID}, "redirect_uri": {oauthTestRedirect}}
	if !validClientAuthentication(formRequest(valid), "authorization_code", store) {
		t.Fatal("registered public client was rejected")
	}
	if !validClientAuthentication(formRequest(url.Values{"client_id": {clientID}}), "refresh_token", store) {
		t.Fatal("registered refresh-token client was rejected")
	}
	withSecret := cloneValues(valid)
	withSecret.Set("client_secret", "legacy-secret")
	if validClientAuthentication(formRequest(withSecret), "authorization_code", store) {
		t.Fatal("client_secret_post was accepted")
	}
	withEmptySecret := cloneValues(valid)
	withEmptySecret.Set("client_secret", "")
	if validClientAuthentication(formRequest(withEmptySecret), "authorization_code", store) {
		t.Fatal("empty client_secret was accepted for a public client")
	}
	basic := formRequest(valid)
	basic.SetBasicAuth(clientID, "secret")
	if validClientAuthentication(basic, "authorization_code", store) {
		t.Fatal("client_secret_basic was accepted")
	}
	bearer := formRequest(valid)
	bearer.Header.Set("Authorization", "Bearer unexpected")
	if validClientAuthentication(bearer, "authorization_code", store) {
		t.Fatal("unexpected Authorization header was accepted")
	}
	wrongRedirect := url.Values{"client_id": {clientID}, "redirect_uri": {"https://other.example/callback"}}
	if validClientAuthentication(formRequest(wrongRedirect), "authorization_code", store) {
		t.Fatal("unregistered redirect URI was accepted")
	}
}

func TestOAuthTokenUsesRFCInvalidClientStatusForBasicAuthentication(t *testing.T) {
	cfg := oauthTestConfig(t)
	store := auth.NewOAuthStore()
	clientID := oauthRegisteredClientID(t, store, oauthTestRedirect)
	values := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {"unused"},
		"client_id":     {clientID},
		"redirect_uri":  {oauthTestRedirect},
		"code_verifier": {oauthTestVerifier},
		"resource":      {cfg.OAuthServerURL + "/mcp"},
	}
	request := formRequest(values)
	request.SetBasicAuth(clientID, "not-supported")
	response := httptest.NewRecorder()
	handleToken(response, request, cfg, store)
	assertOAuthError(t, response, http.StatusUnauthorized, "invalid_client")
	if got := response.Header().Get("WWW-Authenticate"); got != `Basic realm="oauth-token"` {
		t.Fatalf("WWW-Authenticate = %q", got)
	}
}

func TestOAuthRegistrationRejectsInvalidMetadata(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "token-signing-secret")
	cfg := oauthTestConfig(t)
	store := auth.NewOAuthStore()
	cases := []string{
		`{"token_endpoint_auth_method":"none"}`,
		`{"redirect_uris":["https://client.example/callback"]}`,
		`{"redirect_uris":["http://attacker.example/callback"],"token_endpoint_auth_method":"none"}`,
		`{"redirect_uris":["https://client.example/callback"],"token_endpoint_auth_method":"client_secret_post"}`,
		`{"redirect_uris":["https://client.example/callback"],"grant_types":["client_credentials"]}`,
		`{"redirect_uris":["https://client.example/callback"],"grant_types":["refresh_token"]}`,
	}
	for _, body := range cases {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		handleRegister(response, request, cfg, store)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %s status = %d; response=%s", body, response.Code, response.Body.String())
		}
	}
}

func TestOAuthRegistrationRequiresJSONAndUsesRedirectError(t *testing.T) {
	cfg := oauthTestConfig(t)
	store := auth.NewOAuthStore()

	wrongContentType := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{"redirect_uris":["https://client.example/callback"]}`))
	wrongContentType.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()
	handleRegister(response, wrongContentType, cfg, store)
	assertOAuthError(t, response, http.StatusBadRequest, "invalid_client_metadata")

	invalidRedirect := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{"redirect_uris":["http://remote.example/callback"],"token_endpoint_auth_method":"none"}`))
	invalidRedirect.Header.Set("Content-Type", "application/json; charset=utf-8")
	response = httptest.NewRecorder()
	handleRegister(response, invalidRedirect, cfg, store)
	assertOAuthError(t, response, http.StatusBadRequest, "invalid_redirect_uri")
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("registration cache headers = %v", response.Header())
	}

	duplicateField := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{"redirect_uris":["https://client.example/callback"],"redirect_uris":["https://other.example/callback"],"token_endpoint_auth_method":"none"}`))
	duplicateField.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handleRegister(response, duplicateField, cfg, store)
	assertOAuthError(t, response, http.StatusBadRequest, "invalid_client_metadata")

	nonASCIIValue := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{"redirect_uris":["https://client.example/callback"],"token_endpoint_auth_method":"密钥"}`))
	nonASCIIValue.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handleRegister(response, nonASCIIValue, cfg, store)
	assertOAuthError(t, response, http.StatusBadRequest, "invalid_client_metadata")
	if strings.Contains(response.Body.String(), "密钥") {
		t.Fatal("registration error_description reflected non-ASCII client input")
	}
}

func TestOAuthAuthorizeBindsClientRedirectAndResource(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_PASSWORD", "")
	t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "token-signing-secret")
	cfg := oauthTestConfig(t)
	store := auth.NewOAuthStore()
	clientID := oauthRegisteredClientID(t, store, oauthTestRedirect)
	base := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {oauthTestRedirect},
		"code_challenge":        {oauthTestChallenge},
		"code_challenge_method": {"S256"},
		"resource":              {cfg.OAuthServerURL + "/mcp"},
	}
	tests := map[string]struct {
		mutate       func(url.Values)
		wantStatus   int
		wantRedirect bool
	}{
		"wrong redirect":         {func(v url.Values) { v.Set("redirect_uri", "https://other.example/callback") }, http.StatusBadRequest, false},
		"wrong resource":         {func(v url.Values) { v.Set("resource", cfg.OAuthServerURL+"/other") }, http.StatusFound, true},
		"wrong response type":    {func(v url.Values) { v.Set("response_type", "token") }, http.StatusFound, true},
		"wrong challenge method": {func(v url.Values) { v.Set("code_challenge_method", "plain") }, http.StatusFound, true},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			values := cloneValues(base)
			test.mutate(values)
			response := httptest.NewRecorder()
			handleAuthorizeForTest(response, httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+values.Encode(), nil), cfg, store)
			if response.Code != test.wantStatus || (response.Header().Get("Location") != "") != test.wantRedirect {
				t.Fatalf("status=%d location=%q body=%s", response.Code, response.Header().Get("Location"), response.Body.String())
			}
		})
	}
}

func TestOAuthAuthorizeRedirectsProtocolErrorsWithState(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_PASSWORD", "")
	cfg := oauthTestConfig(t)
	store := auth.NewOAuthStore()
	clientID := oauthRegisteredClientID(t, store, oauthTestRedirect)
	values := url.Values{
		"response_type": {"token"}, "client_id": {clientID},
		"redirect_uri": {oauthTestRedirect}, "state": {"client-state"},
	}
	response := httptest.NewRecorder()
	handleAuthorizeForTest(response, httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+values.Encode(), nil), cfg, store)
	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want redirect", response.Code)
	}
	location, err := url.Parse(response.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if location.Query().Get("error") != "unsupported_response_type" || location.Query().Get("state") != "client-state" {
		t.Fatalf("redirect query = %v", location.Query())
	}
}

func TestOAuthAuthorizeRequiresResourceAndRejectsRepeatedParameters(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_PASSWORD", "")
	cfg := oauthTestConfig(t)
	store := auth.NewOAuthStore()
	clientID := oauthRegisteredClientID(t, store, oauthTestRedirect)
	base := url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {oauthTestRedirect},
		"code_challenge": {oauthTestChallenge}, "code_challenge_method": {"S256"}, "state": {"strict"},
	}
	for name, test := range map[string]struct {
		mutate func(url.Values)
		error  string
	}{
		"missing resource": {func(url.Values) {}, "invalid_target"},
		"repeated state":   {func(v url.Values) { v["state"] = []string{"strict", "duplicate"} }, "invalid_request"},
	} {
		t.Run(name, func(t *testing.T) {
			values := cloneValues(base)
			test.mutate(values)
			response := httptest.NewRecorder()
			handleAuthorizeForTest(response, httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+values.Encode(), nil), cfg, store)
			if response.Code != http.StatusFound {
				t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
			}
			location, err := url.Parse(response.Header().Get("Location"))
			if err != nil || location.Query().Get("error") != test.error {
				t.Fatalf("location = %q, err=%v", response.Header().Get("Location"), err)
			}
		})
	}

	repeatedClient := cloneValues(base)
	repeatedClient.Set("resource", cfg.OAuthServerURL+"/mcp")
	repeatedClient["client_id"] = []string{clientID, clientID}
	response := httptest.NewRecorder()
	handleAuthorizeForTest(response, httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+repeatedClient.Encode(), nil), cfg, store)
	if response.Code != http.StatusBadRequest || response.Header().Get("Location") != "" {
		t.Fatalf("repeated client_id status=%d location=%q", response.Code, response.Header().Get("Location"))
	}

	postValues := cloneValues(base)
	postValues.Set("resource", cfg.OAuthServerURL+"/mcp")
	postRequest := formAuthorizeRequest(postValues)
	postRequest.URL.RawQuery = "state=query-state"
	response = httptest.NewRecorder()
	handleAuthorizeForTest(response, postRequest, cfg, store)
	if response.Code != http.StatusBadRequest || response.Header().Get("Location") != "" {
		t.Fatalf("POST query OAuth parameter status=%d location=%q", response.Code, response.Header().Get("Location"))
	}

	multipleResources := cloneValues(base)
	multipleResources["resource"] = []string{cfg.OAuthServerURL + "/mcp", cfg.OAuthServerURL + "/other"}
	response = httptest.NewRecorder()
	handleAuthorizeForTest(response, httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+multipleResources.Encode(), nil), cfg, store)
	location, err := url.Parse(response.Header().Get("Location"))
	if err != nil || location.Query().Get("error") != "invalid_target" {
		t.Fatalf("multiple resource location=%q err=%v", response.Header().Get("Location"), err)
	}
}

func TestOAuthTokenGrantsRequireResource(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "token-signing-secret")
	cfg := oauthTestConfig(t)
	store := auth.NewOAuthStore()
	clientID := oauthRegisteredClientID(t, store, oauthTestRedirect)
	code, err := store.Create(auth.OAuthCode{
		ClientID: clientID, RedirectURI: oauthTestRedirect,
		Challenge: oauthTestChallenge, Resource: cfg.OAuthServerURL + "/mcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	missingCodeResource := url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID},
		"redirect_uri": {oauthTestRedirect}, "code_verifier": {oauthTestVerifier},
	}
	assertOAuthError(t, postTokenRequest(t, cfg, store, missingCodeResource), http.StatusBadRequest, "invalid_target")

	missingRefreshResource := url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {"unused"}, "client_id": {clientID},
	}
	assertOAuthError(t, postTokenRequest(t, cfg, store, missingRefreshResource), http.StatusBadRequest, "invalid_target")
}

func TestOAuthTokenRejectsNonFormQueryAndDuplicateParameters(t *testing.T) {
	cfg := oauthTestConfig(t)
	store := auth.NewOAuthStore()
	resource := cfg.OAuthServerURL + "/mcp"
	for name, request := range map[string]*http.Request{
		"content type": httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(`{"grant_type":"authorization_code"}`)),
		"query":        formRequest(url.Values{"grant_type": {"authorization_code"}}),
		"resource query": formRequest(url.Values{
			"grant_type": {"authorization_code"}, "resource": {resource},
		}),
		"duplicate": formRequest(url.Values{"grant_type": {"authorization_code", "refresh_token"}}),
	} {
		if name == "query" {
			request.URL.RawQuery = "client_id=query-client"
		}
		if name == "resource query" {
			request.URL.RawQuery = "resource=" + url.QueryEscape(resource)
		}
		response := httptest.NewRecorder()
		handleToken(response, request, cfg, store)
		assertOAuthError(t, response, http.StatusBadRequest, "invalid_request")
	}
}

func handleAuthorizeForTest(w http.ResponseWriter, r *http.Request, cfg config.Config, store *auth.OAuthStore) {
	handleAuthorize(w, r, cfg, store)
}

func oauthRegisteredClientID(t *testing.T, store *auth.OAuthStore, redirectURI string) string {
	t.Helper()
	clientID, err := store.RegisterClient(
		"test client",
		[]string{redirectURI},
		[]string{"authorization_code", "refresh_token"},
	)
	if err != nil {
		t.Fatal(err)
	}
	return clientID
}

func oauthTestConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := testConfig(t)
	cfg.OAuthEnabled = true
	cfg.OAuthServerURL = "https://agentdock.example"
	return cfg
}

func postTokenRequest(t *testing.T, cfg config.Config, codes *auth.OAuthStore, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	request := formRequest(values)
	response := httptest.NewRecorder()
	handleToken(response, request, cfg, codes)
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

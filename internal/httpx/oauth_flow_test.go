package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/auth"
	"github.com/uvwt/agentdock/internal/config"
)

const (
	oauthTestVerifier  = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	oauthTestChallenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	oauthTestRedirect  = "https://client.example/callback"
)

func TestOAuthAuthorizationCodeFlow(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_PASSWORD", "")
	t.Setenv("AGENTDOCK_OAUTH_CLIENT_SECRET", "")
	t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "token-signing-secret")
	cfg := oauthTestConfig(t)
	codes := auth.NewOAuthStore()

	authorizeValues := url.Values{
		"client_id":             {cfg.OAuthClientID},
		"redirect_uri":          {oauthTestRedirect},
		"code_challenge":        {oauthTestChallenge},
		"code_challenge_method": {"S256"},
		"state":                 {"state-value"},
	}
	authorizeRequest := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+authorizeValues.Encode(), nil)
	authorizeResponse := httptest.NewRecorder()
	handleAuthorize(authorizeResponse, authorizeRequest, cfg, codes)
	if authorizeResponse.Code != http.StatusFound {
		t.Fatalf("authorize status = %d, want %d; body=%s", authorizeResponse.Code, http.StatusFound, authorizeResponse.Body.String())
	}
	location, err := url.Parse(authorizeResponse.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse authorization redirect: %v", err)
	}
	code := location.Query().Get("code")
	if code == "" {
		t.Fatalf("authorization redirect missing code: %s", location)
	}
	if got := location.Query().Get("state"); got != "state-value" {
		t.Fatalf("state = %q", got)
	}

	tokenValues := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {oauthTestRedirect},
		"client_id":     {cfg.OAuthClientID},
		"code_verifier": {oauthTestVerifier},
	}
	tokenResponse := postTokenRequest(t, cfg, codes, tokenValues)
	if tokenResponse.Code != http.StatusOK {
		t.Fatalf("token status = %d, want %d; body=%s", tokenResponse.Code, http.StatusOK, tokenResponse.Body.String())
	}
	var tokenPayload struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(tokenResponse.Body.Bytes(), &tokenPayload); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if tokenPayload.AccessToken == "" || tokenPayload.TokenType != "Bearer" || tokenPayload.ExpiresIn != 2592000 {
		t.Fatalf("token payload = %#v", tokenPayload)
	}

	authorizedRequest := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	authorizedRequest.Header.Set("Authorization", "Bearer "+tokenPayload.AccessToken)
	if !authorizedOAuth(authorizedRequest, cfg) {
		t.Fatal("authorizedOAuth() rejected exchanged access token")
	}

	replayResponse := postTokenRequest(t, cfg, codes, tokenValues)
	assertOAuthError(t, replayResponse, http.StatusBadRequest, "invalid_grant")
}

func TestOAuthClientAuthenticationContract(t *testing.T) {
	cfg := oauthTestConfig(t)
	request := func(values url.Values, basicUser, basicSecret string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if basicUser != "" || basicSecret != "" {
			req.SetBasicAuth(basicUser, basicSecret)
		}
		return req
	}

	t.Setenv("AGENTDOCK_OAUTH_CLIENT_SECRET", "")
	if !validClientAuthentication(request(url.Values{"client_id": {cfg.OAuthClientID}}, "", ""), cfg) {
		t.Fatal("public client authentication was rejected")
	}
	if validClientAuthentication(request(url.Values{}, "", ""), cfg) {
		t.Fatal("missing public client_id was accepted")
	}
	if validClientAuthentication(request(url.Values{"client_id": {cfg.OAuthClientID}}, cfg.OAuthClientID, "secret"), cfg) {
		t.Fatal("unexpected Basic authentication was accepted for public client")
	}

	t.Setenv("AGENTDOCK_OAUTH_CLIENT_SECRET", "client-secret")
	if !validClientAuthentication(request(url.Values{
		"client_id": {cfg.OAuthClientID}, "client_secret": {"client-secret"},
	}, "", ""), cfg) {
		t.Fatal("client_secret_post authentication was rejected")
	}
	if validClientAuthentication(request(url.Values{
		"client_id": {cfg.OAuthClientID}, "client_secret": {"wrong"},
	}, "", ""), cfg) {
		t.Fatal("wrong client_secret_post secret was accepted")
	}
	if validClientAuthentication(request(url.Values{"client_id": {cfg.OAuthClientID}}, cfg.OAuthClientID, "client-secret"), cfg) {
		t.Fatal("client_secret_basic was accepted despite client_secret_post metadata")
	}
}

func TestOAuthTokenErrorsUseHTTPFailureStatuses(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_PASSWORD", "")
	t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "token-signing-secret")
	cfg := oauthTestConfig(t)

	t.Run("invalid client", func(t *testing.T) {
		t.Setenv("AGENTDOCK_OAUTH_CLIENT_SECRET", "client-secret")
		response := postTokenRequest(t, cfg, auth.NewOAuthStore(), url.Values{
			"grant_type":    {"authorization_code"},
			"client_id":     {cfg.OAuthClientID},
			"client_secret": {"wrong"},
		})
		assertOAuthError(t, response, http.StatusUnauthorized, "invalid_client")
	})

	t.Run("unsupported grant", func(t *testing.T) {
		t.Setenv("AGENTDOCK_OAUTH_CLIENT_SECRET", "")
		response := postTokenRequest(t, cfg, auth.NewOAuthStore(), url.Values{
			"grant_type": {"client_credentials"},
			"client_id":  {cfg.OAuthClientID},
		})
		assertOAuthError(t, response, http.StatusBadRequest, "unsupported_grant_type")
	})

	t.Run("invalid authorization code", func(t *testing.T) {
		t.Setenv("AGENTDOCK_OAUTH_CLIENT_SECRET", "")
		response := postTokenRequest(t, cfg, auth.NewOAuthStore(), url.Values{
			"grant_type": {"authorization_code"},
			"client_id":  {cfg.OAuthClientID},
			"code":       {"unknown"},
		})
		assertOAuthError(t, response, http.StatusBadRequest, "invalid_grant")
	})

	t.Run("missing signing key", func(t *testing.T) {
		t.Setenv("AGENTDOCK_OAUTH_CLIENT_SECRET", "")
		t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "")
		codes := auth.NewOAuthStore()
		code, err := codes.Create(auth.OAuthCode{
			ClientID: cfg.OAuthClientID, RedirectURI: oauthTestRedirect, Challenge: oauthTestChallenge,
		})
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		response := postTokenRequest(t, cfg, codes, url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"redirect_uri":  {oauthTestRedirect},
			"client_id":     {cfg.OAuthClientID},
			"code_verifier": {oauthTestVerifier},
		})
		assertOAuthError(t, response, http.StatusInternalServerError, "server_error")
	})
}

func TestOAuthAuthorizePasswordGate(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_PASSWORD", "login-secret")
	cfg := oauthTestConfig(t)
	codes := auth.NewOAuthStore()
	values := url.Values{
		"client_id":             {cfg.OAuthClientID},
		"redirect_uri":          {oauthTestRedirect},
		"code_challenge":        {oauthTestChallenge},
		"code_challenge_method": {"S256"},
		"state":                 {"state-value"},
	}

	getRequest := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+values.Encode(), nil)
	getResponse := httptest.NewRecorder()
	handleAuthorize(getResponse, getRequest, cfg, codes)
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), "type='password'") {
		t.Fatalf("GET authorize response = status %d body %q", getResponse.Code, getResponse.Body.String())
	}

	values.Set("password", "wrong")
	wrongRequest := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(values.Encode()))
	wrongRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	wrongResponse := httptest.NewRecorder()
	handleAuthorize(wrongResponse, wrongRequest, cfg, codes)
	if wrongResponse.Code != http.StatusOK || !strings.Contains(wrongResponse.Body.String(), "invalid password") {
		t.Fatalf("wrong password response = status %d body %q", wrongResponse.Code, wrongResponse.Body.String())
	}

	values.Set("password", "login-secret")
	validRequest := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(values.Encode()))
	validRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	validResponse := httptest.NewRecorder()
	handleAuthorize(validResponse, validRequest, cfg, codes)
	if validResponse.Code != http.StatusFound {
		t.Fatalf("valid password status = %d, want %d; body=%s", validResponse.Code, http.StatusFound, validResponse.Body.String())
	}
}

func TestOAuthRedirectURIValidation(t *testing.T) {
	valid := []string{
		"https://client.example/callback",
		"https://client.example:8443/callback?source=agentdock",
		"http://localhost:3000/callback",
		"http://127.0.0.1:3000/callback",
		"http://[::1]:3000/callback",
	}
	for _, redirectURI := range valid {
		if !validOAuthRedirectURI(redirectURI) {
			t.Errorf("validOAuthRedirectURI(%q) = false, want true", redirectURI)
		}
	}
	invalid := []string{
		"", "relative/callback", "javascript:alert(1)",
		"http://client.example/callback", "ftp://client.example/callback",
		"https://user:pass@client.example/callback", "https://client.example/callback#fragment",
	}
	for _, redirectURI := range invalid {
		if validOAuthRedirectURI(redirectURI) {
			t.Errorf("validOAuthRedirectURI(%q) = true, want false", redirectURI)
		}
	}
}

func TestOAuthAuthorizeRejectsInvalidPKCEChallenge(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_PASSWORD", "")
	cfg := oauthTestConfig(t)
	for _, challenge := range []string{"short", strings.Repeat("!", 43), oauthTestChallenge + "="} {
		values := url.Values{
			"client_id": {cfg.OAuthClientID}, "redirect_uri": {oauthTestRedirect},
			"code_challenge": {challenge}, "code_challenge_method": {"S256"},
		}
		request := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+values.Encode(), nil)
		response := httptest.NewRecorder()
		handleAuthorize(response, request, cfg, auth.NewOAuthStore())
		if response.Code != http.StatusBadRequest || response.Header().Get("Location") != "" {
			t.Fatalf("challenge %q status=%d location=%q", challenge, response.Code, response.Header().Get("Location"))
		}
	}
}

func TestOAuthAuthorizeRejectsUnsafeRedirectURI(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_PASSWORD", "")
	cfg := oauthTestConfig(t)
	values := url.Values{
		"client_id": {cfg.OAuthClientID}, "redirect_uri": {"http://attacker.example/callback"},
		"code_challenge": {oauthTestChallenge}, "code_challenge_method": {"S256"},
	}
	request := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+values.Encode(), nil)
	response := httptest.NewRecorder()
	handleAuthorize(response, request, cfg, auth.NewOAuthStore())
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; location=%q body=%s", response.Code, http.StatusBadRequest, response.Header().Get("Location"), response.Body.String())
	}
	if response.Header().Get("Location") != "" {
		t.Fatalf("unsafe redirect produced Location header %q", response.Header().Get("Location"))
	}
}

func TestOAuthEndpointsRejectMalformedForms(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_PASSWORD", "")
	cfg := oauthTestConfig(t)
	for _, endpoint := range []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request, config.Config, *auth.OAuthStore)
	}{
		{name: "authorize", handler: handleAuthorize},
		{name: "token", handler: handleToken},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/oauth/"+endpoint.name, strings.NewReader("broken=%ZZ"))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response := httptest.NewRecorder()
			endpoint.handler(response, request, cfg, auth.NewOAuthStore())
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), "bad form") {
				t.Fatalf("body = %q, want bad form", response.Body.String())
			}
		})
	}
}

func oauthTestConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := testConfig(t)
	cfg.OAuthClientID = "client-id"
	cfg.OAuthServerURL = "https://agentdock.example"
	return cfg
}

func postTokenRequest(t *testing.T, cfg config.Config, codes *auth.OAuthStore, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handleToken(response, request, cfg, codes)
	return response
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
		t.Fatalf("decode OAuth error: %v", err)
	}
	if payload.Error != code {
		t.Fatalf("error = %q, want %q", payload.Error, code)
	}
}

func TestOAuthRegistrationContract(t *testing.T) {
	cfg := oauthTestConfig(t)

	t.Run("public client", func(t *testing.T) {
		t.Setenv("AGENTDOCK_OAUTH_CLIENT_SECRET", "")
		request := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{"token_endpoint_auth_method":"none"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handleRegister(response, request, cfg)
		if response.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
		}
		var payload struct {
			ClientID                string `json:"client_id"`
			TokenEndpointAuthMethod string `json:"token_endpoint_auth_method"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.ClientID != cfg.OAuthClientID || payload.TokenEndpointAuthMethod != "none" {
			t.Fatalf("registration response = %#v", payload)
		}
	})

	t.Run("confidential client", func(t *testing.T) {
		t.Setenv("AGENTDOCK_OAUTH_CLIENT_SECRET", "client-secret")
		request := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{"token_endpoint_auth_method":"client_secret_post"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handleRegister(response, request, cfg)
		if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"token_endpoint_auth_method":"client_secret_post"`) {
			t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("unsupported method", func(t *testing.T) {
		t.Setenv("AGENTDOCK_OAUTH_CLIENT_SECRET", "")
		request := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{"token_endpoint_auth_method":"client_secret_basic"}`))
		response := httptest.NewRecorder()
		handleRegister(response, request, cfg)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "not supported") {
			t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("malformed metadata", func(t *testing.T) {
		t.Setenv("AGENTDOCK_OAUTH_CLIENT_SECRET", "")
		for _, body := range []string{"", `{`, `{}` + `{}`} {
			response := httptest.NewRecorder()
			handleRegister(response, httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(body)), cfg)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("body %q status = %d, want %d", body, response.Code, http.StatusBadRequest)
			}
		}
	})

	t.Run("oversized metadata", func(t *testing.T) {
		t.Setenv("AGENTDOCK_OAUTH_CLIENT_SECRET", "")
		body := `{"token_endpoint_auth_method":"none","padding":"` + strings.Repeat("x", 1<<20) + `"}`
		response := httptest.NewRecorder()
		handleRegister(response, httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(body)), cfg)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "request body too large") {
			t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("method not allowed", func(t *testing.T) {
		response := httptest.NewRecorder()
		handleRegister(response, httptest.NewRequest(http.MethodGet, "/register", nil), cfg)
		if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodPost {
			t.Fatalf("status = %d Allow=%q", response.Code, response.Header().Get("Allow"))
		}
	})
}

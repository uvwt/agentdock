package httpx

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/auth"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/mcp"
	"github.com/uvwt/agentdock/internal/tools"
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	root := t.TempDir()
	cfg := config.Config{AgentDockDefaultDir: root, AgentDockHome: filepath.Join(root, ".agentdock")}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	return cfg
}

func TestHTTPServerHasDefensiveConnectionLimits(t *testing.T) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	server := newHTTPServer("127.0.0.1:0", handler)
	if server.Addr != "127.0.0.1:0" || server.Handler == nil {
		t.Fatalf("server = %#v", server)
	}
	if server.ReadHeaderTimeout != 10*time.Second || server.ReadTimeout != 15*time.Second || server.IdleTimeout != 60*time.Second {
		t.Fatalf("timeouts = header:%s read:%s idle:%s", server.ReadHeaderTimeout, server.ReadTimeout, server.IdleTimeout)
	}
	if server.MaxHeaderBytes != 1<<20 {
		t.Fatalf("MaxHeaderBytes = %d, want %d", server.MaxHeaderBytes, 1<<20)
	}
}

func TestMCPEndpointNotificationReturnsAcceptedWithEmptyBody(t *testing.T) {
	cfg := testConfig(t)
	runtime, err := tools.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	handler := mcpEndpointHandler(mcp.NewServer(runtime, cfg), cfg)

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusAccepted)
	}
	if body := recorder.Body.String(); body != "" {
		t.Fatalf("body = %q, want empty", body)
	}
}

func TestMCPEndpointRejectsTrailingJSONValue(t *testing.T) {
	cfg := testConfig(t)
	runtime, err := tools.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	handler := mcpEndpointHandler(mcp.NewServer(runtime, cfg), cfg)
	body := `{"jsonrpc":"2.0","id":1,"method":"ping"} {"jsonrpc":"2.0","id":2,"method":"ping"}`
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Body.String(), `"code":-32700`) || !strings.Contains(recorder.Body.String(), "exactly one JSON value") {
		t.Fatalf("response = %s", recorder.Body.String())
	}
}

func TestMCPEndpointRejectsOversizedBody(t *testing.T) {
	cfg := testConfig(t)
	runtime, err := tools.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	handler := mcpEndpointHandler(mcp.NewServer(runtime, cfg), cfg)
	body := `{"jsonrpc":"2.0","id":1,"method":"ping"}` + strings.Repeat(" ", (1<<20)+1)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body)))
	if !strings.Contains(recorder.Body.String(), `"code":-32700`) || !strings.Contains(recorder.Body.String(), "request body too large") {
		t.Fatalf("response = %s", recorder.Body.String())
	}
}

func TestRuntimeAPIRequiresBearerWhenConfigured(t *testing.T) {
	cfg := testConfig(t)
	cfg.AuthToken = "secret-token"
	runtime, err := tools.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	handler := runtimeAPIHandler(mcp.NewServer(runtime, cfg), cfg)

	req := httptest.NewRequest(http.MethodGet, "/internal/runtime/status", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestRuntimeAPIStatusWithBearer(t *testing.T) {
	cfg := testConfig(t)
	cfg.AuthToken = "secret-token"
	runtime, err := tools.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	handler := runtimeAPIHandler(mcp.NewServer(runtime, cfg), cfg)

	req := httptest.NewRequest(http.MethodGet, "/internal/runtime/status", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"source":"agentdock-runtime-api"`) {
		t.Fatalf("body missing source: %s", body)
	}
	if strings.Contains(body, "secret-token") {
		t.Fatalf("status response leaked token: %s", body)
	}
}

func TestRuntimeAPISkillsNoAuthWhenUnconfigured(t *testing.T) {
	cfg := testConfig(t)
	runtime, err := tools.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	handler := runtimeAPIHandler(mcp.NewServer(runtime, cfg), cfg)

	req := httptest.NewRequest(http.MethodGet, "/internal/runtime/skills", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"skills"`) {
		t.Fatalf("body missing skills: %s", recorder.Body.String())
	}
}

func TestRuntimeAPIRejectsInvalidTaskQuery(t *testing.T) {
	cfg := testConfig(t)
	runtime, err := tools.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	handler := runtimeAPIHandler(mcp.NewServer(runtime, cfg), cfg)
	tests := []struct {
		name string
		url  string
		code string
	}{
		{name: "non-integer limit", url: "/internal/runtime/tasks?limit=many", code: "INVALID_LIMIT"},
		{name: "negative limit", url: "/internal/runtime/tasks?limit=-1", code: "INVALID_LIMIT"},
		{name: "excessive limit", url: "/internal/runtime/tasks?limit=201", code: "INVALID_LIMIT"},
		{name: "invalid status", url: "/internal/runtime/tasks?status=paused", code: "INVALID_STATUS"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.url, nil))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("body missing code %s: %s", test.code, recorder.Body.String())
			}
		})
	}
}

func TestRuntimeAPIUnknownRouteReturnsNotFound(t *testing.T) {
	cfg := testConfig(t)
	runtime, err := tools.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	handler := runtimeAPIHandler(mcp.NewServer(runtime, cfg), cfg)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/internal/runtime/unknown", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"code":"NOT_FOUND"`) {
		t.Fatalf("body missing NOT_FOUND code: %s", recorder.Body.String())
	}
}

func TestRuntimeAPIMethodContract(t *testing.T) {
	cfg := testConfig(t)
	runtime, err := tools.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	handler := runtimeAPIHandler(mcp.NewServer(runtime, cfg), cfg)
	tests := []struct {
		method string
		path   string
		status int
		allow  string
	}{
		{method: http.MethodGet, path: "/internal/runtime/status", status: http.StatusOK},
		{method: http.MethodPost, path: "/internal/runtime/capabilities", status: http.StatusOK},
		{method: http.MethodPost, path: "/internal/runtime/status", status: http.StatusMethodNotAllowed, allow: "GET"},
		{method: http.MethodDelete, path: "/internal/runtime/capabilities", status: http.StatusMethodNotAllowed, allow: "GET, POST"},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, nil))
			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.status, recorder.Body.String())
			}
			if got := recorder.Header().Get("Allow"); got != test.allow {
				t.Fatalf("Allow = %q, want %q", got, test.allow)
			}
		})
	}
}

func TestAgentDockContextRequiresBearerEvenOnLoopback(t *testing.T) {
	cfg := testConfig(t)
	cfg.Host = "127.0.0.1"
	cfg.AuthToken = "secret-token"
	runtime, err := tools.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	handler := agentDockContextHandler(mcp.NewServer(runtime, cfg), cfg)

	req := httptest.NewRequest(http.MethodGet, "/capabilities/context", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestAgentDockContextAcceptsBearer(t *testing.T) {
	cfg := testConfig(t)
	cfg.AuthToken = "secret-token"
	runtime, err := tools.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	handler := agentDockContextHandler(mcp.NewServer(runtime, cfg), cfg)

	req := httptest.NewRequest(http.MethodGet, "/capabilities/context", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
}

func TestServerURLAloneDoesNotRequireAuthOrDeclareOAuth(t *testing.T) {
	cfg := testConfig(t)
	cfg.OAuthServerURL = "https://agentdock.example.com"
	runtime, err := tools.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	handler := mcpEndpointHandler(mcp.NewServer(runtime, cfg), cfg)

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}

	card := serverCard(cfg, httptest.NewRequest(http.MethodGet, "/.well-known/mcp.json", nil))
	authInfo := card["auth"].(map[string]any)
	if authInfo["type"] != "none" {
		t.Fatalf("auth type = %v, want none", authInfo["type"])
	}
}

func TestServerCardDeclaresOAuthOnlyWhenOAuthEnabled(t *testing.T) {
	cfg := testConfig(t)
	cfg.OAuthClientID = "client-id"
	cfg.OAuthServerURL = "https://agentdock.example.com"
	card := serverCard(cfg, httptest.NewRequest(http.MethodGet, "/.well-known/mcp.json", nil))
	authInfo := card["auth"].(map[string]any)
	if authInfo["type"] != "oauth2" {
		t.Fatalf("auth type = %v, want oauth2", authInfo["type"])
	}
	if authInfo["authorizationUrl"] != "https://agentdock.example.com/oauth/authorize" {
		t.Fatalf("authorizationUrl = %v", authInfo["authorizationUrl"])
	}
	metadata := oauthMetadata(cfg, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil))
	if metadata["issuer"] != "https://agentdock.example.com" {
		t.Fatalf("issuer = %v", metadata["issuer"])
	}
	if metadata["token_endpoint"] != "https://agentdock.example.com/oauth/token" {
		t.Fatalf("token_endpoint = %v", metadata["token_endpoint"])
	}
}

func TestOAuthMetadataOmitsNoneWhenClientSecretConfigured(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_CLIENT_SECRET", "client-secret")
	cfg := testConfig(t)
	cfg.OAuthClientID = "client-id"
	cfg.OAuthServerURL = "https://agentdock.example.com"

	metadata := oauthMetadata(cfg, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil))
	methods, ok := metadata["token_endpoint_auth_methods_supported"].([]string)
	if !ok {
		t.Fatalf("auth methods type = %T", metadata["token_endpoint_auth_methods_supported"])
	}
	for _, method := range methods {
		if method == "none" {
			t.Fatalf("auth methods include none despite configured client secret: %#v", methods)
		}
	}
	if len(methods) != 1 || methods[0] != "client_secret_post" {
		t.Fatalf("auth methods = %#v, want client_secret_post", methods)
	}
}

func TestOAuthMetadataAllowsNoneWhenClientSecretUnconfigured(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_CLIENT_SECRET", "")
	cfg := testConfig(t)
	cfg.OAuthClientID = "client-id"
	cfg.OAuthServerURL = "https://agentdock.example.com"

	metadata := oauthMetadata(cfg, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil))
	methods, ok := metadata["token_endpoint_auth_methods_supported"].([]string)
	if !ok || len(methods) != 1 || methods[0] != "none" {
		t.Fatalf("auth methods = %#v, want none", metadata["token_endpoint_auth_methods_supported"])
	}
}

func TestValidClientAuthenticationRequiresConfiguredSecret(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_CLIENT_SECRET", "client-secret")
	cfg := testConfig(t)
	cfg.OAuthClientID = "client-id"

	missing := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader("grant_type=authorization_code&client_id=client-id"))
	missing.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if validClientAuthentication(missing, cfg) {
		t.Fatal("missing client_secret authenticated despite configured secret")
	}

	wrong := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader("client_id=client-id&client_secret=wrong"))
	wrong.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if validClientAuthentication(wrong, cfg) {
		t.Fatal("wrong client_secret authenticated")
	}

	post := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader("client_id=client-id&client_secret=client-secret"))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if !validClientAuthentication(post, cfg) {
		t.Fatal("valid client_secret_post rejected")
	}

	basic := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader("client_id=client-id"))
	basic.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	basic.SetBasicAuth("client-id", "client-secret")
	if validClientAuthentication(basic, cfg) {
		t.Fatal("client_secret_basic accepted despite client_secret_post metadata")
	}
}

func TestValidClientAuthenticationAllowsPublicClientWhenSecretUnconfigured(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_CLIENT_SECRET", "")
	cfg := testConfig(t)
	cfg.OAuthClientID = "client-id"
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader("client_id=client-id"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if !validClientAuthentication(req, cfg) {
		t.Fatal("public OAuth client rejected when no client secret is configured")
	}
}

func TestOAuthEntrypointsDisabledWhenOAuthNotEnabled(t *testing.T) {
	cfg := testConfig(t)
	cfg.OAuthServerURL = "https://agentdock.example.com"
	codes := auth.NewOAuthStore()

	endpoints := []struct {
		name    string
		handler http.HandlerFunc
		method  string
		path    string
	}{
		{name: "register", handler: func(w http.ResponseWriter, r *http.Request) { handleRegister(w, r, cfg) }, method: http.MethodPost, path: "/register"},
		{name: "authorize", handler: func(w http.ResponseWriter, r *http.Request) { handleAuthorize(w, r, cfg, codes) }, method: http.MethodGet, path: "/oauth/authorize"},
		{name: "token", handler: func(w http.ResponseWriter, r *http.Request) { handleToken(w, r, cfg, codes) }, method: http.MethodPost, path: "/oauth/token"},
	}
	for _, endpoint := range endpoints {
		t.Run(endpoint.name, func(t *testing.T) {
			req := httptest.NewRequest(endpoint.method, endpoint.path, nil)
			recorder := httptest.NewRecorder()
			endpoint.handler.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusNotFound, recorder.Body.String())
			}
		})
	}
}

func TestAuthorizedOAuthFalseWhenOAuthDisabled(t *testing.T) {
	cfg := testConfig(t)
	cfg.OAuthServerURL = "https://agentdock.example.com"
	t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "token-secret")
	token, err := auth.IssueToken("https://agentdock.example.com", "token-secret", time.Hour)
	if err != nil {
		t.Fatalf("IssueToken() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if authorizedOAuth(req, cfg) {
		t.Fatalf("authorizedOAuth() = true when OAuth is disabled")
	}
}

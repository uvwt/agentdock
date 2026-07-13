package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	handler := mcpEndpointHandler(mcp.NewServer(runtime, cfg), cfg, auth.NewOAuthStore())

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
	handler := mcpEndpointHandler(mcp.NewServer(runtime, cfg), cfg, auth.NewOAuthStore())
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
	handler := mcpEndpointHandler(mcp.NewServer(runtime, cfg), cfg, auth.NewOAuthStore())
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
	handler := runtimeAPIHandler(mcp.NewServer(runtime, cfg), cfg, auth.NewOAuthStore())

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
	handler := runtimeAPIHandler(mcp.NewServer(runtime, cfg), cfg, auth.NewOAuthStore())

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
	handler := runtimeAPIHandler(mcp.NewServer(runtime, cfg), cfg, auth.NewOAuthStore())

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
	handler := runtimeAPIHandler(mcp.NewServer(runtime, cfg), cfg, auth.NewOAuthStore())
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

func TestRuntimeAPIDeletesOnlySelectedTask(t *testing.T) {
	cfg := testConfig(t)
	runtime, err := tools.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	createTask := func(title string) string {
		t.Helper()
		created, err := runtime.Call(context.Background(), "task_manage", map[string]any{
			"action":                "create",
			"title":                 title,
			"goal":                  "Verify Runtime API task deletion",
			"steps":                 []map[string]any{{"id": "verify", "title": "Verify deletion"}},
			"completion_conditions": []string{"only the selected task is deleted"},
		})
		if err != nil {
			t.Fatalf("create task: %v", err)
		}
		taskID, _ := created["task_id"].(string)
		if taskID == "" {
			t.Fatalf("created task missing id: %#v", created)
		}
		return taskID
	}
	deletedTaskID := createTask("Delete me")
	keptTaskID := createTask("Keep me")

	handler := runtimeAPIHandler(mcp.NewServer(runtime, cfg), cfg, auth.NewOAuthStore())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/internal/runtime/tasks/"+deletedTaskID, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var payload struct {
		Action      string `json:"action"`
		TaskID      string `json:"task_id"`
		DeletedTask struct {
			Title string `json:"title"`
		} `json:"deleted_task"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Action != "delete" || payload.TaskID != deletedTaskID || payload.DeletedTask.Title != "Delete me" {
		t.Fatalf("unexpected delete response: %#v", payload)
	}

	deletedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deletedRecorder, httptest.NewRequest(http.MethodGet, "/internal/runtime/tasks/"+deletedTaskID, nil))
	if deletedRecorder.Code != http.StatusNotFound || !strings.Contains(deletedRecorder.Body.String(), `"code":"TASK_NOT_FOUND"`) {
		t.Fatalf("deleted task lookup status=%d body=%s", deletedRecorder.Code, deletedRecorder.Body.String())
	}
	keptRecorder := httptest.NewRecorder()
	handler.ServeHTTP(keptRecorder, httptest.NewRequest(http.MethodGet, "/internal/runtime/tasks/"+keptTaskID, nil))
	if keptRecorder.Code != http.StatusOK {
		t.Fatalf("kept task lookup status=%d body=%s", keptRecorder.Code, keptRecorder.Body.String())
	}
}

func TestRuntimeAPIUnknownRouteReturnsNotFound(t *testing.T) {
	cfg := testConfig(t)
	runtime, err := tools.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	handler := runtimeAPIHandler(mcp.NewServer(runtime, cfg), cfg, auth.NewOAuthStore())
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
	handler := runtimeAPIHandler(mcp.NewServer(runtime, cfg), cfg, auth.NewOAuthStore())
	tests := []struct {
		method string
		path   string
		status int
		allow  string
	}{
		{method: http.MethodGet, path: "/internal/runtime/status", status: http.StatusOK},
		{method: http.MethodPost, path: "/internal/runtime/capabilities", status: http.StatusOK},
		{method: http.MethodPost, path: "/internal/runtime/status", status: http.StatusMethodNotAllowed, allow: "GET"},
		{method: http.MethodPost, path: "/internal/runtime/tasks/tsk_1234567890abcdef", status: http.StatusMethodNotAllowed, allow: "GET, DELETE"},
		{method: http.MethodDelete, path: "/internal/runtime/tasks", status: http.StatusMethodNotAllowed, allow: "GET"},
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
	handler := agentDockContextHandler(mcp.NewServer(runtime, cfg), cfg, auth.NewOAuthStore())

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
	handler := agentDockContextHandler(mcp.NewServer(runtime, cfg), cfg, auth.NewOAuthStore())

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
	handler := mcpEndpointHandler(mcp.NewServer(runtime, cfg), cfg, auth.NewOAuthStore())

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
	cfg.OAuthEnabled = true
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
	if metadata["registration_endpoint"] != "https://agentdock.example.com/register" {
		t.Fatalf("registration_endpoint = %v", metadata["registration_endpoint"])
	}
}

func TestOAuthMetadataUsesPublicPKCEClients(t *testing.T) {
	cfg := testConfig(t)
	cfg.OAuthEnabled = true
	cfg.OAuthServerURL = "https://agentdock.example.com"

	metadata := oauthMetadata(cfg, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil))
	methods, ok := metadata["token_endpoint_auth_methods_supported"].([]string)
	if !ok || len(methods) != 1 || methods[0] != "none" {
		t.Fatalf("auth methods = %#v, want none", metadata["token_endpoint_auth_methods_supported"])
	}
	if supported, ok := metadata["resource_indicators_supported"].(bool); !ok || !supported {
		t.Fatalf("resource_indicators_supported = %#v", metadata["resource_indicators_supported"])
	}
	grantTypes, ok := metadata["grant_types_supported"].([]string)
	if !ok || !containsString(grantTypes, "authorization_code") || !containsString(grantTypes, "refresh_token") {
		t.Fatalf("grant_types_supported = %#v", metadata["grant_types_supported"])
	}
}

func TestValidClientAuthenticationRequiresRegisteredPublicClient(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "token-secret")
	redirectURI := "https://client.example/callback"
	store := auth.NewOAuthStore()
	clientID, err := store.RegisterClient(
		"test client",
		[]string{redirectURI},
		[]string{"authorization_code", "refresh_token"},
	)
	if err != nil {
		t.Fatal(err)
	}

	values := url.Values{"client_id": {clientID}, "redirect_uri": {redirectURI}}
	valid := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(values.Encode()))
	valid.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if !validClientAuthentication(valid, "authorization_code", store) {
		t.Fatal("registered public client rejected")
	}

	values.Set("client_secret", "not-allowed")
	withSecret := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(values.Encode()))
	withSecret.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if validClientAuthentication(withSecret, "authorization_code", store) {
		t.Fatal("client_secret_post accepted for public client")
	}

	wrongRedirectValues := url.Values{"client_id": {clientID}, "redirect_uri": {"https://other.example/callback"}}
	wrongRedirect := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(wrongRedirectValues.Encode()))
	wrongRedirect.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if validClientAuthentication(wrongRedirect, "authorization_code", store) {
		t.Fatal("unregistered redirect URI accepted")
	}
}

func TestBearerChallengeReferencesPathSpecificResourceMetadata(t *testing.T) {
	cfg := testConfig(t)
	cfg.OAuthEnabled = true
	cfg.OAuthServerURL = "https://agentdock.example.com"
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	setBearerChallenge(recorder, cfg, request, false)
	want := `Bearer resource_metadata="https://agentdock.example.com/.well-known/oauth-protected-resource/mcp"`
	if got := recorder.Header().Get("WWW-Authenticate"); got != want {
		t.Fatalf("WWW-Authenticate = %q, want %q", got, want)
	}
	recorder = httptest.NewRecorder()
	setBearerChallenge(recorder, cfg, request, true)
	want = `Bearer resource_metadata="https://agentdock.example.com/.well-known/oauth-protected-resource/mcp", error="invalid_token"`
	if got := recorder.Header().Get("WWW-Authenticate"); got != want {
		t.Fatalf("invalid-token WWW-Authenticate = %q, want %q", got, want)
	}
}

func TestFixedWindowLimiterResetsAfterWindow(t *testing.T) {
	limiter := newFixedWindowLimiter(2, time.Minute)
	now := time.Now()
	if !limiter.Allow("client", now) || !limiter.Allow("client", now) {
		t.Fatal("limiter rejected requests within allowance")
	}
	if limiter.Allow("client", now) {
		t.Fatal("limiter accepted request above allowance")
	}
	if !limiter.Allow("client", now.Add(time.Minute)) {
		t.Fatal("limiter did not reset after its window")
	}
}

func TestRegisterOAuthRoutesExposesOnlyCanonicalEndpoints(t *testing.T) {
	cfg := testConfig(t)
	cfg.OAuthEnabled = true
	cfg.OAuthServerURL = "https://agentdock.example.com"
	mux := http.NewServeMux()
	registerOAuthRoutes(mux, cfg, auth.NewOAuthStore())

	canonical := []struct {
		path       string
		wantStatus int
	}{
		{path: "/.well-known/oauth-authorization-server", wantStatus: http.StatusOK},
		{path: "/.well-known/oauth-protected-resource/mcp", wantStatus: http.StatusOK},
		{path: "/register", wantStatus: http.StatusMethodNotAllowed},
		{path: "/oauth/authorize", wantStatus: http.StatusBadRequest},
		{path: "/oauth/token", wantStatus: http.StatusMethodNotAllowed},
	}
	for _, endpoint := range canonical {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, endpoint.path, nil))
		if recorder.Code != endpoint.wantStatus {
			t.Fatalf("canonical endpoint %s status = %d, want %d", endpoint.path, recorder.Code, endpoint.wantStatus)
		}
	}

	for _, oldPath := range []string{
		"/authorize",
		"/token",
		"/.well-known/openid-configuration",
		"/.well-known/oauth-protected-resource",
		"/mcp/.well-known/oauth-protected-resource",
	} {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, oldPath, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("old OAuth endpoint %s status = %d, want %d", oldPath, recorder.Code, http.StatusNotFound)
		}
	}
}

func TestOAuthMetadataEndpointsOnlyAllowGet(t *testing.T) {
	cfg := oauthTestConfig(t)
	mux := http.NewServeMux()
	registerOAuthRoutes(mux, cfg, auth.NewOAuthStore())
	for _, path := range []string{
		"/.well-known/oauth-authorization-server",
		"/.well-known/oauth-protected-resource/mcp",
	} {
		request := httptest.NewRequest(http.MethodPost, path, nil)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodGet {
			t.Fatalf("POST %s status=%d Allow=%q", path, response.Code, response.Header().Get("Allow"))
		}
	}
}

func TestRegisterOAuthRoutesDoesNothingWhenOAuthDisabled(t *testing.T) {
	cfg := testConfig(t)
	cfg.OAuthServerURL = "https://agentdock.example.com"
	mux := http.NewServeMux()
	registerOAuthRoutes(mux, cfg, auth.NewOAuthStore())

	for _, path := range []string{
		"/.well-known/oauth-authorization-server",
		"/.well-known/oauth-protected-resource/mcp",
		"/register",
		"/oauth/authorize",
		"/oauth/token",
	} {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("disabled OAuth endpoint %s status = %d, want %d", path, recorder.Code, http.StatusNotFound)
		}
	}
}

func TestAuthorizedOAuthFalseWhenOAuthDisabled(t *testing.T) {
	cfg := testConfig(t)
	cfg.OAuthServerURL = "https://agentdock.example.com"
	t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "token-secret")
	token, err := auth.IssueToken("https://agentdock.example.com", "https://agentdock.example.com/mcp", "grant-id", "token-secret", time.Hour)
	if err != nil {
		t.Fatalf("IssueToken() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if authorizedOAuth(req, cfg, auth.NewOAuthStore()) {
		t.Fatalf("authorizedOAuth() = true when OAuth is disabled")
	}
}

func TestServeHTTPStopsCleanlyWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server := newHTTPServer("127.0.0.1:0", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	done := make(chan error, 1)
	go func() { done <- serveHTTP(ctx, server) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveHTTP() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serveHTTP did not stop after context cancellation")
	}
}

func TestRequestRemoteIPOnlyTrustsConfiguredProxyChain(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		forwarded  string
		trusted    []string
		want       string
	}{
		{name: "untrusted direct peer ignores spoofed header", remoteAddr: "198.51.100.10:443", forwarded: "203.0.113.99", trusted: []string{"127.0.0.0/8"}, want: "198.51.100.10"},
		{name: "trusted proxy uses nearest untrusted hop", remoteAddr: "127.0.0.1:1234", forwarded: "203.0.113.9, 10.1.2.3", trusted: []string{"127.0.0.0/8", "10.0.0.0/8"}, want: "203.0.113.9"},
		{name: "attacker prefix cannot replace nearest client", remoteAddr: "127.0.0.1:1234", forwarded: "192.0.2.123, 198.51.100.25", trusted: []string{"127.0.0.0/8"}, want: "198.51.100.25"},
		{name: "invalid chain falls back to direct proxy", remoteAddr: "127.0.0.1:1234", forwarded: "bad-value", trusted: []string{"127.0.0.0/8"}, want: "127.0.0.1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://agent.test/oauth/authorize", nil)
			req.RemoteAddr = test.remoteAddr
			req.Header.Set("X-Forwarded-For", test.forwarded)
			if got := requestRemoteIP(req, config.Config{TrustedProxyCIDRs: test.trusted}); got != test.want {
				t.Fatalf("requestRemoteIP() = %q, want %q", got, test.want)
			}
		})
	}
}

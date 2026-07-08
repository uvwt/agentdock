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

func TestCapabilityContextRequiresBearerEvenOnLoopback(t *testing.T) {
	cfg := testConfig(t)
	cfg.Host = "127.0.0.1"
	cfg.AuthToken = "secret-token"
	runtime, err := tools.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	handler := capabilityContextHandler(mcp.NewServer(runtime, cfg), cfg, false)

	req := httptest.NewRequest(http.MethodGet, "/capabilities/context", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestCapabilityContextAcceptsBearer(t *testing.T) {
	cfg := testConfig(t)
	cfg.AuthToken = "secret-token"
	runtime, err := tools.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	handler := capabilityContextHandler(mcp.NewServer(runtime, cfg), cfg, false)

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
	token := auth.IssueToken("https://agentdock.example.com", "token-secret", time.Hour)
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if authorizedOAuth(req, cfg) {
		t.Fatalf("authorizedOAuth() = true when OAuth is disabled")
	}
}

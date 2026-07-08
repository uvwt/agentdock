package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/mcp"
	"github.com/uvwt/agentdock/internal/tools"
)

func TestMCPEndpointNotificationReturnsAcceptedWithEmptyBody(t *testing.T) {
	cfg := config.Config{
		Workspace: t.TempDir(),
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}

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
	cfg := config.Config{
		Workspace:    t.TempDir(),
		AuthToken:    "secret-token",
		AgentDockDir: "AgentDock",
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
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
	cfg := config.Config{
		Workspace:    t.TempDir(),
		AuthToken:    "secret-token",
		AgentDockDir: "AgentDock",
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
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
	if strings.Contains(body, "secret-token") || strings.Contains(body, cfg.Workspace) {
		t.Fatalf("status response leaked token or workspace path: %s", body)
	}
}

func TestRuntimeAPISkillsNoAuthWhenUnconfigured(t *testing.T) {
	cfg := config.Config{Workspace: t.TempDir(), AgentDockDir: "AgentDock"}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
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

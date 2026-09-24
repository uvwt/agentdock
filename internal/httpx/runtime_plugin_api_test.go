package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/app"
	"github.com/uvwt/agentdock/internal/auth"
	pluginruntime "github.com/uvwt/agentdock/internal/plugin"
)

func TestRuntimePluginAPIIsReadOnlyAuthenticatedAndPathSafe(t *testing.T) {
	cfg := testConfig(t)
	runtime, err := app.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close() })

	source := filepath.Join(cfg.AgentDockDefaultDir, "portable-plugin")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"$schema":     "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json",
		"name":        "runtime-demo",
		"version":     "1.0.0",
		"description": "Runtime API Plugin fixture.",
		"provenance": map[string]any{
			"origin":      "https://github.com/example/plugins",
			"revision":    "abc123",
			"subdir":      "plugins/runtime-demo",
			"vendor_note": "opaque metadata",
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "plugin.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	validated, err := runtime.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "validate", "source": source,
	})
	if err != nil {
		t.Fatal(err)
	}
	review, ok := validated["review"].(pluginruntime.Review)
	if !ok || review.ReviewToken == "" {
		t.Fatalf("validate result has no review_token: %#v", validated)
	}
	if _, err := runtime.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "install", "source": source, "review_token": review.ReviewToken,
	}); err != nil {
		t.Fatal(err)
	}

	handler := runtimeAPIHandler(runtime, cfg, auth.NewOAuthStore())
	for _, target := range []string{"/internal/runtime/plugins", "/internal/runtime/plugins/runtime-demo"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", target, response.Code, response.Body.String())
		}
		body := response.Body.String()
		if !strings.Contains(body, "runtime-demo") || !strings.Contains(body, "https://github.com/example/plugins") {
			t.Fatalf("GET %s missing Plugin identity: %s", target, body)
		}
		if strings.Contains(body, cfg.AgentDockHome) || strings.Contains(body, source) {
			t.Fatalf("GET %s leaked a local Plugin path: %s", target, body)
		}
	}

	methodResponse := httptest.NewRecorder()
	handler.ServeHTTP(methodResponse, httptest.NewRequest(http.MethodPost, "/internal/runtime/plugins", nil))
	if methodResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST Plugin API status=%d body=%s", methodResponse.Code, methodResponse.Body.String())
	}
	if got := methodResponse.Header().Get("Allow"); got != "GET" {
		t.Fatalf("POST Plugin API Allow=%q, want GET", got)
	}

	authConfig := cfg
	authConfig.AuthToken = "runtime-secret"
	authHandler := runtimeAPIHandler(runtime, authConfig, auth.NewOAuthStore())
	unauthorized := httptest.NewRecorder()
	authHandler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/internal/runtime/plugins", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized Plugin API status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
}

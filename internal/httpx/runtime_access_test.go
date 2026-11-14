package httpx

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/app"
	"github.com/uvwt/agentdock/internal/auth"
)

func TestRuntimeAPIRejectsUntrustedUnauthenticatedRequests(t *testing.T) {
	cfg := testConfig(t)
	runtime, err := app.NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	handler := runtimeAPIHandler(runtime, cfg, auth.NewOAuthStore())
	for _, test := range []struct{ name, host, origin, site, remote string }{
		{name: "rebinding", host: "attacker.example:8765", origin: "http://attacker.example:8765", remote: "127.0.0.1:12345"},
		{name: "cross origin", host: "127.0.0.1:8765", origin: "https://attacker.example", remote: "127.0.0.1:12345"},
		{name: "cross site", host: "127.0.0.1:8765", site: "cross-site", remote: "127.0.0.1:12345"},
		{name: "external peer", host: "127.0.0.1:8765", remote: "192.0.2.1:12345"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				body := `{"action":"add","name":"blocked","description":"Blocked MCP","transport":"stdio","command":"echo","enabled":false}`
				req := httptest.NewRequest(method, "http://"+test.host+"/internal/runtime/mcp", strings.NewReader(body))
				req.RemoteAddr = test.remote
				req.Header.Set("Content-Type", "text/plain")
				req.Header.Set("Origin", test.origin)
				req.Header.Set("Sec-Fetch-Site", test.site)
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, req)
				if response.Code != http.StatusForbidden {
					t.Fatalf("%s: status=%d body=%s", method, response.Code, response.Body.String())
				}
			}
		})
	}
	servers, err := runtime.RuntimeMCPServers(t.Context())
	if err != nil || servers["count"] != 0 {
		t.Fatalf("rejected request changed MCP registry: %v, %v", servers, err)
	}
}

func TestRuntimeAPIRequiresJSONContentTypeForBodyPostEndpoints(t *testing.T) {
	cfg := testConfig(t)
	runtime, err := app.NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	handler := runtimeAPIHandler(runtime, cfg, auth.NewOAuthStore())

	endpoints := []struct {
		path string
		body string
	}{
		{
			path: "/internal/runtime/mcp",
			body: `{"action":"add","name":"blocked","description":"Blocked MCP","transport":"stdio","command":"echo","enabled":false}`,
		},
		{
			path: "/internal/runtime/mcp/oauth/callback",
			body: `{"state":"missing","code":"abc"}`,
		},
		{
			path: "/internal/runtime/evolve",
			body: `{"intent":"propose"}`,
		},
		{
			path: "/internal/runtime/resource-library",
			body: `{"action":"export_prepare","kind":"skill","name":"demo"}`,
		},
	}

	for _, endpoint := range endpoints {
		for _, contentType := range []string{"", "text/plain", "application/x-www-form-urlencoded", "multipart/form-data"} {
			t.Run(endpoint.path+" "+contentType, func(t *testing.T) {
				req := newLocalRuntimeRequest(http.MethodPost, endpoint.path, strings.NewReader(endpoint.body))
				if contentType != "" {
					req.Header.Set("Content-Type", contentType)
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, req)
				if response.Code != http.StatusUnsupportedMediaType {
					t.Fatalf("status=%d, want %d; body=%s", response.Code, http.StatusUnsupportedMediaType, response.Body.String())
				}
			})
		}
	}

	// 验证被拒绝的 text/plain CORS 简单请求没有修改 MCP 注册表。
	servers, err := runtime.RuntimeMCPServers(t.Context())
	if err != nil || servers["count"] != 0 {
		t.Fatalf("rejected text/plain request changed MCP registry: %v, %v", servers, err)
	}
}

func TestRuntimeAPIRejectsUnauthenticatedRequestsWhenPublicURLConfigured(t *testing.T) {
	cfg := testConfig(t)
	cfg.OAuthServerURL = "https://public-tunnel.trycloudflare.com"
	runtime, err := app.NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	handler := runtimeAPIHandler(runtime, cfg, auth.NewOAuthStore())

	// 即便是看似本机直连的 loopback 请求，只要配置了公网 URL 且未开启任何认证，就必须拒绝。
	req := newLocalRuntimeRequest(http.MethodPost, "/internal/runtime/mcp", strings.NewReader(`{"action":"add","name":"blocked","description":"Blocked MCP","transport":"stdio","command":"echo","enabled":false}`))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want %d; body=%s", response.Code, http.StatusForbidden, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"code":"AUTH_REQUIRED_FOR_PUBLIC_EXPOSURE"`) {
		t.Fatalf("unexpected error body: %s", response.Body.String())
	}
}

func TestRuntimeAPIAuthenticatedEnforcesHostAndOrigin(t *testing.T) {
	cfg := testConfig(t)
	cfg.AuthToken = "secret-token"
	runtime, err := app.NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	handler := runtimeAPIHandler(runtime, cfg, auth.NewOAuthStore())

	t.Run("rejects non-loopback host for loopback peer without public server url even with bearer token", func(t *testing.T) {
		req := newLocalRuntimeRequest(http.MethodGet, "/internal/runtime/mcp", nil)
		req.RemoteAddr = "127.0.0.1:54321"
		req.Host = "attacker.example:8765"
		req.Header.Set("Authorization", "Bearer secret-token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != http.StatusForbidden {
			t.Fatalf("status=%d, want %d; body=%s", response.Code, http.StatusForbidden, response.Body.String())
		}
	})

	t.Run("allows non-loopback host when public server url matches", func(t *testing.T) {
		pubCfg := cfg
		pubCfg.OAuthServerURL = "https://agentdock.example"
		pubHandler := runtimeAPIHandler(runtime, pubCfg, auth.NewOAuthStore())

		req := newLocalRuntimeRequest(http.MethodGet, "/internal/runtime/mcp", nil)
		req.RemoteAddr = "127.0.0.1:54321"
		req.Host = "agentdock.example"
		req.Header.Set("Authorization", "Bearer secret-token")
		response := httptest.NewRecorder()
		pubHandler.ServeHTTP(response, req)
		if response.Code != http.StatusOK {
			t.Fatalf("status=%d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
		}
	})

	t.Run("rejects cross-origin and cross-site even with bearer token", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			origin string
			site   string
		}{
			{name: "cross-origin", origin: "https://evil.example"},
			{name: "cross-site", site: "cross-site"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				req := newLocalRuntimeRequest(http.MethodPost, "/internal/runtime/mcp", strings.NewReader(`{"action":"list"}`))
				req.Header.Set("Authorization", "Bearer secret-token")
				req.Header.Set("Content-Type", "application/json")
				if tc.origin != "" {
					req.Header.Set("Origin", tc.origin)
				}
				if tc.site != "" {
					req.Header.Set("Sec-Fetch-Site", tc.site)
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, req)
				if response.Code != http.StatusForbidden {
					t.Fatalf("status=%d, want %d; body=%s", response.Code, http.StatusForbidden, response.Body.String())
				}
			})
		}
	})

	t.Run("allows authenticated request with public server url and matching https origin", func(t *testing.T) {
		pubCfg := cfg
		pubCfg.OAuthServerURL = "https://agentdock.example"
		pubHandler := runtimeAPIHandler(runtime, pubCfg, auth.NewOAuthStore())

		req := newLocalRuntimeRequest(http.MethodGet, "/internal/runtime/mcp", nil)
		req.RemoteAddr = "127.0.0.1:54321"
		req.Host = "agentdock.example"
		req.Header.Set("Authorization", "Bearer secret-token")
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set("Origin", "https://agentdock.example")
		response := httptest.NewRecorder()
		pubHandler.ServeHTTP(response, req)
		if response.Code != http.StatusOK {
			t.Fatalf("status=%d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
		}
	})
}

func TestWarnIfUnauthenticatedPublicExposureLogsLoudWarning(t *testing.T) {
	previous := slog.Default()
	defer slog.SetDefault(previous)
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))

	cfg := testConfig(t)
	cfg.OAuthServerURL = "https://demo-tunnel.trycloudflare.com"
	warnIfUnauthenticatedPublicExposure(cfg)

	output := logs.String()
	if !strings.Contains(output, `"level":"WARN"`) || !strings.Contains(output, "AGENTDOCK_AUTH_TOKEN") {
		t.Fatalf("expected loud warning in log output, got: %s", output)
	}

	logs.Reset()
	cfg.AuthToken = "configured-token"
	warnIfUnauthenticatedPublicExposure(cfg)
	if logs.Len() != 0 {
		t.Fatalf("unexpected warning when authentication is configured: %s", logs.String())
	}
}

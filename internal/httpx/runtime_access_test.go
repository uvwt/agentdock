package httpx

import (
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

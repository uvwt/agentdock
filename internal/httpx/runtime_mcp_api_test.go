package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/auth"
	"github.com/uvwt/agentdock/internal/mcp"
	"github.com/uvwt/agentdock/internal/tools"
)

func TestRuntimeMCPAPIManagesServersWithoutReturningSecrets(t *testing.T) {
	cfg := testConfig(t)
	runtime, err := tools.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	defer runtime.Close()
	handler := runtimeAPIHandler(mcp.NewServer(runtime, cfg), cfg, auth.NewOAuthStore())

	postRuntimeMCP(t, handler, `{"action":"add","name":"demo","description":"Demo MCP","transport":"streamable_http","url":"http://127.0.0.1:1/mcp","enabled":false}`, http.StatusOK)

	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/internal/runtime/mcp", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"name":"demo"`) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}

	detail := httptest.NewRecorder()
	handler.ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/internal/runtime/mcp/demo", nil))
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"url":"http://127.0.0.1:1/mcp"`) {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}

	const secret = "runtime-mcp-secret-value"
	setEnv := postRuntimeMCP(t, handler, `{"action":"env_set","name":"demo","key":"DEMO_TOKEN","value":"`+secret+`"}`, http.StatusOK)
	if strings.Contains(setEnv, secret) {
		t.Fatalf("env_set response leaked secret: %s", setEnv)
	}
	listEnv := postRuntimeMCP(t, handler, `{"action":"env_list","name":"demo"}`, http.StatusOK)
	if !strings.Contains(listEnv, `"key":"DEMO_TOKEN"`) || strings.Contains(listEnv, secret) {
		t.Fatalf("unexpected env_list response: %s", listEnv)
	}

	invalid := postRuntimeMCP(t, handler, `{"action":"env_list","name":"demo","unknown":true}`, http.StatusBadRequest)
	if !strings.Contains(invalid, `"code":"INVALID_MCP_REQUEST"`) {
		t.Fatalf("unexpected invalid response: %s", invalid)
	}
	unsupported := postRuntimeMCP(t, handler, `{"action":"inspect","name":"demo"}`, http.StatusBadRequest)
	if !strings.Contains(unsupported, `"code":"MCP_ACTION_UNSUPPORTED"`) {
		t.Fatalf("unexpected unsupported response: %s", unsupported)
	}

	postRuntimeMCP(t, handler, `{"action":"remove","name":"demo"}`, http.StatusOK)
	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/internal/runtime/mcp/demo", nil))
	if missing.Code != http.StatusBadRequest || !strings.Contains(missing.Body.String(), `"code":"MCP_SERVER_NOT_FOUND"`) {
		t.Fatalf("missing status=%d body=%s", missing.Code, missing.Body.String())
	}
}

func postRuntimeMCP(t *testing.T, handler http.Handler, body string, wantStatus int) string {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/internal/runtime/mcp", strings.NewReader(body)))
	if response.Code != wantStatus {
		t.Fatalf("POST /internal/runtime/mcp status=%d want=%d body=%s", response.Code, wantStatus, response.Body.String())
	}
	return response.Body.String()
}

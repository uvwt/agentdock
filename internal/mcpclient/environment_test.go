package mcpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStdioEnvironmentIsIsolatedAndScopedValuesWin(t *testing.T) {
	t.Setenv("AGENTDOCK_UNRELATED_PARENT_SECRET", "parent-only")
	t.Setenv("HOST_MAPPED_VALUE", "mapped-from-host")

	environment, err := stdioEnvironment(ServerConfig{
		Name:       "demo",
		EnvFromEnv: map[string]string{"MAPPED": "HOST_MAPPED_VALUE"},
		RuntimeEnv: map[string]string{
			"MAPPED": "scoped-wins",
			"SCOPED": "configured",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	values := formattedEnvironmentMap(environment)
	if _, exists := values["AGENTDOCK_UNRELATED_PARENT_SECRET"]; exists {
		t.Fatal("stdio MCP inherited an unrelated parent environment variable")
	}
	if values["MAPPED"] != "scoped-wins" || values["SCOPED"] != "configured" {
		t.Fatalf("unexpected merged environment: %#v", values)
	}
	for _, key := range []string{"PATH", "HOME", "TMPDIR"} {
		if values[key] == "" {
			t.Fatalf("minimal environment omitted %s", key)
		}
	}
}

func TestStdioEnvironmentRequiresMappedHostValue(t *testing.T) {
	_, err := stdioEnvironment(ServerConfig{
		Name:       "demo",
		EnvFromEnv: map[string]string{"CHILD": "AGENTDOCK_MISSING_HOST_VALUE"},
	})
	mcpErr, ok := err.(*Error)
	if !ok || mcpErr.Code != "MCP_AUTH_REQUIRED" {
		t.Fatalf("unexpected error: %T %v", err, err)
	}
}

func TestHTTPHeaderUsesScopedEnvironmentBeforeHost(t *testing.T) {
	t.Setenv("MCP_HEADER_TOKEN", "host-token")
	var received string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		received = request.Header.Get("Authorization")
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer server.Close()

	client := newStreamableHTTPClient(ServerConfig{
		Name:       "demo",
		URL:        server.URL,
		HeaderEnv:  map[string]string{"Authorization": "MCP_HEADER_TOKEN"},
		RuntimeEnv: map[string]string{"MCP_HEADER_TOKEN": "scoped-token"},
	})
	response, err := client.do(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if received != "scoped-token" {
		t.Fatalf("HTTP header = %q, want scoped-token", received)
	}
}

func formattedEnvironmentMap(environment []string) map[string]string {
	values := make(map[string]string, len(environment))
	for _, item := range environment {
		key, value, found := strings.Cut(item, "=")
		if found {
			values[key] = value
		}
	}
	return values
}

package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
)

func TestDynamicMCPToolsStaySeparateAndAppearLightweightInContext(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var rpc struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&rpc); err != nil {
			t.Errorf("decode upstream request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch rpc.Method {
		case "initialize":
			writeDynamicMCPRPCResult(t, w, rpc.ID, map[string]any{
				"protocolVersion": config.ProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "demo-upstream", "version": "1.0.0"},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			writeDynamicMCPRPCResult(t, w, rpc.ID, map[string]any{
				"tools": []map[string]any{{
					"name":        "echo",
					"description": "Echo supplied text",
					"inputSchema": map[string]any{
						"type":       "object",
						"required":   []string{"text"},
						"properties": map[string]any{"text": map[string]any{"type": "string"}},
					},
				}},
			})
		case "tools/call":
			arguments, _ := rpc.Params["arguments"].(map[string]any)
			writeDynamicMCPRPCResult(t, w, rpc.ID, map[string]any{
				"content":           []map[string]any{{"type": "text", "text": arguments["text"]}},
				"structuredContent": map[string]any{"echo": arguments["text"]},
			})
		default:
			t.Errorf("unexpected upstream method %q", rpc.Method)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	cfg := config.Config{
		AgentDockDefaultDir: t.TempDir(),
		AgentDockHome:       filepath.Join(t.TempDir(), ".agentdock"),
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	added, err := runtime.Call(context.Background(), "mcp_manage", map[string]any{
		"action":      "add",
		"name":        "demo",
		"description": "Demo external capabilities",
		"transport":   "streamable_http",
		"url":         upstream.URL,
	})
	if err != nil {
		t.Fatalf("mcp_manage add: %v", err)
	}
	if added["action"] != "add" {
		t.Fatalf("unexpected add result: %#v", added)
	}

	contextResult, err := runtime.Call(context.Background(), "agentdock_context", map[string]any{})
	if err != nil {
		t.Fatalf("agentdock_context: %v", err)
	}
	if len(contextResult) != 1 {
		t.Fatalf("agentdock_context should return only context: %#v", contextResult)
	}
	contextText, _ := contextResult["context"].(string)
	for _, forbidden := range []string{upstream.URL, "streamable_http", "demo:echo", "inputSchema"} {
		if strings.Contains(contextText, forbidden) {
			t.Fatalf("agentdock_context leaked %q: %s", forbidden, contextText)
		}
	}
	for _, required := range []string{"name: demo", "description: Demo external capabilities", "mcp_tool_search", "mcp_tool_inspect", "mcp_tool_call"} {
		if !strings.Contains(contextText, required) {
			t.Fatalf("agentdock_context missing %q: %s", required, contextText)
		}
	}

	search, err := runtime.Call(context.Background(), "mcp_tool_search", map[string]any{
		"server": "demo",
		"query":  "echo text",
	})
	if err != nil {
		t.Fatalf("mcp_tool_search: %v", err)
	}
	if search["count"] != 1 {
		t.Fatalf("unexpected search result: %#v", search)
	}

	inspect, err := runtime.Call(context.Background(), "mcp_tool_inspect", map[string]any{"name": "demo:echo"})
	if err != nil {
		t.Fatalf("mcp_tool_inspect: %v", err)
	}
	if inspect["tool_name"] != "echo" {
		t.Fatalf("unexpected inspect result: %#v", inspect)
	}

	called, err := runtime.Call(context.Background(), "mcp_tool_call", map[string]any{
		"name":      "demo:echo",
		"arguments": map[string]any{"text": "hello"},
	})
	if err != nil {
		t.Fatalf("mcp_tool_call: %v", err)
	}
	remote, _ := called["result"].(map[string]any)
	structured, _ := remote["structuredContent"].(map[string]any)
	if structured["echo"] != "hello" {
		t.Fatalf("unexpected call result: %#v", called)
	}

	for _, name := range runtime.ToolNames() {
		if name == "demo:echo" {
			t.Fatal("dynamic upstream tool leaked into AgentDock built-in tools/list")
		}
	}
}

func writeDynamicMCPRPCResult(t *testing.T, writer http.ResponseWriter, id any, result any) {
	t.Helper()
	if err := json.NewEncoder(writer).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}); err != nil {
		t.Errorf("write upstream response: %v", err)
	}
}

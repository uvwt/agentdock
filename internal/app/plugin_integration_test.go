package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
)

func TestHeavyPluginProgressiveDisclosureAndAvailabilityOverlay(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{
		AgentDockDefaultDir: root,
		AgentDockHome:       filepath.Join(root, ".agentdock"),
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	installDocumentSkillForTest(t, rt, "pcb-layout", "1.0.0", "Plan and verify PCB layout.")

	mcpServer := newPluginTestMCPServer(t)
	defer mcpServer.Close()
	if _, err := rt.Call(context.Background(), "mcp_manage", map[string]any{
		"action": "add", "name": "easyeda-test", "description": "EasyEDA test tools.",
		"transport": "streamable_http", "url": mcpServer.URL, "enabled": true, "timeout_ms": 2000,
	}); err != nil {
		t.Fatalf("add MCP server: %v", err)
	}
	if _, err := rt.Call(context.Background(), "plugin_manage", map[string]any{
		"action": "upsert", "name": "pcb", "description": "PCB design capabilities.",
		"skills": []string{"pcb-layout"}, "mcp_servers": []string{"easyeda-test"}, "enabled": true,
	}); err != nil {
		t.Fatalf("upsert plugin: %v", err)
	}

	contextResult, err := rt.Call(context.Background(), "agentdock_context", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var initial capabilityContext
	if err := remarshal(contextResult, &initial); err != nil {
		t.Fatal(err)
	}
	if len(initial.Plugins) != 1 || initial.Plugins[0].Name != "pcb" || initial.Plugins[0].SkillCount != 1 || initial.Plugins[0].MCPServerCount != 1 {
		t.Fatalf("plugin index = %#v", initial.Plugins)
	}
	if containsCapabilitySkill(initial.Skills, "pcb-layout") {
		t.Fatalf("plugin-owned Skill leaked into top-level index: %#v", initial.Skills)
	}
	if containsCapabilityMCP(initial.DynamicMCP, "easyeda-test") {
		t.Fatalf("plugin-owned MCP server leaked into top-level index: %#v", initial.DynamicMCP)
	}

	loaded, err := rt.Call(context.Background(), "plugin_load", map[string]any{"name": "pcb"})
	if err != nil {
		t.Fatal(err)
	}
	var expanded struct {
		Skills []struct {
			Name string `json:"name"`
			File string `json:"file"`
		} `json:"skills"`
		MCPServers []struct {
			Name  string `json:"name"`
			Tools []struct {
				QualifiedName string `json:"qualified_name"`
				Description   string `json:"description"`
			} `json:"tools"`
		} `json:"mcp_servers"`
		Unavailable []map[string]any `json:"unavailable_members"`
	}
	if err := remarshal(loaded, &expanded); err != nil {
		t.Fatal(err)
	}
	if len(expanded.Skills) != 1 || expanded.Skills[0].Name != "pcb-layout" || expanded.Skills[0].File != "skill://pcb-layout/SKILL.md" {
		t.Fatalf("expanded Skills = %#v", expanded.Skills)
	}
	if len(expanded.MCPServers) != 1 || expanded.MCPServers[0].Name != "easyeda-test" ||
		len(expanded.MCPServers[0].Tools) != 1 || expanded.MCPServers[0].Tools[0].QualifiedName != "easyeda-test:route" ||
		expanded.MCPServers[0].Tools[0].Description != "Route PCB traces" || len(expanded.Unavailable) != 0 {
		t.Fatalf("expanded MCP/unavailable = %#v / %#v", expanded.MCPServers, expanded.Unavailable)
	}
	if _, _, err := rt.skills.ResolveResource("skill://pcb-layout/SKILL.md"); err != nil {
		t.Fatalf("resolve enabled plugin Skill: %v", err)
	}

	genericSearch, err := rt.Call(context.Background(), "mcp_tool_search", map[string]any{"query": "anything", "limit": 10})
	if err != nil {
		t.Fatal(err)
	}
	if count, _ := genericSearch["count"].(int); count != 0 {
		t.Fatalf("generic MCP search exposed plugin-owned server: %#v", genericSearch)
	}

	if _, err := rt.Call(context.Background(), "plugin_manage", map[string]any{"action": "disable", "name": "pcb"}); err != nil {
		t.Fatal(err)
	}
	contextResult, err = rt.Call(context.Background(), "agentdock_context", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var disabled capabilityContext
	if err := remarshal(contextResult, &disabled); err != nil {
		t.Fatal(err)
	}
	if len(disabled.Plugins) != 0 || containsCapabilitySkill(disabled.Skills, "pcb-layout") || containsCapabilityMCP(disabled.DynamicMCP, "easyeda-test") {
		t.Fatalf("disabled plugin leaked capabilities: %#v", disabled)
	}
	assertToolErrorCode(t, callPluginLoad(rt, "pcb"), "PLUGIN_DISABLED")
	_, _, resolveErr := rt.skills.ResolveResource("skill://pcb-layout/SKILL.md")
	assertToolErrorCode(t, resolveErr, "PLUGIN_DISABLED")
	_, searchErr := rt.Call(context.Background(), "mcp_tool_search", map[string]any{"query": "anything", "server": "easyeda-test"})
	assertToolErrorCode(t, searchErr, "PLUGIN_DISABLED")

	if _, err := rt.Call(context.Background(), "plugin_manage", map[string]any{"action": "enable", "name": "pcb"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := rt.skills.ResolveResource("skill://pcb-layout/SKILL.md"); err != nil {
		t.Fatalf("resolve re-enabled plugin Skill: %v", err)
	}
	if _, err := rt.Call(context.Background(), "skill_package", map[string]any{"action": "disable", "skill": "pcb-layout"}); err != nil {
		t.Fatal(err)
	}
	loaded, err = rt.Call(context.Background(), "plugin_load", map[string]any{"name": "pcb"})
	if err != nil {
		t.Fatal(err)
	}
	var baseDisabled struct {
		Skills      []map[string]any `json:"skills"`
		Unavailable []map[string]any `json:"unavailable_members"`
	}
	if err := remarshal(loaded, &baseDisabled); err != nil {
		t.Fatal(err)
	}
	if len(baseDisabled.Skills) != 0 || len(baseDisabled.Unavailable) != 1 || baseDisabled.Unavailable[0]["reason"] != "disabled" {
		t.Fatalf("base-disabled plugin load = %#v", baseDisabled)
	}
	_, _, resolveErr = rt.skills.ResolveResource("skill://pcb-layout/SKILL.md")
	assertToolErrorCode(t, resolveErr, "SKILL_DISABLED")
}

func callPluginLoad(rt *Runtime, name string) error {
	_, err := rt.Call(context.Background(), "plugin_load", map[string]any{"name": name})
	return err
}

func containsCapabilitySkill(items []capabilitySkillItem, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}

func containsCapabilityMCP(items []capabilityDynamicMCPItem, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}

func newPluginTestMCPServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodDelete {
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		var rpcRequest struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(request.Body).Decode(&rpcRequest); err != nil {
			t.Errorf("decode MCP request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		switch rpcRequest.Method {
		case "server/discover":
			writePluginTestRPC(writer, rpcRequest.ID, nil, map[string]any{"code": -32601, "message": "Method not found"})
		case "initialize":
			writer.Header().Set("Mcp-Session-Id", "plugin-session")
			writePluginTestRPC(writer, rpcRequest.ID, map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "plugin-test", "version": "1.0.0"},
			}, nil)
		case "notifications/initialized":
			writer.WriteHeader(http.StatusAccepted)
		case "tools/list":
			writer.Header().Set("Content-Type", "text/event-stream")
			payload, err := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": rpcRequest.ID,
				"result": map[string]any{"tools": []map[string]any{{
					"name": "route", "title": "Route PCB", "description": "Route PCB traces",
					"inputSchema": map[string]any{"type": "object", "additionalProperties": false},
				}}},
			})
			if err != nil {
				t.Errorf("encode MCP tool response: %v", err)
				writer.WriteHeader(http.StatusInternalServerError)
				return
			}
			_, _ = fmt.Fprintf(writer, "event: message\ndata: %s\n\n", payload)
		default:
			t.Errorf("unexpected MCP method %q", rpcRequest.Method)
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
}

func writePluginTestRPC(writer http.ResponseWriter, id any, result any, rpcError any) {
	writer.Header().Set("Content-Type", "application/json")
	response := map[string]any{"jsonrpc": "2.0", "id": id}
	if rpcError != nil {
		response["error"] = rpcError
	} else {
		response["result"] = result
	}
	_ = json.NewEncoder(writer).Encode(response)
}

package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestManagerStreamableHTTPFlowAndPersistence(t *testing.T) {
	t.Setenv("TEST_MCP_AUTH", "Bearer test-token")
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		var request struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch request.Method {
		case "initialize":
			if got := r.Header.Get("MCP-Protocol-Version"); got != "2025-06-18" {
				t.Errorf("initialize protocol version = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "session-1")
			writeRPCResult(t, w, request.ID, map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "test", "version": "1.0.0"},
			})
		case "notifications/initialized":
			if got := r.Header.Get("Mcp-Session-Id"); got != "session-1" {
				t.Errorf("notification session id = %q", got)
			}
			if got := r.Header.Get("MCP-Protocol-Version"); got != "2024-11-05" {
				t.Errorf("notification negotiated protocol version = %q", got)
			}
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			if got := r.Header.Get("Mcp-Session-Id"); got != "session-1" {
				t.Errorf("tools/list session id = %q", got)
			}
			if got := r.Header.Get("MCP-Protocol-Version"); got != "2024-11-05" {
				t.Errorf("tools/list negotiated protocol version = %q", got)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			payload, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"result": map[string]any{"tools": []map[string]any{
					{
						"name":        "echo",
						"title":       "Echo",
						"description": "Echo supplied text",
						"inputSchema": map[string]any{
							"type":       "object",
							"required":   []string{"text"},
							"properties": map[string]any{"text": map[string]any{"type": "string"}},
						},
					},
				}},
			})
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", payload)
		case "tools/call":
			callCount.Add(1)
			arguments, _ := request.Params["arguments"].(map[string]any)
			writeRPCResult(t, w, request.ID, map[string]any{
				"content":           []map[string]any{{"type": "text", "text": arguments["text"]}},
				"structuredContent": map[string]any{"echo": arguments["text"]},
			})
		default:
			t.Errorf("unexpected method %q", request.Method)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	added, err := manager.Add(ServerConfig{
		Name:        "demo",
		Description: "Demo MCP server",
		Transport:   TransportStreamableHTTP,
		URL:         server.URL,
		HeaderEnv:   map[string]string{"Authorization": "TEST_MCP_AUTH"},
		Enabled:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if added.Status != "idle" {
		t.Fatalf("added status = %q", added.Status)
	}

	refreshed, tools, err := manager.Refresh(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Status != "ready" || refreshed.ToolCount != 1 || len(tools) != 1 {
		t.Fatalf("unexpected refresh: summary=%#v tools=%#v", refreshed, tools)
	}

	matches, err := manager.Search(context.Background(), "echo text", "demo", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].QualifiedName != "demo:echo" {
		t.Fatalf("unexpected search matches: %#v", matches)
	}
	allTools, err := manager.Search(context.Background(), "*", "demo", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(allTools) != 1 || allTools[0].QualifiedName != "demo:echo" {
		t.Fatalf("unexpected wildcard search matches: %#v", allTools)
	}

	_, tool, err := manager.InspectTool(context.Background(), "demo:echo")
	if err != nil {
		t.Fatal(err)
	}
	if tool.Name != "echo" || tool.InputSchema["type"] != "object" {
		t.Fatalf("unexpected inspected tool: %#v", tool)
	}

	if _, err := manager.Call(context.Background(), "demo:echo", map[string]any{}); err == nil {
		t.Fatal("missing required argument was accepted")
	} else {
		var mcpErr *Error
		if !errors.As(err, &mcpErr) || mcpErr.Code != "MCP_ARGUMENT_INVALID" || !strings.Contains(fmt.Sprint(mcpErr.Details["reason"]), "$.text is required") {
			t.Fatalf("unexpected missing argument error: %#v", err)
		}
	}
	if _, err := manager.Call(context.Background(), "demo:echo", map[string]any{"text": 42}); err == nil {
		t.Fatal("wrong argument type was accepted")
	} else {
		var mcpErr *Error
		if !errors.As(err, &mcpErr) || mcpErr.Code != "MCP_ARGUMENT_INVALID" || !strings.Contains(fmt.Sprint(mcpErr.Details["reason"]), "$.text must be string") {
			t.Fatalf("unexpected argument type error: %#v", err)
		}
	}
	result, err := manager.Call(context.Background(), "demo:echo", map[string]any{"text": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if callCount.Load() != 1 {
		t.Fatalf("tools/call count = %d", callCount.Load())
	}
	structured, _ := result["structuredContent"].(map[string]any)
	if structured["echo"] != "hello" {
		t.Fatalf("unexpected call result: %#v", result)
	}

	reloaded, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	listed := reloaded.List()
	if len(listed) != 1 || listed[0].Name != "demo" || listed[0].Description != "Demo MCP server" {
		t.Fatalf("persisted registry mismatch: %#v", listed)
	}
}

func TestManagerStdioFlow(t *testing.T) {
	t.Setenv("TEST_MCP_HELPER_MODE", "1")
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	_, err = manager.Add(ServerConfig{
		Name:        "local",
		Description: "Local stdio MCP",
		Transport:   TransportStdio,
		Command:     os.Args[0],
		Args:        []string{"-test.run=TestMCPStdioHelperProcess"},
		EnvFromEnv:  map[string]string{"GO_WANT_MCP_HELPER": "TEST_MCP_HELPER_MODE"},
		Enabled:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Refresh(context.Background(), "local"); err != nil {
		t.Fatal(err)
	}
	result, err := manager.Call(context.Background(), "local:echo", map[string]any{"text": "stdio"})
	if err != nil {
		t.Fatal(err)
	}
	structured, _ := result["structuredContent"].(map[string]any)
	if structured["echo"] != "stdio" {
		t.Fatalf("unexpected stdio result: %#v", result)
	}
	if err := manager.Remove("local"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
}

func TestManagerRejectsReservedHTTPHeaders(t *testing.T) {
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	_, err = manager.Add(ServerConfig{
		Name:        "unsafe",
		Description: "Invalid header override",
		Transport:   TransportStreamableHTTP,
		URL:         "https://example.invalid/mcp",
		HeaderEnv:   map[string]string{"MCP-Protocol-Version": "TEST_MCP_VERSION"},
		Enabled:     true,
	})
	if err == nil {
		t.Fatal("reserved MCP protocol header override was accepted")
	}
	var mcpErr *Error
	if !errors.As(err, &mcpErr) || mcpErr.Code != "MCP_CONFIG_INVALID" {
		t.Fatalf("unexpected reserved header error: %#v", err)
	}
}

func TestMCPStdioHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_MCP_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			os.Exit(2)
		}
		switch request.Method {
		case "initialize":
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"result": map[string]any{
					"protocolVersion": "2025-06-18",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "helper", "version": "1.0.0"},
				},
			})
		case "notifications/initialized":
		case "tools/list":
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"result": map[string]any{"tools": []map[string]any{{
					"name":        "echo",
					"description": "Echo text over stdio",
					"inputSchema": map[string]any{"type": "object", "required": []string{"text"}},
				}}},
			})
		case "tools/call":
			arguments, _ := request.Params["arguments"].(map[string]any)
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"result": map[string]any{
					"content":           []map[string]any{{"type": "text", "text": arguments["text"]}},
					"structuredContent": map[string]any{"echo": arguments["text"]},
				},
			})
		}
	}
	os.Exit(0)
}

func writeRPCResult(t *testing.T, w http.ResponseWriter, id any, result any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result}); err != nil {
		t.Errorf("write response: %v", err)
	}
}

func TestIndependentManagersDoNotOverwriteRegistryUpdates(t *testing.T) {
	home := t.TempDir()
	first, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	configs := []struct {
		manager *Manager
		config  ServerConfig
	}{
		{manager: first, config: ServerConfig{Name: "first", Description: "First server", Transport: TransportStreamableHTTP, URL: "https://example.invalid/first", Enabled: true}},
		{manager: second, config: ServerConfig{Name: "second", Description: "Second server", Transport: TransportStreamableHTTP, URL: "https://example.invalid/second", Enabled: true}},
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(configs))
	for _, item := range configs {
		wg.Add(1)
		go func(item struct {
			manager *Manager
			config  ServerConfig
		}) {
			defer wg.Done()
			_, err := item.manager.Add(item.config)
			errs <- err
		}(item)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Add() error = %v", err)
		}
	}

	visible := first.List()
	if len(visible) != 2 || visible[0].Name != "first" || visible[1].Name != "second" {
		t.Fatalf("cross-process visible registry = %#v", visible)
	}

	reloaded, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	listed := reloaded.List()
	if len(listed) != 2 || listed[0].Name != "first" || listed[1].Name != "second" {
		t.Fatalf("persisted registry = %#v, want both independent updates", listed)
	}
}

func TestSameManagerConcurrentRegistryUpdatesRemainVisible(t *testing.T) {
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	configs := []ServerConfig{
		{Name: "alpha", Description: "Alpha", Transport: TransportStreamableHTTP, URL: "https://example.invalid/alpha", Enabled: true},
		{Name: "beta", Description: "Beta", Transport: TransportStreamableHTTP, URL: "https://example.invalid/beta", Enabled: true},
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(configs))
	for _, cfg := range configs {
		wg.Add(1)
		go func(cfg ServerConfig) {
			defer wg.Done()
			_, err := manager.Add(cfg)
			errs <- err
		}(cfg)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Add() error = %v", err)
		}
	}
	listed := manager.List()
	if len(listed) != 2 || listed[0].Name != "alpha" || listed[1].Name != "beta" {
		t.Fatalf("manager registry = %#v", listed)
	}
}

func TestManagerStatePinBlocksDisableUntilOperationFinishes(t *testing.T) {
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if _, err := manager.Add(ServerConfig{
		Name:        "demo",
		Description: "Demo",
		Transport:   TransportStreamableHTTP,
		URL:         "https://example.invalid/mcp",
		Enabled:     true,
	}); err != nil {
		t.Fatal(err)
	}

	_, _, unlockState, err := manager.lockServer("demo")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := manager.SetEnabled("demo", false)
		done <- err
	}()
	select {
	case err := <-done:
		unlockState()
		t.Fatalf("SetEnabled() completed while state was pinned: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	unlockState()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SetEnabled() remained blocked after state release")
	}
}

func TestManagerRejectsOperationsAfterClose(t *testing.T) {
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if _, err := manager.Add(ServerConfig{
		Name:        "demo",
		Description: "Demo",
		Transport:   TransportStreamableHTTP,
		URL:         "https://example.invalid/mcp",
		Enabled:     true,
	}); err == nil {
		t.Fatal("Add() succeeded after Close()")
	}
	if _, _, err := manager.Refresh(context.Background(), "demo"); err == nil {
		t.Fatal("Refresh() succeeded after Close()")
	}
}

func TestLockServerRejectsCloseRace(t *testing.T) {
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Add(ServerConfig{
		Name:        "demo",
		Description: "Demo",
		Transport:   TransportStreamableHTTP,
		URL:         "https://example.invalid/mcp",
		Enabled:     true,
	}); err != nil {
		t.Fatal(err)
	}
	manager.mu.RLock()
	state := manager.states["demo"]
	manager.mu.RUnlock()
	state.mu.Lock()
	lockResult := make(chan error, 1)
	go func() {
		_, _, unlock, err := manager.lockServer("demo")
		if unlock != nil {
			unlock()
		}
		lockResult <- err
	}()
	time.Sleep(20 * time.Millisecond)
	closeResult := make(chan error, 1)
	go func() { closeResult <- manager.Close() }()
	time.Sleep(20 * time.Millisecond)
	state.mu.Unlock()
	if err := <-closeResult; err != nil {
		t.Fatal(err)
	}
	err = <-lockResult
	var mcpErr *Error
	if !errors.As(err, &mcpErr) || mcpErr.Code != "MCP_MANAGER_CLOSED" {
		t.Fatalf("lockServer() error = %#v, want MCP_MANAGER_CLOSED", err)
	}
}

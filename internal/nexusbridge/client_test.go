package nexusbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	protocol "github.com/uvwt/agentdock-protocol"
	"github.com/uvwt/agentdock/internal/app"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/mcp"
	"github.com/uvwt/agentdock/internal/observability"
	"github.com/uvwt/agentdock/internal/publicartifacts"
)

func TestBridgeToolDescriptorsPreservePresentationBinding(t *testing.T) {
	descriptors, err := bridgeToolDescriptors([]map[string]any{
		{
			"name":        "file_edit",
			"inputSchema": map[string]any{"type": "object"},
			"_meta":       map[string]any{"ui": map[string]any{"resourceUri": protocol.FileChangeUIResourceURI}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(descriptors) != 1 || descriptors[0].Name != "file_edit" {
		t.Fatalf("descriptors = %#v", descriptors)
	}
	ui, ok := descriptors[0].Meta["ui"].(map[string]any)
	if !ok || ui["resourceUri"] != protocol.FileChangeUIResourceURI {
		t.Fatalf("presentation meta = %#v", descriptors[0].Meta)
	}
}

func TestBridgeHelloSeparatesToolsFromBridgeCapabilities(t *testing.T) {
	tools := []string{"read_file", "exec_command"}
	hello := bridgeHello(
		Identity{DeviceID: "device_abcdefgh"},
		tools,
		[]protocol.ToolDescriptor{{Name: "read_file"}, {Name: "exec_command"}},
		[]protocol.UIResourceCapability{},
		"sha256:test",
	)
	if !reflect.DeepEqual(hello.Capabilities, tools) {
		t.Fatalf("capabilities = %#v, want tools %#v", hello.Capabilities, tools)
	}
	wantBridgeCapabilities := []string{
		protocol.CapabilitiesNegotiationCapability,
		protocol.ContextLocalCapability,
		protocol.RuntimeRequestCapability,
		protocol.ResourceReadCapability,
		protocol.ArtifactReadCapability,
	}
	if !reflect.DeepEqual(hello.BridgeCapabilities, wantBridgeCapabilities) {
		t.Fatalf("bridge_capabilities = %#v, want %#v", hello.BridgeCapabilities, wantBridgeCapabilities)
	}
	for _, capability := range hello.Capabilities {
		for _, bridgeCapability := range wantBridgeCapabilities {
			if capability == bridgeCapability {
				t.Fatalf("Bridge capability %q leaked into model-facing tool capabilities", capability)
			}
		}
	}
}

func TestBridgeWebSocketInvokeRecoveryAndShutdown(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	helloCh := make(chan protocol.Message, 1)
	responses := make(chan protocol.Message, 2)
	serverErr := make(chan error, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/nodes/connect" || r.Header.Get("Authorization") != "Bearer test-device-token" {
			http.Error(w, "invalid bridge request", http.StatusUnauthorized)
			return
		}
		socket, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			serverErr <- err
			return
		}
		defer socket.Close()

		var hello protocol.Message
		if err := socket.ReadJSON(&hello); err != nil {
			serverErr <- err
			return
		}
		helloCh <- hello
		if err := socket.WriteJSON(protocol.Message{Type: protocol.MessageNodeReady, ProtocolVersion: protocol.ConnectionProtocolVersion, HeartbeatMS: 60_000}); err != nil {
			serverErr <- err
			return
		}

		if err := socket.WriteJSON(protocol.Message{Type: protocol.MessageToolInvoke, RequestID: "unsupported", Operation: "unsupported.operation"}); err != nil {
			serverErr <- err
			return
		}
		var unsupported protocol.Message
		if err := socket.ReadJSON(&unsupported); err != nil {
			serverErr <- err
			return
		}
		responses <- unsupported

		panicArgs := []byte(`{"method":"GET","path":"/internal/runtime/status"}`)
		if err := socket.WriteJSON(protocol.Message{Type: protocol.MessageToolInvoke, RequestID: "panic", Operation: protocol.OperationRuntimeRequest, Arguments: panicArgs}); err != nil {
			serverErr <- err
			return
		}
		var recovered protocol.Message
		if err := socket.ReadJSON(&recovered); err != nil {
			serverErr <- err
			return
		}
		responses <- recovered

		// 保持服务端连接打开，确保测试取消的是 Client Context，而不是依赖服务端主动断链。
		for {
			if _, _, err := socket.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	state := &ConnectionState{}
	client := NewClient(
		Identity{Endpoint: server.URL, NodeID: "node-test", DeviceID: "device-test", DeviceToken: "test-device-token"},
		mcp.NewServer(nil, config.Config{}),
		nil,
		publicartifacts.Store{},
		state,
	)
	done := make(chan struct{})
	go func() {
		client.Run(ctx)
		close(done)
	}()

	select {
	case hello := <-helloCh:
		if hello.Type != protocol.MessageNodeHello || hello.Hello == nil || hello.Hello.DeviceID != "device-test" {
			t.Fatalf("hello = %#v", hello)
		}
	case err := <-serverErr:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for bridge hello")
	}

	for _, requestID := range []string{"unsupported", "panic"} {
		select {
		case response := <-responses:
			if response.Type != protocol.MessageToolError || response.RequestID != requestID || response.Error == nil {
				t.Fatalf("response = %#v", response)
			}
			if requestID == "panic" && (response.Error.Code != "NODE_OPERATION_FAILED" || response.Error.Category != "internal") {
				t.Fatalf("panic error = %#v", response.Error)
			}
		case err := <-serverErr:
			t.Fatal(err)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for %s response", requestID)
		}
	}
	if !state.Connected() {
		t.Fatal("bridge should remain connected after recovered invoke panic")
	}

	cancel()
	select {
	case <-done:
	case err := <-serverErr:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not exit after context cancellation")
	}
	if state.Connected() {
		t.Fatal("bridge connection state remained connected after shutdown")
	}
}

func TestBridgeRunReturnsAfterCanceledDialContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := NewClient(
		Identity{Endpoint: "http://127.0.0.1:1", DeviceToken: "test-device-token"},
		mcp.NewServer(nil, config.Config{}),
		nil,
		publicartifacts.Store{},
		&ConnectionState{},
	)
	done := make(chan struct{})
	go func() {
		client.Run(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("bridge Run did not return for canceled context")
	}
}

func TestBridgeToolInvokeContinuesTraceContextIntoRuntime(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{
		AgentDockDefaultDir: filepath.Join(root, "workspace"),
		AgentDockHome:       filepath.Join(root, ".agentdock"),
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	runtime, err := app.NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	node := mcp.NewServer(runtime, cfg)

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	responseCh := make(chan protocol.Message, 1)
	serverErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			serverErr <- err
			return
		}
		defer socket.Close()
		var hello protocol.Message
		if err := socket.ReadJSON(&hello); err != nil {
			serverErr <- err
			return
		}
		if err := socket.WriteJSON(protocol.Message{Type: protocol.MessageNodeReady, ProtocolVersion: protocol.ConnectionProtocolVersion, HeartbeatMS: 60_000}); err != nil {
			serverErr <- err
			return
		}
		arguments, _ := json.Marshal(map[string]any{"tool": "agentdock_context", "arguments": map[string]any{}})
		if err := socket.WriteJSON(protocol.Message{
			Type: protocol.MessageToolInvoke, RequestID: "trace-call", Operation: protocol.OperationToolCall, Arguments: arguments,
			Traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			Tracestate:  "rojo=00f067aa0ba902b7",
		}); err != nil {
			serverErr <- err
			return
		}
		var response protocol.Message
		if err := socket.ReadJSON(&response); err != nil {
			serverErr <- err
			return
		}
		responseCh <- response
		for {
			if _, _, err := socket.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	client := NewClient(
		Identity{Endpoint: server.URL, NodeID: "node-trace", DeviceID: "device-trace", DeviceToken: "trace-token"},
		node, runtime, publicartifacts.Store{}, &ConnectionState{},
	)
	done := make(chan struct{})
	go func() {
		client.Run(ctx)
		close(done)
	}()

	select {
	case response := <-responseCh:
		if response.Type != protocol.MessageToolResult || response.RequestID != "trace-call" {
			t.Fatalf("response = %#v", response)
		}
	case err := <-serverErr:
		t.Fatal(err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for traced bridge response")
	}

	recent, ok := runtime.RuntimeAnalytics()["recent_calls"].([]observability.ExecutionRecord)
	if !ok || len(recent) == 0 {
		t.Fatalf("recent calls = %#v", runtime.RuntimeAnalytics()["recent_calls"])
	}
	record := recent[0]
	if record.Tool != "agentdock_context" || record.Source != observability.SourceNexus {
		t.Fatalf("bridge runtime record = %#v", record)
	}
	if record.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace id = %q", record.TraceID)
	}
	if record.SpanID != "" {
		t.Fatalf("runtime span id = %q, want empty without an installed SDK", record.SpanID)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not stop after cancellation")
	}
}

func TestConnectionEndpointsPreferDedicatedWebSocketEndpoint(t *testing.T) {
	client := &Client{identity: Identity{
		Endpoint:          "https://mcp.nexusdock.co",
		WebSocketEndpoint: "https://ws.nexusdock.co",
	}}
	if got, want := client.connectionEndpoints(), []string{"https://ws.nexusdock.co", "https://mcp.nexusdock.co"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("connection endpoints = %v, want %v", got, want)
	}

	client.identity.WebSocketEndpoint = ""
	if got, want := client.connectionEndpoints(), []string{"https://ws.nexusdock.co", "https://mcp.nexusdock.co"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy official connection endpoints = %v, want %v", got, want)
	}

	client.identity.Endpoint = "https://selfhost.example.com"
	if got, want := client.connectionEndpoints(), []string{"https://selfhost.example.com"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("self-hosted connection endpoints = %v, want %v", got, want)
	}
}

func TestFallbackClassificationDoesNotHideAuthenticationOrProtocolFailures(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusBadRequest} {
		if fallbackAllowedForHTTPStatus(status) {
			t.Errorf("HTTP %d must not allow fallback", status)
		}
	}
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable} {
		if !fallbackAllowedForHTTPStatus(status) {
			t.Errorf("HTTP %d should allow transport fallback", status)
		}
	}
	if fallbackAllowedForSocketError(&websocket.CloseError{Code: websocket.ClosePolicyViolation}) {
		t.Fatal("policy close must not allow fallback")
	}
	if !fallbackAllowedForSocketError(&websocket.CloseError{Code: websocket.CloseAbnormalClosure}) {
		t.Fatal("abnormal close should allow fallback")
	}
}

func TestBackoffResetsAfterStableConnection(t *testing.T) {
	if got := resetBackoffAfterStableConnection(32*time.Second, stableConnectionDuration-time.Millisecond); got != 32*time.Second {
		t.Fatalf("short connection reset backoff to %v", got)
	}
	if got := resetBackoffAfterStableConnection(32*time.Second, stableConnectionDuration); got != time.Second {
		t.Fatalf("stable connection backoff = %v, want 1s", got)
	}
}

func TestBridgeFallsBackToPairedEndpointAfterPreferredGatewayFailure(t *testing.T) {
	preferredHit := make(chan struct{}, 1)
	preferred := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		preferredHit <- struct{}{}
		http.Error(w, "edge unavailable", http.StatusServiceUnavailable)
	}))
	defer preferred.Close()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	fallbackConnected := make(chan struct{}, 1)
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != protocol.ConnectPath {
			http.NotFound(w, r)
			return
		}
		socket, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer socket.Close()
		var hello protocol.Message
		if err := socket.ReadJSON(&hello); err != nil {
			return
		}
		if err := socket.WriteJSON(protocol.Message{
			Type: protocol.MessageNodeReady, ProtocolVersion: protocol.ConnectionProtocolVersion, HeartbeatMS: 60_000,
		}); err != nil {
			return
		}
		fallbackConnected <- struct{}{}
		for {
			if _, _, err := socket.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer fallback.Close()

	ctx, cancel := context.WithCancel(context.Background())
	client := NewClient(
		Identity{
			Endpoint: fallback.URL, WebSocketEndpoint: preferred.URL,
			NodeID: "node-fallback", DeviceID: "device-fallback", DeviceToken: "device-token",
		},
		mcp.NewServer(nil, config.Config{}), nil, publicartifacts.Store{}, &ConnectionState{},
	)
	done := make(chan struct{})
	go func() {
		client.Run(ctx)
		close(done)
	}()

	select {
	case <-preferredHit:
	case <-time.After(2 * time.Second):
		t.Fatal("preferred websocket endpoint was not attempted")
	}
	select {
	case <-fallbackConnected:
	case <-time.After(2 * time.Second):
		t.Fatal("paired endpoint was not used as fallback")
	}
	deadline := time.Now().Add(time.Second)
	for !client.state.Connected() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !client.state.Connected() {
		t.Fatal("bridge should be connected through fallback endpoint")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not stop after cancellation")
	}
}

func TestConnectAuthenticationFailureDoesNotAllowFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewClient(
		Identity{Endpoint: server.URL, DeviceToken: "bad-token"},
		mcp.NewServer(nil, config.Config{}), nil, publicartifacts.Store{}, &ConnectionState{},
	)
	outcome := client.connect(t.Context(), server.URL)
	if outcome.err == nil || outcome.fallbackAllowed {
		t.Fatalf("authentication failure outcome = %#v", outcome)
	}
}

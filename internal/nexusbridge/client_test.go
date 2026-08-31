package nexusbridge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	protocol "github.com/uvwt/agentdock-protocol"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/mcp"
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
	if len(hello.BridgeCapabilities) != 1 || hello.BridgeCapabilities[0] != protocol.ArtifactReadCapability {
		t.Fatalf("bridge_capabilities = %#v", hello.BridgeCapabilities)
	}
	for _, capability := range hello.Capabilities {
		if capability == protocol.ArtifactReadCapability {
			t.Fatal("Bridge capability leaked into model-facing tool capabilities")
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

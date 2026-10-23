package nexusbridge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	protocol "github.com/uvwt/agentdock-protocol"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/mcp"
	"github.com/uvwt/agentdock/internal/publicartifacts"
)

func newRecoveryTestClient(endpoint string) *Client {
	client := NewClient(Identity{Endpoint: endpoint}, mcp.NewServer(nil, config.Config{}), nil, publicartifacts.Store{}, &ConnectionState{})
	client.handshakeTimeout = 100 * time.Millisecond
	client.readTimeout = 150 * time.Millisecond
	return client
}

func TestBridgeHandshakeTimeout(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer socket.Close()
		// 接受 WebSocket 但始终不返回 node.ready。
		for {
			if _, _, err := socket.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	client := newRecoveryTestClient(server.URL)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	started := time.Now()
	outcome := client.connect(ctx, server.URL)
	if outcome.err == nil || !outcome.fallbackAllowed || client.state.Connected() {
		t.Fatalf("invalid handshake timeout outcome: %+v", outcome)
	}
	if time.Since(started) > time.Second {
		t.Fatal("handshake waited for parent cancellation instead of its own timeout")
	}
}

func TestBridgeReconnectsWhenPeerStopsResponding(t *testing.T) {
	upgrader := websocket.Upgrader{}
	var connections atomic.Int32
	reconnected := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer socket.Close()
		attempt := connections.Add(1)
		var hello protocol.Message
		if socket.ReadJSON(&hello) != nil {
			return
		}
		if attempt == 1 {
			// TCP 仍能接收写入，但远端应用已不再回应，模拟休眠/断网后的失效连接。
			socket.SetPingHandler(func(string) error { return nil })
		}
		if socket.WriteJSON(protocol.Message{Type: protocol.MessageNodeReady, ProtocolVersion: protocol.ConnectionProtocolVersion, HeartbeatMS: 30}) != nil {
			return
		}
		if attempt > 1 {
			select {
			case reconnected <- struct{}{}:
			default:
			}
		}
		for {
			if _, _, err := socket.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	client := newRecoveryTestClient(server.URL)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { client.Run(ctx); close(done) }()
	select {
	case <-reconnected:
	case <-time.After(3 * time.Second):
		t.Error("bridge did not reconnect after peer stopped responding")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("bridge did not stop")
	}
}

func TestBridgeKeepsIdleResponsiveConnection(t *testing.T) {
	upgrader := websocket.Upgrader{}
	ready := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer socket.Close()
		var hello protocol.Message
		if socket.ReadJSON(&hello) != nil {
			return
		}
		if socket.WriteJSON(protocol.Message{Type: protocol.MessageNodeReady, ProtocolVersion: protocol.ConnectionProtocolVersion, HeartbeatMS: 30}) != nil {
			return
		}
		close(ready)
		// 无业务消息，gorilla 默认处理控制帧 Ping/Pong。
		for {
			if _, _, err := socket.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	client := newRecoveryTestClient(server.URL)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan connectionOutcome, 1)
	go func() { done <- client.connect(ctx, server.URL) }()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("no handshake")
	}
	select {
	case outcome := <-done:
		t.Fatalf("responsive idle connection was closed: %+v", outcome)
	case <-time.After(4 * client.readTimeout):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("connection did not stop")
	}
}

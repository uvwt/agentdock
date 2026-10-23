package nexusbridge

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	protocol "github.com/uvwt/agentdock-protocol"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/mcp"
	"github.com/uvwt/agentdock/internal/publicartifacts"
)

type cancelTestNode struct {
	NodeAPI
	canceled chan bool
}

func (n *cancelTestNode) Invoke(ctx context.Context, _ string, _ map[string]any) (map[string]any, error) {
	select {
	case <-ctx.Done():
		n.canceled <- true
	case <-time.After(80 * time.Millisecond):
		n.canceled <- false
	}
	return map[string]any{}, nil
}
func serverTestFrame(message protocol.Message) []byte {
	data, _ := json.Marshal(message)
	header := []byte{0x81}
	if len(data) < 126 {
		header = append(header, byte(len(data)))
	} else {
		header = append(header, 126, 0, 0)
		binary.BigEndian.PutUint16(header[2:], uint16(len(data)))
	}
	return append(header, data...)
}
func TestRegressionImmediateCancelIsDelivered(t *testing.T) {
	prior := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(prior)
	node := &cancelTestNode{NodeAPI: mcp.NewServer(nil, config.Config{}), canceled: make(chan bool, 1)}
	upgrader := websocket.Upgrader{}
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
		frames := serverTestFrame(protocol.Message{Type: protocol.MessageNodeReady, ProtocolVersion: protocol.ConnectionProtocolVersion, HeartbeatMS: 60000})
		frames = append(frames, serverTestFrame(protocol.Message{Type: protocol.MessageToolInvoke, RequestID: "probe", Operation: protocol.OperationToolCall, Arguments: json.RawMessage(`{"tool":"probe","arguments":{}}`)})...)
		frames = append(frames, serverTestFrame(protocol.Message{Type: protocol.MessageToolCancel, RequestID: "probe"})...)
		if _, err := socket.UnderlyingConn().Write(frames); err != nil {
			return
		}
		for {
			if _, _, err := socket.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := NewClient(Identity{Endpoint: server.URL}, node, nil, publicartifacts.Store{}, &ConnectionState{})
	done := make(chan struct{})
	go func() { client.Run(ctx); close(done) }()
	var canceled bool
	select {
	case canceled = <-node.canceled:
	case <-time.After(time.Second):
		t.Fatal("invoke did not execute")
	}
	cancel()
	<-done
	if !canceled {
		t.Fatal("cancel immediately following invoke was lost; tool ran for full 80 ms")
	}
}

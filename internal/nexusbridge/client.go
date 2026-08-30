package nexusbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	protocol "github.com/uvwt/agentdock-protocol"
	"github.com/uvwt/agentdock/internal/app"
	"github.com/uvwt/agentdock/internal/buildinfo"
	"github.com/uvwt/agentdock/internal/httpx"
	"github.com/uvwt/agentdock/internal/mcp"
)

const maxMessageBytes = 8 << 20

type Client struct {
	identity Identity
	server   *mcp.Server
	runtime  httpx.RuntimeAPI
	state    *ConnectionState
	writeMu  sync.Mutex
	cancelMu sync.Mutex
	cancels  map[string]context.CancelFunc
}

func NewClient(identity Identity, server *mcp.Server, runtime httpx.RuntimeAPI, state *ConnectionState) *Client {
	return &Client{identity: identity, server: server, runtime: runtime, state: state, cancels: make(map[string]context.CancelFunc)}
}

func (c *Client) Run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := c.connect(ctx)
		if ctx.Err() != nil {
			return
		}
		slog.Warn("NexusDock connection lost", "error", err, "retry_in", backoff)
		timer := time.NewTimer(backoff + time.Duration(rand.IntN(500))*time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (c *Client) connect(ctx context.Context) error {
	c.state.SetConnected(false)
	endpoint, err := url.Parse(c.identity.Endpoint)
	if err != nil {
		return err
	}
	if endpoint.Scheme == "https" {
		endpoint.Scheme = "wss"
	} else {
		endpoint.Scheme = "ws"
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/v1/nodes/connect"
	header := http.Header{"Authorization": []string{"Bearer " + c.identity.DeviceToken}}
	socket, response, err := websocket.DefaultDialer.DialContext(ctx, endpoint.String(), header)
	if err != nil {
		if response != nil {
			return fmt.Errorf("连接 NexusDock（HTTP %d）: %w", response.StatusCode, err)
		}
		return fmt.Errorf("连接 NexusDock: %w", err)
	}
	defer socket.Close()
	socket.SetReadLimit(maxMessageBytes)

	tools := c.server.ToolNames()
	descriptors, err := bridgeToolDescriptors(c.server.ToolDescriptors())
	if err != nil {
		return err
	}
	if err := c.write(socket, protocol.Message{
		Type: protocol.MessageNodeHello, ProtocolVersion: protocol.ConnectionProtocolVersion,
		Hello: bridgeHello(c.identity, tools, descriptors, c.server.UIResources(), c.server.ToolContractHash()),
	}); err != nil {
		return err
	}
	var ready protocol.Message
	if err := socket.ReadJSON(&ready); err != nil {
		return fmt.Errorf("读取 NexusDock 握手响应: %w", err)
	}
	if ready.Type != protocol.MessageNodeReady || ready.ProtocolVersion != protocol.ConnectionProtocolVersion {
		return errors.New("NexusDock 返回了不兼容的节点协议")
	}
	c.state.SetConnected(true)
	defer c.state.SetConnected(false)
	slog.Info("NexusDock node connected", "node_id", c.identity.NodeID, "endpoint", c.identity.Endpoint)
	heartbeat := time.Duration(ready.HeartbeatMS) * time.Millisecond
	if heartbeat <= 0 {
		heartbeat = 30 * time.Second
	}
	connectionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go c.heartbeat(connectionCtx, socket, heartbeat)
	for {
		var incoming protocol.Message
		if err := socket.ReadJSON(&incoming); err != nil {
			return err
		}
		switch incoming.Type {
		case protocol.MessageToolInvoke:
			go c.invoke(connectionCtx, socket, incoming)
		case protocol.MessageToolCancel:
			c.cancel(incoming.RequestID)
		case protocol.MessageNodeHeartbeat:
		}
	}
}

func bridgeHello(identity Identity, tools []string, descriptors []protocol.ToolDescriptor, uiResources []protocol.UIResourceCapability, toolContractHash string) *protocol.Hello {
	return &protocol.Hello{
		DeviceID:           identity.DeviceID,
		Version:            buildinfo.Version,
		ProtocolVersion:    protocol.ConnectionProtocolVersion,
		OS:                 runtime.GOOS,
		Arch:               runtime.GOARCH,
		Capabilities:       append([]string(nil), tools...),
		BridgeCapabilities: []string{protocol.ArtifactReadCapability},
		ToolContractHash:   toolContractHash,
		Tools:              descriptors,
		UIResources:        uiResources,
	}
}

func (c *Client) invoke(parent context.Context, socket *websocket.Conn, incoming protocol.Message) {
	ctx, cancel := context.WithCancel(parent)
	c.cancelMu.Lock()
	c.cancels[incoming.RequestID] = cancel
	c.cancelMu.Unlock()
	defer func() {
		cancel()
		c.cancelMu.Lock()
		delete(c.cancels, incoming.RequestID)
		c.cancelMu.Unlock()
	}()

	var result map[string]any
	var err error
	switch incoming.Operation {
	case protocol.OperationRuntimeRequest:
		var request httpx.RuntimeBridgeRequest
		if decodeErr := json.Unmarshal(incoming.Arguments, &request); decodeErr != nil {
			err = fmt.Errorf("解析 Runtime 请求: %w", decodeErr)
		} else {
			result, err = httpx.DispatchRuntimeBridgeRequest(ctx, c.runtime, request)
		}
	case protocol.OperationContextLocal:
		result, err = c.server.AgentDockLocalContext(ctx)
	case protocol.OperationToolCall:
		var request struct {
			Tool      string         `json:"tool"`
			Arguments map[string]any `json:"arguments"`
		}
		if decodeErr := json.Unmarshal(incoming.Arguments, &request); decodeErr != nil {
			err = fmt.Errorf("解析工具请求: %w", decodeErr)
		} else {
			result, err = c.server.Invoke(ctx, request.Tool, request.Arguments)
		}
	case protocol.OperationResourceRead:
		var request struct {
			URI string `json:"uri"`
		}
		if decodeErr := json.Unmarshal(incoming.Arguments, &request); decodeErr != nil {
			err = fmt.Errorf("解析 MCP App resource 请求: %w", decodeErr)
		} else {
			result, err = c.server.ReadAppResource(request.URI)
		}
	case protocol.OperationArtifactRead:
		var request struct {
			ArtifactID string `json:"artifact_id"`
			Offset     int64  `json:"offset"`
			MaxBytes   int    `json:"max_bytes"`
		}
		if decodeErr := json.Unmarshal(incoming.Arguments, &request); decodeErr != nil {
			err = fmt.Errorf("解析 Artifact 读取请求: %w", decodeErr)
		} else {
			result, err = c.server.ReadArtifactChunk(request.ArtifactID, request.Offset, request.MaxBytes)
		}
	default:
		err = fmt.Errorf("不支持的 NexusDock 节点操作: %s", incoming.Operation)
	}
	if err != nil {
		_ = c.write(socket, protocol.Message{Type: protocol.MessageToolError, RequestID: incoming.RequestID, Error: bridgeError(err)})
		return
	}
	encoded, encodeErr := json.Marshal(result)
	if encodeErr != nil {
		_ = c.write(socket, protocol.Message{Type: protocol.MessageToolError, RequestID: incoming.RequestID, Error: bridgeError(fmt.Errorf("编码节点结果: %w", encodeErr))})
		return
	}
	_ = c.write(socket, protocol.Message{Type: protocol.MessageToolResult, RequestID: incoming.RequestID, Result: encoded})
}

func bridgeToolDescriptors(descriptors []map[string]any) ([]protocol.ToolDescriptor, error) {
	encoded, err := json.Marshal(descriptors)
	if err != nil {
		return nil, fmt.Errorf("编码 Nexus Bridge 工具契约: %w", err)
	}
	var tools []protocol.ToolDescriptor
	if err := json.Unmarshal(encoded, &tools); err != nil {
		return nil, fmt.Errorf("解析 Nexus Bridge 工具契约: %w", err)
	}
	return tools, nil
}

func (c *Client) heartbeat(ctx context.Context, socket *websocket.Conn, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.write(socket, protocol.Message{Type: protocol.MessageNodeHeartbeat}); err != nil {
				_ = socket.Close()
				return
			}
		}
	}
}

func (c *Client) write(socket *websocket.Conn, outgoing protocol.Message) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = socket.SetWriteDeadline(time.Now().Add(15 * time.Second))
	return socket.WriteJSON(outgoing)
}

func (c *Client) cancel(requestID string) {
	c.cancelMu.Lock()
	cancel := c.cancels[requestID]
	c.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func bridgeError(err error) *protocol.RemoteError {
	converted := &protocol.RemoteError{Code: "NODE_OPERATION_FAILED", Message: err.Error()}
	var toolErr *app.ToolError
	if errors.As(err, &toolErr) {
		converted.Code = toolErr.Code
		converted.Message = toolErr.Message
		converted.Category = toolErr.Category
		converted.Retryable = toolErr.Retryable
		converted.Details = toolErr.Details
	}
	return converted
}

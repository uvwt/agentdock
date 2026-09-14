package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
)

const maxRPCLineBytes = 4 << 20

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcResponse struct {
	result json.RawMessage
	err    error
}

type RequestHandler func(context.Context, string, json.RawMessage) (any, *rpcError)
type NotificationHandler func(string, json.RawMessage)

type Connection struct {
	reader io.ReadCloser
	writer io.WriteCloser

	writeMu sync.Mutex

	pendingMu sync.Mutex
	pending   map[string]chan rpcResponse
	nextID    atomic.Uint64
	inboundMu sync.Mutex
	inbound   map[string]context.CancelFunc

	requestHandler      RequestHandler
	notificationHandler NotificationHandler

	closed    chan struct{}
	closeOnce sync.Once
	closeErr  error
}

func NewConnection(reader io.ReadCloser, writer io.WriteCloser, requestHandler RequestHandler, notificationHandler NotificationHandler) *Connection {
	connection := &Connection{
		reader:              reader,
		writer:              writer,
		pending:             make(map[string]chan rpcResponse),
		inbound:             make(map[string]context.CancelFunc),
		requestHandler:      requestHandler,
		notificationHandler: notificationHandler,
		closed:              make(chan struct{}),
	}
	go connection.readLoop()
	return connection
}

func (c *Connection) Request(ctx context.Context, method string, params any, result any) error {
	return c.request(ctx, method, params, result, nil)
}

func (c *Connection) request(ctx context.Context, method string, params any, result any, dispatched func()) error {
	if c == nil {
		return errors.New("ACP connection is nil")
	}
	id := c.nextID.Add(1)
	idRaw := json.RawMessage(strconv.FormatUint(id, 10))
	key := string(idRaw)
	response := make(chan rpcResponse, 1)
	paramsRaw, err := marshalRaw(params)
	if err != nil {
		return newError("ACP_PROTOCOL_ERROR", "encode ACP request parameters", false, map[string]any{"method": method}, err)
	}

	c.pendingMu.Lock()
	select {
	case <-c.closed:
		c.pendingMu.Unlock()
		return newError("ACP_CONNECTION_CLOSED", "ACP connection is closed", true, nil, c.closeErr)
	default:
	}
	c.pending[key] = response
	c.pendingMu.Unlock()

	if err := c.writeMessage(rpcMessage{
		JSONRPC: "2.0",
		ID:      idRaw,
		Method:  method,
		Params:  paramsRaw,
	}); err != nil {
		c.removePending(key)
		return err
	}
	if dispatched != nil {
		dispatched()
	}

	select {
	case reply := <-response:
		if reply.err != nil {
			return reply.err
		}
		if result == nil || len(reply.result) == 0 || string(reply.result) == "null" {
			return nil
		}
		if err := json.Unmarshal(reply.result, result); err != nil {
			return newError("ACP_INVALID_RESPONSE", "decode ACP response", false, map[string]any{"method": method}, err)
		}
		return nil
	case <-ctx.Done():
		c.removePending(key)
		_ = c.Notify("$/cancel_request", map[string]any{"requestId": id})
		return newError("ACP_REQUEST_CANCELLED", "ACP request was cancelled", true, map[string]any{"method": method}, ctx.Err())
	case <-c.closed:
		c.removePending(key)
		return newError("ACP_CONNECTION_CLOSED", "ACP connection closed while waiting for a response", true, map[string]any{"method": method}, c.closeErr)
	}
}

func (c *Connection) Notify(method string, params any) error {
	if c == nil {
		return errors.New("ACP connection is nil")
	}
	paramsRaw, err := marshalRaw(params)
	if err != nil {
		return newError("ACP_PROTOCOL_ERROR", "encode ACP notification parameters", false, map[string]any{"method": method}, err)
	}
	return c.writeMessage(rpcMessage{JSONRPC: "2.0", Method: method, Params: paramsRaw})
}

func (c *Connection) Close() error {
	if c == nil {
		return nil
	}
	c.finish(nil)
	return c.closeErr
}

func (c *Connection) Closed() <-chan struct{} { return c.closed }

func (c *Connection) readLoop() {
	scanner := bufio.NewScanner(c.reader)
	scanner.Buffer(make([]byte, 64<<10), maxRPCLineBytes)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		var message rpcMessage
		if err := json.Unmarshal(line, &message); err != nil {
			c.finish(newError("ACP_PROTOCOL_ERROR", "decode ACP JSON-RPC message", false, nil, err))
			return
		}
		if message.JSONRPC != "2.0" {
			c.finish(newError("ACP_PROTOCOL_ERROR", "ACP message omitted JSON-RPC version 2.0", false, nil, nil))
			return
		}
		if message.Method != "" {
			if len(message.ID) > 0 && string(message.ID) != "null" {
				c.startRequest(message)
			} else if message.Method == "$/cancel_request" {
				c.cancelInbound(message.Params)
			} else if c.notificationHandler != nil {
				c.notificationHandler(message.Method, message.Params)
			}
			continue
		}
		if len(message.ID) == 0 || string(message.ID) == "null" {
			c.finish(newError("ACP_PROTOCOL_ERROR", "ACP response omitted a request id", false, nil, nil))
			return
		}
		c.handleResponse(message)
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		c.finish(newError("ACP_CONNECTION_FAILED", "read ACP stream", true, nil, err))
		return
	}
	c.finish(newError("ACP_CONNECTION_CLOSED", "ACP stream reached EOF", true, nil, io.EOF))
}

func (c *Connection) startRequest(message rpcMessage) {
	key := string(message.ID)
	requestCtx, cancel := context.WithCancel(context.Background())
	c.inboundMu.Lock()
	if _, exists := c.inbound[key]; exists {
		c.inboundMu.Unlock()
		cancel()
		go func() {
			_ = c.writeMessage(rpcMessage{JSONRPC: "2.0", ID: message.ID, Error: &rpcError{Code: -32600, Message: "Duplicate request id"}})
		}()
		return
	}
	c.inbound[key] = cancel
	c.inboundMu.Unlock()
	go c.handleRequest(requestCtx, key, cancel, message)
}

func (c *Connection) handleRequest(requestCtx context.Context, key string, cancel context.CancelFunc, message rpcMessage) {
	defer func() {
		c.inboundMu.Lock()
		delete(c.inbound, key)
		c.inboundMu.Unlock()
		cancel()
	}()
	response := rpcMessage{JSONRPC: "2.0", ID: message.ID}
	func() {
		defer func() {
			if recover() != nil {
				response.Result = nil
				response.Error = &rpcError{Code: -32603, Message: "Internal error"}
			}
		}()
		if c.requestHandler == nil {
			response.Error = &rpcError{Code: -32601, Message: "Method not found"}
			return
		}
		result, rpcErr := c.requestHandler(requestCtx, message.Method, message.Params)
		response.Error = rpcErr
		if rpcErr == nil {
			encoded, err := marshalRaw(result)
			if err != nil {
				response.Error = &rpcError{Code: -32603, Message: "Internal error"}
			} else {
				response.Result = encoded
			}
		}
	}()
	_ = c.writeMessage(response)
}

func (c *Connection) handleResponse(message rpcMessage) {
	key := string(message.ID)
	c.pendingMu.Lock()
	response := c.pending[key]
	delete(c.pending, key)
	c.pendingMu.Unlock()
	if response == nil {
		return
	}
	if message.Error != nil {
		details := map[string]any{"rpc_code": message.Error.Code}
		if len(message.Error.Data) > 0 {
			details["rpc_data"] = string(message.Error.Data)
		}
		response <- rpcResponse{err: newError("ACP_REMOTE_ERROR", message.Error.Message, false, details, nil)}
		return
	}
	response <- rpcResponse{result: message.Result}
}

func (c *Connection) writeMessage(message rpcMessage) error {
	data, err := json.Marshal(message)
	if err != nil {
		return newError("ACP_PROTOCOL_ERROR", "encode ACP JSON-RPC message", false, nil, err)
	}
	if len(data) > maxRPCLineBytes {
		return newError("ACP_MESSAGE_TOO_LARGE", "ACP JSON-RPC message exceeds limit", false, map[string]any{"bytes": len(data), "limit": maxRPCLineBytes}, nil)
	}
	data = append(data, '\n')
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	select {
	case <-c.closed:
		return newError("ACP_CONNECTION_CLOSED", "ACP connection is closed", true, nil, c.closeErr)
	default:
	}
	if _, err := c.writer.Write(data); err != nil {
		return newError("ACP_CONNECTION_FAILED", "write ACP stream", true, nil, err)
	}
	return nil
}

func (c *Connection) removePending(key string) {
	c.pendingMu.Lock()
	delete(c.pending, key)
	c.pendingMu.Unlock()
}

func (c *Connection) failPending(err error) {
	c.pendingMu.Lock()
	pending := c.pending
	c.pending = make(map[string]chan rpcResponse)
	c.pendingMu.Unlock()
	for _, response := range pending {
		response <- rpcResponse{err: err}
	}
}

func (c *Connection) finish(err error) {
	c.closeOnce.Do(func() {
		c.closeErr = errors.Join(c.closeErr, err)
		if c.writer != nil {
			c.closeErr = errors.Join(c.closeErr, c.writer.Close())
		}
		if c.reader != nil {
			// Closing a Windows anonymous-pipe reader while another goroutine is
			// blocked in Read can wait for the peer to close and deadlock shutdown.
			// Publish the terminal state synchronously, but release the owned reader
			// in the background; the process controller or peer cleanup supplies EOF.
			reader := c.reader
			go func() { _ = reader.Close() }()
		}
		close(c.closed)
		c.failPending(newError("ACP_CONNECTION_CLOSED", "ACP connection closed", true, nil, c.closeErr))
		c.cancelAllInbound()
	})
}

func marshalRaw(value any) (json.RawMessage, error) {
	if value == nil {
		return json.RawMessage("null"), nil
	}
	if raw, ok := value.(json.RawMessage); ok {
		return raw, nil
	}
	return json.Marshal(value)
}

func (c *Connection) cancelInbound(params json.RawMessage) {
	var cancellation struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(params, &cancellation) != nil || len(cancellation.RequestID) == 0 {
		return
	}
	c.inboundMu.Lock()
	cancel := c.inbound[string(cancellation.RequestID)]
	c.inboundMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (c *Connection) cancelAllInbound() {
	c.inboundMu.Lock()
	inbound := c.inbound
	c.inbound = make(map[string]context.CancelFunc)
	c.inboundMu.Unlock()
	for _, cancel := range inbound {
		cancel()
	}
}

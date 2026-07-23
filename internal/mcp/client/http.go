package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/uvwt/agentdock/internal/config"
)

const maxHTTPResponseBytes = 32 << 20

type streamableHTTPClient struct {
	cfg             ServerConfig
	http            *http.Client
	mu              sync.Mutex
	nextID          int64
	sessionID       string
	protocolVersion string
}

func newStreamableHTTPClient(cfg ServerConfig) *streamableHTTPClient {
	return &streamableHTTPClient{cfg: cfg, http: &http.Client{}, protocolVersion: config.ProtocolVersion}
}

func (c *streamableHTTPClient) initialize(ctx context.Context) error {
	var result initializeResult
	if err := c.request(ctx, rpcRequest{
		JSONRPC: "2.0",
		ID:      c.requestID(),
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": config.ProtocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": config.ServerName, "version": config.Version},
		},
	}, &result); err != nil {
		return err
	}
	if strings.TrimSpace(result.ProtocolVersion) == "" {
		return newError("MCP_INVALID_RESPONSE", "MCP initialize response omitted protocolVersion", false, nil, nil)
	}
	c.mu.Lock()
	c.protocolVersion = result.ProtocolVersion
	c.mu.Unlock()
	return c.notify(ctx, "notifications/initialized", map[string]any{})
}

func (c *streamableHTTPClient) listTools(ctx context.Context) ([]Tool, error) {
	tools := make([]Tool, 0)
	cursor := ""
	for page := 0; page < 100; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var result listToolsResult
		if err := c.request(ctx, rpcRequest{JSONRPC: "2.0", ID: c.requestID(), Method: "tools/list", Params: params}, &result); err != nil {
			return nil, err
		}
		tools = append(tools, result.Tools...)
		if result.NextCursor == "" {
			return tools, nil
		}
		cursor = result.NextCursor
	}
	return nil, newError("MCP_INVALID_RESPONSE", "MCP tools/list pagination exceeded 100 pages", false, nil, nil)
}

func (c *streamableHTTPClient) callTool(ctx context.Context, name string, arguments map[string]any) (map[string]any, error) {
	var result map[string]any
	if err := c.request(ctx, rpcRequest{
		JSONRPC: "2.0",
		ID:      c.requestID(),
		Method:  "tools/call",
		Params:  map[string]any{"name": name, "arguments": arguments},
	}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *streamableHTTPClient) close() error { return nil }

func (c *streamableHTTPClient) notify(ctx context.Context, method string, params any) error {
	payload, err := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("encode MCP notification: %w", err)
	}
	response, err := c.do(ctx, payload)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusAccepted || response.StatusCode == http.StatusNoContent {
		return nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return c.httpStatusError(response)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxHTTPResponseBytes))
	return nil
}

func (c *streamableHTTPClient) request(ctx context.Context, request rpcRequest, output any) error {
	payload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode MCP request: %w", err)
	}
	response, err := c.do(ctx, payload)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return c.httpStatusError(response)
	}
	rpcResponse, err := decodeHTTPRPCResponse(response)
	if err != nil {
		return err
	}
	return decodeRPCResult(rpcResponse, output)
}

func (c *streamableHTTPClient) do(ctx context.Context, payload []byte) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return nil, newError("MCP_REQUEST_FAILED", "build MCP HTTP request", false, nil, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("User-Agent", config.ServerName+"/"+config.Version)

	c.mu.Lock()
	protocolVersion := c.protocolVersion
	sessionID := c.sessionID
	c.mu.Unlock()
	request.Header.Set("MCP-Protocol-Version", protocolVersion)
	if sessionID != "" {
		request.Header.Set("Mcp-Session-Id", sessionID)
	}
	for header, envName := range c.cfg.HeaderEnv {
		value, ok := c.cfg.RuntimeEnv[envName]
		if !ok {
			value, ok = os.LookupEnv(envName)
		}
		if !ok || value == "" {
			return nil, newError(
				"MCP_AUTH_REQUIRED",
				"required MCP HTTP header environment variable is missing",
				false,
				map[string]any{"server": c.cfg.Name, "header": header, "env": envName},
				nil,
			)
		}
		request.Header.Set(header, value)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, newError("MCP_CONNECTION_FAILED", "call MCP HTTP server", true, map[string]any{"server": c.cfg.Name}, err)
	}
	if id := strings.TrimSpace(response.Header.Get("Mcp-Session-Id")); id != "" {
		c.mu.Lock()
		c.sessionID = id
		c.mu.Unlock()
	}
	return response, nil
}

func (c *streamableHTTPClient) httpStatusError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	return newError(
		"MCP_HTTP_ERROR",
		fmt.Sprintf("MCP HTTP server returned %s", response.Status),
		response.StatusCode >= 500,
		map[string]any{"server": c.cfg.Name, "status_code": response.StatusCode, "body": strings.TrimSpace(string(body))},
		nil,
	)
}

func (c *streamableHTTPClient) requestID() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	return c.nextID
}

func decodeHTTPRPCResponse(response *http.Response) (rpcResponse, error) {
	mediaType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if mediaType == "text/event-stream" {
		return decodeSSERPCResponse(response.Body)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxHTTPResponseBytes+1))
	if err != nil {
		return rpcResponse{}, newError("MCP_INVALID_RESPONSE", "read MCP HTTP response", true, nil, err)
	}
	if len(data) > maxHTTPResponseBytes {
		return rpcResponse{}, newError("MCP_RESPONSE_TOO_LARGE", "MCP HTTP response exceeds 32 MiB", false, nil, nil)
	}
	var decoded rpcResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return rpcResponse{}, newError("MCP_INVALID_RESPONSE", "decode MCP HTTP JSON-RPC response", false, nil, err)
	}
	return decoded, nil
}

func decodeSSERPCResponse(reader io.Reader) (rpcResponse, error) {
	scanner := bufio.NewScanner(io.LimitReader(reader, maxHTTPResponseBytes+1))
	scanner.Buffer(make([]byte, 64<<10), maxHTTPResponseBytes)
	dataLines := make([]string, 0, 1)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if len(dataLines) == 0 {
				continue
			}
			var response rpcResponse
			if err := json.Unmarshal([]byte(strings.Join(dataLines, "\n")), &response); err == nil && (len(response.ID) > 0 || response.Error != nil || len(response.Result) > 0) {
				return response, nil
			}
			dataLines = dataLines[:0]
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return rpcResponse{}, newError("MCP_INVALID_RESPONSE", "read MCP SSE response", true, nil, err)
	}
	if len(dataLines) > 0 {
		var response rpcResponse
		if err := json.Unmarshal([]byte(strings.Join(dataLines, "\n")), &response); err == nil {
			return response, nil
		}
	}
	return rpcResponse{}, newError("MCP_INVALID_RESPONSE", "MCP SSE response did not contain a JSON-RPC response", false, nil, nil)
}

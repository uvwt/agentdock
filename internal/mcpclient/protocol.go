package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
)

type protocolClient interface {
	initialize(context.Context) error
	listTools(context.Context) ([]Tool, error)
	callTool(context.Context, string, map[string]any) (map[string]any, error)
	close() error
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type initializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      map[string]any `json:"serverInfo"`
}

type listToolsResult struct {
	Tools      []Tool `json:"tools"`
	NextCursor string `json:"nextCursor,omitempty"`
}

func decodeRPCResult(response rpcResponse, output any) error {
	if response.Error != nil {
		message := response.Error.Message
		if message == "" {
			message = fmt.Sprintf("MCP JSON-RPC error %d", response.Error.Code)
		}
		return newError(
			"MCP_REMOTE_ERROR",
			message,
			false,
			map[string]any{"rpc_code": response.Error.Code, "rpc_data": string(response.Error.Data)},
			nil,
		)
	}
	if output == nil {
		return nil
	}
	if len(response.Result) == 0 {
		return newError("MCP_INVALID_RESPONSE", "MCP response omitted result", false, nil, nil)
	}
	if err := json.Unmarshal(response.Result, output); err != nil {
		return newError("MCP_INVALID_RESPONSE", "decode MCP response result", false, nil, err)
	}
	return nil
}

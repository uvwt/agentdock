package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/local/coding-tools-mcp-go/internal/config"
	"github.com/local/coding-tools-mcp-go/internal/jsonrpc"
	"github.com/local/coding-tools-mcp-go/internal/logx"
	"github.com/local/coding-tools-mcp-go/internal/tools"
)

type Server struct {
	runtime *tools.Runtime
	cfg     config.Config
}

type callToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func NewServer(runtime *tools.Runtime, cfg config.Config) *Server {
	return &Server{runtime: runtime, cfg: cfg}
}

func (s *Server) Dispatch(ctx context.Context, req jsonrpc.Request) jsonrpc.Response {
	if req.JSONRPC != "" && req.JSONRPC != jsonrpc.Version {
		return jsonrpc.Failure(req.ID, -32600, "Invalid Request", "jsonrpc must be 2.0")
	}
	switch req.Method {
	case "initialize":
		return jsonrpc.Success(req.ID, map[string]any{
			"protocolVersion": config.ProtocolVersion,
			"serverInfo":      map[string]any{"name": config.ServerName, "version": config.Version},
			"capabilities":    map[string]any{"tools": map[string]any{}},
		})
	case "notifications/initialized":
		return jsonrpc.Success(req.ID, map[string]any{})
	case "ping":
		return jsonrpc.Success(req.ID, map[string]any{})
	case "tools/list":
		return jsonrpc.Success(req.ID, map[string]any{"tools": s.toolDescriptors()})
	case "tools/call":
		return s.callTool(ctx, req)
	default:
		return jsonrpc.Failure(req.ID, -32601, "Method not found", req.Method)
	}
}

func (s *Server) ServeStdio(in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	encoder := json.NewEncoder(out)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req jsonrpc.Request
		if err := json.Unmarshal(line, &req); err != nil {
			_ = encoder.Encode(jsonrpc.Failure(nil, -32700, "Parse error", err.Error()))
			continue
		}
		resp := s.Dispatch(context.Background(), req)
		if req.ID != nil {
			_ = encoder.Encode(resp)
		}
	}
	return scanner.Err()
}

func (s *Server) callTool(ctx context.Context, req jsonrpc.Request) jsonrpc.Response {
	started := time.Now()
	var params callToolParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			logx.Warn("tool params invalid", "duration_ms", time.Since(started).Milliseconds())
			return jsonrpc.Failure(req.ID, -32602, "Invalid params", err.Error())
		}
	}
	logx.Info("tool started", "tool", params.Name)
	if params.Name == "tool_descriptors" {
		// 这个工具用于排查“源码已更新但 ChatGPT 侧工具描述缓存没刷新”的情况。
		// 直接从 MCP server 返回当前实际暴露的完整 descriptor，避免只看到 runtime 工具名。
		resp := jsonrpc.Success(req.ID, toolEnvelope(params.Name, map[string]any{"ok": true, "tools": s.toolDescriptors(), "count": len(s.toolDescriptors())}, nil))
		logx.Info("tool finished", "tool", params.Name, "duration_ms", time.Since(started).Milliseconds(), "ok", true)
		return resp
	}
	result, err := s.runtime.Call(ctx, params.Name, params.Arguments)
	logx.Info("tool finished", "tool", params.Name, "duration_ms", time.Since(started).Milliseconds(), "ok", err == nil)
	return jsonrpc.Success(req.ID, toolEnvelope(params.Name, result, err))
}

func (s *Server) toolDescriptors() []map[string]any {
	names := s.runtime.ToolNames()
	descriptors := make([]map[string]any, 0, len(names))
	for _, name := range names {
		def, _ := toolDefinition(name)
		descriptors = append(descriptors, map[string]any{
			"name":         name,
			"title":        def.Title,
			"description":  def.Description,
			"inputSchema":  inputSchema(name),
			"outputSchema": outputSchema(name),
			"annotations":  map[string]any{"readOnlyHint": def.ReadOnly || s.cfg.ToolProfile == config.ProfileCompatReadOnlyAll, "destructiveHint": false, "openWorldHint": false},
		})
	}
	return descriptors
}

func toolEnvelope(name string, structured any, err error) map[string]any {
	if err != nil {
		payload := map[string]any{"tool": name, "error": err.Error()}
		var toolErr *tools.ToolError
		if errors.As(err, &toolErr) {
			payload["code"] = toolErr.Code
			payload["category"] = toolErr.Category
			payload["retryable"] = toolErr.Retryable
			payload["details"] = toolErr.Details
			if toolErr.Code == "PERMISSION_REQUIRED" {
				payload["permission_request"] = map[string]any{
					"tool_name":  name,
					"permission": toolErr.Details["permission"],
					"status":     "required",
				}
			}
		}
		return map[string]any{"isError": true, "structuredContent": payload, "content": []map[string]any{{"type": "text", "text": pretty(payload)}}}
	}
	if name == "view_image" {
		payload := asMap(structured)
		if output, _ := payload["output"].(string); output == "mcp_image" {
			return map[string]any{"isError": false, "structuredContent": structured, "content": []map[string]any{{"type": "image", "data": payload["data_base64"], "mimeType": payload["mime_type"]}}}
		}
	}
	return map[string]any{"isError": false, "structuredContent": structured, "content": []map[string]any{{"type": "text", "text": pretty(structured)}}}
}

func asMap(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case tools.Result:
		return map[string]any(typed)
	default:
		return map[string]any{}
	}
}

func pretty(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}

func toolDescription(name string) string {
	if def, ok := toolDefinition(name); ok {
		return def.Description
	}
	return "Coding Tools MCP tool."
}

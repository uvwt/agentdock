package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/uvwt/agentdock/internal/app"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/jsonrpc"
)

type Server struct {
	runtime *app.Runtime
	cfg     config.Config
}

type callToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func NewServer(runtime *app.Runtime, cfg config.Config) *Server {
	return &Server{runtime: runtime, cfg: cfg}
}

func (s *Server) AgentDockContext(ctx context.Context) (app.Result, error) {
	return s.runtime.AgentDockContext(ctx)
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
			if writeErr := encoder.Encode(jsonrpc.Failure(nil, -32700, "Parse error", err.Error())); writeErr != nil {
				return fmt.Errorf("write parse error response: %w", writeErr)
			}
			continue
		}
		resp := s.Dispatch(context.Background(), req)
		if req.ID != nil {
			if err := encoder.Encode(resp); err != nil {
				return fmt.Errorf("write JSON-RPC response: %w", err)
			}
		}
	}
	return scanner.Err()
}

func (s *Server) callTool(ctx context.Context, req jsonrpc.Request) jsonrpc.Response {
	started := time.Now()
	var params callToolParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			slog.Warn("tool params invalid", "duration_ms", time.Since(started).Milliseconds())
			return jsonrpc.Failure(req.ID, -32602, "Invalid params", err.Error())
		}
	}
	slog.Info("tool started", "tool", params.Name)
	result, err := s.runtime.Call(ctx, params.Name, params.Arguments)
	slog.Info("tool finished", "tool", params.Name, "duration_ms", time.Since(started).Milliseconds(), "ok", err == nil)
	return jsonrpc.Success(req.ID, toolEnvelope(params.Name, result, err))
}

func (s *Server) toolDescriptors() []map[string]any {
	return toolDescriptorsForNames(s.runtime.ToolNames())
}

func toolDescriptorsForNames(names []string) []map[string]any {
	descriptors := make([]map[string]any, 0, len(names))
	for _, name := range names {
		def, _ := toolDefinition(name)
		descriptor := map[string]any{
			"name":         name,
			"title":        def.Title,
			"description":  def.Description,
			"inputSchema":  inputSchema(name),
			"outputSchema": outputSchema(name),
		}
		meta := map[string]any{}
		if len(def.FileArgRewritePaths) > 0 {
			paths := append([]string(nil), def.FileArgRewritePaths...)
			descriptor["file_arg_rewrite_paths"] = paths
			meta["file_arg_rewrite_paths"] = paths
			meta["openai/fileParams"] = paths
		}
		if len(def.FileResultRewritePaths) > 0 {
			paths := append([]string(nil), def.FileResultRewritePaths...)
			descriptor["file_result_rewrite_paths"] = paths
			meta["file_result_rewrite_paths"] = paths
			meta["openai/fileResultPaths"] = paths
			meta["openai/fileOutputs"] = paths
		}
		if len(meta) > 0 {
			descriptor["_meta"] = meta
		}
		descriptors = append(descriptors, descriptor)
	}
	return descriptors
}

func toolEnvelope(name string, structured any, err error) map[string]any {
	if err != nil {
		payload := map[string]any{"tool": name, "error": err.Error()}
		var toolErr *app.ToolError
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
		if data, _ := payload["_mcp_image_base64"].(string); data != "" {
			mimeType, _ := payload["_mcp_image_mime_type"].(string)
			clean := cloneWithoutInternalImage(payload)
			return map[string]any{"isError": false, "structuredContent": clean, "content": []map[string]any{{"type": "image", "data": data, "mimeType": mimeType}}}
		}
	}
	if name == "mcp_tool_call" {
		return dynamicMCPToolEnvelope(structured)
	}
	return map[string]any{"isError": false, "structuredContent": structured, "content": []map[string]any{{"type": "text", "text": pretty(structured)}}}
}

func dynamicMCPToolEnvelope(structured any) map[string]any {
	payload := asMap(structured)
	remote, _ := payload["result"].(map[string]any)
	isError, _ := remote["isError"].(bool)
	content, ok := remote["content"]
	if !ok {
		content = []map[string]any{{"type": "text", "text": pretty(payload)}}
	}
	return map[string]any{
		"isError":           isError,
		"structuredContent": payload,
		"content":           content,
	}
}

func cloneWithoutInternalImage(value map[string]any) map[string]any {
	clean := make(map[string]any, len(value))
	for key, item := range value {
		if key == "_mcp_image_base64" || key == "_mcp_image_mime_type" {
			continue
		}
		clean[key] = item
	}
	return clean
}

func asMap(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case app.Result:
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

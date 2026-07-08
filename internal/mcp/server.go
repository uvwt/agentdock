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

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/jsonrpc"
	"github.com/uvwt/agentdock/internal/tools"
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

func (s *Server) CapabilityContext(ctx context.Context, refresh bool) (tools.Result, error) {
	return s.runtime.CapabilityContext(ctx, refresh)
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
			"annotations": map[string]any{
				"readOnlyHint":    def.ReadOnly,
				"destructiveHint": def.Destructive,
				"openWorldHint":   def.OpenWorld,
			},
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
	if name == "browser_act" || name == "browser_snapshot" {
		payload := asMap(structured)
		if attached, _ := payload["image_attached"].(bool); attached {
			return map[string]any{"isError": false, "structuredContent": structured, "content": []map[string]any{{"type": "image", "data": payload["image_base64"], "mimeType": payload["image_mime_type"]}}}
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
	return "AgentDock tool."
}

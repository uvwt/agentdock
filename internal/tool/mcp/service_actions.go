package mcp

import (
	"context"
	"errors"
	"strings"

	"github.com/uvwt/agentdock/internal/envstore"
	mcpclient "github.com/uvwt/agentdock/internal/mcp/client"
)

func (s *Service) Manage(ctx context.Context, request ManageRequest) (Result, error) {
	action := strings.ToLower(strings.TrimSpace(request.Action))
	if action == "" {
		action = "list"
	}
	switch action {
	case "list":
		servers := s.mcpClients.List()
		return Result{"action": action, "servers": servers, "count": len(servers)}, nil
	case "inspect":
		name := request.Name
		cfg, summary, err := s.mcpClients.Inspect(name)
		if err != nil {
			return nil, dynamicMCPToolError(err)
		}
		return Result{"action": action, "server": summary, "config": cfg}, nil
	case "add":
		cfg := mcpclient.ServerConfig{
			Name:        request.Name,
			Description: request.Description,
			Transport:   request.Transport,
			URL:         request.URL,
			Command:     request.Command,
			Args:        append([]string(nil), request.Args...),
			Cwd:         request.CWD,
			HeaderEnv:   cloneStringMap(request.HeaderEnv),
			EnvFromEnv:  cloneStringMap(request.EnvFromEnv),
			Enabled:     boolValue(request.Enabled, true),
			TimeoutMS:   intValue(request.TimeoutMS, 30000),
		}
		server, err := s.mcpClients.Add(cfg)
		if err != nil {
			return nil, dynamicMCPToolError(err)
		}
		return Result{"action": action, "server": server}, nil
	case "remove":
		name := request.Name
		if err := s.mcpClients.Remove(name); err != nil {
			return nil, dynamicMCPToolError(err)
		}
		return Result{"action": action, "name": strings.TrimSpace(name), "removed": true}, nil
	case "enable", "disable":
		name := request.Name
		server, err := s.mcpClients.SetEnabled(name, action == "enable")
		if err != nil {
			return nil, dynamicMCPToolError(err)
		}
		return Result{"action": action, "server": server}, nil
	case "env_set", "env_unset", "env_list":
		name := strings.TrimSpace(request.Name)
		cfg, _, err := s.mcpClients.Inspect(name)
		if err != nil {
			return nil, dynamicMCPToolError(err)
		}
		return s.envAction(envstore.ScopeMCP, cfg.Name, action, request)
	case "refresh":
		name := request.Name
		server, tools, err := s.mcpClients.Refresh(ctx, name)
		if err != nil {
			return nil, dynamicMCPToolError(err)
		}
		return Result{"action": action, "server": server, "tools": tools, "tool_count": len(tools)}, nil
	default:
		return nil, toolErrorDetails(
			"INVALID_ACTION",
			"unsupported mcp_manage action",
			"validation",
			map[string]any{"action": action, "allowed": []string{"list", "inspect", "add", "remove", "enable", "disable", "env_set", "env_unset", "env_list", "refresh"}},
		)
	}
}

func (s *Service) Search(ctx context.Context, request SearchRequest) (Result, error) {
	query := request.Query
	server := request.Server
	limit := boundedInt(intValue(request.Limit, 10), 10, 1, 100)
	tools, err := s.mcpClients.Search(ctx, query, server, limit)
	if err != nil {
		return nil, dynamicMCPToolError(err)
	}
	return Result{"query": query, "server": server, "tools": tools, "count": len(tools)}, nil
}

func (s *Service) Inspect(ctx context.Context, request InspectRequest) (Result, error) {
	qualifiedName := request.Name
	server, tool, err := s.mcpClients.InspectTool(ctx, qualifiedName)
	if err != nil {
		return nil, dynamicMCPToolError(err)
	}
	result := Result{
		"name":         qualifiedName,
		"server":       server,
		"tool_name":    tool.Name,
		"title":        tool.Title,
		"description":  tool.Description,
		"input_schema": tool.InputSchema,
	}
	if tool.OutputSchema != nil {
		result["output_schema"] = tool.OutputSchema
	}
	if tool.Annotations != nil {
		result["annotations"] = tool.Annotations
	}
	return result, nil
}

func (s *Service) Call(ctx context.Context, request CallRequest) (Result, error) {
	qualifiedName := request.Name
	arguments := request.Arguments
	if arguments == nil {
		arguments = map[string]any{}
	}
	result, err := s.mcpClients.Call(ctx, qualifiedName, arguments)
	if err != nil {
		return nil, dynamicMCPToolError(err)
	}
	return Result{"name": qualifiedName, "result": result}, nil
}

func dynamicMCPToolError(err error) error {
	var mcpErr *mcpclient.Error
	if !errors.As(err, &mcpErr) {
		return toolErrorCause("MCP_ERROR", err.Error(), "external", nil, err)
	}
	category := "external"
	if strings.Contains(mcpErr.Code, "INVALID") || strings.Contains(mcpErr.Code, "NOT_FOUND") || strings.Contains(mcpErr.Code, "EXISTS") || strings.Contains(mcpErr.Code, "DISABLED") || strings.Contains(mcpErr.Code, "REQUIRED") {
		category = "validation"
	}
	if mcpErr.Code == "MCP_AUTH_REQUIRED" {
		category = "auth"
	}
	toolErr := toolErrorCause(mcpErr.Code, mcpErr.Message, category, mcpErr.Details, err)
	toolErr.Retryable = mcpErr.Retryable
	return toolErr
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for name, value := range values {
		out[name] = strings.TrimSpace(value)
	}
	return out
}

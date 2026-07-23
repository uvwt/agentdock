package mcp

import (
	"context"
	"errors"
	"strings"

	"github.com/uvwt/agentdock/internal/envstore"
	"github.com/uvwt/agentdock/internal/mcpclient"
)

func (s *Service) Manage(ctx context.Context, args map[string]any) (Result, error) {
	action := strings.ToLower(strings.TrimSpace(stringArg(args, "action", "list")))
	switch action {
	case "list":
		servers := s.mcpClients.List()
		return Result{"action": action, "servers": servers, "count": len(servers)}, nil
	case "inspect":
		name := stringArg(args, "name", "")
		cfg, summary, err := s.mcpClients.Inspect(name)
		if err != nil {
			return nil, dynamicMCPToolError(err)
		}
		return Result{"action": action, "server": summary, "config": cfg}, nil
	case "add":
		cfg := mcpclient.ServerConfig{
			Name:        stringArg(args, "name", ""),
			Description: stringArg(args, "description", ""),
			Transport:   stringArg(args, "transport", ""),
			URL:         stringArg(args, "url", ""),
			Command:     stringArg(args, "command", ""),
			Args:        stringSliceArg(args, "args"),
			Cwd:         stringArg(args, "cwd", ""),
			HeaderEnv:   stringMapArg(args, "header_env"),
			EnvFromEnv:  stringMapArg(args, "env_from_env"),
			Enabled:     boolArg(args, "enabled", true),
			TimeoutMS:   intArg(args, "timeout_ms", 30000),
		}
		server, err := s.mcpClients.Add(cfg)
		if err != nil {
			return nil, dynamicMCPToolError(err)
		}
		return Result{"action": action, "server": server}, nil
	case "remove":
		name := stringArg(args, "name", "")
		if err := s.mcpClients.Remove(name); err != nil {
			return nil, dynamicMCPToolError(err)
		}
		return Result{"action": action, "name": strings.TrimSpace(name), "removed": true}, nil
	case "enable", "disable":
		name := stringArg(args, "name", "")
		server, err := s.mcpClients.SetEnabled(name, action == "enable")
		if err != nil {
			return nil, dynamicMCPToolError(err)
		}
		return Result{"action": action, "server": server}, nil
	case "env_set", "env_unset", "env_list":
		name := strings.TrimSpace(stringArg(args, "name", ""))
		cfg, _, err := s.mcpClients.Inspect(name)
		if err != nil {
			return nil, dynamicMCPToolError(err)
		}
		return s.envAction(envstore.ScopeMCP, cfg.Name, action, args)
	case "refresh":
		name := stringArg(args, "name", "")
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

func (s *Service) Search(ctx context.Context, args map[string]any) (Result, error) {
	query := stringArg(args, "query", "")
	server := stringArg(args, "server", "")
	limit := boundedInt(intArg(args, "limit", 10), 10, 1, 100)
	tools, err := s.mcpClients.Search(ctx, query, server, limit)
	if err != nil {
		return nil, dynamicMCPToolError(err)
	}
	return Result{"query": query, "server": server, "tools": tools, "count": len(tools)}, nil
}

func (s *Service) Inspect(ctx context.Context, args map[string]any) (Result, error) {
	qualifiedName := stringArg(args, "name", "")
	server, tool, err := s.mcpClients.InspectTool(ctx, qualifiedName)
	if err != nil {
		return nil, dynamicMCPToolError(err)
	}
	return Result{
		"name":          qualifiedName,
		"server":        server,
		"tool_name":     tool.Name,
		"title":         tool.Title,
		"description":   tool.Description,
		"input_schema":  tool.InputSchema,
		"output_schema": tool.OutputSchema,
		"annotations":   tool.Annotations,
	}, nil
}

func (s *Service) Call(ctx context.Context, args map[string]any) (Result, error) {
	qualifiedName := stringArg(args, "name", "")
	arguments := mapArg(args, "arguments")
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

func stringMapArg(args map[string]any, key string) map[string]string {
	raw := mapArg(args, key)
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for name, value := range raw {
		out[name] = strings.TrimSpace(stringArg(map[string]any{"value": value}, "value", ""))
	}
	return out
}

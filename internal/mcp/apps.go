package mcp

import (
	"context"
	_ "embed"
	"fmt"
	"net/url"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	protocol "github.com/uvwt/agentdock-protocol"
)

type appResourceDefinition struct {
	URI         string
	Name        string
	Title       string
	Description string
	HTML        string
}

func (s *Server) appResourceDefinitions() []appResourceDefinition {
	definitions := []appResourceDefinition{
		{
			URI:         protocol.ContextUIResourceURI,
			Name:        "agentdock-context",
			Title:       "AgentDock context",
			Description: "Compact read-only AgentDock capability summary with expandable bootstrap context.",
			HTML:        mcpAppHTML("agentdock_context", "AgentDock context"),
		},
		{
			URI:         protocol.TaskProgressUIResourceURI,
			Name:        "agentdock-task-progress",
			Title:       "AgentDock task",
			Description: "Compact read-only task lifecycle view for task_manage results and task snapshots.",
			HTML:        mcpAppHTML("task_progress", "Task"),
		},
		{
			URI:         protocol.FileChangeUIResourceURI,
			Name:        "agentdock-file-change",
			Title:       "AgentDock file change",
			Description: "Read-only view of the file_edit result, including diff preview and file operation summary.",
			HTML:        mcpAppHTML("file_change", "File change"),
		},
		{
			URI:         protocol.DynamicMCPUIResourceURI,
			Name:        "agentdock-dynamic-mcp",
			Title:       "AgentDock dynamic MCP",
			Description: "Compact external MCP tool invocation view.",
			HTML:        mcpAppHTML("dynamic_mcp", "Dynamic MCP"),
		},
		{
			URI:         protocol.ArtifactUIResourceURI,
			Name:        "agentdock-artifact",
			Title:       "AgentDock artifact",
			Description: "Compact published Artifact summary with expandable file metadata and signed URL information.",
			HTML:        mcpAppHTML("artifact", "Artifact"),
		},
	}
	if s.cfg.NexusEndpoint != "" {
		definitions = append(definitions,
			appResourceDefinition{
				URI:         protocol.RecallUIResourceURI,
				Name:        "agentdock-recall",
				Title:       "AgentDock Recall",
				Description: "Compact NexusDock Recall write results.",
				HTML:        mcpAppHTML("recall", "Recall"),
			},
			appResourceDefinition{
				URI:         protocol.WorkflowUIResourceURI,
				Name:        "agentdock-workflow",
				Title:       "AgentDock workflow",
				Description: "Compact workflow template match recommendation view.",
				HTML:        mcpAppHTML("workflow", "Workflow"),
			},
		)
	}
	if s.cfg.ACPEnabled {
		definitions = append(definitions, appResourceDefinition{
			URI:         protocol.ACPStatusUIResourceURI,
			Name:        "agentdock-acp-status",
			Title:       "AgentDock ACP conversation",
			Description: "Read-only ACP session view with concise user and assistant conversation output.",
			HTML:        mcpAppHTML("acp_status", "ACP status"),
		})
	}
	return definitions
}

// UIResources returns the exact MCP App resources this server can serve through the Nexus bridge.
// Capability discovery is derived from the resource registry, never from tool/result UI bindings.
func (s *Server) UIResources() []protocol.UIResourceCapability {
	definitions := s.appResourceDefinitions()
	resources := make([]protocol.UIResourceCapability, 0, len(definitions))
	for _, definition := range definitions {
		contract, ok := protocol.UIResourceContract(definition.URI)
		if !ok {
			continue
		}
		resources = append(resources, protocol.UIResourceCapability{
			URI: definition.URI, Contract: contract, MIMEType: protocol.MCPAppMIMEType,
		})
	}
	return resources
}

func (s *Server) registerAppResources() {
	widgetDomain := appWidgetDomain(s.cfg.OAuthServerURL)

	for _, definition := range s.appResourceDefinitions() {
		definition := definition
		meta := appResourceMeta(widgetDomain)
		s.sdk.AddResource(&mcpsdk.Resource{
			URI:         definition.URI,
			Name:        definition.Name,
			Title:       definition.Title,
			Description: definition.Description,
			MIMEType:    protocol.MCPAppMIMEType,
			Meta:        meta,
		}, func(_ context.Context, request *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
			if request == nil || request.Params == nil || request.Params.URI != definition.URI {
				return nil, mcpsdk.ResourceNotFoundError(definition.URI)
			}
			return appResourceReadResult(definition, widgetDomain), nil
		})
	}
}

// ReadAppResource exposes only AgentDock-owned MCP App resources to the Nexus bridge.
// Tool execution and arbitrary local resources are intentionally not routed through this path.
func (s *Server) ReadAppResource(uri string) (map[string]any, error) {
	if s == nil || s.runtime == nil {
		return nil, fmt.Errorf("AgentDock runtime is not initialized")
	}
	uri = strings.TrimSpace(uri)
	for _, definition := range s.appResourceDefinitions() {
		if definition.URI != uri {
			continue
		}
		meta := appResourceMeta(appWidgetDomain(s.cfg.OAuthServerURL))
		return map[string]any{
			"contents": []any{map[string]any{
				"uri": definition.URI, "mimeType": protocol.MCPAppMIMEType, "text": definition.HTML, "_meta": meta,
			}},
		}, nil
	}
	return nil, fmt.Errorf("MCP App resource not found: %s", uri)
}

func appResourceReadResult(definition appResourceDefinition, widgetDomain string) *mcpsdk.ReadResourceResult {
	return &mcpsdk.ReadResourceResult{Contents: []*mcpsdk.ResourceContents{{
		URI:      definition.URI,
		MIMEType: protocol.MCPAppMIMEType,
		Text:     definition.HTML,
		Meta:     appResourceMeta(widgetDomain),
	}}}
}

func appResourceMeta(widgetDomain string) mcpsdk.Meta {
	ui := map[string]any{
		"csp": map[string]any{
			"connectDomains":  []string{},
			"resourceDomains": []string{},
		},
		"prefersBorder": true,
	}
	if widgetDomain != "" {
		ui["domain"] = widgetDomain
	}
	return mcpsdk.Meta{"ui": ui}
}

func appWidgetDomain(serverURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(serverURL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return ""
	}
	return "https://" + parsed.Host
}

func mcpAppHTML(view, title string) string {
	return strings.NewReplacer("{{VIEW}}", view, "{{TITLE}}", title).Replace(mcpAppHTMLTemplate)
}

//go:embed app.html
var mcpAppHTMLTemplate string

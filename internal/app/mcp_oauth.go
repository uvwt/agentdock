package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"strings"

	mcpclient "github.com/uvwt/agentdock/internal/mcp/client"
	"github.com/uvwt/agentdock/internal/mcp/oauthclient"
)

const mcpOAuthCallbackPath = "/oauth/mcp/callback"

func (r *Runtime) configureMCPOAuthCallbacks() error {
	if r == nil || r.dynamicMCP == nil {
		return nil
	}
	if redirectURL, ok := localMCPOAuthCallback(r.cfg.Host, r.cfg.Port, r.cfg.Stdio); ok {
		if err := r.dynamicMCP.SetOAuthCallback(oauthclient.CallbackOption{
			ID:          oauthclient.CallbackLocal,
			Label:       "在 AgentDock 所在设备授权",
			RedirectURL: redirectURL,
		}); err != nil {
			return fmt.Errorf("configure local MCP OAuth callback: %w", err)
		}
	}

	if publicURL, ok := publicAgentDockOAuthCallback(r.cfg.OAuthServerURL); ok {
		if err := r.dynamicMCP.SetOAuthCallback(oauthclient.CallbackOption{
			ID:          oauthclient.CallbackAgentDock,
			Label:       "通过 AgentDock 公网地址授权",
			RedirectURL: publicURL,
		}); err != nil {
			return fmt.Errorf("configure public MCP OAuth callback: %w", err)
		}
	}
	return nil
}

func localMCPOAuthCallback(bindHost string, port int, stdio bool) (string, bool) {
	if stdio || port < 1 || port > 65535 {
		return "", false
	}
	host := strings.TrimSpace(strings.Trim(strings.TrimSpace(bindHost), "[]"))
	switch {
	case host == "" || host == "0.0.0.0":
		host = "127.0.0.1"
	case host == "::":
		host = "::1"
	case strings.EqualFold(host, "localhost"):
		host = "localhost"
	default:
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			// 只绑定某个非 loopback 网卡时，127.0.0.1 并不一定被该 listener 接收；
			// HTTP loopback redirect 因此不能宣称“可用”。
			return "", false
		}
	}
	return (&url.URL{Scheme: "http", Host: net.JoinHostPort(host, fmt.Sprintf("%d", port)), Path: mcpOAuthCallbackPath}).String(), true
}

func publicAgentDockOAuthCallback(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	if u.Path != "" && u.Path != "/" {
		return "", false
	}
	if isLoopbackOAuthHost(u.Hostname()) {
		return "", false
	}
	u.Path = mcpOAuthCallbackPath
	u.RawPath = ""
	return u.String(), true
}

// SetNexusOAuthCallback 只接收 Nexus 握手声明的公网 Origin。配对 endpoint 可能是
// 内网地址或反代内部地址，不能推导成第三方 OAuth Provider 可访问的 redirect URI。
func (r *Runtime) SetNexusOAuthCallback(publicURL, nodeID string) error {
	if r == nil || r.dynamicMCP == nil {
		return nil
	}
	publicURL = strings.TrimSpace(publicURL)
	if publicURL == "" {
		r.dynamicMCP.RemoveOAuthCallback(oauthclient.CallbackNexus)
		return nil
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" || strings.Contains(nodeID, "/") {
		return errors.New("Nexus OAuth callback requires a valid node id")
	}
	u, err := url.Parse(publicURL)
	if err != nil || u == nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("Nexus public URL must be an HTTPS origin: %q", publicURL)
	}
	u.Path = path.Join("/oauth/mcp/nodes", nodeID, "callback")
	u.RawPath = ""
	return r.dynamicMCP.SetOAuthCallback(oauthclient.CallbackOption{
		ID:          oauthclient.CallbackNexus,
		Label:       "通过 NexusDock 在当前设备授权",
		RedirectURL: u.String(),
	})
}

func (r *Runtime) RuntimeMCPOAuthCallback(_ context.Context, result oauthclient.CallbackResult) error {
	if r == nil || r.dynamicMCP == nil {
		return &ToolError{Code: "MCP_AUTH_UNSUPPORTED", Message: "Remote MCP OAuth is unavailable", Category: "not_found"}
	}
	if err := r.dynamicMCP.DeliverOAuthCallback(result); err != nil {
		var mcpErr *mcpclient.Error
		if errors.As(err, &mcpErr) {
			category := "auth"
			if mcpErr.Code == "MCP_AUTH_STATE_MISMATCH" || mcpErr.Code == "MCP_AUTH_CALLBACK_INVALID" {
				category = "validation"
			}
			return toolErrorCause(mcpErr.Code, mcpErr.Message, category, mcpErr.Details, err)
		}
		return toolErrorCause("MCP_AUTH_FAILED", "deliver Remote MCP OAuth callback", "auth", nil, err)
	}
	return nil
}

func isLoopbackOAuthHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	return ip != nil && ip.IsLoopback()
}

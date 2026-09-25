package oauthclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
)

type Handler struct {
	manager    *Manager
	server     string
	storageKey string
	endpoint   string
}

func (h *Handler) TokenSource(context.Context) (oauth2.TokenSource, error) {
	if h == nil || h.manager == nil {
		return nil, nil
	}
	return h.manager.tokenSource(h.storageKey, h.endpoint)
}

func (h *Handler) Authorize(_ context.Context, _ *http.Request, response *http.Response) error {
	if response == nil {
		return errors.New("OAuth authorization response is required")
	}
	defer response.Body.Close()
	if h == nil || h.manager == nil {
		return errors.New("Remote MCP OAuth runtime is unavailable")
	}

	headers := response.Header.Values("WWW-Authenticate")
	challenges, err := oauthex.ParseWWWAuthenticate(headers)
	if err != nil {
		return fmt.Errorf("parse Remote MCP WWW-Authenticate challenge: %w", err)
	}
	bearerChallenge := false
	insufficientScope := false
	for _, challenge := range challenges {
		if challenge.Scheme != "bearer" {
			continue
		}
		bearerChallenge = true
		if strings.EqualFold(strings.TrimSpace(challenge.Params["error"]), "insufficient_scope") {
			insufficientScope = true
		}
	}
	if !bearerChallenge {
		return errors.New("Remote MCP rejected the request without an OAuth Bearer challenge")
	}
	if response.StatusCode == http.StatusForbidden && !insufficientScope {
		// MCP OAuth 的 403 只在 insufficient_scope 时代表 step-up。普通权限拒绝
		// 必须回到上游，而不是误导模型让用户重复做 OAuth consent。
		return errors.New("Remote MCP returned forbidden without an OAuth insufficient_scope challenge")
	}

	h.manager.RecordChallenge(h.storageKey, h.endpoint, headers)
	// 这里不能等待浏览器交互。普通 MCP 调用必须立即回到模型，由显式
	// mcp_manage authorize 开启一次短期 OAuth Flow。
	return &AuthRequiredError{Server: h.server}
}

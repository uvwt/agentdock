package runtimeapi

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/uvwt/agentdock/internal/app"
	"github.com/uvwt/agentdock/internal/mcp/oauthclient"
)

// Runtime 定义 Runtime API 路由真正需要的应用能力。
// HTTP、Nexus Bridge 等传输层都只依赖这份传输无关契约。
type Runtime interface {
	RuntimeStatus() app.Result
	RuntimeSkills() (app.Result, error)
	RuntimeSkill(skill string) (app.Result, error)
	RuntimeSkillFiles(skill string) (app.Result, error)
	RuntimeSkillFile(skill, path string) (app.Result, error)
	RuntimePlugins(context.Context) (app.Result, error)
	RuntimePlugin(context.Context, string) (app.Result, error)
	RuntimeTasks(status string, limit int) (app.Result, error)
	RuntimeTask(id string) (app.Result, error)
	RuntimeTaskDelete(id string) (app.Result, error)
	RuntimeCapabilities(context.Context, bool) (app.Result, error)
	RuntimeMCPServers(context.Context) (app.Result, error)
	RuntimeMCPServer(context.Context, string) (app.Result, error)
	RuntimeMCPManage(context.Context, map[string]any) (app.Result, error)
	RuntimeEvolve(context.Context, map[string]any) (app.Result, error)
}

// MCPOAuthRuntime 是可选能力：只有支持 Remote MCP OAuth 的 Runtime 才实现。
// 保持它独立于 Runtime 主接口，避免 callback relay 把所有测试替身和只读调用方一起扩展。
type MCPOAuthRuntime interface {
	RuntimeMCPOAuthCallback(context.Context, oauthclient.CallbackResult) error
}

// NexusOAuthCallbackRuntime 由节点 Bridge 在握手后提供 Nexus 公网 callback origin。
type NexusOAuthCallbackRuntime interface {
	SetNexusOAuthCallback(publicURL, nodeID string) error
}

// Request 是 HTTP 与 Nexus Bridge 共用的 Runtime API 请求表示。
type Request struct {
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Query  url.Values      `json:"query,omitempty"`
	Body   json.RawMessage `json:"body,omitempty"`
}

func (r Request) queryValue(key string) string {
	if r.Query == nil {
		return ""
	}
	return r.Query.Get(key)
}

package runtimeapi

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/uvwt/agentdock/internal/app"
)

// Runtime 定义 Runtime API 路由真正需要的应用能力。
// HTTP、Nexus Bridge 等传输层都只依赖这份传输无关契约。
type Runtime interface {
	RuntimeStatus() app.Result
	RuntimeSkills() (app.Result, error)
	RuntimeSkill(skill string) (app.Result, error)
	RuntimeSkillFiles(skill string) (app.Result, error)
	RuntimeSkillFile(skill, path string) (app.Result, error)
	RuntimeTasks(status string, limit int) (app.Result, error)
	RuntimeTask(id string) (app.Result, error)
	RuntimeTaskDelete(id string) (app.Result, error)
	RuntimeCapabilities(context.Context, bool) (app.Result, error)
	RuntimeMCPServers(context.Context) (app.Result, error)
	RuntimeMCPServer(context.Context, string) (app.Result, error)
	RuntimeMCPManage(context.Context, map[string]any) (app.Result, error)
	RuntimeEvolve(context.Context, map[string]any) (app.Result, error)
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

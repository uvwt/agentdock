package mcp

import (
	"context"

	"github.com/uvwt/agentdock/internal/tools"
)

func (s *Server) RuntimeStatus() tools.Result {
	return s.runtime.RuntimeStatus()
}

func (s *Server) RuntimeSkills() (tools.Result, error) {
	return s.runtime.RuntimeSkills()
}

func (s *Server) RuntimeSkill(skill string) (tools.Result, error) {
	return s.runtime.RuntimeSkill(skill)
}

func (s *Server) RuntimeTasks(status string, limit int) (tools.Result, error) {
	return s.runtime.RuntimeTasks(status, limit)
}

func (s *Server) RuntimeTask(id string) (tools.Result, error) {
	return s.runtime.RuntimeTask(id)
}

func (s *Server) RuntimeTaskDelete(id string) (tools.Result, error) {
	return s.runtime.RuntimeTaskDelete(id)
}

func (s *Server) RuntimeCapabilities(ctx context.Context, refresh bool) (tools.Result, error) {
	return s.runtime.RuntimeCapabilities(ctx, refresh)
}

func (s *Server) RuntimeMCPServers(ctx context.Context) (tools.Result, error) {
	return s.runtime.RuntimeMCPServers(ctx)
}

func (s *Server) RuntimeMCPServer(ctx context.Context, name string) (tools.Result, error) {
	return s.runtime.RuntimeMCPServer(ctx, name)
}

func (s *Server) RuntimeMCPManage(ctx context.Context, args map[string]any) (tools.Result, error) {
	return s.runtime.RuntimeMCPManage(ctx, args)
}

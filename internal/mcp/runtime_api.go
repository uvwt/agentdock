package mcp

import (
	"context"

	"github.com/uvwt/agentdock/internal/app"
)

func (s *Server) RuntimeStatus() app.Result {
	return s.runtime.RuntimeStatus()
}

func (s *Server) RuntimeSkills() (app.Result, error) {
	return s.runtime.RuntimeSkills()
}

func (s *Server) RuntimeSkill(skill string) (app.Result, error) {
	return s.runtime.RuntimeSkill(skill)
}

func (s *Server) RuntimeSkillFiles(skill string) (app.Result, error) {
	return s.runtime.RuntimeSkillFiles(skill)
}

func (s *Server) RuntimeSkillFile(skill, path string) (app.Result, error) {
	return s.runtime.RuntimeSkillFile(skill, path)
}

func (s *Server) RuntimeTasks(status string, limit int) (app.Result, error) {
	return s.runtime.RuntimeTasks(status, limit)
}

func (s *Server) RuntimeTask(id string) (app.Result, error) {
	return s.runtime.RuntimeTask(id)
}

func (s *Server) RuntimeTaskDelete(id string) (app.Result, error) {
	return s.runtime.RuntimeTaskDelete(id)
}

func (s *Server) RuntimeCapabilities(ctx context.Context, refresh bool) (app.Result, error) {
	return s.runtime.RuntimeCapabilities(ctx, refresh)
}

func (s *Server) RuntimeMCPServers(ctx context.Context) (app.Result, error) {
	return s.runtime.RuntimeMCPServers(ctx)
}

func (s *Server) RuntimeMCPServer(ctx context.Context, name string) (app.Result, error) {
	return s.runtime.RuntimeMCPServer(ctx, name)
}

func (s *Server) RuntimeMCPManage(ctx context.Context, args map[string]any) (app.Result, error) {
	return s.runtime.RuntimeMCPManage(ctx, args)
}

package mcp

import (
	"context"

	"github.com/uvwt/agentdock/internal/tools"
)

func (s *Server) RuntimeStatus() tools.Result {
	return s.runtime.RuntimeStatus()
}

func (s *Server) RuntimeSetDefaultCWD(path string) (tools.Result, error) {
	return s.runtime.RuntimeSetDefaultCWD(path)
}

func (s *Server) RuntimeSkills() (tools.Result, error) {
	return s.runtime.RuntimeSkills()
}

func (s *Server) RuntimeSkill(skill string) (tools.Result, error) {
	return s.runtime.RuntimeSkill(skill)
}

func (s *Server) RuntimeSkillFiles(skill string) (tools.Result, error) {
	return s.runtime.RuntimeSkillFiles(skill)
}

func (s *Server) RuntimeSkillFile(skill, path string) (tools.Result, error) {
	return s.runtime.RuntimeSkillFile(skill, path)
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

func (s *Server) RuntimeGoals(status string, limit int) (tools.Result, error) {
	return s.runtime.RuntimeGoals(status, limit)
}

func (s *Server) RuntimeGoal(id string) (tools.Result, error) {
	return s.runtime.RuntimeGoal(id)
}

func (s *Server) RuntimeResolveGoalApproval(goalID, approvalID, decision, note string) (tools.Result, error) {
	return s.runtime.RuntimeResolveGoalApproval(goalID, approvalID, decision, note)
}

func (s *Server) RuntimeGoalPause(goalID, summary string) (tools.Result, error) {
	return s.runtime.RuntimeGoalPause(goalID, summary)
}

func (s *Server) RuntimeGoalResume(goalID, summary string) (tools.Result, error) {
	return s.runtime.RuntimeGoalResume(goalID, summary)
}

func (s *Server) RuntimeGoalCancel(goalID, summary string) (tools.Result, error) {
	return s.runtime.RuntimeGoalCancel(goalID, summary)
}

func (s *Server) RuntimeGoalBind(goalID string) (tools.Result, error) {
	return s.runtime.RuntimeGoalBind(goalID)
}

func (s *Server) RuntimeGoalUnbind() tools.Result {
	return s.runtime.RuntimeGoalUnbind()
}

func (s *Server) RuntimeChatGPTWorkerStatus() tools.Result {
	return s.runtime.RuntimeChatGPTWorkerStatus()
}

func (s *Server) RuntimeChatGPTOpen(ctx context.Context) (tools.Result, error) {
	return s.runtime.RuntimeChatGPTOpen(ctx)
}

func (s *Server) RuntimeChatGPTWake(ctx context.Context, goalID string) (tools.Result, error) {
	return s.runtime.RuntimeChatGPTWake(ctx, goalID)
}

func (s *Server) RuntimeRequestReasoning(goalID, request, problem string) (tools.Result, error) {
	return s.runtime.RuntimeRequestReasoning(goalID, request, problem)
}

func (s *Server) RuntimeSetChatGPTAutoWake(enabled bool) tools.Result {
	return s.runtime.RuntimeSetChatGPTAutoWake(enabled)
}

func (s *Server) RuntimeSetChatGPTAutoApproveTools(enabled bool) tools.Result {
	return s.runtime.RuntimeSetChatGPTAutoApproveTools(enabled)
}

func (s *Server) RuntimeChatGPTForceRotate() tools.Result {
	return s.runtime.RuntimeChatGPTForceRotate()
}

func (s *Server) RuntimeOrchestratorStart(goalID string) (tools.Result, error) {
	return s.runtime.RuntimeOrchestratorStart(goalID)
}

func (s *Server) RuntimeOrchestratorStop(goalID string) tools.Result {
	return s.runtime.RuntimeOrchestratorStop(goalID)
}

func (s *Server) RuntimeOrchestratorStatus(goalID string) tools.Result {
	return s.runtime.RuntimeOrchestratorStatus(goalID)
}

func (s *Server) RuntimeTunnelStatus() tools.Result {
	return s.runtime.RuntimeTunnelStatus()
}

func (s *Server) RuntimeTunnelStart(ctx context.Context, mode string) (tools.Result, error) {
	return s.runtime.RuntimeTunnelStart(ctx, mode)
}

func (s *Server) RuntimeTunnelStartOpts(ctx context.Context, opts tools.TunnelStartOptions) (tools.Result, error) {
	return s.runtime.RuntimeTunnelStartOpts(ctx, opts)
}

func (s *Server) RuntimeTunnelStop() (tools.Result, error) {
	return s.runtime.RuntimeTunnelStop()
}

func (s *Server) RuntimeDevices() (tools.Result, error) {
	return s.runtime.RuntimeDevices()
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

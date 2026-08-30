package app

import (
	"context"

	toolacp "github.com/uvwt/agentdock/internal/tool/acp"
)

func acpToolSpecs() []ToolSpec {
	return []ToolSpec{
		{Name: "acp_session", Contract: acpToolContract, Title: "Manage ACP sessions", Description: "Inspect or authenticate the configured ACP agent and create, load, resume, fork, configure, list, inspect, close, or delete persistent ACP sessions through one action-based entrypoint. Session workspaces may use any host-accessible directory and optional methods are capability-gated.", Annotations: mutatingToolAnnotations(true, true), Availability: requiresACP, Handler: typedToolHandler("acp_session", func(ctx context.Context, r *Runtime, request toolacp.SessionRequest) (Result, error) {
			return r.acp.Session(ctx, request)
		})},
		{Name: "acp_prompt", Contract: acpToolContract, Title: "Run ACP prompts", Description: "Start asynchronous ACP prompt turns, poll ordered session events, steer a running turn, or cancel a turn. start returns immediately with a run_id; use action=events for bounded long-poll observation.", Annotations: mutatingToolAnnotations(true, true), Availability: requiresACP, Handler: typedToolHandler("acp_prompt", func(ctx context.Context, r *Runtime, request toolacp.PromptRequest) (Result, error) {
			return r.acp.Prompt(ctx, request)
		})},
		{Name: "acp_interaction", Contract: acpToolContract, Title: "Handle ACP interactions", Description: "List, inspect, respond to, or cancel pending ACP permission interactions. Only options offered by the agent and permitted by the local AgentDock policy may be selected.", Annotations: mutatingToolAnnotations(true, true), Availability: requiresACP, Handler: typedToolHandler("acp_interaction", func(ctx context.Context, r *Runtime, request toolacp.InteractionRequest) (Result, error) {
			return r.acp.Interaction(ctx, request)
		})},
	}
}

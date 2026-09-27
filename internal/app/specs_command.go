package app

import (
	"context"

	toolcommand "github.com/uvwt/agentdock/internal/tool/command"
)

func commandToolSpecs() []ToolSpec {
	return []ToolSpec{
		{Name: "exec_command", Contract: commandToolContract, Title: "Run command", Description: toolcommand.Description(), Annotations: mutatingToolAnnotations(true, true), Handler: typedToolHandler("exec_command", func(ctx context.Context, r *Runtime, request toolcommand.ExecRequest) (Result, error) {
			return r.command.Exec(ctx, request)
		})},
		{Name: "session_observe", Contract: commandToolContract, Title: "Observe command sessions", Description: "List command sessions, consume unread output with status, or replay retained output pages with read and independent stdout/stderr byte offsets. read supports retries and multiple observers; sessions are in-memory, bounded, and may be removed by legacy status or mutation actions.", Annotations: readOnlyToolAnnotations(false), Handler: typedToolHandler("session_observe", func(_ context.Context, r *Runtime, request toolcommand.SessionObserveRequest) (Result, error) {
			return r.command.Observe(request)
		})},
		{Name: "session_act", Contract: commandToolContract, Title: "Act on command sessions", Description: "Write to or stop command sessions through a mutating session tool.", Annotations: mutatingToolAnnotations(true, true), Handler: typedToolHandler("session_act", func(_ context.Context, r *Runtime, request toolcommand.SessionActRequest) (Result, error) {
			return r.command.Act(request)
		})},
	}
}

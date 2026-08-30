package app

import (
	"context"

	toolcommand "github.com/uvwt/agentdock/internal/tool/command"
)

func commandToolSpecs() []ToolSpec {
	return []ToolSpec{
		{Name: "exec_command", Title: "Run command", Description: toolcommand.Description(), Annotations: mutatingToolAnnotations(true, true), Handler: typedToolHandler("exec_command", func(ctx context.Context, r *Runtime, request toolcommand.ExecRequest) (Result, error) {
			return r.command.Exec(ctx, request)
		})},
		{Name: "session_observe", Title: "Observe command sessions", Description: "List or inspect command sessions through a read-only session tool.", Annotations: readOnlyToolAnnotations(false), Handler: typedToolHandler("session_observe", func(_ context.Context, r *Runtime, request toolcommand.SessionObserveRequest) (Result, error) {
			return r.command.Observe(request)
		})},
		{Name: "session_act", Title: "Act on command sessions", Description: "Write to or stop command sessions through a mutating session tool.", Annotations: mutatingToolAnnotations(true, true), Handler: typedToolHandler("session_act", func(_ context.Context, r *Runtime, request toolcommand.SessionActRequest) (Result, error) {
			return r.command.Act(request)
		})},
	}
}

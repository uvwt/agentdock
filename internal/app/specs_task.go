package app

import (
	"context"

	tooltask "github.com/uvwt/agentdock/internal/tool/task"
)

func taskManageToolSpecs() []ToolSpec {
	return []ToolSpec{
		{Name: "task_create", Contract: taskToolContract, Title: "Create recoverable task", Description: "Create one recoverable task before substantial multi-step work. Continue progress updates with task_manage.", Annotations: mutatingToolAnnotations(false, false), Handler: typedToolHandler("task_create", func(ctx context.Context, r *Runtime, request tooltask.CreateRequest) (Result, error) {
			return r.taskTools.Manage(ctx, request.ManageRequest())
		})},
		{Name: "task_manage", Contract: taskToolContract, Title: "Manage recoverable tasks", Description: "Persist substantial AgentDock tasks and update live step progress with checkpoint.", Annotations: mutatingToolAnnotations(false, false), Handler: typedToolHandler("task_manage", func(ctx context.Context, r *Runtime, request tooltask.ManageRequest) (Result, error) {
			return r.taskTools.Manage(ctx, request)
		})},
		{Name: "task_snapshot", Contract: taskToolContract, Title: "Read task snapshot", Description: "Read the latest authoritative compact state for one task. Reserved for MCP Apps task progress refresh.", UIVisibility: []string{"app"}, Annotations: readOnlyToolAnnotations(false), Availability: requiresMCPApps, Handler: typedToolHandler("task_snapshot", func(ctx context.Context, r *Runtime, request tooltask.SnapshotRequest) (Result, error) {
			return r.taskTools.Snapshot(ctx, request)
		})},
	}
}

func workflowToolSpecs() []ToolSpec {
	return []ToolSpec{{Name: "workflow_template_manage", Contract: canonicalToolContract, Title: "Manage workflow templates", Description: "List, get, get multiple, publish, retire, or match AgentDock workflow templates. publish validates and activates a complete immutable template version; get_many requires the model to compose the returned templates before task creation.", Availability: requiresNexus, Handler: typedToolHandler("workflow_template_manage", func(ctx context.Context, r *Runtime, request tooltask.WorkflowRequest) (Result, error) {
		return r.taskTools.WorkflowManage(ctx, request)
	})}}
}

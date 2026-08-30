package app

import (
	"context"

	tooltask "github.com/uvwt/agentdock/internal/tool/task"
)

func taskManageToolSpecs() []ToolSpec {
	return []ToolSpec{{Name: "task_manage", Contract: taskToolContract, Title: "Manage recoverable tasks", Description: "Persist substantial AgentDock tasks and update live step progress with checkpoint.", Annotations: mutatingToolAnnotations(false, false), Handler: typedToolHandler("task_manage", func(ctx context.Context, r *Runtime, request tooltask.ManageRequest) (Result, error) {
		return r.taskTools.Manage(ctx, request)
	})}}
}

func workflowToolSpecs() []ToolSpec {
	return []ToolSpec{{Name: "workflow_template_manage", Contract: canonicalToolContract, Title: "Manage workflow templates", Description: "List, get, get multiple, publish, retire, or match AgentDock workflow templates. publish validates and activates a complete immutable template version; get_many requires the model to compose the returned templates before task creation.", Availability: requiresNexus, Handler: typedToolHandler("workflow_template_manage", func(ctx context.Context, r *Runtime, request tooltask.WorkflowRequest) (Result, error) {
		return r.taskTools.WorkflowManage(ctx, request)
	})}}
}

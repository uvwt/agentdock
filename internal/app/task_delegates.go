package app

import "context"

func (r *Runtime) taskManage(ctx context.Context, args map[string]any) (Result, error) {
	return r.taskTools.Manage(ctx, args)
}

func (r *Runtime) workflowTemplateManage(ctx context.Context, args map[string]any) (Result, error) {
	return r.taskTools.WorkflowManage(ctx, args)
}

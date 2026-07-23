package app

import "context"

func (r *Runtime) execCommand(ctx context.Context, args map[string]any) (Result, error) {
	return r.command.Exec(ctx, args)
}

func (r *Runtime) sessionObserve(args map[string]any) (Result, error) {
	return r.command.Observe(args)
}

func (r *Runtime) sessionAct(args map[string]any) (Result, error) {
	return r.command.Act(args)
}

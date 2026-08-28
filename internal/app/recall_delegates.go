package app

import "context"

func (r *Runtime) recallContextIndex(ctx context.Context, maxBytes int) (Result, error) {
	return r.recall.ContextIndex(ctx, maxBytes)
}
func (r *Runtime) recallSearch(ctx context.Context, args map[string]any) (Result, error) {
	return r.recall.Search(ctx, args)
}
func (r *Runtime) recallRead(ctx context.Context, args map[string]any) (Result, error) {
	return r.recall.Read(ctx, args)
}
func (r *Runtime) recallWrite(ctx context.Context, args map[string]any) (Result, error) {
	return r.recall.Write(ctx, args)
}
func (r *Runtime) recallMaintain(ctx context.Context, args map[string]any) (Result, error) {
	return r.recall.Maintain(ctx, args)
}
func (r *Runtime) privateNoteManage(ctx context.Context, args map[string]any) (Result, error) {
	return r.recall.PrivateNoteManage(ctx, args)
}

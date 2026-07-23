package app

import "context"

func (r *Runtime) readFile(ctx context.Context, args map[string]any) (Result, error) {
	return r.files.ReadFile(ctx, args)
}
func (r *Runtime) listDir(ctx context.Context, args map[string]any) (Result, error) {
	return r.files.ListDir(ctx, args)
}
func (r *Runtime) listFiles(ctx context.Context, args map[string]any) (Result, error) {
	return r.files.ListFiles(ctx, args)
}
func (r *Runtime) searchText(ctx context.Context, args map[string]any) (Result, error) {
	return r.files.SearchText(ctx, args)
}
func (r *Runtime) fileEdit(ctx context.Context, args map[string]any) (Result, error) {
	return r.files.Edit(ctx, args)
}
